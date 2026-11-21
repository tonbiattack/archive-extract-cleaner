package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/tonbiattack/archive-extract-cleaner/internal/advisor"
	"github.com/tonbiattack/archive-extract-cleaner/internal/archive"
	"github.com/tonbiattack/archive-extract-cleaner/internal/cache"
	"github.com/tonbiattack/archive-extract-cleaner/internal/config"
	"github.com/tonbiattack/archive-extract-cleaner/internal/dashboard"
	"github.com/tonbiattack/archive-extract-cleaner/internal/duplicates"
	"github.com/tonbiattack/archive-extract-cleaner/internal/engine"
	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/oplog"
	"github.com/tonbiattack/archive-extract-cleaner/internal/removal"
	"github.com/tonbiattack/archive-extract-cleaner/internal/shell"
	"github.com/tonbiattack/archive-extract-cleaner/internal/transfer"
	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
)

const appVersion = "0.1.0"

type App struct {
	ctx context.Context

	mu                   sync.RWMutex
	items                []model.Item
	index                map[string]int
	roots                []string
	summary              model.Summary
	warnings             []string
	warningTotal         int
	scanning             bool
	deleting             bool
	cancelScan           context.CancelFunc
	log                  *oplog.Logger
	logPath              string
	limits               model.Limits
	workers              int
	configPath           string
	configWarning        string
	mover                removal.Mover
	multiDriveCopyScript string
	multiDriveCopyPreset string
	previewMu            sync.Mutex
	previewCache         map[string]cachedPreview
	advisor              advisor.Advisor
	aiEnabled            bool
	aiModel              string
	aiKeyEnv             string
	suggestions          *aiSuggestionStore
	archiveHashes        *cache.ArchiveHashes
	scanStore            *cache.ScanStore
	lastCacheStats       engine.CacheStats
	moves                dashboard.Moves
	triagePath           string
	discardPreview       *discardSnapshot
	triageDisplay        map[string]triageDisplayEntry
	// duplicateSearch は E/F/G の同名アーカイブ検索の設定。
	duplicateSearch config.ArchiveSearch
	// duplicateRoots はドライブレターごとの保存先を明示する。テスト用。
	duplicateRoots map[string]string
	// duplicateResults は項目IDごとの検索結果。未検索の項目は含まない。
	duplicateResults map[string]duplicates.Result
	// duplicateDrives は直近の検索でのドライブ状況。
	duplicateDrives []duplicates.DriveState
	// duplicateSearching は検索の実行中フラグ。
	duplicateSearching bool
	cancelDuplicate    context.CancelFunc
}

type ScanInfo struct {
	Summary      model.Summary     `json:"summary"`
	Warnings     []string          `json:"warnings"`
	WarningTotal int               `json:"warningTotal"`
	Roots        []string          `json:"roots"`
	Cancelled    bool              `json:"cancelled"`
	Cache        engine.CacheStats `json:"cache"`
}

type AppState struct {
	Summary                 model.Summary  `json:"summary"`
	Warnings                []string       `json:"warnings"`
	WarningTotal            int            `json:"warningTotal"`
	Roots                   []string       `json:"roots"`
	Scanning                bool           `json:"scanning"`
	Deleting                bool           `json:"deleting"`
	LogPath                 string         `json:"logPath"`
	ConfigPath              string         `json:"configPath"`
	ConfigWarning           string         `json:"configWarning"`
	Limits                  model.Limits   `json:"limits"`
	Workers                 int            `json:"workers"`
	MultiDriveCopyScript    string         `json:"multiDriveCopyScript"`
	MultiDriveCopyPreset    string         `json:"multiDriveCopyPreset"`
	MultiDriveCopyAvailable bool           `json:"multiDriveCopyAvailable"`
	AI                      AIState        `json:"ai"`
	Cache                   CacheState     `json:"cache"`
	Archive                 ArchiveState   `json:"archive"`
	Duplicate               DuplicateState `json:"duplicate"`
	Version                 string         `json:"version"`
}

