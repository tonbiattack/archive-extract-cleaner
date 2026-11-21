package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tonbiattack/archive-extract-cleaner/internal/dashboard"
	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/oplog"
	"github.com/tonbiattack/archive-extract-cleaner/internal/removal"
	"github.com/tonbiattack/archive-extract-cleaner/internal/verify"
	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
)

type MoveMethod string

const (
	MoveMethodFolder MoveMethod = "folder"
	MoveMethodZip    MoveMethod = "zip"
	MoveMethodBoth   MoveMethod = "both"
)

type MoveRequest struct {
	ID     string     `json:"id"`
	Method MoveMethod `json:"method"`
}

type MoveTargetOption struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type MoveOption struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	Verdict       string             `json:"verdict"`
	Status        string             `json:"status"`
	ZipPath       string             `json:"zipPath"`
	FolderPath    string             `json:"folderPath"`
	ZipExists     bool               `json:"zipExists"`
	FolderExists  bool               `json:"folderExists"`
	ZipSize       int64              `json:"zipSize"`
	FolderSize    int64              `json:"folderSize"`
	ZipMoved      bool               `json:"zipMoved"`
	FolderMoved   bool               `json:"folderMoved"`
	CanFolder     bool               `json:"canFolder"`
	CanZip        bool               `json:"canZip"`
	CanBoth       bool               `json:"canBoth"`
	DefaultMethod MoveMethod         `json:"defaultMethod"`
	Warning       string             `json:"warning,omitempty"`
	Targets       []MoveTargetOption `json:"targets"`
}

type MovePreviewItem struct {
	ID      string             `json:"id"`
	Name    string             `json:"name"`
	Method  MoveMethod         `json:"method"`
	Targets []MoveTargetOption `json:"targets"`
}

type MovePreview struct {
	ItemCount   int               `json:"itemCount"`
	TargetCount int               `json:"targetCount"`
	Bytes       int64             `json:"bytes"`
	Items       []MovePreviewItem `json:"items"`
	Warnings    []string          `json:"warnings"`
}

type MoveTargetResult struct {
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Name    string `json:"name"`
	Success bool   `json:"success"`
	Reason  string `json:"reason"`
}

type MoveResult struct {
	ID      string             `json:"id"`
	Name    string             `json:"name"`
	Method  MoveMethod         `json:"method"`
	Success bool               `json:"success"`
	Partial bool               `json:"partial"`
	Skipped bool               `json:"skipped"`
	Reason  string             `json:"reason"`
	Targets []MoveTargetResult `json:"targets"`
	MovedAt time.Time          `json:"movedAt"`
	LogPath string             `json:"logPath"`
}

type MoveResponse struct {
	Results   []MoveResult    `json:"results"`
	Summary   model.Summary   `json:"summary"`
	Succeeded int             `json:"succeeded"`
	Partial   int             `json:"partial"`
	Failed    int             `json:"failed"`
	Skipped   int             `json:"skipped"`
	Moves     dashboard.Moves `json:"moves"`
}

func (a *App) GetMoveOptions(ids []string) []MoveOption {
	a.mu.RLock()
	defer a.mu.RUnlock()
	options := make([]MoveOption, 0, len(ids))
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
		options = append(options, buildMoveOption(a.items[index]))
	}
	return options
}

func (a *App) PreviewMove(requests []MoveRequest) (MovePreview, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if len(requests) == 0 {
		return MovePreview{}, errors.New("移動対象が選択されていません")
	}
	preview := MovePreview{}
	seen := map[string]struct{}{}
	for _, request := range requests {
		if _, ok := seen[request.ID]; ok {
			continue
		}
		seen[request.ID] = struct{}{}
		index, ok := a.index[request.ID]
		if !ok {
			return MovePreview{}, fmt.Errorf("項目が見つかりません: %s", request.ID)
		}
		item := a.items[index]
		option := buildMoveOption(item)
		targets, allowed := moveTargetsForMethod(option, request.Method)
		if !allowed {
			return MovePreview{}, fmt.Errorf("%s では選択した移動方法を利用できません", item.DisplayName())
		}
		targetCount := len(targets)
		if targetCount == 0 {
			return MovePreview{}, fmt.Errorf("%s に移動できる対象がありません", item.DisplayName())
		}
		previewItem := MovePreviewItem{ID: item.ID, Name: item.DisplayName(), Method: request.Method, Targets: targets}
		preview.Items = append(preview.Items, previewItem)
		preview.ItemCount++
		preview.TargetCount += targetCount
		for _, target := range targets {
			preview.Bytes += target.Size
		}
		if request.Method == MoveMethodBoth {
			preview.Warnings = append(preview.Warnings, item.DisplayName()+": ZIPとフォルダの両方を移動します。最後のローカルコピーを失う可能性があります。")
		} else if item.Status == model.StatusError || item.ZipMoved || item.FolderMoved {
			if option.Warning != "" {
				preview.Warnings = append(preview.Warnings, item.DisplayName()+": "+option.Warning)
			}
		}
	}
	return preview, nil
}

