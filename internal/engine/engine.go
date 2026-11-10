package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tonbiattack/archive-extract-cleaner/internal/fswalk"
	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/verify"
	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
	"github.com/tonbiattack/archive-extract-cleaner/internal/ziparchive"
)

type Options struct {
	Roots     []string
	Recursive bool
	Limits    model.Limits
	Progress  func(model.Progress)
}

type Result struct {
	Items        []model.Item
	Summary      model.Summary
	Warnings     []string
	WarningTotal int
	Roots        []string
	StartedAt    time.Time
	FinishedAt   time.Time
}

type walkState struct {
	root         string
	recursive    bool
	limits       model.Limits
	visited      map[string]struct{}
	entries      map[string][]os.DirEntry
	dirIndex     map[string]map[string][]candidate
	archives     map[string]int64
	directDirs   map[string]struct{}
	scanned      int
	completed    int
	compared     int64
	progress     func(model.Progress)
	started      time.Time
	lastEmit     time.Time
	warnings     []string
	warningTotal int
}

type candidate struct {
	path   string
	unsafe bool
	reason string
}

func Scan(ctx context.Context, opts Options) (*Result, error) {
	started := time.Now()
	limits := opts.Limits
	if limits.MaxEntries <= 0 {
		limits = model.DefaultLimits()
	}
	roots, err := validateRoots(opts.Roots)
	if err != nil {
		return nil, err
	}
	state := &walkState{
		recursive:  opts.Recursive,
		limits:     limits,
		visited:    map[string]struct{}{},
		entries:    map[string][]os.DirEntry{},
		dirIndex:   map[string]map[string][]candidate{},
		archives:   map[string]int64{},
		directDirs: map[string]struct{}{},
		progress:   opts.Progress,
		started:    started,
	}
	for _, root := range roots {
		state.root = root
		if err := state.walk(ctx, root, 0); err != nil {
			return nil, err
		}
	}
	state.emit(model.PhaseWalking, "", 0)

	archivePaths := make([]string, 0, len(state.archives))
	for path := range state.archives {
		archivePaths = append(archivePaths, path)
	}
	sort.Slice(archivePaths, func(i, j int) bool {
		return strings.ToLower(archivePaths[i]) < strings.ToLower(archivePaths[j])
	})

	containers := map[string]struct{}{}
	for _, archivePath := range archivePaths {
		for _, root := range roots {
			if !winpath.IsWithin(root, archivePath) {
				continue
			}
			dir := filepath.Dir(archivePath)
			for {
				containers[winpath.Key(dir)] = struct{}{}
				if winpath.EqualPath(dir, root) {
					break
				}
				parent := filepath.Dir(dir)
				if parent == dir {
					break
				}
				dir = parent
			}
			break
		}
	}
	rootKeys := state.rootSet(roots)

	folderOnly := map[string]struct{}{}
	for dir := range state.directDirs {
		folderOnly[dir] = struct{}{}
	}
	seenParents := map[string]struct{}{}
	for _, archivePath := range archivePaths {
		parent := filepath.Dir(archivePath)
		if _, done := seenParents[parent]; done {
			continue
		}
		seenParents[parent] = struct{}{}
		for _, entry := range state.entries[parent] {
			if !entry.IsDir() {
				continue
			}
			full := filepath.Join(parent, entry.Name())
			folderOnly[full] = struct{}{}
		}
	}
	for dir := range folderOnly {
		if _, isRoot := rootKeys[winpath.Key(dir)]; isRoot {
			delete(folderOnly, dir)
			continue
		}
		if _, isContainer := containers[winpath.Key(dir)]; isContainer {
			delete(folderOnly, dir)
		}
	}

	usedFolders := map[string]struct{}{}
	for _, archivePath := range archivePaths {
		for _, c := range state.candidates(archivePath) {
			usedFolders[winpath.Key(c.path)] = struct{}{}
		}
	}
	folderPaths := make([]string, 0, len(folderOnly))
	for path := range folderOnly {
		if _, used := usedFolders[winpath.Key(path)]; used {
			continue
		}
		folderPaths = append(folderPaths, path)
	}
	sort.Slice(folderPaths, func(i, j int) bool {
		return strings.ToLower(folderPaths[i]) < strings.ToLower(folderPaths[j])
	})

	total := len(archivePaths) + len(folderPaths)
	state.emit(model.PhaseVerifying, "", total)

	items := make([]model.Item, 0, total)
	for _, archivePath := range archivePaths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		item := analyzeArchive(ctx, archivePath, state, limits, roots, &usedFolders)
		state.compared += item.ComparedBytes
		state.completed++
		state.emit(model.PhaseVerifying, archivePath, total)
		state.lastEmit = time.Now()
		items = append(items, item)
	}

	for _, folder := range folderPaths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		item := folderItem(ctx, folder, limits)
		state.completed++
		state.emit(model.PhaseVerifying, folder, total)
		state.lastEmit = time.Now()
		items = append(items, item)
	}

	model.SortItems(items)
	finished := time.Now()
	summary := model.Summarize(items)
	summary.ScannedAt = finished
	summary.DurationMS = finished.Sub(started).Milliseconds()
	state.emit(model.PhaseDone, "", total)
	state.lastEmit = time.Now()

	return &Result{
		Items:        items,
		Summary:      summary,
		Warnings:     state.warnings,
		WarningTotal: state.warningTotal,
		Roots:        roots,
		StartedAt:    started,
		FinishedAt:   finished,
	}, nil
}

