// Package cache はスキャン結果の永続キャッシュを扱う。
//
// 2種類のキャッシュを持つ:
//
//   - ArchiveHasher: ZIP エントリの展開後SHA-256を保持する。ZIPのメタデータと
//     構造指紋が一致する場合だけ再利用する。
//   - ScanStore: 前回の項目判定（フォルダ走査結果を含む）を保持し、差分スキャンに使う。
//
// どちらも「高速化のヒント」であり、削除の可否を決める根拠にはしない。
// ごみ箱移動直前の再照合（verify.Verify）は従来どおり必ず実行する。
package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// SchemaVersion はキャッシュ形式のバージョン。
// 構造を変えたら必ず上げる。上げると古いキャッシュは破棄される。
const SchemaVersion = 1

var (
	// ErrMiss はキャッシュに該当がないことを示す。
	ErrMiss = errors.New("cache miss")
	// ErrCorrupt はキャッシュが壊れていることを示す。
	ErrCorrupt = errors.New("cache corrupt")
)

// envelope はファイルへ保存する共通の外枠。
type envelope struct {
	Version   int             `json:"version"`
	Kind      string          `json:"kind"`
	UpdatedAt time.Time       `json:"updatedAt"`
	Payload   json.RawMessage `json:"payload"`
}

// Store はJSONファイル1つを読み書きするキャッシュ。
// 破損・スキーマ不一致は空として扱い、呼び出し側は再計算できる。
type Store struct {
	path string
	mu   sync.Mutex
}

// NewStore はパスを指定して Store を作る。path が空ならメモリのみで動作する。
func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// DefaultDir はキャッシュを置くディレクトリを返す。
// WebView2 のユーザーデータと同じ場所に置き、アンインストール時に一緒に消えるようにする。
func DefaultDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "archive-extract-cleaner", "cache")
}

// DefaultHashesPath は ZIP ハッシュキャッシュの既定パスを返す。
func DefaultHashesPath() string {
	return filepath.Join(DefaultDir(), "archives.json")
}

// DefaultScanPath は差分スキャン用キャッシュの既定パスを返す。
func DefaultScanPath() string {
	return filepath.Join(DefaultDir(), "scan.json")
}

// Load は payload へ読み込む。該当なし・破損・スキーマ不一致は ErrMiss を返す。
func (s *Store) Load(kind string, payload any) error {
	if s == nil || s.path == "" {
		return ErrMiss
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrMiss
		}
		return err
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if env.Version != SchemaVersion || env.Kind != kind {
		return ErrMiss
	}
	if err := json.Unmarshal(env.Payload, payload); err != nil {
		return fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	return nil
}

// Save は payload を原子的に保存する。
// 一時ファイルへ書いてから置き換えるため、中断しても壊れたファイルが残らない。
func (s *Store) Save(kind string, payload any) error {
	if s == nil || s.path == "" {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	env := envelope{
		Version:   SchemaVersion,
		Kind:      kind,
		UpdatedAt: time.Now().UTC(),
		Payload:   raw,
	}
	data, err := json.Marshal(&env)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".cache-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() {
		if temp != nil {
			temp.Close()
			os.Remove(tempPath)
		}
	}()
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	temp = nil
	if err := os.Rename(tempPath, s.path); err != nil {
		os.Remove(tempPath)
		return err
	}
	return nil
}

// Remove はキャッシュファイルを削除する。
func (s *Store) Remove() error {
	if s == nil || s.path == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(s.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// CapMap はマップが上限を超えたら古い順に削除する。
// map の値は accessTime を持つ必要がある。
func CapMap[T any](entries map[string]T, limit int, accessTime func(T) time.Time, remove func(string)) {
	if limit <= 0 || len(entries) <= limit {
		return
	}
	type pair struct {
		key  string
		when time.Time
	}
	pairs := make([]pair, 0, len(entries))
	for key, value := range entries {
		pairs = append(pairs, pair{key: key, when: accessTime(value)})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].when.Before(pairs[j].when) })
	for i := 0; i < len(pairs)-limit; i++ {
		remove(pairs[i].key)
	}
}
