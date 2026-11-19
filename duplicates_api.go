package main

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/tonbiattack/archive-extract-cleaner/internal/duplicates"
	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
)

// E/F/G の同名アーカイブ横断チェック。
//
// この機能は「同じ名前のアーカイブが保存先に既にあるか」を表示するだけで、
// 内容の一致は判定しない。同名・同サイズでも同一内容とは限らないため、
// 削除の可否や移動の判断には使わない。既存の ZIP/フォルダ照合とは独立。

// DuplicateState は検索の設定と進行状況。UI へ返す。
type DuplicateState struct {
	// Enabled は設定で有効になっているか。
	Enabled bool `json:"enabled"`
	// Drives は検索対象のドライブレター。
	Drives []string `json:"drives"`
	// RelativePath は各ドライブ内の保存先フォルダ。空ならドライブ直下。
	RelativePath string `json:"relativePath"`
	// Recursive は配下サブフォルダまで検索するか。
	Recursive bool `json:"recursive"`
	// Searching は検索の実行中か。
	Searching bool `json:"searching"`
	// Searched は検索済みの項目数。
	Searched int `json:"searched"`
	// Total は検索対象の項目数。
	Total int `json:"total"`
	// Drives の状態は DriveStates で返す。
	DriveStates []duplicates.DriveState `json:"driveStates"`
	// Error は直近の検索で発生したエラー。
	Error string `json:"error,omitempty"`
}

// DuplicateProgress は検索の進捗通知。
type DuplicateProgress struct {
	Phase    string `json:"phase"`
	Searched int    `json:"searched"`
	Total    int    `json:"total"`
	Current  string `json:"current"`
	Elapsed  int64  `json:"elapsedMs"`
}

// GetDuplicateState は現在の設定と進行状況を返す。
func (a *App) GetDuplicateState() DuplicateState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.duplicateStateLocked()
}

// duplicateStateLocked は a.mu を保持したまま状態を組み立てる。
func (a *App) duplicateStateLocked() DuplicateState {
	state := DuplicateState{
		Enabled:      a.duplicateSearch.Enabled,
		Drives:       append([]string(nil), a.duplicateSearch.Drives...),
		RelativePath: a.duplicateSearch.RelativePath,
		Recursive:    a.duplicateSearch.Recursive,
		Searching:    a.duplicateSearching,
		Searched:     len(a.duplicateResults),
		Total:        len(a.items),
		DriveStates:  append([]duplicates.DriveState(nil), a.duplicateDrives...),
	}
	return state
}

// SearchDuplicates は全項目について E/F/G の同名アーカイブを検索する。
//
// 索引は保存先フォルダ単位で1回だけ作り、項目ごとに全件走査しない。
// 検索中も UI を止めないよう、進捗を emit する。
func (a *App) SearchDuplicates() (DuplicateState, error) {
	a.mu.Lock()
	if a.scanning || a.deleting || a.duplicateSearching {
		a.mu.Unlock()
		return DuplicateState{}, errors.New("別の処理を実行中です")
	}
	if !a.duplicateSearch.Enabled {
		a.mu.Unlock()
		return DuplicateState{}, errors.New("同名アーカイブの検索が設定で無効です")
	}
	base := a.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	a.cancelDuplicate = cancel
	a.duplicateSearching = true
	opts := a.duplicateSearch
	items := append([]model.Item(nil), a.items...)
	a.mu.Unlock()

	defer func() {
		cancel()
		a.mu.Lock()
		a.duplicateSearching = false
		a.cancelDuplicate = nil
		a.mu.Unlock()
	}()

	if len(items) == 0 {
		return a.GetDuplicateState(), errors.New("スキャン結果がありません")
	}

	searcher := duplicates.NewSearcher(duplicates.Options{
		Drives:       opts.Drives,
		RelativePath: opts.RelativePath,
		Roots:        a.duplicateRoots,
		Recursive:    opts.Recursive,
		MaxDepth:     opts.MaxDepth,
		MaxEntries:   opts.MaxEntries,
	})

	started := time.Now()
	results := make(map[string]duplicates.Result, len(items))
	lastEmit := time.Time{}
	for index, item := range items {
		if err := ctx.Err(); err != nil {
			return a.GetDuplicateState(), err
		}
		// アーカイブを持たない項目（フォルダのみ）は検索対象外。
		if item.ZipPath == "" {
			continue
		}
		size := item.ZipSize
		if size == 0 {
			if info, err := os.Stat(item.ZipPath); err == nil {
				size = info.Size()
			}
		}
		results[item.ID] = searcher.Search(ctx, duplicates.Target{Path: item.ZipPath, Size: size})

		if time.Since(lastEmit) >= 100*time.Millisecond {
			lastEmit = time.Now()
			a.emit("duplicate:progress", DuplicateProgress{
				Phase:    "searching",
				Searched: index + 1,
				Total:    len(items),
				Current:  item.DisplayName(),
				Elapsed:  time.Since(started).Milliseconds(),
			})
		}
	}

	a.mu.Lock()
	a.duplicateResults = results
	a.duplicateDrives = searcher.DriveStates()
	state := a.duplicateStateLocked()
	a.mu.Unlock()

	a.emit("duplicate:done", state)
	return state, nil
}

// CancelDuplicateSearch は検索を中断する。
func (a *App) CancelDuplicateSearch() {
	a.mu.Lock()
	cancel := a.cancelDuplicate
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// ClearDuplicateResults は検索結果を破棄する。
func (a *App) ClearDuplicateResults() DuplicateState {
	a.mu.Lock()
	a.duplicateResults = map[string]duplicates.Result{}
	a.duplicateDrives = nil
	state := a.duplicateStateLocked()
	a.mu.Unlock()
	return state
}

// duplicateFor は項目IDの検索結果を返す。未検索なら nil。
func (a *App) duplicateFor(id string) *duplicates.Result {
	a.mu.RLock()
	defer a.mu.RUnlock()
	result, ok := a.duplicateResults[id]
	if !ok {
		return nil
	}
	copied := result
	return &copied
}

// duplicateResultsSnapshot は検索結果の写しを返す。一覧の組み立てで使う。
func (a *App) duplicateResultsSnapshot() map[string]duplicates.Result {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if len(a.duplicateResults) == 0 {
		return nil
	}
	out := make(map[string]duplicates.Result, len(a.duplicateResults))
	for id, result := range a.duplicateResults {
		out[id] = result
	}
	return out
}

// 未使用の警告を避けるための参照。
var _ = sync.Mutex{}
