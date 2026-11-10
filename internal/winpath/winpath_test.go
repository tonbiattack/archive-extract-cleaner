package winpath

import (
	"errors"
	"testing"
)

func TestNormalizeArchiveEntry(t *testing.T) {
	valid := map[string]struct {
		key   string
		isDir bool
	}{
		"a.txt":        {"a.txt", false},
		"dir/file.txt": {"dir/file.txt", false},
		"dir/sub/":     {"dir/sub", true},
		`dir\file.txt`: {"dir/file.txt", false},
		"日本語/資料 1.txt": {"日本語/資料 1.txt", false},
		"a/b/c":        {"a/b/c", false},
	}
	for name, want := range valid {
		key, _, isDir, err := NormalizeArchiveEntry(name)
		if err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		if key != want.key || isDir != want.isDir {
			t.Fatalf("%q: got %q %v", name, key, isDir)
		}
	}

	invalid := []string{
		"", "/", "/abs.txt", "../a.txt", "a/../b", "a//b", "C:/a.txt",
		"CON", "nul.txt", "lpt9.log", "a.", "a ", "a:b", "a?b", "a*b",
		"a\x00b", "foo/COM1/bar",
	}
	for _, name := range invalid {
		if _, _, _, err := NormalizeArchiveEntry(name); err == nil {
			t.Fatalf("%q: expected error", name)
		} else if !errors.Is(err, ErrInvalidPath) {
			t.Fatalf("%q: error = %v", name, err)
		}
	}
}

func TestValidateSegment(t *testing.T) {
	if err := ValidateSegment("normal.txt"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSegment("..foo"); err != nil {
		t.Fatalf("..foo should be valid: %v", err)
	}
}
