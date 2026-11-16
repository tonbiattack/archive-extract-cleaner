package cache

import (
	"path/filepath"
	"sync"
	"time"
)

const (
	archiveKind = "archive-hashes"
	// DefaultMaxArchives は保持するZIP指紋の数。
	DefaultMaxArchives = 20000
)

// ArchiveKey は ZIP の同一性を表す。サイズ・更新時刻・構造指紋を組み合わせる。
// メタデータだけでは足りないため、セントラルディレクトリの指紋も必須にする。
type ArchiveKey struct {
	Path          string `json:"path"`
	Size          int64  `json:"size"`
	ModTimeUnixNS int64  `json:"modTimeUnixNs"`
	StructureHash string `json:"structureHash"`
	// SchemaVersion はアーカイブ処理側の互換性。ハッシュ計算方法を変えたら上げる。
	SchemaVersion int `json:"schemaVersion"`
}

// NewArchiveKey は ZIP のメタデータと構造指紋からキーを作る。
func NewArchiveKey(path string, size int64, modTime time.Time, structureHash string) ArchiveKey {
	return ArchiveKey{
		Path:          path,
		Size:          size,
		ModTimeUnixNS: modTime.UnixNano(),
		StructureHash: structureHash,
		SchemaVersion: SchemaVersion,
	}
}

// ArchiveEntry は1エントリ分の展開後ハッシュ。
type ArchiveEntry struct {
	// Path は ZIP 内の表示用パス（スラッシュ区切り）。
	Path string `json:"path"`
	// SHA256 は展開後のバイト列のハッシュ（16進）。
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	// CRC32 は ZIP セントラルディレクトリの値。ハッシュとの対応確認に使う。
	CRC32 uint32 `json:"crc32"`
}

// ArchiveRecord は1つの ZIP に対するハッシュ群。
type ArchiveRecord struct {
	Key     ArchiveKey     `json:"key"`
	Entries []ArchiveEntry `json:"entries"`
	// TotalBytes は展開後の合計サイズ。キャッシュ全体の上限判定に使う。
	TotalBytes int64     `json:"totalBytes"`
	AccessedAt time.Time `json:"accessedAt"`
	CreatedAt  time.Time `json:"createdAt"`
}

// ArchiveHashes は ZIP エントリのハッシュキャッシュ。
type ArchiveHashes struct {
	store   *Store
	mu      sync.Mutex
	records map[string]ArchiveRecord
	loaded  bool
	max     int
}

// NewArchiveHashes はキャッシュを読み込んで返す。
// 読み込みに失敗しても空のキャッシュとして動作する（スキャンを止めない）。
func NewArchiveHashes(path string) *ArchiveHashes {
	cache := &ArchiveHashes{
		store:   NewStore(path),
		records: map[string]ArchiveRecord{},
		max:     DefaultMaxArchives,
	}
	var payload map[string]ArchiveRecord
	if err := cache.store.Load(archiveKind, &payload); err == nil && payload != nil {
		cache.records = payload
	}
	cache.loaded = true
	return cache
}

// SetLimit は保持するレコード数の上限を変更する。
func (c *ArchiveHashes) SetLimit(limit int) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if limit > 0 {
		c.max = limit
	}
}

// Lookup はキーに一致するレコードを返す。
// パス・サイズ・更新時刻・構造指紋・スキーマのすべてが一致する場合だけ返す。
func (c *ArchiveHashes) Lookup(key ArchiveKey) (ArchiveRecord, bool) {
	if c == nil {
		return ArchiveRecord{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	record, ok := c.records[cacheKey(key)]
	if !ok {
		return ArchiveRecord{}, false
	}
	if !sameKey(record.Key, key) {
		return ArchiveRecord{}, false
	}
	// 参照時刻を更新する。保存は呼び出し側の Save に任せる。
	record.AccessedAt = time.Now().UTC()
	c.records[cacheKey(key)] = record
	return record, true
}

// Put はレコードを保存する。既存より新しい情報で上書きする。
func (c *ArchiveHashes) Put(record ArchiveRecord) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UTC()
	if record.AccessedAt.IsZero() {
		record.AccessedAt = now
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	c.records[cacheKey(record.Key)] = record
	c.capLocked()
}

// capLocked は上限を超えた古いレコードを削除する。c.mu を保持して呼ぶ。
func (c *ArchiveHashes) capLocked() {
	if c.max <= 0 || len(c.records) <= c.max {
		return
	}
	CapMap(c.records, c.max, func(r ArchiveRecord) time.Time { return r.AccessedAt }, func(key string) {
		delete(c.records, key)
	})
}

// Flush はキャッシュをディスクへ書き出す。
func (c *ArchiveHashes) Flush() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	snapshot := make(map[string]ArchiveRecord, len(c.records))
	for key, record := range c.records {
		snapshot[key] = record
	}
	c.mu.Unlock()
	return c.store.Save(archiveKind, snapshot)
}

// Len は保持しているレコード数を返す。
func (c *ArchiveHashes) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.records)
}

// Path はキャッシュファイルの場所を返す。
func (c *ArchiveHashes) Path() string {
	if c == nil {
		return ""
	}
	return c.store.Path()
}

// Clear はすべての ZIP ハッシュを破棄する。
func (c *ArchiveHashes) Clear() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	c.records = map[string]ArchiveRecord{}
	c.mu.Unlock()
	return c.store.Remove()
}

// Invalidate は指定パスのレコードを削除する。
func (c *ArchiveHashes) Invalidate(path string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, record := range c.records {
		if record.Key.Path == path {
			delete(c.records, key)
		}
	}
}

// cacheKey は 1 つの ZIP につき 1 レコードへ正規化するためのキー。
// パスをキーにすると、更新されると古いレコードが残り続けるため、
// パス＋新しいキーで引き、上書きする。
func cacheKey(key ArchiveKey) string {
	return filepath.Clean(key.Path)
}

func sameKey(a, b ArchiveKey) bool {
	return a.Path == b.Path &&
		a.Size == b.Size &&
		a.ModTimeUnixNS == b.ModTimeUnixNS &&
		a.StructureHash == b.StructureHash &&
		a.SchemaVersion == b.SchemaVersion
}
