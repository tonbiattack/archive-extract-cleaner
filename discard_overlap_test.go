package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/testutil"
)

// AMBIGUOUS の RelatedFolders は「対応先」ではなく「候補」なので、
// 破棄対象の重複判定に使ってはならない。
// 実データでは 0 (1) (15).zip の候補 46 件に 0 (1) (13) が含まれ、
// 正当な 0 (1) (13) の破棄がブロックされていた。
func TestDiscardPreviewIgnoresAmbiguousCandidateFolders(t *testing.T) {
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
		{ID: "A", ZipPath: zipA, FolderPath: folderA, Verdict: model.VerdictDeleteCandidate},
		{ID: "B", ZipPath: zipB, RelatedFolders: []string{folderA}, Verdict: model.VerdictAmbiguous},
	}
	a.index = map[string]int{"A": 0, "B": 1}
	a.roots = []string{root}
	if _, err := a.SetTriageDetailed("A", "discard"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PreviewDiscard([]string{"A"}); err != nil {
		t.Fatalf("ambiguous candidate folder must not block a valid discard: %v", err)
	}
}

// 対応が確定している項目（AMBIGUOUS 以外）との重複は従来どおり拒否する。
func TestDiscardPreviewStillRejectsConfirmedOverlap(t *testing.T) {
	root := t.TempDir()
	zipA := filepath.Join(root, "x.zip")
	folderA := filepath.Join(root, "x")
	if err := testutil.WriteZip(zipA, []testutil.Entry{{Name: "a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(folderA, "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	// 確定項目 B が、A のフォルダの中にファイルを持つ。
	inner := filepath.Join(folderA, "inner.txt")
	if err := os.WriteFile(inner, []byte("inner"), 0600); err != nil {
		t.Fatal(err)
	}

	a := newTestApp(t)
	a.items = []model.Item{
		{ID: "A", ZipPath: zipA, FolderPath: folderA, Verdict: model.VerdictDeleteCandidate},
		{ID: "B", ZipPath: inner, Verdict: model.VerdictKeepArchive},
	}
	a.index = map[string]int{"A": 0, "B": 1}
	a.roots = []string{root}
	if _, err := a.SetTriageDetailed("A", "discard"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PreviewDiscard([]string{"A"}); err == nil {
		t.Fatal("folder containing another confirmed item must still be rejected")
	}
}
