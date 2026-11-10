package fswalk

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
)

var (
	ErrLink          = errors.New("link or reparse point is not allowed")
	ErrIrregular     = errors.New("irregular file is not allowed")
	ErrLimit         = errors.New("archive or folder exceeds configured limits")
	ErrCaseCollision = errors.New("path case collision")
)

type File struct {
	Key     string    `json:"key"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

type Tree struct {
	Files     map[string]File
	Dirs      map[string]struct{}
	Size      int64
	FileCount int
	DirCount  int
}

func (t *Tree) SameAs(other *Tree) bool {
	if t == nil || other == nil {
		return false
	}
	if t.FileCount != other.FileCount || t.DirCount != other.DirCount || len(t.Files) != len(other.Files) || len(t.Dirs) != len(other.Dirs) {
		return false
	}
	for key, file := range t.Files {
		of, ok := other.Files[key]
		if !ok || of.Size != file.Size || !of.ModTime.Equal(file.ModTime) || of.Path != file.Path {
			return false
		}
	}
	for key := range t.Dirs {
		if _, ok := other.Dirs[key]; !ok {
			return false
		}
	}
	return true
}

func Walk(ctx context.Context, root string, limits model.Limits) (*Tree, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", abs)
	}
	reparse, err := winpath.IsReparsePoint(abs)
	if err != nil {
		return nil, err
	}
	if reparse {
		return nil, fmt.Errorf("%w: %s", ErrLink, abs)
	}
	tree := &Tree{Files: map[string]File{}, Dirs: map[string]struct{}{}}
	if err := walkDir(ctx, abs, "", tree, limits); err != nil {
		return nil, err
	}
	for key := range tree.Files {
		if _, ok := tree.Dirs[key]; ok {
			return nil, fmt.Errorf("%w: %s is both file and directory", ErrCaseCollision, key)
		}
	}
	return tree, nil
}

func walkDir(ctx context.Context, absDir, relDir string, tree *Tree, limits model.Limits) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := entry.Name()
		if err := winpath.ValidateSegment(name); err != nil {
			return fmt.Errorf("invalid name %q: %w", name, err)
		}
		rel := name
		if relDir != "" {
			rel = relDir + "/" + name
		}
		key := strings.ToLower(rel)
		full := filepath.Join(absDir, name)

		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrLink, full)
		}
		reparse, err := winpath.IsReparsePoint(full)
		if err != nil {
			return err
		}
		if reparse {
			return fmt.Errorf("%w: %s", ErrLink, full)
		}

		if entry.IsDir() {
			if _, exists := tree.Dirs[key]; exists {
				return fmt.Errorf("%w: %s", ErrCaseCollision, rel)
			}
			tree.Dirs[key] = struct{}{}
			tree.DirCount++
			if tree.DirCount+tree.FileCount > limits.MaxEntries {
				return fmt.Errorf("%w: too many entries", ErrLimit)
			}
			if err := walkDir(ctx, full, rel, tree, limits); err != nil {
				return err
			}
			continue
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: %s", ErrIrregular, full)
		}
		if info.Size() < 0 || uint64(info.Size()) > limits.MaxFileBytes {
			return fmt.Errorf("%w: file too large: %s", ErrLimit, rel)
		}
		if uint64(tree.Size)+uint64(info.Size()) > limits.MaxTotalBytes {
			return fmt.Errorf("%w: total size too large", ErrLimit)
		}
		if tree.FileCount+tree.DirCount+1 > limits.MaxEntries {
			return fmt.Errorf("%w: too many entries", ErrLimit)
		}
		if existing, exists := tree.Files[key]; exists {
			return fmt.Errorf("%w: %s conflicts with %s", ErrCaseCollision, rel, existing.Path)
		}
		tree.Files[key] = File{Key: key, Path: rel, Size: info.Size(), ModTime: info.ModTime()}
		tree.FileCount++
		tree.Size += info.Size()
	}
	return nil
}

func CheckNoLinks(ctx context.Context, root string) error {
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", abs)
	}
	reparse, err := winpath.IsReparsePoint(abs)
	if err != nil {
		return err
	}
	if reparse {
		return fmt.Errorf("%w: %s", ErrLink, abs)
	}
	return checkNoLinksDir(ctx, abs)
}

func checkNoLinksDir(ctx context.Context, absDir string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		full := filepath.Join(absDir, entry.Name())
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrLink, full)
		}
		reparse, err := winpath.IsReparsePoint(full)
		if err != nil {
			return err
		}
		if reparse {
			return fmt.Errorf("%w: %s", ErrLink, full)
		}
		if entry.IsDir() {
			if err := checkNoLinksDir(ctx, full); err != nil {
				return err
			}
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: %s", ErrIrregular, full)
		}
	}
	return nil
}

func BestEffortSize(ctx context.Context, root string) int64 {
	abs, err := filepath.Abs(root)
	if err != nil {
		return 0
	}
	var total int64
	var visit func(string)
	visit = func(dir string) {
		if ctx.Err() != nil {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if ctx.Err() != nil {
				return
			}
			full := filepath.Join(dir, entry.Name())
			if entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			reparse, err := winpath.IsReparsePoint(full)
			if err != nil || reparse {
				continue
			}
			if entry.IsDir() {
				visit(full)
				continue
			}
			info, err := entry.Info()
			if err == nil && info.Mode().IsRegular() && info.Size() > 0 {
				total += info.Size()
			}
		}
	}
	visit(abs)
	return total
}
