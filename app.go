package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/tonbiattack/archive-extract-cleaner/internal/config"
	"github.com/tonbiattack/archive-extract-cleaner/internal/engine"
	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/oplog"
	"github.com/tonbiattack/archive-extract-cleaner/internal/removal"
	"github.com/tonbiattack/archive-extract-cleaner/internal/verify"
	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
)

const appVersion = "0.1.0"

type App struct {
	ctx context.Context

	mu            sync.RWMutex
	items         []model.Item
	index         map[string]int
	roots         []string
	summary       model.Summary
	warnings      []string
	warningTotal  int
	scanning      bool
	deleting      bool
	cancelScan    context.CancelFunc
	log           *oplog.Logger
	logPath       string
	limits        model.Limits
	configPath    string
	configWarning string
}

type ScanInfo struct {
	Summary      model.Summary `json:"summary"`
	Warnings     []string      `json:"warnings"`
	WarningTotal int           `json:"warningTotal"`
	Roots        []string      `json:"roots"`
	Cancelled    bool          `json:"cancelled"`
}

type AppState struct {
	Summary       model.Summary `json:"summary"`
	Warnings      []string      `json:"warnings"`
	WarningTotal  int           `json:"warningTotal"`
	Roots         []string      `json:"roots"`
	Scanning      bool          `json:"scanning"`
	Deleting      bool          `json:"deleting"`
	LogPath       string        `json:"logPath"`
	ConfigPath    string        `json:"configPath"`
	ConfigWarning string        `json:"configWarning"`
	Limits        model.Limits  `json:"limits"`
	Version       string        `json:"version"`
}

type DeleteProgress struct {
	Index int    `json:"index"`
	Total int    `json:"total"`
	Name  string `json:"name"`
	Stage string `json:"stage"`
}

type DeleteResponse struct {
	Results []model.DeleteResult `json:"results"`
	Summary model.Summary        `json:"summary"`
	Deleted int                  `json:"deleted"`
	Failed  int                  `json:"failed"`
	Skipped int                  `json:"skipped"`
}

func NewApp() *App {
	logger := oplog.New("")
	cfg, configPath, configErr := config.Load("")
	app := &App{
		index:      map[string]int{},
		log:        logger,
		logPath:    logger.Path(),
		limits:     cfg.Limits,
		configPath: configPath,
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
		Summary:       a.summary,
		Warnings:      append([]string(nil), a.warnings...),
		WarningTotal:  a.warningTotal,
		Roots:         append([]string(nil), a.roots...),
		Scanning:      a.scanning,
		Deleting:      a.deleting,
		LogPath:       a.logPath,
		ConfigPath:    a.configPath,
		ConfigWarning: a.configWarning,
		Limits:        a.limits,
		Version:       appVersion,
	}
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

func (a *App) PreviewDelete(ids []string) model.DeletePreview {
	a.mu.RLock()
	defer a.mu.RUnlock()
	preview := model.DeletePreview{}
	seen := map[string]struct{}{}
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		index, ok := a.index[id]
		if !ok {
			continue
		}
		item := a.items[index]
		if !item.Deletable() {
			continue
		}
		preview.Count++
		preview.Bytes += item.Size
		preview.Items = append(preview.Items, model.DeletePreviewItem{
			ID:         item.ID,
			Name:       item.DisplayName(),
			ZipPath:    item.ZipPath,
			FolderPath: item.FolderPath,
			Size:       item.Size,
		})
	}
	return preview
}

