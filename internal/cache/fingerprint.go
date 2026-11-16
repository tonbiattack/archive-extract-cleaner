package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/tonbiattack/archive-extract-cleaner/internal/fswalk"
)

// FolderFingerprintFromTree は走査済みツリーからフォルダ指紋を作る。
// 相対パス・サイズ・更新時刻を並べた文字列のハッシュなので、
// ファイルの追加・削除・リネーム・内容更新のいずれでも変化する。
func FolderFingerprintFromTree(path string, tree *fswalk.Tree) *FolderFingerprint {
	if tree == nil {
		return nil
	}
	hash := sha256.New()
	// ファイルは Key（小文字相対パス）で並べる。走査結果は map なので順序を固定する。
	keys := make([]string, 0, len(tree.Files))
	for key := range tree.Files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		file := tree.Files[key]
		fmt.Fprintf(hash, "f\x00%s\x00%d\x00%d\n", file.Key, file.Size, file.ModTime.UnixNano())
	}
	dirKeys := make([]string, 0, len(tree.Dirs))
	for key := range tree.Dirs {
		dirKeys = append(dirKeys, key)
	}
	sort.Strings(dirKeys)
	for _, key := range dirKeys {
		fmt.Fprintf(hash, "d\x00%s\n", key)
	}
	return &FolderFingerprint{
		Path:       path,
		Hash:       hex.EncodeToString(hash.Sum(nil)),
		FileCount:  tree.FileCount,
		DirCount:   tree.DirCount,
		TotalBytes: tree.Size,
	}
}

// Same は2つのフォルダ指紋が同一かを返す。
func (f *FolderFingerprint) Same(other *FolderFingerprint) bool {
	if f == nil || other == nil {
		return f == nil && other == nil
	}
	return f.Hash == other.Hash &&
		f.FileCount == other.FileCount &&
		f.DirCount == other.DirCount &&
		f.TotalBytes == other.TotalBytes
}

// IsZero は指紋が空かを返す。
func (f *FolderFingerprint) IsZero() bool {
	return f == nil || f.Hash == ""
}

// ArchiveEpoch は ZIP ハッシュキャッシュの互換性を表す識別子。
// ハッシュ計算方法やスキーマを変えたら変える。
func ArchiveEpoch() int {
	return SchemaVersion
}

// NewRootRecord は現在のルート結果を保存用に組み立てる。
func NewRootRecord(root string, recursive bool, items []ItemRecord, scannedAt time.Time) RootRecord {
	return RootRecord{
		Root:         root,
		Recursive:    recursive,
		Items:        items,
		ScannedAt:    scannedAt,
		ArchiveEpoch: ArchiveEpoch(),
	}
}
