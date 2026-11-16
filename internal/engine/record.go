package engine

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/tonbiattack/archive-extract-cleaner/internal/cache"
	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
)

// itemFromRecord は前回の項目記録から model.Item を復元する。
// キャッシュ由来であることを Reason に明示し、削除可否の根拠にしない。
func itemFromRecord(record cache.ItemRecord) model.Item {
	item := model.Item{
		ID:            record.ID,
		Verdict:       model.Verdict(record.Verdict),
		Status:        model.StatusReady,
		ZipPath:       record.ZipPath,
		FolderPath:    record.FolderPath,
		ParentPath:    parentOf(record),
		Name:          nameOf(record),
		MatchMode:     model.MatchMode(record.MatchMode),
		NameVariant:   record.NameVariant,
		FolderSize:    record.FolderSize,
		Size:          record.FolderSize,
		FileCount:     record.FileCount,
		DirCount:      record.DirCount,
		ZipEntries:    record.ZipEntries,
		ComparedBytes: record.ComparedBytes,
		Reason:        record.Reason,
		Details:       record.Details,
		CheckedAt:     time.Now(),
	}
	return item
}

// recordFromItem は次回の差分スキャン用に項目記録を作る。
func recordFromItem(item model.Item, zipKey *cache.ArchiveKey, print *cache.FolderFingerprint) cache.ItemRecord {
	record := cache.ItemRecord{
		ID:            item.ID,
		Verdict:       string(item.Verdict),
		Reason:        item.Reason,
		ZipPath:       item.ZipPath,
		FolderPath:    item.FolderPath,
		ZipKey:        zipKey,
		Folder:        print,
		MatchMode:     string(item.MatchMode),
		NameVariant:   item.NameVariant,
		FolderSize:    item.FolderSize,
		FileCount:     item.FileCount,
		DirCount:      item.DirCount,
		ZipEntries:    item.ZipEntries,
		ComparedBytes: item.ComparedBytes,
		Details:       item.Details,
		CheckedAt:     item.CheckedAt,
		Cacheable:     cacheableVerdict(item),
	}
	return record
}

// cacheableVerdict は結果を再利用してよい判定かを返す。
// 未検証・エラー・移動途中の項目は、状態が変わりやすいので再利用しない。
func cacheableVerdict(item model.Item) bool {
	if item.Status != model.StatusReady {
		return false
	}
	if item.ZipMoved || item.FolderMoved {
		return false
	}
	if item.Verdict == model.VerdictUnverified {
		return false
	}
	return true
}

func parentOf(record cache.ItemRecord) string {
	if record.FolderPath != "" {
		return filepath.Dir(record.FolderPath)
	}
	if record.ZipPath != "" {
		return filepath.Dir(record.ZipPath)
	}
	return ""
}

func nameOf(record cache.ItemRecord) string {
	if record.ZipPath != "" {
		base := filepath.Base(record.ZipPath)
		return strings.TrimSuffix(base, filepath.Ext(base))
	}
	if record.FolderPath != "" {
		return filepath.Base(record.FolderPath)
	}
	return record.ID
}