func (a *App) DeleteSelected(ids []string, confirmed bool) (DeleteResponse, error) {
	if !confirmed {
		return DeleteResponse{}, errors.New("明示的な確認が必要です")
	}
	if len(ids) == 0 {
		return DeleteResponse{}, errors.New("削除対象が選択されていません")
	}
	a.mu.Lock()
	if a.deleting || a.scanning {
		a.mu.Unlock()
		return DeleteResponse{}, errors.New("別の処理を実行中です")
	}
	base := a.ctx
	if base == nil {
		base = context.Background()
	}
	items := append([]model.Item(nil), a.items...)
	roots := append([]string(nil), a.roots...)
	limits := a.limits
	logger := a.log
	index := a.index
	a.deleting = true
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.deleting = false
		a.mu.Unlock()
	}()
	seen := map[string]struct{}{}
	results := make([]model.DeleteResult, 0, len(ids))
	updates := map[string]model.Item{}
	deleted, failed, skipped := 0, 0, 0

	for position, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		indexInItems, ok := index[id]
		if !ok {
			skipped++
			results = append(results, model.DeleteResult{ID: id, Skipped: true, Reason: "項目が見つかりません", LogPath: a.logPath})
			continue
		}
		item := items[indexInItems]
		a.emit("delete:progress", DeleteProgress{Index: position + 1, Total: len(ids), Name: item.DisplayName(), Stage: "revalidating"})
		if !item.Deletable() {
			skipped++
			results = append(results, model.DeleteResult{
				ID: item.ID, Name: item.DisplayName(), ZipPath: item.ZipPath, FolderPath: item.FolderPath,
				Skipped: true, Reason: "削除可能な項目ではありません", LogPath: a.logPath,
			})
			a.appendLog(logger, item, false, "削除可能な項目ではありません")
			continue
		}

		verification := verify.Verify(base, item.ZipPath, item.FolderPath, verify.Options{Limits: limits, Roots: roots})
		if verification.Verdict != model.VerdictDeleteCandidate {
			item.Verdict = verification.Verdict
			item.Status = model.StatusError
			item.Reason = "再検証に失敗しました: " + verification.Reason
			item.Details = verification.Details
			item.DeleteError = verification.Reason
			item.CheckedAt = time.Now()
			updates[id] = item
			skipped++
			results = append(results, model.DeleteResult{
				ID: item.ID, Name: item.DisplayName(), ZipPath: item.ZipPath, FolderPath: item.FolderPath,
				Skipped: true, Reason: item.Reason, LogPath: a.logPath,
			})
			a.appendLog(logger, item, false, item.Reason)
			a.emit("delete:progress", DeleteProgress{Index: position + 1, Total: len(ids), Name: item.DisplayName(), Stage: "skipped"})
			continue
		}

		a.emit("delete:progress", DeleteProgress{Index: position + 1, Total: len(ids), Name: item.DisplayName(), Stage: "deleting"})
		removalResult := removal.Delete(base, item.FolderPath, roots)
		now := time.Now()
		logItem := item
		if removalResult.Success {
			item.Verdict = model.VerdictKeepArchive
			item.Status = model.StatusDeleted
			item.DeletedAt = now
			item.DeleteError = ""
			item.Details = nil
			item.MatchMode = ""
			item.Reason = "解凍済みフォルダを削除しました"
			item.Size = item.ZipSize
			deleted++
			results = append(results, model.DeleteResult{
				ID: item.ID, Name: item.DisplayName(), ZipPath: item.ZipPath, FolderPath: item.FolderPath,
				Success: true, Reason: removalResult.Reason, DeletedAt: now, LogPath: a.logPath,
			})
			a.appendLog(logger, logItem, true, "")
		} else {
			reason := removalResult.Reason
			if removalResult.Quarantined != "" {
				reason += "（退避先: " + removalResult.Quarantined + "）"
			}
			item.Verdict = model.VerdictUnverified
			item.Status = model.StatusError
			item.Reason = "削除に失敗しました: " + reason
			item.DeleteError = reason
			item.CheckedAt = now
			failed++
			results = append(results, model.DeleteResult{
				ID: item.ID, Name: item.DisplayName(), ZipPath: item.ZipPath, FolderPath: item.FolderPath,
				Reason: item.Reason, DeletedAt: now, LogPath: a.logPath,
			})
			a.appendLog(logger, logItem, false, item.Reason)
		}
		updates[id] = item
		a.emit("delete:progress", DeleteProgress{Index: position + 1, Total: len(ids), Name: item.DisplayName(), Stage: "done"})
	}

	a.mu.Lock()
	for id, item := range updates {
		if position, ok := a.index[id]; ok {
			a.items[position] = item
		}
	}
	old := a.summary
	summary := model.Summarize(a.items)
	summary.ScannedAt = old.ScannedAt
	summary.DurationMS = old.DurationMS
	a.summary = summary
	a.mu.Unlock()

	response := DeleteResponse{Results: results, Summary: summary, Deleted: deleted, Failed: failed, Skipped: skipped}
	a.emit("delete:done", response)
	return response, nil
}

func (a *App) appendLog(logger *oplog.Logger, item model.Item, success bool, message string) {
	if logger == nil {
		return
	}
	err := logger.Append(oplog.Entry{
		Time:       time.Now(),
		Action:     "delete",
		ID:         item.ID,
		ZipPath:    item.ZipPath,
		FolderPath: item.FolderPath,
		Verdict:    string(item.Verdict),
		Success:    success,
		Error:      message,
	})
	if err != nil {
		a.emit("delete:log_error", err.Error())
	}
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
	if _, err := os.Lstat(abs); err != nil {
		return err
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer.exe", "/select,"+abs).Start()
	case "darwin":
		return exec.Command("open", "-R", abs).Start()
	default:
		return exec.Command("xdg-open", filepath.Dir(abs)).Start()
	}
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
		return a.OpenInExplorer(item.ZipPath)
	}
	return errors.New("開けるパスがありません")
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
