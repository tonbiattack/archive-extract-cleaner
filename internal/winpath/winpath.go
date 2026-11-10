package winpath

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var ErrInvalidPath = errors.New("invalid path")

func NormalizeArchiveEntry(name string) (key, display string, isDir bool, err error) {
	if name == "" {
		return "", "", false, fmt.Errorf("%w: empty name", ErrInvalidPath)
	}
	name = strings.ReplaceAll(name, "\\", "/")
	if strings.HasSuffix(name, "/") {
		isDir = true
		name = strings.TrimSuffix(name, "/")
	}
	if name == "" {
		return "", "", false, fmt.Errorf("%w: empty name", ErrInvalidPath)
	}
	if strings.HasPrefix(name, "/") {
		return "", "", false, fmt.Errorf("%w: absolute path %q", ErrInvalidPath, name)
	}
	parts := strings.Split(name, "/")
	for _, part := range parts {
		if err := ValidateSegment(part); err != nil {
			return "", "", false, fmt.Errorf("%w: %q: %v", ErrInvalidPath, name, err)
		}
	}
	display = strings.Join(parts, "/")
	if len(display) > 32767 {
		return "", "", false, fmt.Errorf("%w: path too long", ErrInvalidPath)
	}
	return strings.ToLower(display), display, isDir, nil
}

func ValidateSegment(seg string) error {
	if seg == "" {
		return errors.New("empty segment")
	}
	if seg == "." || seg == ".." {
		return errors.New("relative segment")
	}
	if len(seg) > 255 {
		return errors.New("segment too long")
	}
	if strings.HasSuffix(seg, " ") || strings.HasSuffix(seg, ".") {
		return errors.New("trailing space or dot")
	}
	for _, r := range seg {
		if r < 0x20 || r == 0x7f {
			return errors.New("control character")
		}
		switch r {
		case '<', '>', ':', '"', '|', '?', '*', '/', '\\':
			return fmt.Errorf("invalid character %q", r)
		}
	}
	if isReservedName(seg) {
		return errors.New("reserved device name")
	}
	return nil
}

func isReservedName(seg string) bool {
	base := seg
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	base = strings.ToUpper(strings.TrimRight(base, " "))
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(base) == 4 {
		prefix := base[:3]
		if prefix == "COM" || prefix == "LPT" {
			return base[3] >= '1' && base[3] <= '9'
		}
	}
	return false
}

func IsWithin(root, target string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	if err != nil || filepath.IsAbs(rel) || rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func EqualPath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func Key(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

func CheckNoReparseAncestors(root, target string) error {
	root = filepath.Clean(root)
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", root)
	}
	reparse, err := IsReparsePoint(root)
	if err != nil {
		return err
	}
	if reparse {
		return fmt.Errorf("%s is a link or reparse point", root)
	}
	rel, err := filepath.Rel(root, filepath.Clean(target))
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	parts := strings.Split(rel, string(filepath.Separator))
	current := root
	for i, part := range parts {
		current = filepath.Join(current, part)
		if i == len(parts)-1 {
			break
		}
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", current)
		}
		reparse, err := IsReparsePoint(current)
		if err != nil {
			return err
		}
		if reparse {
			return fmt.Errorf("%s is a link or reparse point", current)
		}
	}
	return nil
}

func CleanAbs(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", ErrInvalidPath
	}
	return filepath.Abs(path)
}