func (s *walkState) rootSet(roots []string) map[string]struct{} {
	result := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		result[winpath.Key(root)] = struct{}{}
	}
	return result
}

func validateRoots(roots []string) ([]string, error) {
	if len(roots) == 0 {
		return nil, errors.New("対象フォルダが指定されていません")
	}
	seen := map[string]struct{}{}
	var cleaned []string
	for _, root := range roots {
		abs, err := winpath.CleanAbs(root)
		if err != nil {
			return nil, err
		}
		key := winpath.Key(abs)
		if _, ok := seen[key]; ok {
			continue
		}
		info, err := os.Lstat(abs)
		if err != nil {
			return nil, fmt.Errorf("対象フォルダを確認できません: %w", err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("対象がフォルダではありません: %s", abs)
		}
		reparse, err := winpath.IsReparsePoint(abs)
		if err != nil {
			return nil, err
		}
		if reparse {
			return nil, fmt.Errorf("リンク/ジャンクションは対象にできません: %s", abs)
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, abs)
	}
	sort.Slice(cleaned, func(i, j int) bool {
		return len(cleaned[i]) < len(cleaned[j])
	})
	return cleaned, nil
}

func (s *walkState) walk(ctx context.Context, absDir string, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key := winpath.Key(absDir)
	if _, done := s.visited[key]; done {
		return nil
	}
	s.visited[key] = struct{}{}

	entries, err := os.ReadDir(absDir)
	if err != nil {
		if depth == 0 {
			return fmt.Errorf("対象フォルダを読み取れません: %w", err)
		}
		s.warn("フォルダを読み取れません: " + absDir + ": " + err.Error())
		return nil
	}
	s.entries[absDir] = entries
	s.scanned++
	if time.Since(s.lastEmit) >= 100*time.Millisecond {
		s.emit(model.PhaseWalking, absDir, 0)
		s.lastEmit = time.Now()
	}

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		full := filepath.Join(absDir, entry.Name())
		reparse, err := winpath.IsReparsePoint(full)
		if err != nil {
			s.warn("項目を確認できません: " + full + ": " + err.Error())
			continue
		}
		if reparse {
			s.warn("リンク/ジャンクションは走査しません: " + full)
			continue
		}
		if entry.IsDir() {
			if depth == 0 {
				s.directDirs[full] = struct{}{}
			}
			if s.recursive {
				if err := s.walk(ctx, full, depth+1); err != nil {
					return err
				}
			}
			continue
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			s.warn("ZIPを確認できません: " + full + ": " + err.Error())
			continue
		}
		if !info.Mode().IsRegular() {
			s.warn("通常ファイルではないZIPを無視します: " + full)
			continue
		}
		s.archives[full] = info.Size()
	}
	return nil
}

func (s *walkState) warn(message string) {
	s.warningTotal++
	if len(s.warnings) < 200 {
		s.warnings = append(s.warnings, message)
	}
}

func (s *walkState) emit(phase model.Phase, current string, total int) {
	if s.progress == nil {
		return
	}
	archives := len(s.archives)
	folders := len(s.directDirs)
	s.progress(model.Progress{
		Phase:      phase,
		Current:    current,
		WalkedDirs: s.scanned,
		Archives:   archives,
		Folders:    folders,
		Completed:  s.completed,
		Total:      total,
		Bytes:      s.compared,
		ElapsedMS:  time.Since(s.started).Milliseconds(),
	})
}