// ArchiveState はアーカイブ読み取りの対応状況。
type ArchiveState struct {
	// Formats は対応する拡張子。
	Formats []string `json:"formats"`
	// ToolPath は検出した 7z.exe。空なら未検出。
	ToolPath string `json:"toolPath"`
	// ToolAvailable は 7z / RAR を読めるか。
	ToolAvailable bool `json:"toolAvailable"`
	// ToolRequired は 7z / RAR の読み取りに外部ツールが必要か。
	ToolRequired bool `json:"toolRequired"`
}

// CacheState はキャッシュの状態。UI とテストで利用する。
type CacheState struct {
	// Enabled はキャッシュが有効か。
	Enabled bool `json:"enabled"`
	// HashPath / ScanPath はキャッシュファイルの場所。
	HashPath string `json:"hashPath"`
	ScanPath string `json:"scanPath"`
	// Archives / Roots は保持している件数。
	Archives int `json:"archives"`
	Roots    int `json:"roots"`
	// LastScan は直近のスキャンでのキャッシュ利用状況。
	LastScan engine.CacheStats `json:"lastScan"`
}

type DeleteProgress struct {
	Index int    `json:"index"`
	Total int    `json:"total"`
	Name  string `json:"name"`
	Stage string `json:"stage"`
}

func NewApp() *App {
	logger := oplog.New("")
	cfg, configPath, configErr := config.Load("")
	// 7-Zip の場所は起動時に一度だけ設定する（差分スキャンやプレビューでも使う）。
	archive.ConfigureTool(cfg.ArchiveTool)
	scriptPath := cfg.MultiDriveCopy.ScriptPath
	if scriptPath == "" {
		scriptPath = transfer.DefaultScriptPath()
	} else if abs, err := filepath.Abs(scriptPath); err == nil {
		scriptPath = abs
	}
	app := &App{
		index:                map[string]int{},
		log:                  logger,
		logPath:              logger.Path(),
		limits:               cfg.Limits,
		workers:              cfg.Workers,
		configPath:           configPath,
		mover:                removal.MoveToRecycleBin,
		multiDriveCopyScript: scriptPath,
		multiDriveCopyPreset: cfg.MultiDriveCopy.Preset,
		previewCache:         map[string]cachedPreview{},
		advisor:              aiConfigured(cfg.AI.Enabled, cfg.AI.Endpoint, cfg.AI.Model, cfg.AI.APIKeyEnv, cfg.AI.AllowInsecure),
		aiEnabled:            cfg.AI.Enabled,
		aiModel:              cfg.AI.Model,
		aiKeyEnv:             cfg.AI.APIKeyEnv,
		suggestions:          &aiSuggestionStore{},
		archiveHashes:        cache.NewArchiveHashes(cache.DefaultHashesPath()),
		scanStore:            cache.NewScanStore(cache.DefaultScanPath()),
		duplicateSearch:      cfg.ArchiveSearch,
		duplicateResults:     map[string]duplicates.Result{},
	}
	if configErr != nil {
		app.configWarning = "設定ファイルを読み込めないため既定値を使用します: " + configErr.Error()
	}
	return app
}

func (a *App) startup(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	if a.logPath == "" && a.log != nil {
		a.logPath = a.log.Path()
	}
	a.mu.Unlock()
}

func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	cancel := a.cancelScan
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *App) GetState() AppState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return AppState{
		Summary:                 a.summary,
		Warnings:                append([]string(nil), a.warnings...),
		WarningTotal:            a.warningTotal,
		Roots:                   append([]string(nil), a.roots...),
		Scanning:                a.scanning,
		Deleting:                a.deleting,
		LogPath:                 a.logPath,
		ConfigPath:              a.configPath,
		ConfigWarning:           a.configWarning,
		Limits:                  a.limits,
		Workers:                 a.workers,
		MultiDriveCopyScript:    a.multiDriveCopyScript,
		MultiDriveCopyPreset:    a.multiDriveCopyPreset,
		MultiDriveCopyAvailable: transfer.ScriptAvailable(a.multiDriveCopyScript),
		AI:                      a.aiStateLocked(),
		Cache:                   a.cacheStateLocked(),
		Archive:                 archiveState(),
		Duplicate:               a.duplicateStateLocked(),
		Version:                 appVersion,
	}
}