func (a *App) DeleteSelected(requests []MoveRequest, confirmed bool) (MoveResponse, error) {
	if !confirmed {
		return MoveResponse{}, errors.New("明示的な確認が必要です")
	}
	if len(requests) == 0 {
		return MoveResponse{}, errors.New("移動対象が選択されていません")
	}
	// Archive disposal is exclusively authorized by the explicit discard preview.
	// This legacy API permits only verified duplicate-folder cleanup.
	for _, request := range requests {
		if request.Method != MoveMethodFolder {
			return MoveResponse{}, errors.New("ZIP disposal requires discard preview")
		}
	}
	a.mu.Lock()
	if a.deleting || a.scanning {
		a.mu.Unlock()
		return MoveResponse{}, errors.New("別の処理を実行中です")
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
	mover := a.mover
	a.deleting = true
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.deleting = false
		a.mu.Unlock()
	}()

	// Coordinate with discard across app instances before verifying either copy.
	// Never recycle a folder if the shared lock cannot be acquired.
	unlockTriage, err := lockTriage(a.triageFile())
	if err != nil {
		return MoveResponse{}, err
	}
	defer unlockTriage()

	seen := map[string]struct{}{}
	results := make([]MoveResult, 0, len(requests))
	updates := map[string]model.Item{}
	succeeded, partial, failed, skipped := 0, 0, 0, 0
	moves := dashboard.Moves{}

	for position, request := range requests {
		if _, ok := seen[request.ID]; ok {
			continue
		}
		seen[request.ID] = struct{}{}
		indexInItems, ok := index[request.ID]
		if !ok {
			skipped++
			moves.AddMoveResult(dashboard.MoveOutcome{Skipped: true})
			results = append(results, MoveResult{ID: request.ID, Method: request.Method, Skipped: true, Reason: "項目が見つかりません", LogPath: a.logPath})
			continue
		}
		item := items[indexInItems]
		a.emit("delete:progress", DeleteProgress{Index: position + 1, Total: len(requests), Name: item.DisplayName(), Stage: "revalidating"})
		unsafeFolder := item.ZipMoved || item.FolderMoved || item.ZipPath == "" || item.FolderPath == ""
		for _, other := range items {
			if other.ID == item.ID {
				continue
			}
			// AMBIGUOUS の RelatedFolders は「対応先」ではなく「候補」であり、
			// どれが実際に対応するか決まっていない。これを重複判定に使うと、
			// 候補に含まれるだけの無関係なフォルダ（例: 0 (1) (15).zip の候補に
			// 0 (1) (13) が入る）が永久に移動できなくなる。
			// 実際に対応が確定している項目だけを重複判定の対象にする。
			if other.Verdict == model.VerdictAmbiguous {
				continue
			}
			for _, p := range append([]string{other.ZipPath, other.FolderPath}, other.RelatedFolders...) {
				if p != "" && (winpath.EqualPath(item.FolderPath, p) || winpath.IsWithin(item.FolderPath, p) || winpath.IsWithin(p, item.FolderPath)) {
					unsafeFolder = true
				}
			}
		}
		if unsafeFolder {
			skipped++
			moves.AddMoveResult(dashboard.MoveOutcome{Skipped: true, Bytes: item.Size})
			results = append(results, MoveResult{ID: item.ID, Method: request.Method, Skipped: true, Reason: unsafeFolderReason(item), LogPath: a.logPath})
			continue
		}
		option := buildMoveOption(item)
		targets, allowed := moveTargetsForMethod(option, request.Method)
		if !allowed || len(targets) == 0 {
			skipped++
			moves.AddMoveResult(dashboard.MoveOutcome{Skipped: true, Bytes: item.Size})
			results = append(results, MoveResult{
				ID: item.ID, Name: item.DisplayName(), Method: request.Method, Skipped: true,
				Reason: "選択した移動方法を利用できません", LogPath: a.logPath,
			})
			a.appendMoveLog(logger, item, request.Method, false, "選択した移動方法を利用できません", nil)
			continue
		}

		if !item.ZipMoved && !item.FolderMoved {
			// 移動直前の再照合ではキャッシュを渡さない。
			// スキャン結果がキャッシュ由来でも、削除の根拠は必ず実体の再計算にする。
			verification := verify.Verify(base, item.ZipPath, item.FolderPath, verify.Options{Limits: limits, Roots: roots})
			if !verification.Verdict.IsDeleteCandidate() {
				item.Verdict = verification.Verdict
				item.Status = model.StatusError
				item.Reason = "再検証に失敗しました: " + verification.Reason
				item.Details = verification.Details
				item.DeleteError = verification.Reason
				item.CheckedAt = time.Now()
				updates[request.ID] = item
				skipped++
				moves.AddMoveResult(dashboard.MoveOutcome{Skipped: true, Bytes: item.Size})
				results = append(results, MoveResult{
					ID: item.ID, Name: item.DisplayName(), Method: request.Method, Skipped: true,
					Reason: item.Reason, LogPath: a.logPath,
				})
				a.appendMoveLog(logger, item, request.Method, false, item.Reason, nil)
				a.emit("delete:progress", DeleteProgress{Index: position + 1, Total: len(requests), Name: item.DisplayName(), Stage: "skipped"})
				continue
			}
		}

		logItem := item
		// Capture a valid saved decision and the expected ZIP-only fingerprint before
		// recycling. A stale decision or an unreadable store must never migrate.
		var migrationFingerprint string
		migrated := false
		var decisions map[string]triageDecision
		if request.Method == MoveMethodFolder && !item.ZipMoved && !item.FolderMoved {
			decisions, _ = readTriage(a.triageFile())
			if triageState(item, roots, decisions) == "save" {
				zipOnly := model.Item{ID: model.MakeID(item.ZipPath, ""), ZipPath: item.ZipPath, Verdict: model.VerdictKeepArchive}
				migrationFingerprint, _ = triageFingerprint(zipOnly, roots)
			}
		}
		targetResults := make([]MoveTargetResult, 0, len(targets))
		for _, target := range targets {
			a.emit("delete:progress", DeleteProgress{Index: position + 1, Total: len(requests), Name: item.DisplayName() + " / " + target.Kind, Stage: "recycling"})
			kind := removal.KindFile
			if target.Kind == "folder" {
				kind = removal.KindFolder
			}
			moveResult := removal.Move(base, target.Path, roots, kind, mover)
			targetResult := MoveTargetResult{Kind: target.Kind, Path: target.Path, Name: target.Name, Success: moveResult.Success, Reason: moveResult.Reason}
			targetResults = append(targetResults, targetResult)
			if moveResult.Success {
				if target.Kind == "folder" {
					item.FolderMoved = true
				} else {
					item.ZipMoved = true
				}
			}
		}

		now := time.Now()
		item.CheckedAt = now
		allSuccess := true
		anySuccess := false
		for _, targetResult := range targetResults {
			if targetResult.Success {
				anySuccess = true
			} else {
				allSuccess = false
			}
		}

		if unlockTriage != nil {
			if allSuccess && migrationFingerprint != "" {
				zipOnly := model.Item{ID: model.MakeID(logItem.ZipPath, ""), ZipPath: logItem.ZipPath, Verdict: model.VerdictKeepArchive}
				fp, fpErr := triageFingerprint(zipOnly, roots)
				_, folderErr := os.Lstat(logItem.FolderPath)
				if fpErr == nil && fp == migrationFingerprint && errors.Is(folderErr, os.ErrNotExist) {
					_, indexed := index[zipOnly.ID]
					if _, exists := decisions[zipOnly.ID]; !exists && !indexed && len(item.RelatedFolders) == 0 {
						decisions[zipOnly.ID] = triageDecision{State: "save", Fingerprint: fp, RemovedFolder: logItem.FolderPath}
						delete(decisions, logItem.ID)
						if err := writeTriage(a.triageFile(), decisions); err != nil {
							a.emit("delete:log_error", "triage migration: "+err.Error())
						} else {
							migrated = true
						}
					}
				}
			}
		}

		// ダッシュボード用に、対象サイズと実際に移せたサイズを記録する。
		requestedBytes := targetsBytes(targets)
		movedBytes := int64(0)
		for index, targetResult := range targetResults {
			if targetResult.Success {
				movedBytes += targets[index].Size
			}
		}

		switch {
		case allSuccess:
			item.DeleteError = ""
			item.Details = nil
			item.MatchMode = ""
			item.DeletedAt = now
			if item.ZipMoved && item.FolderMoved {
				item.Verdict = model.VerdictKeepArchive
				item.Status = model.StatusDeleted
				item.Reason = "ZIPと解凍済みフォルダをごみ箱に移動しました"
				item.Size = 0
			} else if item.FolderMoved {
				item.Verdict = model.VerdictKeepArchive
				item.Status = model.StatusDeleted
				item.Reason = "解凍済みフォルダをごみ箱に移動しました"
				item.Size = item.ZipSize
			} else if item.ZipMoved {
				item.Verdict = model.VerdictKeepDirectory
				item.Status = model.StatusReady
				item.DeletedAt = time.Time{}
				item.Reason = "ZIPをごみ箱に移動しました（フォルダは保持）"
				item.Size = item.FolderSize
			}
			succeeded++
			moves.AddMoveResult(dashboard.MoveOutcome{
				Success:    true,
				Bytes:      requestedBytes,
				MovedBytes: movedBytes,
			})
			results = append(results, MoveResult{
				ID: item.ID, Name: item.DisplayName(), Method: request.Method, Success: true,
				Reason: item.Reason, Targets: targetResults, MovedAt: now, LogPath: a.logPath,
			})
			a.appendMoveLog(logger, logItem, request.Method, true, "", targetResults)
		case anySuccess:
			item.Status = model.StatusError
			item.Verdict = model.VerdictDeleteCandidate
			item.DeleteError = failureReason(targetResults)
			item.Reason = "一部の対象をごみ箱に移動できませんでした: " + item.DeleteError
			item.Size = remainingSize(item)
			partial++
			moves.AddMoveResult(dashboard.MoveOutcome{
				Partial:    true,
				Bytes:      requestedBytes,
				MovedBytes: movedBytes,
			})
			results = append(results, MoveResult{
				ID: item.ID, Name: item.DisplayName(), Method: request.Method, Partial: true,
				Reason: item.Reason, Targets: targetResults, MovedAt: now, LogPath: a.logPath,
			})
			a.appendMoveLog(logger, logItem, request.Method, false, item.DeleteError, targetResults)
		default:
			item.Status = model.StatusError
			item.DeleteError = failureReason(targetResults)
			item.Reason = "ごみ箱への移動に失敗しました: " + item.DeleteError
			item.Size = remainingSize(item)
			failed++
			moves.AddMoveResult(dashboard.MoveOutcome{Bytes: requestedBytes})
			results = append(results, MoveResult{
				ID: item.ID, Name: item.DisplayName(), Method: request.Method,
				Reason: item.Reason, Targets: targetResults, MovedAt: now, LogPath: a.logPath,
			})
			a.appendMoveLog(logger, logItem, request.Method, false, item.DeleteError, targetResults)
		}
		if migrated {
			item.ID = model.MakeID(item.ZipPath, "")
			item.FolderPath = ""
			item.FolderMoved = false
		}
		updates[request.ID] = item
		a.emit("delete:progress", DeleteProgress{Index: position + 1, Total: len(requests), Name: item.DisplayName(), Stage: "done"})
	}

	a.mu.Lock()
	a.triageDisplay = nil
	a.discardPreview = nil
	for id, item := range updates {
		if position, ok := a.index[id]; ok {
			a.items[position] = item
			if item.ID != id {
				delete(a.index, id)
				a.index[item.ID] = position
			}
		}
	}
	old := a.summary
	summary := model.Summarize(a.items)
	summary.ScannedAt = old.ScannedAt
	summary.DurationMS = old.DurationMS
	a.summary = summary
	// 移動集計は累積する。複数回に分けて移動しても合計が増えていく。
	merged := a.moves
	merged.Succeeded.Add(moves.Succeeded.Count, moves.Succeeded.Bytes)
	merged.Partial.Add(moves.Partial.Count, moves.Partial.Bytes)
	merged.Failed.Add(moves.Failed.Count, moves.Failed.Bytes)
	merged.Skipped.Add(moves.Skipped.Count, moves.Skipped.Bytes)
	merged.MovedBytes += moves.MovedBytes
	merged.AllSucceeded = merged.Failed.Count == 0 && merged.Partial.Count == 0
	a.moves = merged
	a.mu.Unlock()
	a.clearPreviewCache()

	response := MoveResponse{
		Results:   results,
		Summary:   summary,
		Succeeded: succeeded,
		Partial:   partial,
		Failed:    failed,
		Skipped:   skipped,
		Moves:     merged,
	}
	a.emit("delete:done", response)
	return response, nil
}

