package preview

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/testutil"
)

func findEntry(entries []Entry, path string) (Entry, bool) {
	for _, entry := range entries {
		if entry.Path == path {
			return entry, true
		}
	}
	return Entry{}, false
}

func TestListZIP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "photos.zip")
	if err := testutil.WriteZip(path, []testutil.Entry{
		{Name: "photos/a.txt", Content: "alpha"},
		{Name: "photos/sub/b.txt", Content: "beta"},
		{Name: "photos/empty/"},
		{Name: "photos/日本語 ファイル & # (test)'s.txt", Content: "unicode"},
	}); err != nil {
		t.Fatal(err)
	}
	entries, truncated, err := ListZIP(path, model.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Fatal("unexpected truncation")
	}
	for _, expected := range []string{"photos", "photos/sub", "photos/empty", "photos/a.txt", "photos/sub/b.txt", "photos/日本語 ファイル & # (test)'s.txt"} {
		if _, ok := findEntry(entries, expected); !ok {
			t.Fatalf("entry not found: %s (%+v)", expected, entries)
		}
	}
	file, _ := findEntry(entries, "photos/a.txt")
	if file.Kind != "file" || file.Size != 5 || file.Depth != 1 {
		t.Fatalf("file = %+v", file)
	}
	dir, _ := findEntry(entries, "photos/sub")
	if dir.Kind != "directory" || dir.Depth != 1 {
		t.Fatalf("dir = %+v", dir)
	}
}

func TestListZIPRejectsInvalidArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.zip")
	if err := os.WriteFile(path, []byte("broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ListZIP(path, model.DefaultLimits()); err == nil {
		t.Fatal("broken zip must fail")
	}
}

func TestListZIPRejectsTraversal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traversal.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("../escape.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("escape")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ListZIP(path, model.DefaultLimits()); err == nil {
		t.Fatal("path traversal entry must fail")
	}
}

func TestListFolderAndSkipLinks(t *testing.T) {
	root := t.TempDir()
	if err := testutil.WriteFile(root, "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(root, "sub/b.txt", "beta"); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	linkCreated := true
	if err := testutil.Junction(filepath.Join(root, "link"), target); err != nil {
		linkCreated = false
		t.Logf("junction skipped: %v", err)
	}
	entries, truncated, err := ListFolder(context.Background(), root, MaxEntries)
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Fatal("unexpected truncation")
	}
	for _, expected := range []string{"a.txt", "sub", "sub/b.txt"} {
		if _, ok := findEntry(entries, expected); !ok {
			t.Fatalf("entry not found: %s (%+v)", expected, entries)
		}
	}
	if linkCreated {
		if _, ok := findEntry(entries, "link"); !ok {
			t.Fatalf("link entry should be visible but not followed: %+v", entries)
		}
		if _, ok := findEntry(entries, "link/a.txt"); ok {
			t.Fatal("link target must not be traversed")
		}
	}
}

func TestListFolderTruncates(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := testutil.WriteFile(root, name, "x"); err != nil {
			t.Fatal(err)
		}
	}
	entries, truncated, err := ListFolder(context.Background(), root, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len(entries) != 2 {
		t.Fatalf("entries = %d, truncated = %v", len(entries), truncated)
	}
}
