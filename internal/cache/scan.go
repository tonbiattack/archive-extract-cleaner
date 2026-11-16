package cache

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
)

const (
	scanKind = "scan-items"
	// DefaultMaxRoots は保持する対象ルートの数。
	DefaultMaxRoots = 8
)

// FolderFingerprint はフォルダ側の変更検知に使う指紋。
// 走査で得たファイル一覧のサイズ・更新時刻・パス集合から作る。
type FolderFingerprint struct {
	// Path はフォルダの絶対パス。
	Path string `json:"path"`
	// Hash はファイル一覧（相対パス・サイズ・更新時刻）のハッシュ。
	Hash string `json:"hash"`
	// FileCount / DirCount は件数の照合用。
	FileCount int `json:"fileCount"`
	DirCount  int `json:"dirCount"`
	// TotalBytes は合計サイズ。
	TotalBytes int64 `json:"totalBytes"`
}

// ItemRecord は前回の1項目分の判定。
type ItemRecord struct {
	ID         string `json:"id"`
	Verdict    string `json:"verdict"`
	Reason     string `json:"reason"`
	ZipPath    string `json:"zipPath"`
	FolderPath string `json:"folderPath"`
	// ZipKey は ZIP 側の同一性。空なら ZIP なし。
	ZipKey *ArchiveKey `json:"zipKey,omitempty"`
	// Folder はフォルダ側の指紋。空ならフォルダなし。
	Folder *FolderFingerprint `json:"folder,omitempty"`
	// 判定に使った値。キャッシュヒット時にそのまま復元する。
	MatchMode     string   `json:"matchMode,omitempty"`
	NameVariant   bool     `json:"nameVariant,omitempty"`
	FolderSize    int64    `json:"folderSize"`
	FileCount     int      `json:"fileCount"`
	DirCount      int      `json:"dirCount"`
	ZipEntries    int      `json:"zipEntries"`
	ComparedBytes int64    `json:"comparedBytes"`
	Details       []string `json:"details,omitempty"`
	// Cacheable が false の項目は再利用しない（UNVERIFIED・エラー・部分移動など）。
	Cacheable bool      `json:"cacheable"`
	CheckedAt time.Time `json:"checkedAt"`
}

// RootRecord は対象ルート1つ分の前回結果。
type RootRecord struct {
	Root       string       `json:"root"`
	Recursive  bool         `json:"recursive"`
	Items      []ItemRecord `json:"items"`
	ScannedAt  time.Time    `json:"scannedAt"`
	AccessedAt time.Time    `json:"accessedAt"`
	// ArchiveEpoch は ZIP ハッシュキャッシュのスキーマ。変わったら再利用しない。
	ArchiveEpoch int `json:"archiveEpoch"`
}

// ScanStore は前回のスキャン結果を保持する。
type ScanStore struct {
	store   *Store
	mu      sync.Mutex
	records map[string]RootRecord
	max     int
}

// NewScanStore はキャッシュを読み込んで返す。
func NewScanStore(path string) *ScanStore {
	cache := &ScanStore{
		store:   NewStore(path),
		records: map[string]RootRecord{},
		max:     DefaultMaxRoots,
	}
	var payload map[string]RootRecord
	if err := cache.store.Load(scanKind, &payload); err == nil && payload != nil {
		cache.records = payload
	}
	return cache
}

// Path はキャッシュファイルの場所を返す。
func (s *ScanStore) Path() string {
	if s == nil {
		return ""
	}
	return s.store.Path()
}

// SetLimit は保持するルート数を変更する。
func (s *ScanStore) SetLimit(limit int) {
	if s == nil || limit <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.max = limit
}

// Lookup はルートの前回結果を返す。
func (s *ScanStore) Lookup(root string, recursive bool, archiveEpoch int) (RootRecord, bool) {
	if s == nil {
		return RootRecord{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[rootKey(root)]
	if !ok {
		return RootRecord{}, false
	}
	if !samePath(record.Root, root) || record.Recursive != recursive {
		return RootRecord{}, false
	}
	if record.ArchiveEpoch != archiveEpoch {
		return RootRecord{}, false
	}
	record.AccessedAt = time.Now().UTC()
	s.records[rootKey(root)] = record
	return record, true
}

// Put はルートの結果を保存する。
func (s *ScanStore) Put(record RootRecord) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	record.AccessedAt = now
	s.records[rootKey(record.Root)] = record
	if s.max > 0 && len(s.records) > s.max {
		CapMap(s.records, s.max, func(r RootRecord) time.Time { return r.AccessedAt }, func(key string) {
			delete(s.records, key)
		})
	}
}

// Flush はキャッシュをディスクへ書き出す。
func (s *ScanStore) Flush() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	snapshot := make(map[string]RootRecord, len(s.records))
	for key, record := range s.records {
		snapshot[key] = record
	}
	s.mu.Unlock()
	return s.store.Save(scanKind, snapshot)
}

// Clear はすべての記録を破棄する。
func (s *ScanStore) Clear() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.records = map[string]RootRecord{}
	s.mu.Unlock()
	return s.store.Remove()
}

// Len は保持しているルート数を返す。
func (s *ScanStore) Len() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.records)
}

// FindItem は指定ルートの前回項目を ID で引く。
func (r RootRecord) FindItem(id string) (ItemRecord, bool) {
	for _, item := range r.Items {
		if item.ID == id {
			return item, true
		}
	}
	return ItemRecord{}, false
}

// FolderIndex は前回のフォルダ指紋をパスで引けるようにする。
func (r RootRecord) FolderIndex() map[string]FolderFingerprint {
	index := make(map[string]FolderFingerprint, len(r.Items))
	for _, item := range r.Items {
		if item.Folder == nil || item.Folder.Path == "" {
			continue
		}
		index[winpath.Key(item.Folder.Path)] = *item.Folder
	}
	return index
}

// ZipIndex は前回の ZIP キーをパスで引けるようにする。
func (r RootRecord) ZipIndex() map[string]ArchiveKey {
	index := make(map[string]ArchiveKey, len(r.Items))
	for _, item := range r.Items {
		if item.ZipKey == nil || item.ZipKey.Path == "" {
			continue
		}
		index[winpath.Key(item.ZipKey.Path)] = *item.ZipKey
	}
	return index
}

func rootKey(root string) string {
	return normalizePath(root)
}

func normalizePath(path string) string {
	return filepath.Clean(path)
}

func samePath(a, b string) bool {
	return normalizePath(a) == normalizePath(b)
}