// targetsBytes は移動対象の合計サイズを返す。
// unsafeFolderReason は移動を見送った理由を、利用者が対処できる言葉で返す。
// 以前は "archive unavailable or folder overlaps another item" とだけ出していたため、
// 何を直せばよいか分からなかった。
func unsafeFolderReason(item model.Item) string {
	switch {
	case item.ZipMoved:
		return "アーカイブは既に移動済みです。残ったフォルダを移動するには再スキャンしてください"
	case item.FolderMoved:
		return "フォルダは既に移動済みです"
	case item.ZipPath == "":
		return "対応するアーカイブがないため、この操作では移動できません"
	case item.FolderPath == "":
		return "対応するフォルダがないため、この操作では移動できません"
	default:
		return "このフォルダは別の項目と範囲が重なっているため移動できません。重なっている項目を先に処理するか、対象から外してください"
	}
}

func targetsBytes(targets []MoveTargetOption) int64 {
	var total int64
	for _, target := range targets {
		total += target.Size
	}
	return total
}

func buildMoveOption(item model.Item) MoveOption {
	option := MoveOption{
		ID:          item.ID,
		Name:        item.DisplayName(),
		Verdict:     string(item.Verdict),
		Status:      string(item.Status),
		ZipPath:     item.ZipPath,
		FolderPath:  item.FolderPath,
		FolderSize:  item.FolderSize,
		ZipMoved:    item.ZipMoved,
		FolderMoved: item.FolderMoved,
	}
	if !item.ZipMoved {
		option.ZipExists, option.ZipSize = statMoveTarget(item.ZipPath, false)
	}
	if !item.FolderMoved {
		option.FolderExists, _ = statMoveTarget(item.FolderPath, true)
	}
	if option.FolderExists {
		option.Targets = append(option.Targets, MoveTargetOption{
			Kind: "folder", Path: item.FolderPath, Name: filepath.Base(item.FolderPath), Size: option.FolderSize,
		})
	}
	if option.ZipExists {
		option.Targets = append(option.Targets, MoveTargetOption{
			Kind: "zip", Path: item.ZipPath, Name: filepath.Base(item.ZipPath), Size: option.ZipSize,
		})
	}

	eligible := item.Verdict.IsDeleteCandidate() && item.Status != model.StatusDeleted
	if eligible {
		if !item.ZipMoved && !item.FolderMoved {
			if option.ZipExists && option.FolderExists {
				option.CanFolder = true
				option.CanZip = true
				option.CanBoth = true
				if item.Verdict == model.VerdictDeleteCandidateIgnorable {
					option.Warning = "ZIPにのみ補助ファイル（__MACOSX/.DS_Store など）が含まれます。解凍済みフォルダには含まれません。"
				}
			} else {
				option.Warning = "ZIPとフォルダの両方を確認できないため再スキャンしてください。"
			}
		} else {
			option.CanFolder = option.FolderExists
			option.CanZip = option.ZipExists
			if option.CanBoth {
				option.CanBoth = false
			}
		}
	}
	switch {
	case option.CanFolder:
		option.DefaultMethod = MoveMethodFolder
	case option.CanZip:
		option.DefaultMethod = MoveMethodZip
	}
	if option.CanBoth {
		bothWarning := "ZIPとフォルダの両方を移動すると、最後のローカルコピーを失う可能性があります。転送は行われません。"
		if item.Verdict == model.VerdictDeleteCandidateIgnorable {
			bothWarning = "ZIPにのみ補助ファイルが含まれます。" + bothWarning
		}
		option.Warning = bothWarning
		if item.Status == model.StatusError {
			option.Warning = "前回の移動に失敗しています。" + option.Warning
		}
	} else if item.Status == model.StatusError {
		if item.ZipMoved {
			option.Warning = "ZIPは移動済みです。残った解凍済みフォルダを再試行できます。"
		} else if item.FolderMoved {
			option.Warning = "フォルダは移動済みです。残ったZIPを再試行できます。"
		} else {
			option.Warning = "前回の移動に失敗しています。再試行できます。"
		}
	} else if item.ZipMoved && option.CanFolder {
		option.Warning = "ZIPは移動済みです。残った解凍済みフォルダを移動します。"
	} else if item.FolderMoved && option.CanZip {
		option.Warning = "フォルダは移動済みです。残ったZIPを移動します。"
	}
	if !option.CanFolder && !option.CanZip && !option.CanBoth && option.Warning == "" {
		option.Warning = "移動できる対象がありません。再スキャンしてください。"
	}
	return option
}

