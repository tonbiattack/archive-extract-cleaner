package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tonbiattack/archive-extract-cleaner/internal/archive"
	"github.com/tonbiattack/archive-extract-cleaner/internal/cache"
	"github.com/tonbiattack/archive-extract-cleaner/internal/fswalk"
	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/verify"
	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
)

// maxVerifyWorkers は照合の並列度の上限。ディスクとCPUを占有しすぎない範囲に抑える。
const maxVerifyWorkers = 16

type Options struct {
	Roots     []string
	Recursive bool
	Limits    model.Limits
	// Workers は照合の並列度。0 以下なら既定値（CPU数、上限8）を使う。
	Workers  int
	Progress func(model.Progress)
	// Hashes は ZIP エントリのハッシュキャッシュ。省略可能。
	Hashes verify.HashCache
	// Previous は前回のスキャン結果。差分スキャンに使う。省略可能。
	Previous *cache.RootRecord
	// Full が true ならキャッシュを使わず全件を再検証する。
	Full bool
}

type Result struct {
	Items        []model.Item
	Summary      model.Summary
	Warnings     []string
	WarningTotal int
	Roots        []string
	StartedAt    time.Time
	FinishedAt   time.Time
	// キャッシュ利用状況。ログ・UI・テストでの比較に使う。
	Cache CacheStats
	// Records は次回の差分スキャン用に保存する項目記録。
	Records []cache.ItemRecord
	// FolderTrees は保存対象のフォルダ指紋（項目IDごと）を引くための情報。
	FolderPrints map[string]*cache.FolderFingerprint
}

// CacheStats はキャッシュの利用状況。
type CacheStats struct {
	// ReusedItems は前回結果を再利用した項目数。
	ReusedItems int
	// VerifiedItems は今回検証した項目数。
	VerifiedItems int
	// CachedHashes はキャッシュから復元した ZIP エントリ数。
	CachedHashes int
	// ComputedHashes は実際に展開して計算した ZIP エントリ数。
	ComputedHashes int
}

type walkState struct {
	root         string
	recursive    bool
	limits       model.Limits
	visited      map[string]struct{}
	entries      map[string][]os.DirEntry
	dirIndex     map[string]*dirCandidates
	archives     map[string]int64
	directDirs   map[string]struct{}
	scanned      int
	completed    int
	compared     int64
	progress     func(model.Progress)
	started      time.Time
	verifyStart  time.Time
	lastEmit     time.Time
	warnings     []string
	warningTotal int
}

