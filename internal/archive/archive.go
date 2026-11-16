// Package archive はアーカイブ形式ごとの読み取りを抽象化する。
//
// ZIP は標準ライブラリで直接読み、7z / RAR などは 7-Zip CLI を呼ぶ実装を使う。
// どの実装も「エントリ一覧の取得」と「1エントリのストリーミング展開」だけを提供し、
// 一致判定・安全性の検証は internal/verify が共通に行う。
//
// 安全側の約束:
//   - パスは ZIP と同じ規則（winpath.NormalizeArchiveEntry）で正規化する。
//     絶対パス・".."・予約名・末尾の空白/ドット等は ErrInvalid として拒否する。
//   - 暗号化・分割書庫・リンク・特殊ファイルは ErrEncrypted / ErrUnsupported として扱い、
//     呼び出し側は UNVERIFIED にする（削除候補にしない）。
//   - 展開はディスクへ書き出さず、ストリームのまま読み手へ渡す。
package archive

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
)

var (
	// ErrEncrypted はパスワード保護されたアーカイブ。
	ErrEncrypted = errors.New("archive is encrypted")
	// ErrUnsupported は未対応の形式・圧縮方式・分割書庫。
	ErrUnsupported = errors.New("archive format is not supported")
	// ErrInvalid はパスや構造が不正。
	ErrInvalid = errors.New("archive structure is invalid")
	// ErrLimit は設定された上限を超えた。
	ErrLimit = errors.New("archive exceeds configured limits")
	// ErrToolMissing は外部ツールが見つからない。
	ErrToolMissing = errors.New("required archive tool is not installed")
)

// Format は対応するアーカイブ形式。
type Format string

const (
	FormatZIP Format = "zip"
	Format7z  Format = "7z"
	FormatRAR Format = "rar"
)

// SupportedExtensions は候補として拾う拡張子（小文字）。
var SupportedExtensions = map[string]Format{
	".zip": FormatZIP,
	".7z":  Format7z,
	".rar": FormatRAR,
}

// FormatOf は拡張子から形式を返す。未対応なら空文字。
func FormatOf(path string) Format {
	return SupportedExtensions[strings.ToLower(filepath.Ext(path))]
}

// IsSupported は対応形式かを返す。
func IsSupported(path string) bool {
	return FormatOf(path) != ""
}

// Entry はアーカイブ内の1エントリ。
type Entry struct {
	// Key は小文字の正規化パス。対応付けに使う。
	Key string
	// Path は表示用のパス（スラッシュ区切り）。
	Path string
	// Dir はディレクトリエントリか。
	Dir bool
	// Size は展開後のサイズ。
	Size int64
	// ModTime はエントリの更新日時。取得できない場合はゼロ値。
	ModTime time.Time
	// CRC32 はアーカイブが持つ CRC。キャッシュの妥当性確認に使う。
	CRC32 uint32
	// Original は元のアーカイブ内パス。strip-top variant で Path を
	// 書き換えた後も、元エントリを読み出すために保持する。
	Original string
}

// Variant はトップ階層を除去した比較パターン。
type Variant struct {
	// Mode は "direct" または "strip-top:<name>"。
	Mode string
	// Files はファイルエントリ（キーは小文字正規化パス）。
	Files map[string]Entry
	// Dirs はディレクトリ（キーは小文字正規化パス）。
	Dirs map[string]Entry
	// FileCount / DirCount / TotalBytes は集計値。
	FileCount  int
	DirCount   int
	TotalBytes int64
}

// Source は variant のエントリから元アーカイブのエントリを引くためのキーを返す。
// strip-top でパスを書き換えた場合でも、元のパスで読み出せるようにする。
func (e Entry) Source() string {
	if e.Original != "" {
		return e.Original
	}
	return e.Path
}

// OpenEntry は1エントリの内容を読み出す関数を返す。
// 呼び出し側は必ず Close する。ディスクへは書き出さない。
type OpenEntry func() (io.ReadCloser, error)

