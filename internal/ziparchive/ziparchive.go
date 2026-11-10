package ziparchive

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
)

var (
	ErrEncrypted   = errors.New("ZIP is encrypted")
	ErrUnsupported = errors.New("ZIP entry uses an unsupported compression method")
	ErrInvalid     = errors.New("ZIP structure is invalid")
	ErrLimit       = errors.New("ZIP exceeds configured limits")
)

type File struct {
	Key  string
	Path string
	File *zip.File
}

type Variant struct {
	Mode       string
	Files      map[string]File
	Dirs       map[string]struct{}
	DirPaths   map[string]string
	FileCount  int
	DirCount   int
	TotalBytes uint64
}

type Archive struct {
	Path              string
	Size              int64
	ModTime           time.Time
	Reader            *zip.ReadCloser
	Variants          []Variant
	EntryCount        int
	TotalUncompressed uint64
	TotalCompressed   uint64
}

func (a *Archive) Close() error {
	if a == nil || a.Reader == nil {
		return nil
	}
	return a.Reader.Close()
}

func Open(path string, limits model.Limits) (*Archive, error) {
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
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	a := &Archive{Path: path, Size: info.Size(), ModTime: info.ModTime(), Reader: reader}
	ok := false
	defer func() {
		if !ok {
			_ = reader.Close()
		}
	}()

	if len(reader.File) > limits.MaxEntries {
		return nil, fmt.Errorf("%w: too many entries", ErrLimit)
	}

	files := map[string]File{}
	dirs := map[string]struct{}{}
	dirPaths := map[string]string{}
	entries := map[string]string{}
	top := map[string]struct{}{}

	for _, zf := range reader.File {
		if zf.Flags&0x1 != 0 {
			return nil, fmt.Errorf("%w: %s", ErrEncrypted, zf.Name)
		}
		if !utf8.ValidString(zf.Name) {
			return nil, fmt.Errorf("%w: cannot decode file name", ErrInvalid)
		}
		if zf.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: symbolic link entry %s", ErrInvalid, zf.Name)
		}
		if !zf.Mode().IsRegular() && !zf.FileInfo().IsDir() {
			return nil, fmt.Errorf("%w: irregular entry %s", ErrInvalid, zf.Name)
		}
		key, display, isDir, err := winpath.NormalizeArchiveEntry(zf.Name)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		if zf.Mode().IsDir() && !isDir {
			return nil, fmt.Errorf("%w: directory entry without trailing slash %q", ErrInvalid, zf.Name)
		}
		if previous, exists := entries[key]; exists {
			return nil, fmt.Errorf("%w: duplicate path %q (already %q)", ErrInvalid, display, previous)
		}
		entries[key] = display

		first := display
		if i := strings.IndexByte(display, '/'); i >= 0 {
			first = display[:i]
		}
		top[strings.ToLower(first)] = struct{}{}

		if isDir {
			dirs[key] = struct{}{}
			dirPaths[key] = display
		} else {
			if zf.Method != zip.Store && zf.Method != zip.Deflate {
				return nil, fmt.Errorf("%w: %s", ErrUnsupported, zf.Name)
			}
			if zf.UncompressedSize64 > limits.MaxFileBytes {
				return nil, fmt.Errorf("%w: file too large %s", ErrLimit, zf.Name)
			}
			a.TotalUncompressed += zf.UncompressedSize64
			a.TotalCompressed += zf.CompressedSize64
			if a.TotalUncompressed > limits.MaxTotalBytes {
				return nil, fmt.Errorf("%w: total uncompressed size too large", ErrLimit)
			}
			files[key] = File{Key: key, Path: display, File: zf}
		}
		addParents(dirs, dirPaths, display)
	}

	if a.TotalCompressed > 0 && a.TotalUncompressed > 1<<30 {
		if a.TotalUncompressed/a.TotalCompressed > limits.MaxCompressionRatio {
			return nil, fmt.Errorf("%w: suspicious compression ratio", ErrLimit)
		}
	}
	for key := range files {
		if _, exists := dirs[key]; exists {
			return nil, fmt.Errorf("%w: path is both file and directory", ErrInvalid)
		}
	}

	direct := Variant{Mode: "direct", Files: files, Dirs: dirs, DirPaths: dirPaths, FileCount: len(files), DirCount: len(dirs), TotalBytes: a.TotalUncompressed}
	a.Variants = append(a.Variants, direct)

	if len(top) == 1 {
		var topName string
		for name := range top {
			topName = name
		}
		if _, topIsDir := dirs[topName]; topIsDir {
			stripped, ok := stripVariant(direct, topName)
			if ok {
				a.Variants = append(a.Variants, stripped)
			}
		}
	}

	a.EntryCount = len(reader.File)
	ok = true
	return a, nil
}

func addParents(dirs map[string]struct{}, dirPaths map[string]string, display string) {
	for {
		i := strings.LastIndexByte(display, '/')
		if i < 0 {
			return
		}
		display = display[:i]
		key := strings.ToLower(display)
		dirs[key] = struct{}{}
		dirPaths[key] = display
	}
}

func stripFirst(display, topKey string) (string, bool) {
	i := strings.IndexByte(display, '/')
	if i < 0 {
		return "", false
	}
	if !strings.EqualFold(display[:i], topKey) {
		return "", false
	}
	return display[i+1:], true
}

func stripVariant(direct Variant, topKey string) (Variant, bool) {
	stripped := Variant{
		Mode:       "strip-top:" + topKey,
		Files:      map[string]File{},
		Dirs:       map[string]struct{}{},
		DirPaths:   map[string]string{},
		TotalBytes: direct.TotalBytes,
	}
	for _, file := range direct.Files {
		path, ok := stripFirst(file.Path, topKey)
		if !ok {
			return Variant{}, false
		}
		key := strings.ToLower(path)
		stripped.Files[key] = File{Key: key, Path: path, File: file.File}
	}
	for key, display := range direct.DirPaths {
		if key == topKey {
			continue
		}
		path, ok := stripFirst(display, topKey)
		if !ok {
			return Variant{}, false
		}
		strippedKey := strings.ToLower(path)
		stripped.Dirs[strippedKey] = struct{}{}
		stripped.DirPaths[strippedKey] = path
	}
	stripped.FileCount = len(stripped.Files)
	stripped.DirCount = len(stripped.Dirs)
	return stripped, true
}
