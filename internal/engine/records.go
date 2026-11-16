package engine

import (
	"context"
	"os"

	"github.com/tonbiattack/archive-extract-cleaner/internal/archive"
	"github.com/tonbiattack/archive-extract-cleaner/internal/cache"
	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
)

// buildRecords は次回の差分スキャン用に項目記録を作る。
//
// 記録するのは判定だけでなく、判定の前提になった ZIP の同一性キーと
// フォルダ指紋も含む。次回はこれらが一致する場合だけ再利用する。
//
// 再利用できなかった項目（UNVERIFIED・移動途中など）は Cacheable=false で
// 保存する。次回も必ず再検証される。
func buildRecords(ctx context.Context, items []model.Item, limits model.Limits, walker *folderWalker, previous *cache.RootRecord, stats *cacheStats) []cache.ItemRecord {
	records := make([]cache.ItemRecord, 0, len(items))
	var previousZips map[string]cache.ArchiveKey
	if previous != nil {
		previousZips = previous.ZipIndex()
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return records
		}
		var zipKey *cache.ArchiveKey
		if item.ZipPath != "" {
			if key, ok := archiveKeyFor(item.ZipPath, limits, previousZips); ok {
				zipKey = &key
			}
		}
		var print *cache.FolderFingerprint
		if item.FolderPath != "" {
			tree, err := walker.walk(ctx, item.FolderPath, limits)
			if err == nil && tree != nil {
				print = cache.FolderFingerprintFromTree(item.FolderPath, tree)
			}
		}
		records = append(records, recordFromItem(item, zipKey, print))
	}
	_ = stats
	return records
}

// archiveKeyFor は ZIP の同一性キーを作る。
// 構造指紋はセントラルディレクトリを読んで計算する（圧縮データは読まない）。
// 読めない場合はキーなしを返し、次回はキャッシュを使わない。
func archiveKeyFor(path string, limits model.Limits, previous map[string]cache.ArchiveKey) (cache.ArchiveKey, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return cache.ArchiveKey{}, false
	}
	return archiveIdentity(path, limits, info)
}

// archiveIdentity は既に取得済みのファイル情報から ZIP の同一性キーを作る。
func archiveIdentity(path string, limits model.Limits, info os.FileInfo) (cache.ArchiveKey, bool) {
	opened, err := archive.Open(path, limits)
	if err != nil {
		return cache.ArchiveKey{}, false
	}
	structure := opened.StructureHash()
	_ = opened.Close()
	if structure == "" {
		return cache.ArchiveKey{}, false
	}
	return cache.NewArchiveKey(path, info.Size(), info.ModTime(), structure), true
}