// Archive は読み取り中のアーカイブ。
type Archive interface {
	// Path はアーカイブのパス。
	Path() string
	// Size はファイルサイズ。
	Size() int64
	// ModTime はファイルの更新日時。
	ModTime() time.Time
	// EntryCount はセントラルディレクトリ等に記録されたエントリ数。
	EntryCount() int
	// Variants は比較パターン。先頭は必ず direct。
	Variants() []Variant
	// StructureHash はエントリ属性から作る指紋。キャッシュの妥当性確認に使う。
	StructureHash() string
	// Open は指定エントリの内容をストリームで返す。
	Open(entry Entry) (io.ReadCloser, error)
	// Close は読み取り資源を解放する。
	Close() error
}

// Open はパスと形式から適切な実装を選んで開く。
func Open(path string, limits model.Limits) (Archive, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s is not a regular file", ErrInvalid, path)
	}
	reparse, err := winpath.IsReparsePoint(path)
	if err != nil {
		return nil, err
	}
	if reparse {
		return nil, fmt.Errorf("%w: %s is a reparse point", ErrInvalid, path)
	}

	switch FormatOf(path) {
	case FormatZIP:
		return openZIP(path, info, limits)
	case Format7z, FormatRAR:
		return openSevenZip(path, info, limits)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, filepath.Ext(path))
	}
}

// buildVariants は direct と、単一トップ階層を除去した variant を作る。
func buildVariants(files, dirs map[string]Entry) []Variant {
	total := int64(0)
	for _, file := range files {
		total += file.Size
	}
	direct := Variant{
		Mode:       "direct",
		Files:      files,
		Dirs:       dirs,
		FileCount:  len(files),
		DirCount:   len(dirs),
		TotalBytes: total,
	}
	variants := []Variant{direct}

	top := topLevelName(files, dirs)
	if top == "" {
		return variants
	}
	if stripped, ok := stripTop(direct, top); ok {
		variants = append(variants, stripped)
	}
	return variants
}

// topLevelName は全エントリが単一のトップディレクトリ配下かを判定し、その名前を返す。
func topLevelName(files, dirs map[string]Entry) string {
	seen := map[string]struct{}{}
	for key, entry := range files {
		name, ok := firstSegment(entry.Path)
		if !ok {
			return ""
		}
		seen[strings.ToLower(name)] = struct{}{}
		_ = key
	}
	for _, entry := range dirs {
		name, ok := firstSegment(entry.Path)
		if !ok {
			return ""
		}
		seen[strings.ToLower(name)] = struct{}{}
	}
	if len(seen) != 1 {
		return ""
	}
	for name := range seen {
		// トップがディレクトリとして存在しないなら除去対象にしない。
		if _, ok := dirs[name]; !ok {
			return ""
		}
		return name
	}
	return ""
}

// firstSegment はパスの先頭要素を返す。区切りが無ければ false。
func firstSegment(path string) (string, bool) {
	index := strings.IndexByte(path, '/')
	if index < 0 {
		return "", false
	}
	return path[:index], true
}

// stripTop はトップ階層を取り除いた variant を作る。
func stripTop(direct Variant, top string) (Variant, bool) {
	stripped := Variant{
		Mode:       "strip-top:" + top,
		Files:      map[string]Entry{},
		Dirs:       map[string]Entry{},
		TotalBytes: direct.TotalBytes,
	}
	for key, entry := range direct.Files {
		path, ok := stripPrefix(entry.Path, top)
		if !ok {
			return Variant{}, false
		}
		original := entry.Path
		entry.Path = path
		entry.Key = strings.ToLower(path)
		entry.Original = original
		stripped.Files[entry.Key] = entry
		_ = key
	}
	for key, entry := range direct.Dirs {
		if strings.ToLower(entry.Path) == top {
			continue
		}
		path, ok := stripPrefix(entry.Path, top)
		if !ok {
			return Variant{}, false
		}
		original := entry.Path
		entry.Path = path
		entry.Key = strings.ToLower(path)
		entry.Original = original
		stripped.Dirs[entry.Key] = entry
		_ = key
	}
	stripped.FileCount = len(stripped.Files)
	stripped.DirCount = len(stripped.Dirs)
	return stripped, true
}

// stripPrefix は "top/rest" から "rest" を取り出す。
func stripPrefix(path, top string) (string, bool) {
	index := strings.IndexByte(path, '/')
	if index < 0 {
		return "", false
	}
	if !strings.EqualFold(path[:index], top) {
		return "", false
	}
	return path[index+1:], true
}

// hashStructure はエントリ属性から指紋を作る。
func hashStructure(entries []Entry) string {
	return structureHashOf(entries)
}