// archiveState はアーカイブ対応状況を返す。
func archiveState() ArchiveState {
	return ArchiveState{
		Formats:       []string{".zip", ".7z", ".rar"},
		ToolPath:      archive.ToolPath(),
		ToolAvailable: archive.ToolAvailable(),
		ToolRequired:  true,
	}
}

// cacheStateLocked は a.mu を保持したままキャッシュ状態を組み立てる。
func (a *App) cacheStateLocked() CacheState {
	state := CacheState{LastScan: a.lastCacheStats}
	if a.archiveHashes != nil {
		state.Enabled = true
		state.HashPath = a.archiveHashes.Path()
		state.Archives = a.archiveHashes.Len()
	}
	if a.scanStore != nil {
		state.Enabled = true
		state.ScanPath = a.scanStore.Path()
		state.Roots = a.scanStore.Len()
	}
	return state
}

// aiStateLocked は a.mu を保持したまま AI 状態を組み立てる。
func (a *App) aiStateLocked() AIState {
	available := a.advisor != nil && a.advisor.Enabled()
	return AIState{
		Enabled:      a.aiEnabled,
		Model:        a.aiModel,
		Available:    available,
		KeyEnv:       a.aiKeyEnv,
		KeyPresent:   available,
		PendingCount: len(a.suggestions.all()),
	}
}

func (a *App) safeTransferReviewLocked(busy bool) transfer.Review {
	decisions, err := readTriage(a.triageFile())
	var saved, protected []model.Item
	for _, item := range a.items {
		if err == nil && triageState(item, a.roots, decisions) == "save" {
			saved = append(saved, item)
		} else {
			protected = append(protected, item)
		}
	}
	review := transfer.BuildReview(saved, a.multiDriveCopyScript, a.multiDriveCopyPreset, busy)
	candidates := review.Candidates[:0]
	for _, candidate := range review.Candidates {
		unsafe := false
		for _, item := range protected {
			for _, p := range append([]string{item.ZipPath, item.FolderPath}, item.RelatedFolders...) {
				if p != "" && (winpath.EqualPath(candidate.Path, p) || winpath.IsWithin(candidate.Path, p)) {
					unsafe = true
				}
			}
		}
		if !unsafe {
			candidates = append(candidates, candidate)
		}
	}
	review.Candidates = candidates
	return review
}

func (a *App) GetTransferReview() transfer.Review {
	a.mu.RLock()
	defer a.mu.RUnlock()
	review := a.safeTransferReviewLocked(a.scanning || a.deleting)
	for i := range review.Candidates {
		review.Candidates[i].Triage = "save"
	}
	return review
}