type candidate struct {
	path   string
	unsafe bool
	reason string
	// nameVariant は ZIP名とフォルダ名が完全一致せず、重複名の接尾辞を
	// 除いた正規化名で一致した場合に true。
	nameVariant bool
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
		dirIndex:   map[string]*dirCandidates{},
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

	// candidates() は内部キャッシュを書き換えるため、並列化の前に逐次で確定させる。
	archiveCandidates := make(map[string][]candidate, len(archivePaths))
	for _, archivePath := range archivePaths {
		archiveCandidates[archivePath] = state.candidates(archivePath)
	}

	usedFolders := map[string]struct{}{}
	for _, archivePath := range archivePaths {
		for _, c := range archiveCandidates[archivePath] {
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
	state.verifyStart = time.Now()
	state.emit(model.PhaseVerifying, "", total)

	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
		if workers > 8 {
			workers = 8
		}
	}
	if workers > maxVerifyWorkers {
		workers = maxVerifyWorkers
	}
	if workers > total {
		workers = total
	}
	if workers < 1 {
		workers = 1
	}

	tasks := make([]verifyTask, 0, total)
	for _, archivePath := range archivePaths {
		tasks = append(tasks, verifyTask{archivePath: archivePath, candidates: archiveCandidates[archivePath]})
	}
	for _, folder := range folderPaths {
		tasks = append(tasks, verifyTask{folderPath: folder})
	}

	// 差分スキャン。前回結果があり、対象のフォルダ指紋と ZIP の同一性が
	// 変わっていない項目だけを再利用する。判定に迷う要素があれば再検証する。
	stats := &cacheStats{}
	walker := newFolderWalker()
	// 判定（Verdict）の再利用は行わない。
	//
	// フォルダ指紋は相対パス・サイズ・更新時刻から作るため、同じサイズ・
	// 同じ更新時刻のまま中身だけ差し替えたフォルダを検出できない。実際に
	// 「alpha」を「bravo」へ差し替えて更新時刻を戻すと、前回の
	// DELETE_CANDIDATE がそのまま再利用され、内容が違うフォルダが削除候補に
	// 残ることをテストで確認した（TestCachedScanDetectsSameSizeRestoredTimeFolderChange）。
	//
	// ZIP エントリのハッシュキャッシュも使わない。キーはセントラルディレクトリの
	// 構造指紋（CRC32 を含む）だが、CRC32 は圧縮データの破損を検出できない。
	// 破損した ZIP はセントラルディレクトリが無傷のまま残るため指紋が一致し、
	// 破損 ZIP を「一致」と誤表示することをテストで確認した
	// （TestCachedScanDetectsCorruptArchiveWithUnchangedMetadata）。
	//
	// どちらも「削除候補を誤って提示しない」という不変条件を破るため、
	// 内容の同一性を実体から確認できる方法ができるまで再利用しない。
	prev := (*previousIndex)(nil)

	items, compared, err := runVerifyTasks(ctx, tasks, workers, limits, roots, state, total, scanOptions{
		hashes: nil,
		stats:  stats,
		walker: walker,
		prev:   prev,
		full:   opts.Full,
	})
	if err != nil {
		return nil, err
	}
	state.compared = compared
	state.completed = total

	records := buildRecords(ctx, items, limits, walker, opts.Previous, stats)

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
		Cache:        stats.snapshot(),
		Records:      records,
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
		if !archive.IsSupported(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			s.warn("アーカイブを確認できません: " + full + ": " + err.Error())
			continue
		}
		if !info.Mode().IsRegular() {
			s.warn("通常ファイルではないアーカイブを無視します: " + full)
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

// emitVerifying は並列ワーカーの進捗を通知する。completed/bytes は呼び出し側で確定させる。
func (s *walkState) emitVerifying(current string, total, completed int, bytes int64) {
	if s.progress == nil {
		return
	}
	eta := int64(-1)
	if completed > 0 && total > completed && !s.verifyStart.IsZero() {
		if elapsed := time.Since(s.verifyStart).Milliseconds(); elapsed > 0 {
			eta = elapsed * int64(total-completed) / int64(completed)
		}
	}
	s.progress(model.Progress{
		Phase:      model.PhaseVerifying,
		Current:    current,
		WalkedDirs: s.scanned,
		Archives:   len(s.archives),
		Folders:    len(s.directDirs),
		Completed:  completed,
		Total:      total,
		Bytes:      bytes,
		ElapsedMS:  time.Since(s.started).Milliseconds(),
		ETAMS:      eta,
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
		index = &dirCandidates{exact: map[string][]candidate{}, normalized: map[string][]candidate{}}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			full := filepath.Join(parent, entry.Name())
			name := strings.ToLower(entry.Name())
			unsafe, err := winpath.IsReparsePoint(full)
			if err != nil {
				index.exact[name] = append(index.exact[name], candidate{path: full, unsafe: true, reason: err.Error()})
				continue
			}
			c := candidate{path: full, unsafe: unsafe, reason: "リンク/ジャンクション"}
			index.exact[name] = append(index.exact[name], c)
			normalized := normalizeDuplicateName(name)
			index.normalized[normalized] = append(index.normalized[normalized], c)
		}
		s.dirIndex[parent] = index
	}
	if exact := index.exact[base]; len(exact) > 0 {
		return exact
	}

	// 完全一致がない場合だけ、重複名の接尾辞を除いた正規化名で探す。
	// 例: "photos.zip" に対する "photos (1)"、"photos - コピー"、"photos_2"。
	variants := index.normalized[normalizeDuplicateName(base)]
	if len(variants) == 0 {
		return nil
	}
	result := make([]candidate, 0, len(variants))
	for _, c := range variants {
		// 正規化前の名前が同じものは exact で拾い済みなので除外する。
		if strings.EqualFold(filepath.Base(c.path), base) {
			continue
		}
		c.nameVariant = true
		result = append(result, c)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].path) < strings.ToLower(result[j].path)
	})
	return result
}

