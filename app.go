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

	"github.com/tonbiattack/archive-extract-cleaner/internal/config"
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
	configPath           string
	configWarning        string
	mover                removal.Mover
	multiDriveCopyScript string
	multiDriveCopyPreset string
	previewMu            sync.Mutex
	previewCache         map[string]cachedPreview
}

type ScanInfo struct {
	Summary      model.Summary `json:"summary"`
	Warnings     []string      `json:"warnings"`
	WarningTotal int           `json:"warningTotal"`
	Roots        []string      `json:"roots"`
	Cancelled    bool          `json:"cancelled"`
}

type AppState struct {
	Summary                 model.Summary `json:"summary"`
	Warnings                []string      `json:"warnings"`
	WarningTotal            int           `json:"warningTotal"`
	Roots                   []string      `json:"roots"`
	Scanning                bool          `json:"scanning"`
	Deleting                bool          `json:"deleting"`
	LogPath                 string        `json:"logPath"`
	ConfigPath              string        `json:"configPath"`
	ConfigWarning           string        `json:"configWarning"`
	Limits                  model.Limits  `json:"limits"`
	MultiDriveCopyScript    string        `json:"multiDriveCopyScript"`
	MultiDriveCopyPreset    string        `json:"multiDriveCopyPreset"`
	MultiDriveCopyAvailable bool          `json:"multiDriveCopyAvailable"`
	Version                 string        `json:"version"`
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
		configPath:           configPath,
		mover:                removal.MoveToRecycleBin,
		multiDriveCopyScript: scriptPath,
		multiDriveCopyPreset: cfg.MultiDriveCopy.Preset,
		previewCache:         map[string]cachedPreview{},
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
		MultiDriveCopyScript:    a.multiDriveCopyScript,
		MultiDriveCopyPreset:    a.multiDriveCopyPreset,
		MultiDriveCopyAvailable: transfer.ScriptAvailable(a.multiDriveCopyScript),
		Version:                 appVersion,
	}
}

func (a *App) GetTransferReview() transfer.Review {
	a.mu.RLock()
	defer a.mu.RUnlock()
	items := append([]model.Item(nil), a.items...)
	return transfer.BuildReview(items, a.multiDriveCopyScript, a.multiDriveCopyPreset, a.scanning || a.deleting)
}

func (a *App) LaunchMultiDriveCopy(paths []string) (transfer.LaunchResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.scanning || a.deleting {
		return transfer.LaunchResult{}, errors.New("スキャンまたはごみ箱移動の処理中は転送を開始できません")
	}
	items := append([]model.Item(nil), a.items...)
	review := transfer.BuildReview(items, a.multiDriveCopyScript, a.multiDriveCopyPreset, false)
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
	limits := a.limits
	a.mu.Unlock()

	defer func() {
		cancel()
		a.mu.Lock()
		a.scanning = false
		a.cancelScan = nil
		a.mu.Unlock()
	}()

	lastEmit := time.Time{}
	progress := func(p model.Progress) {
		if p.Phase != model.PhaseDone && time.Since(lastEmit) < 80*time.Millisecond {
			return
		}
		lastEmit = time.Now()
		a.emit("scan:progress", p)
	}

	result, err := engine.Scan(ctx, engine.Options{
		Roots:     roots,
		Recursive: true,
		Limits:    limits,
		Progress:  progress,
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

	index := make(map[string]int, len(result.Items))
	for i, item := range result.Items {
		index[item.ID] = i
	}
	a.mu.Lock()
	a.items = result.Items
	a.index = index
	a.roots = result.Roots
	a.summary = result.Summary
	a.warnings = result.Warnings
	a.warningTotal = result.WarningTotal
	a.mu.Unlock()
	a.clearPreviewCache()

	info := ScanInfo{
		Summary:      result.Summary,
		Warnings:     result.Warnings,
		WarningTotal: result.WarningTotal,
		Roots:        result.Roots,
	}
	a.emit("scan:done", info)
	return info, nil
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
	a.mu.RLock()
	items := append([]model.Item(nil), a.items...)
	summary := a.summary
	warnings := append([]string(nil), a.warnings...)
	warningTotal := a.warningTotal
	roots := append([]string(nil), a.roots...)
	a.mu.RUnlock()

	filtered := filterItems(items, query)
	sortItems(filtered, query.Sort, query.Desc)

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
		rows = append(rows, toRow(item))
	}
	return RowPage{
		Rows:         rows,
		Total:        total,
		Offset:       offset,
		Summary:      summary,
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
	a.mu.RUnlock()
	filtered := filterItems(items, query)
	sortItems(filtered, query.Sort, query.Desc)
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