func (a *App) LaunchMultiDriveCopy(paths []string) (transfer.LaunchResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.scanning || a.deleting {
		return transfer.LaunchResult{}, errors.New("スキャンまたはごみ箱移動の処理中は転送を開始できません")
	}
	unlock, err := lockTriage(a.triageFile())
	if err != nil {
		return transfer.LaunchResult{}, err
	}
	defer unlock()
	review := a.safeTransferReviewLocked(false)
	allowed := make(map[string]transfer.Candidate, len(review.Candidates))
	for _, candidate := range review.Candidates {
		allowed[winpath.Key(candidate.Path)] = candidate
	}

	selected := make([]string, 0, len(paths))
	missing := make([]string, 0)
	seen := map[string]struct{}{}
	for _, path := range paths {
		abs, err := winpath.CleanAbs(path)
		if err != nil {
			return transfer.LaunchResult{}, fmt.Errorf("転送対象のパスが不正です: %w", err)
		}
		key := winpath.Key(abs)
		if _, ok := allowed[key]; !ok {
			return transfer.LaunchResult{}, fmt.Errorf("スキャン結果に含まれないパスは転送できません: %s", abs)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		if _, err := os.Lstat(abs); err != nil {
			missing = append(missing, abs)
			continue
		}
		seen[key] = struct{}{}
		selected = append(selected, abs)
	}
	if len(missing) > 0 {
		preview := missing
		if len(preview) > 5 {
			preview = preview[:5]
		}
		message := "次のパスが見つかりません: " + strings.Join(preview, ", ")
		if len(missing) > len(preview) {
			message += fmt.Sprintf(" ほか %d 件", len(missing)-len(preview))
		}
		return transfer.LaunchResult{}, errors.New(message)
	}
	if len(selected) == 0 {
		return transfer.LaunchResult{}, errors.New("転送対象が選択されていません")
	}
	return transfer.Launch(selected, a.multiDriveCopyScript, a.multiDriveCopyPreset)
}

func (a *App) Scan(roots []string) (ScanInfo, error) {
	return a.scan(roots, false)
}

// Rescan はキャッシュを使わずに完全再スキャンする。
func (a *App) Rescan(roots []string) (ScanInfo, error) {
	return a.scan(roots, true)
}

func (a *App) scan(roots []string, full bool) (ScanInfo, error) {
	a.mu.Lock()
	if a.scanning || a.deleting {
		a.mu.Unlock()
		return ScanInfo{}, errors.New("別の処理を実行中です")
	}
	base := a.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	a.cancelScan = cancel
	a.scanning = true
	a.discardPreview = nil
	limits := a.limits
	workers := a.workers
	hashes := a.archiveHashes
	scans := a.scanStore
	a.mu.Unlock()

	defer func() {
		cancel()
		a.mu.Lock()
		a.scanning = false
		a.cancelScan = nil
		a.mu.Unlock()
	}()

	lastEmit := time.Time{}
	var progressMu sync.Mutex
	progress := func(p model.Progress) {
		progressMu.Lock()
		if p.Phase != model.PhaseDone && time.Since(lastEmit) < 80*time.Millisecond {
			progressMu.Unlock()
			return
		}
		lastEmit = time.Now()
		progressMu.Unlock()
		a.emit("scan:progress", p)
	}

	// 前回結果はルートごとに保持している。差分スキャンはルート単位で有効にする。
	previous := (*cache.RootRecord)(nil)
	if !full && scans != nil {
		roots = normalizeRoots(roots)
		if len(roots) == 1 {
			if record, ok := scans.Lookup(roots[0], true, cache.ArchiveEpoch()); ok {
				previous = &record
			}
		}
	}

	result, err := engine.Scan(ctx, engine.Options{
		Roots:     roots,
		Recursive: true,
		Limits:    limits,
		Workers:   workers,
		Progress:  progress,
		Hashes:    hashes,
		Previous:  previous,
		Full:      full,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			info := ScanInfo{Cancelled: true}
			a.emit("scan:cancelled", info)
			return info, nil
		}
		a.emit("scan:error", err.Error())
		return ScanInfo{}, err
	}

	// 次回の差分スキャンのため、結果をルート単位で保存する。
	if scans != nil && len(result.Roots) == 1 {
		scans.Put(cache.NewRootRecord(result.Roots[0], true, result.Records, result.Summary.ScannedAt))
		if err := scans.Flush(); err != nil {
			a.emit("scan:cache_error", err.Error())
		}
	}
	if hashes != nil {
		if err := hashes.Flush(); err != nil {
			a.emit("scan:cache_error", err.Error())
		}
	}

	index := make(map[string]int, len(result.Items))
	for i, item := range result.Items {
		index[item.ID] = i
	}
	a.mu.Lock()
	a.triageDisplay = nil
	a.items = result.Items
	a.index = index
	a.roots = result.Roots
	a.summary = result.Summary
	a.warnings = result.Warnings
	a.warningTotal = result.WarningTotal
	a.lastCacheStats = result.Cache
	a.mu.Unlock()
	a.clearPreviewCache()

	info := ScanInfo{
		Summary:      result.Summary,
		Warnings:     result.Warnings,
		WarningTotal: result.WarningTotal,
		Roots:        result.Roots,
		Cache:        result.Cache,
	}
	a.emit("scan:done", info)
	return info, nil
}

// normalizeRoots はルートの重複と表記ゆれをならす。キャッシュ引き当ての前に使う。
func normalizeRoots(roots []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(roots))
	for _, root := range roots {
		abs, err := winpath.CleanAbs(root)
		if err != nil {
			continue
		}
		key := winpath.Key(abs)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, abs)
	}
	return result
}

