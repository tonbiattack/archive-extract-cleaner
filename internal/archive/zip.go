package archive

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/ziparchive"
)

// zipArchive は ZIP を標準ライブラリで読む実装。
// 既存の ziparchive の検証済みロジックをそのまま使う。
type zipArchive struct {
	inner    *ziparchive.Archive
	entries  []Entry
	variants []Variant
	byPath   map[string]zipEntryRef
}

// zipEntryRef は Entry から元の zip.File を引くための対応表。
type zipEntryRef struct {
	file ziparchive.File
}

func openZIP(path string, info os.FileInfo, limits model.Limits) (Archive, error) {
	inner, err := ziparchive.Open(path, limits)
	if err != nil {
		return nil, translateZIPError(err)
	}
	a := &zipArchive{
		inner:  inner,
		byPath: map[string]zipEntryRef{},
	}

	// direct variant のエントリを共通形式へ変換する。
	direct := inner.Variants[0]
	entries := make([]Entry, 0, direct.FileCount+direct.DirCount)
	for key, file := range direct.Files {
		entry := Entry{
			Key:      key,
			Path:     file.Path,
			Size:     int64(file.File.UncompressedSize64),
			ModTime:  file.File.Modified,
			CRC32:    file.File.CRC32,
			Original: file.Path,
		}
		entries = append(entries, entry)
		a.byPath[key] = zipEntryRef{file: file}
	}
	for key, display := range direct.DirPaths {
		entries = append(entries, Entry{
			Key:     key,
			Path:    display,
			Dir:     true,
			ModTime: time.Time{},
		})
	}

	a.entries = entries
	a.variants = convertVariants(inner.Variants)
	return a, nil
}

// convertVariants は ziparchive の variant を共通形式へ変換する。
// ziparchive は strip-top 時にパスを書き換えるため、元パスは direct variant
// のエントリから引き継ぐ。
func convertVariants(source []ziparchive.Variant) []Variant {
	original := map[string]string{}
	if len(source) > 0 {
		for _, file := range source[0].Files {
			original[strings.ToLower(file.Path)] = file.Path
		}
	}
	result := make([]Variant, 0, len(source))
	for _, variant := range source {
		files := make(map[string]Entry, len(variant.Files))
		for key, file := range variant.Files {
			sourcePath := file.Path
			// strip-top では "top/a.txt" が "a.txt" になる。
			// 元のパスは direct variant の末尾一致で探す。
			if len(source) > 0 && variant.Mode != "direct" {
				if found := findOriginalPath(original, file.Path); found != "" {
					sourcePath = found
				}
			}
			files[key] = Entry{
				Key:      key,
				Path:     file.Path,
				Size:     int64(file.File.UncompressedSize64),
				ModTime:  file.File.Modified,
				CRC32:    file.File.CRC32,
				Original: sourcePath,
			}
		}
		dirs := make(map[string]Entry, len(variant.DirPaths))
		for key, display := range variant.DirPaths {
			dirs[key] = Entry{Key: key, Path: display, Dir: true, Original: display}
		}
		result = append(result, Variant{
			Mode:       variant.Mode,
			Files:      files,
			Dirs:       dirs,
			FileCount:  variant.FileCount,
			DirCount:   variant.DirCount,
			TotalBytes: int64(variant.TotalBytes),
		})
	}
	return result
}

// findOriginalPath は末尾一致で元のアーカイブ内パスを探す。
func findOriginalPath(original map[string]string, stripped string) string {
	target := strings.ToLower(stripped)
	for _, path := range original {
		lower := strings.ToLower(path)
		if lower == target || strings.HasSuffix(lower, "/"+target) {
			return path
		}
	}
	return ""
}

func (a *zipArchive) Path() string        { return a.inner.Path }
func (a *zipArchive) Size() int64         { return a.inner.Size }
func (a *zipArchive) ModTime() time.Time  { return a.inner.ModTime }
func (a *zipArchive) EntryCount() int     { return a.inner.EntryCount }
func (a *zipArchive) Variants() []Variant { return a.variants }
func (a *zipArchive) StructureHash() string {
	return a.inner.StructureHash()
}

func (a *zipArchive) Open(entry Entry) (io.ReadCloser, error) {
	// 元のアーカイブ内パスで引く。strip-top variant でも正しく解決できる。
	ref, ok := a.byPath[strings.ToLower(entry.Source())]
	if !ok {
		return nil, ErrInvalid
	}
	return ref.file.File.Open()
}

func (a *zipArchive) Close() error { return a.inner.Close() }

// translateZIPError は ziparchive のエラーを共通の番兵へ寄せる。
func translateZIPError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case isZIPEncrypted(err):
		return ErrEncrypted
	case isZIPLimit(err):
		return ErrLimit
	default:
		return err
	}
}
