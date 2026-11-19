package duplicates

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
)

// writeFile はテスト用のファイルを作る。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// 索引の構築と検索を、実在するフォルダで検証する。
func TestBuildIndexAndSearch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "photos.zip"), "12345")
	writeFile(t, filepath.Join(root, "other.zip"), "abc")
	writeFile(t, filepath.Join(root, "notes.txt"), "text")

	idx := buildIndex(context.Background(), "X", Options{Roots: map[string]string{"X": root}})
	if !idx.state.Available {
		t.Fatalf("index not available: %+v", idx.state)
	}
	if idx.state.Indexed != 3 {
		t.Fatalf("indexed = %d, want 3", idx.state.Indexed)
	}
	if len(idx.byName["photos.zip"]) != 1 {
		t.Fatalf("photos.zip not indexed: %+v", idx.byName)
	}
	if len(idx.byName["notes.txt"]) != 0 {
		t.Fatal("non-archive must not be indexed")
	}
}

// 大文字小文字を区別せずに同名を検出する。
func TestIndexIsCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Photos.ZIP"), "12345")

	idx := buildIndex(context.Background(), "X", Options{Roots: map[string]string{"X": root}})
	if len(idx.byName["photos.zip"]) != 1 {
		t.Fatalf("case-insensitive lookup failed: %+v", idx.byName)
	}
}

// Recursive が false なら直下だけを見る。
func TestIndexNonRecursiveSkipsSubfolders(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "top.zip"), "1")
	writeFile(t, filepath.Join(root, "sub", "nested.zip"), "2")

	idx := buildIndex(context.Background(), "X", Options{Roots: map[string]string{"X": root}})
	if len(idx.byName["top.zip"]) != 1 {
		t.Fatal("top-level archive missing")
	}
	if len(idx.byName["nested.zip"]) != 0 {
		t.Fatal("nested archive must be skipped when Recursive is false")
	}
}

// Recursive が true なら配下も見る。
func TestIndexRecursiveIncludesSubfolders(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "sub", "nested.zip"), "2")

	idx := buildIndex(context.Background(), "X", Options{Roots: map[string]string{"X": root}, Recursive: true})
	if len(idx.byName["nested.zip"]) != 1 {
		t.Fatalf("nested archive missing: %+v", idx.byName)
	}
}

// MaxDepth を超えた階層は見ない。
func TestIndexRespectsMaxDepth(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a", "b", "c", "deep.zip"), "3")

	idx := buildIndex(context.Background(), "X", Options{Roots: map[string]string{"X": root}, Recursive: true, MaxDepth: 2})
	if len(idx.byName["deep.zip"]) != 0 {
		t.Fatal("archive beyond MaxDepth must be skipped")
	}
}

// 存在しない保存先は「未確認」であり「なし」ではない。
func TestMissingRootIsUnknownNotNone(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	idx := buildIndex(context.Background(), "X", Options{Roots: map[string]string{"X": missing}})
	if idx.state.Available {
		t.Fatal("missing root must not be available")
	}
	if idx.state.Reason == "" {
		t.Fatal("missing root must carry a reason")
	}
}

// summarize の判定を固定する。
func TestSummarize(t *testing.T) {
	cases := []struct {
		name    string
		hits    []Hit
		unknown bool
		want    Status
	}{
		{name: "no hits, all confirmed", hits: nil, unknown: false, want: StatusNone},
		{name: "no hits, some unknown", hits: nil, unknown: true, want: StatusUnknown},
		{name: "same size wins", hits: []Hit{{SameSize: true}}, unknown: false, want: StatusSameSize},
		{name: "different size", hits: []Hit{{SameSize: false}}, unknown: false, want: StatusDifferentSize},
		{name: "hit beats unknown", hits: []Hit{{SameSize: false}}, unknown: true, want: StatusDifferentSize},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := summarize(tc.hits, tc.unknown); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

// 対象自身は重複として扱わない。
func TestSearchExcludesTargetItself(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "photos.zip")
	writeFile(t, target, "12345")

	idx := buildIndex(context.Background(), "X", Options{Roots: map[string]string{"X": root}})
	if len(idx.byName["photos.zip"]) != 1 {
		t.Fatal("target should be indexed")
	}
	// 同じパスは EqualPath で除外される。
	if !winpath.EqualPath(target, target) {
		t.Fatal("EqualPath must treat identical paths as equal")
	}
}

// アーカイブ拡張子の判定。
func TestIsArchiveName(t *testing.T) {
	for _, name := range []string{"a.zip", "a.ZIP", "a.7z", "a.rar", "a.Rar"} {
		if !isArchiveName(name) {
			t.Fatalf("%s should be an archive", name)
		}
	}
	for _, name := range []string{"a.txt", "a.zip.txt", "a", "a.tar"} {
		if isArchiveName(name) {
			t.Fatalf("%s should not be an archive", name)
		}
	}
}