func (a *App) CancelScan() {
	a.mu.Lock()
	cancel := a.cancelScan
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *App) GetRows(query RowQuery) RowPage {
	a.mu.Lock()
	items := append([]model.Item(nil), a.items...)
	summary := a.summary
	warnings := append([]string(nil), a.warnings...)
	warningTotal := a.warningTotal
	roots := append([]string(nil), a.roots...)
	moves := a.moves
	triagePath := a.triageFile()
	a.mu.Unlock()

	decisions, triageErr := readTriage(triagePath)
	triageError := ""
	if triageErr != nil {
		triageError = triageErr.Error()
	}
	states := a.advisoryTriageStates(items, roots, decisions)
	duplicateResults := a.duplicateResultsSnapshot()
	filtered := filterItemsWithDuplicates(items, query, duplicateResults)
	if query.Triage != "" && query.Triage != "all" {
		out := filtered[:0]
		for _, item := range filtered {
			if states[item.ID] == query.Triage {
				out = append(out, item)
			}
		}
		filtered = out
	}
	sortItemsWithDuplicates(filtered, query.Sort, query.Desc, duplicateResults)

	total := len(filtered)
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	end := offset + limit
	if end > total {
		end = total
	}
	rows := make([]Row, 0, end-offset)
	for _, item := range filtered[offset:end] {
		row := toRow(item)
		row.Triage = states[item.ID]
		if result, ok := duplicateResults[item.ID]; ok {
			copied := result
			row.Duplicate = &copied
		}
		rows = append(rows, row)
	}
	// ダッシュボードはスキャン全体の値。一覧の絞り込みとは独立させる。
	filteredView := len(filtered) != len(items)
	return RowPage{
		TriageError:  triageError,
		Rows:         rows,
		Total:        total,
		Offset:       offset,
		Summary:      summary,
		Dashboard:    dashboard.Build(items, moves, filteredView, len(filtered)),
		Warnings:     warnings,
		WarningTotal: warningTotal,
		Roots:        roots,
	}
}

func (a *App) GetItemDetail(id string) (model.Item, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	index, ok := a.index[id]
	if !ok {
		return model.Item{}, errors.New("項目が見つかりません")
	}
	return a.items[index], nil
}

func (a *App) CandidateIDs(query RowQuery) []string {
	a.mu.RLock()
	items := append([]model.Item(nil), a.items...)
	roots := append([]string(nil), a.roots...)
	triagePath := a.triageFile()
	a.mu.RUnlock()
	duplicateResults := a.duplicateResultsSnapshot()
	filtered := filterItemsWithDuplicates(items, query, duplicateResults)
	if query.Triage != "" && query.Triage != "all" {
		decisions, err := readTriage(triagePath)
		if err != nil {
			return []string{}
		}
		out := filtered[:0]
		for _, item := range filtered {
			if triageState(item, roots, decisions) == query.Triage {
				out = append(out, item)
			}
		}
		filtered = out
	}
	sortItemsWithDuplicates(filtered, query.Sort, query.Desc, duplicateResults)
	ids := make([]string, 0, len(filtered))
	for _, item := range filtered {
		if item.Deletable() {
			ids = append(ids, item.ID)
		}
	}
	return ids
}