// dirCandidates は親ディレクトリ内の候補フォルダの索引。
// exact は小文字名そのまま、normalized は重複名の接尾辞を除いた名前で引く。
type dirCandidates struct {
	exact      map[string][]candidate
	normalized map[string][]candidate
}

// duplicateSuffixPatterns は Windows やブラウザが重複名に付ける接尾辞。
// 小文字化済みの名前に対して適用する。
var duplicateSuffixPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\s*\(\d+\)$`),               // " (1)", "(2)"
	regexp.MustCompile(`\s*-\s*コピー(\s*\(\d+\))?$`),  // " - コピー"
	regexp.MustCompile(`\s*-\s*copy(\s*\(\d+\))?$`), // " - Copy"
	regexp.MustCompile(`_\d+$`),                     // "_2"
	regexp.MustCompile(`\s+のコピー(\s*\(\d+\))?$`),     // " のコピー"
}

// normalizeDuplicateName は重複名の接尾辞を繰り返し取り除く。
// 取り除いた結果が空になる場合は元の名前を返す。
// 戻り値は小文字化されている。
func normalizeDuplicateName(name string) string {
	current := strings.ToLower(strings.TrimSpace(name))
	for i := 0; i < 3; i++ {
		trimmed := current
		for _, pattern := range duplicateSuffixPatterns {
			trimmed = pattern.ReplaceAllString(trimmed, "")
		}
		trimmed = strings.TrimSpace(trimmed)
		if trimmed == "" || trimmed == current {
			return current
		}
		current = trimmed
	}
	return current
}

func analyzeArchive(ctx context.Context, task verifyTask, zipSize int64, limits model.Limits, roots []string, scanOpts scanOptions) model.Item {
	archivePath := task.archivePath
	candidates := task.candidates
	base := strings.TrimSuffix(filepath.Base(archivePath), filepath.Ext(archivePath))
	item := model.Item{
		ID:         model.MakeID(archivePath, ""),
		Status:     model.StatusReady,
		ZipPath:    archivePath,
		ParentPath: filepath.Dir(archivePath),
		Name:       base,
		ZipSize:    zipSize,
		Size:       zipSize,
		CheckedAt:  time.Now(),
	}
	if len(candidates) == 1 {
		item.FolderPath = candidates[0].path
		item.ID = model.MakeID(archivePath, candidates[0].path)
	}

	opened, err := archive.Open(archivePath, limits)
	if err != nil {
		item.Verdict = model.VerdictUnverified
		item.Reason = describeArchiveError(err)
		item.Details = []string{err.Error()}
		return item
	}
	item.ZipEntries = opened.EntryCount()
	_ = opened.Close()

	if len(candidates) == 0 {
		item.Verdict = model.VerdictKeepArchive
		item.Reason = "対応する解凍済みフォルダがありません"
		return item
	}
	if len(candidates) > 1 {
		item.Verdict = model.VerdictAmbiguous
		item.FolderPath = ""
		item.Size = 0
		item.Reason = "対応する解凍済みフォルダの候補が複数あるため自動判定できません"
		for _, c := range candidates {
			item.Details = append(item.Details, c.path)
			item.RelatedFolders = append(item.RelatedFolders, c.path)
		}
		return item
	}
	selected := candidates[0]
	if selected.unsafe {
		item.Verdict = model.VerdictUnverified
		item.Reason = "候補フォルダを安全に扱えません: " + selected.reason
		return item
	}
	// 名前が完全一致していない組は、削除候補になった場合にその旨を明示する。
	nameVariant := selected.nameVariant

	result := verify.Verify(ctx, archivePath, selected.path, verify.Options{
		Limits: limits,
		Roots:  roots,
		Hashes: scanOpts.hashes,
		Tree:   task.tree,
	})
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
	if scanOpts.stats != nil {
		scanOpts.stats.addHashes(result.CachedHashes, result.ComputedHashes)
	}
	if nameVariant && result.Verdict.IsDeleteCandidate() {
		item.NameVariant = true
		item.Details = append([]string{
			fmt.Sprintf("ZIP名とフォルダ名が異なります（%s / %s）", filepath.Base(archivePath), filepath.Base(selected.path)),
		}, item.Details...)
		item.Details = capDetails(item.Details)
	}
	return item
}

// capDetails は詳細の件数上限を適用する。
func capDetails(details []string) []string {
	const limit = 51
	if len(details) <= limit {
		return details
	}
	return details[:limit]
}

func folderItem(ctx context.Context, folder string, limits model.Limits, tree *fswalk.Tree) model.Item {
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
	if tree != nil {
		item.FolderSize = tree.Size
		item.Size = tree.Size
		item.FileCount = tree.FileCount
		item.DirCount = tree.DirCount
		return item
	}
	size := fswalk.BestEffortSize(ctx, folder)
	item.FolderSize = size
	item.Size = size
	return item
}

// verifyTask は1件の照合単位。archivePath が空ならフォルダ単独の項目。
type verifyTask struct {
	archivePath string
	folderPath  string
	candidates  []candidate
	// reused は前回結果を再利用する場合の項目。空なら再検証する。
	reused *cache.ItemRecord
	// tree は差分スキャンで走査済みのツリー。再利用判定で計算したものを渡す。
	tree *fswalk.Tree
}

// withReuse はタスクへ再利用情報を付ける。
func (t verifyTask) withReuse(record cache.ItemRecord, tree *fswalk.Tree) verifyTask {
	t.reused = &record
	t.tree = tree
	return t
}

// scanOptions は照合時に共有する設定。
type scanOptions struct {
	hashes verify.HashCache
	stats  *cacheStats
	walker *folderWalker
	prev   *previousIndex
	// full が true なら前回結果を一切使わない。
	full bool
}

// cacheStats はキャッシュ利用状況をスレッドセーフに集計する。
type cacheStats struct {
	mu       sync.Mutex
	reused   int
	verified int
	hashes   int
	computed int
}

func (s *cacheStats) addReused() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.reused++
	s.mu.Unlock()
}

func (s *cacheStats) addVerified() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.verified++
	s.mu.Unlock()
}

func (s *cacheStats) addHashes(cached, computed int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.hashes += cached
	s.computed += computed
	s.mu.Unlock()
}

func (s *cacheStats) snapshot() CacheStats {
	if s == nil {
		return CacheStats{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return CacheStats{
		ReusedItems:    s.reused,
		VerifiedItems:  s.verified,
		CachedHashes:   s.hashes,
		ComputedHashes: s.computed,
	}
}

// runVerifyTasks は照合をワーカー数だけ並列実行する。
// 結果の順序は tasks の順序に固定し、進捗は共有カウンタから間引いて通知する。
func runVerifyTasks(ctx context.Context, tasks []verifyTask, workers int, limits model.Limits, roots []string, state *walkState, total int, opts scanOptions) ([]model.Item, int64, error) {
	items := make([]model.Item, len(tasks))
	if len(tasks) == 0 {
		return items, 0, nil
	}

	var mu sync.Mutex
	var completed int
	var compared int64
	lastEmit := time.Time{}

	emit := func(current string) {
		if state.progress == nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if time.Since(lastEmit) < 100*time.Millisecond {
			return
		}
		lastEmit = time.Now()
		state.emitVerifying(current, total, completed, compared)
	}

	indexes := make(chan int)
	var wg sync.WaitGroup
	worker := func() {
		defer wg.Done()
		for index := range indexes {
			if ctx.Err() != nil {
				continue
			}
			task := tasks[index]
			current := task.folderPath
			var item model.Item
			if !opts.full && opts.prev != nil {
				if record, tree, ok := opts.prev.canReuse(task, limits, opts.walker, ctx); ok {
					task = task.withReuse(record, tree)
				}
			}
			if task.reused != nil {
				item = itemFromRecord(*task.reused)
				opts.stats.addReused()
			} else if task.archivePath != "" {
				current = task.archivePath
				item = analyzeArchive(ctx, task, state.archives[task.archivePath], limits, roots, opts)
				opts.stats.addVerified()
			} else {
				item = folderItem(ctx, task.folderPath, limits, task.tree)
				opts.stats.addVerified()
			}
			items[index] = item
			mu.Lock()
			completed++
			compared += item.ComparedBytes
			mu.Unlock()
			emit(current)
		}
	}
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go worker()
	}

	for index := range tasks {
		if ctx.Err() != nil {
			break
		}
		indexes <- index
	}
	close(indexes)
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	mu.Lock()
	done, bytes := completed, compared
	mu.Unlock()
	state.emitVerifying("", total, done, bytes)
	return items, bytes, nil
}