func (s *walkState) candidates(archivePath string) []candidate {
	parent := filepath.Dir(archivePath)
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(archivePath), filepath.Ext(archivePath)))
	if base == "" {
		return nil
	}
	index, cached := s.dirIndex[parent]
	if !cached {
		entries, ok := s.entries[parent]
		if !ok {
			var err error
			entries, err = os.ReadDir(parent)
			if err != nil {
				return nil
			}
			s.entries[parent] = entries
		}
		index = map[string][]candidate{}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			full := filepath.Join(parent, entry.Name())
			unsafe, err := winpath.IsReparsePoint(full)
			if err != nil {
				index[strings.ToLower(entry.Name())] = append(index[strings.ToLower(entry.Name())], candidate{path: full, unsafe: true, reason: err.Error()})
				continue
			}
			index[strings.ToLower(entry.Name())] = append(index[strings.ToLower(entry.Name())], candidate{path: full, unsafe: unsafe, reason: "リンク/ジャンクション"})
		}
		s.dirIndex[parent] = index
	}
	return index[base]
}

func analyzeArchive(ctx context.Context, archivePath string, state *walkState, limits model.Limits, roots []string, usedFolders *map[string]struct{}) model.Item {
	base := strings.TrimSuffix(filepath.Base(archivePath), filepath.Ext(archivePath))
	item := model.Item{
		ID:         model.MakeID(archivePath, ""),
		Status:     model.StatusReady,
		ZipPath:    archivePath,
		ParentPath: filepath.Dir(archivePath),
		Name:       base,
		ZipSize:    state.archives[archivePath],
		Size:       state.archives[archivePath],
		CheckedAt:  time.Now(),
	}
	candidates := state.candidates(archivePath)
	if len(candidates) == 1 {
		item.FolderPath = candidates[0].path
		item.ID = model.MakeID(archivePath, candidates[0].path)
	}
	for _, c := range candidates {
		(*usedFolders)[winpath.Key(c.path)] = struct{}{}
	}

	archive, err := ziparchive.Open(archivePath, limits)
	if err != nil {
		item.Verdict = model.VerdictUnverified
		item.Reason = "ZIPを検証できません: " + err.Error()
		item.Details = []string{err.Error()}
		return item
	}
	item.ZipEntries = archive.EntryCount
	_ = archive.Close()

	if len(candidates) == 0 {
		item.Verdict = model.VerdictKeepArchive
		item.Reason = "対応する解凍済みフォルダがありません"
		return item
	}
	if len(candidates) > 1 {
		item.Verdict = model.VerdictAmbiguous
		item.FolderPath = ""
		item.Size = 0
		item.Reason = "同名の解凍済みフォルダが複数あるため自動判定できません"
		for _, c := range candidates {
			item.Details = append(item.Details, c.path)
		}
		return item
	}
	selected := candidates[0]
	if selected.unsafe {
		item.Verdict = model.VerdictUnverified
		item.Reason = "候補フォルダを安全に扱えません: " + selected.reason
		return item
	}

	result := verify.Verify(ctx, archivePath, selected.path, verify.Options{Limits: limits, Roots: roots})
	item.Verdict = result.Verdict
	item.Reason = result.Reason
	item.Details = result.Details
	item.MatchMode = result.MatchMode
	item.FolderSize = result.FolderSize
	item.Size = result.FolderSize
	item.FileCount = result.FileCount
	item.DirCount = result.DirCount
	item.ComparedBytes = result.ComparedBytes
	if result.ZipEntries > 0 {
		item.ZipEntries = result.ZipEntries
	}
	return item
}

func folderItem(ctx context.Context, folder string, limits model.Limits) model.Item {
	item := model.Item{
		ID:         model.MakeID("", folder),
		Verdict:    model.VerdictKeepDirectory,
		Status:     model.StatusReady,
		FolderPath: folder,
		ParentPath: filepath.Dir(folder),
		Name:       filepath.Base(folder),
		Reason:     "対応するZIPがありません",
		CheckedAt:  time.Now(),
	}
	reparse, err := winpath.IsReparsePoint(folder)
	if err != nil {
		item.Verdict = model.VerdictUnverified
		item.Reason = "フォルダを確認できません: " + err.Error()
		return item
	}
	if reparse {
		item.Verdict = model.VerdictUnverified
		item.Reason = "リンク/ジャンクションのため走査しません"
		return item
	}
	size := fswalk.BestEffortSize(ctx, folder)
	item.FolderSize = size
	item.Size = size
	return item
}
