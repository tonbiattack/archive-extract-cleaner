package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/testutil"
)

// AMBIGUOUS の RelatedFolders は「対応先」ではなく「候補」なので、
// 移動時の重複判定に使ってはならない。
//
// 実データでは 0 (1) (15).zip が AMBIGUOUS で 45 件の候補フォルダを持ち、
// その中に 0 (1) (13) が含まれていた。0 (1) (13) は 0 (1) (13).zip と
// 正しく対応して DELETE_CANDIDATE になっているのに、無関係な AMBIGUOUS
// 項目の候補に名前が載っているだけで永久に移動できなくなっていた。
func TestDeleteSelectedIgnoresAmbiguousCandidateFolders(t *testing.T) {
	root := t.TempDir()
	zipA := filepath.Join(root, "0 (1) (13).zip")
	folderA := filepath.Join(root, "0 (1) (13)")
	zipB := filepath.Join(root, "0 (1) (15).zip")
	if err := testutil.WriteZip(zipA, []testutil.Entry{{Name: "a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(folderA, "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteZip(zipB, []testutil.Entry{{Name: "b.txt", Content: "beta"}}); err != nil {
		t.Fatal(err)
	}

	a := newTestApp(t)
	a.items = []model.Item{
		{ID: "A", ZipPath: zipA, FolderPath: folderA, Verdict: model.VerdictDeleteCandidate, Status: model.StatusReady},
		{ID: "B", ZipPath: zipB, RelatedFolders: []string{folderA}, Verdict: model.VerdictAmbiguous, Status: model.StatusReady},
	}
	a.index = map[string]int{"A": 0, "B": 1}
	a.roots = []string{root}

	response, err := a.DeleteSelected([]MoveRequest{{ID: "A", Method: MoveMethodFolder}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if response.Skipped != 0 {
		t.Fatalf("ambiguous candidate folder must not block the move: %+v", response.Results)
	}
	if response.Succeeded != 1 {
		t.Fatalf("succeeded = %d, want 1: %+v", response.Succeeded, response.Results)
	}
	if _, err := os.Stat(folderA); !os.IsNotExist(err) {
		t.Fatalf("folder should have been moved: %v", err)
	}
}

// 対応が確定している項目との重複は従来どおり拒否する。
func TestDeleteSelectedStillRejectsConfirmedOverlap(t *testing.T) {
	root := t.TempDir()
	zipA := filepath.Join(root, "x.zip")
	folderA := filepath.Join(root, "x")
	if err := testutil.WriteZip(zipA, []testutil.Entry{{Name: "a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(folderA, "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(folderA, "inner.txt")
	if err := os.WriteFile(inner, []byte("inner"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := newTestApp(t)
	a.items = []model.Item{
		{ID: "A", ZipPath: zipA, FolderPath: folderA, Verdict: model.VerdictDeleteCandidate, Status: model.StatusReady},
		{ID: "B", ZipPath: inner, Verdict: model.VerdictKeepArchive, Status: model.StatusReady},
	}
	a.index = map[string]int{"A": 0, "B": 1}
	a.roots = []string{root}

	response, err := a.DeleteSelected([]MoveRequest{{ID: "A", Method: MoveMethodFolder}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if response.Skipped != 1 {
		t.Fatalf("folder containing another confirmed item must be skipped: %+v", response.Results)
	}
	if _, err := os.Stat(folderA); err != nil {
		t.Fatalf("overlapping folder must remain: %v", err)
	}
}

// スキップ理由が、利用者が対処できる言葉になっていること。
func TestUnsafeFolderReasonIsActionable(t *testing.T) {
	cases := []struct {
		name string
		item model.Item
		want string
	}{
		{name: "zip moved", item: model.Item{ZipMoved: true}, want: "アーカイブは既に移動済み"},
		{name: "folder moved", item: model.Item{FolderMoved: true}, want: "フォルダは既に移動済み"},
		{name: "no archive", item: model.Item{FolderPath: `C:\x`}, want: "対応するアーカイブがない"},
		{name: "no folder", item: model.Item{ZipPath: `C:\x.zip`}, want: "対応するフォルダがない"},
		{name: "overlap", item: model.Item{ZipPath: `C:\x.zip`, FolderPath: `C:\x`}, want: "範囲が重なっている"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := unsafeFolderReason(tc.item)
			if !contains(got, tc.want) {
				t.Fatalf("reason = %q, want it to contain %q", got, tc.want)
			}
		})
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
