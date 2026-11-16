package main

import (
	"errors"

	"github.com/tonbiattack/archive-extract-cleaner/internal/engine"
)

// キャッシュの操作。
//
// キャッシュはスキャン高速化のためだけに存在し、削除の可否判定には使わない。
// ごみ箱移動直前の再照合はキャッシュの有無にかかわらず必ず実行される。

// ClearCache は保存済みのキャッシュを破棄する。
// 次回スキャンは完全スキャンとして動作する。
func (a *App) ClearCache() error {
	a.mu.Lock()
	hashes := a.archiveHashes
	scans := a.scanStore
	a.lastCacheStats = engine.CacheStats{}
	a.mu.Unlock()
	if hashes == nil && scans == nil {
		return errors.New("キャッシュが初期化されていません")
	}
	var firstErr error
	if hashes != nil {
		if err := hashes.Clear(); err != nil {
			firstErr = err
		}
	}
	if scans != nil {
		if err := scans.Clear(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// GetCacheState は現在のキャッシュ状態を返す。
func (a *App) GetCacheState() CacheState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cacheStateLocked()
}