func statMoveTarget(path string, folder bool) (bool, int64) {
	if strings.TrimSpace(path) == "" {
		return false, 0
	}
	info, err := os.Lstat(path)
	if err != nil {
		return false, 0
	}
	reparse, err := winpath.IsReparsePoint(path)
	if err != nil || reparse {
		return false, 0
	}
	if folder {
		return info.IsDir(), 0
	}
	if !info.Mode().IsRegular() {
		return false, 0
	}
	return true, info.Size()
}

func moveTargetsForMethod(option MoveOption, method MoveMethod) ([]MoveTargetOption, bool) {
	switch method {
	case MoveMethodFolder:
		if !option.CanFolder {
			return nil, false
		}
		for _, target := range option.Targets {
			if target.Kind == "folder" {
				return []MoveTargetOption{target}, true
			}
		}
	case MoveMethodZip:
		if !option.CanZip {
			return nil, false
		}
		for _, target := range option.Targets {
			if target.Kind == "zip" {
				return []MoveTargetOption{target}, true
			}
		}
	case MoveMethodBoth:
		if !option.CanBoth {
			return nil, false
		}
		targets := make([]MoveTargetOption, 0, 2)
		for _, target := range option.Targets {
			if target.Kind == "folder" || target.Kind == "zip" {
				targets = append(targets, target)
			}
		}
		return targets, len(targets) == 2
	}
	return nil, false
}