func (a *App) PickFolder() (string, error) {
	if a.ctx == nil {
		return "", errors.New("アプリケーションが初期化されていません")
	}
	return wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "対象フォルダを選択",
	})
}

func (a *App) OpenInExplorer(path string) error {
	if path == "" {
		return errors.New("パスが空です")
	}
	abs, err := winpath.CleanAbs(path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return shell.OpenFolder(abs)
	}
	return shell.RevealFile(abs)
}

func (a *App) RevealItem(id string) error {
	a.mu.RLock()
	index, ok := a.index[id]
	var item model.Item
	if ok {
		item = a.items[index]
	}
	a.mu.RUnlock()
	if !ok {
		return errors.New("項目が見つかりません")
	}
	if item.FolderPath != "" {
		if _, err := os.Lstat(item.FolderPath); err == nil {
			return a.OpenInExplorer(item.FolderPath)
		}
	}
	if item.ZipPath != "" {
		if _, err := os.Lstat(item.ZipPath); err == nil {
			return a.OpenInExplorer(item.ZipPath)
		}
	}
	return errors.New("表示できるパスがありません（既にごみ箱へ移動済み、または消失）")
}

func (a *App) RevealFolder(id string) error {
	a.mu.RLock()
	index, ok := a.index[id]
	var item model.Item
	if ok {
		item = a.items[index]
	}
	a.mu.RUnlock()
	if !ok {
		return errors.New("項目が見つかりません")
	}
	return a.revealKind(item.FolderPath, item.FolderMoved, "解凍済みフォルダ")
}

func (a *App) RevealZip(id string) error {
	a.mu.RLock()
	index, ok := a.index[id]
	var item model.Item
	if ok {
		item = a.items[index]
	}
	a.mu.RUnlock()
	if !ok {
		return errors.New("項目が見つかりません")
	}
	return a.revealKind(item.ZipPath, item.ZipMoved, "ZIP")
}

func (a *App) revealKind(path string, moved bool, label string) error {
	if path == "" {
		return fmt.Errorf("%sのパスがありません", label)
	}
	if _, err := os.Lstat(path); err != nil {
		if moved {
			return fmt.Errorf("%sは既にごみ箱へ移動済みです", label)
		}
		return fmt.Errorf("%sが見つかりません: %w", label, err)
	}
	return a.OpenInExplorer(path)
}

func (a *App) CopyText(text string) error {
	a.mu.RLock()
	ctx := a.ctx
	a.mu.RUnlock()
	if ctx == nil {
		return errors.New("アプリケーションが初期化されていません")
	}
	if text == "" {
		return errors.New("コピーするテキストがありません")
	}
	return wailsruntime.ClipboardSetText(ctx, text)
}

func (a *App) OpenConfig() error {
	a.mu.RLock()
	path := a.configPath
	a.mu.RUnlock()
	if path == "" {
		return errors.New("設定ファイルの場所が特定できません")
	}
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		if err := config.WriteDefault(path); err != nil {
			return err
		}
	}
	return a.OpenInExplorer(path)
}

func (a *App) OpenLog() error {
	if a.logPath == "" {
		return errors.New("ログファイルが設定されていません")
	}
	if _, err := os.Lstat(a.logPath); err == nil {
		return a.OpenInExplorer(a.logPath)
	}
	if err := os.MkdirAll(filepath.Dir(a.logPath), 0o700); err != nil {
		return err
	}
	return a.OpenInExplorer(filepath.Dir(a.logPath))
}

func (a *App) GetLogPath() string {
	return a.logPath
}

func (a *App) GetVersion() string {
	return appVersion
}

func (a *App) emit(name string, data any) {
	a.mu.RLock()
	ctx := a.ctx
	a.mu.RUnlock()
	if ctx == nil {
		return
	}
	wailsruntime.EventsEmit(ctx, name, data)
}