func remainingSize(item model.Item) int64 {
	if !item.ZipMoved && !item.FolderMoved {
		return item.Size
	}
	if !item.ZipMoved && item.ZipPath != "" {
		return item.ZipSize
	}
	if !item.FolderMoved && item.FolderPath != "" {
		return item.FolderSize
	}
	return 0
}

func failureReason(targets []MoveTargetResult) string {
	var failures []string
	for _, target := range targets {
		if !target.Success {
			failures = append(failures, target.Kind+": "+target.Reason)
		}
	}
	return strings.Join(failures, " / ")
}

func (a *App) appendMoveLog(logger *oplog.Logger, item model.Item, method MoveMethod, success bool, message string, targets []MoveTargetResult) {
	if logger == nil {
		return
	}
	movedTargets := make([]string, 0, len(targets))
	failedTargets := make([]string, 0, len(targets))
	for _, target := range targets {
		label := target.Kind + ":" + target.Path
		if target.Success {
			movedTargets = append(movedTargets, label)
		} else {
			failedTargets = append(failedTargets, label)
		}
	}
	err := logger.Append(oplog.Entry{
		Time:          time.Now(),
		Action:        "recycle",
		Method:        string(method),
		ID:            item.ID,
		ZipPath:       item.ZipPath,
		FolderPath:    item.FolderPath,
		Verdict:       string(item.Verdict),
		Success:       success,
		Error:         message,
		MovedTargets:  movedTargets,
		FailedTargets: failedTargets,
	})
	if err != nil {
		a.emit("delete:log_error", err.Error())
	}
}
