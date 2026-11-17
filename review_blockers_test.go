package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/testutil"
)

func TestTransferRejectsSharedOrContainingNonSavedItems(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(map[bool]string{false: "shared", true: "nested"}[nested], func(t *testing.T) {
			root := t.TempDir()
			folder := filepath.Join(root, "dir")
			if err := os.Mkdir(folder, 0700); err != nil {
				t.Fatal(err)
			}
			p := folder
			if nested {
				p = filepath.Join(folder, "nested.zip")
				if err := os.WriteFile(p, []byte("x"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			a := &App{items: []model.Item{{ID: "saved", FolderPath: folder, Verdict: model.VerdictKeepDirectory}, {ID: "other", ZipPath: p}}, index: map[string]int{"saved": 0, "other": 1}, roots: []string{root}, triagePath: filepath.Join(t.TempDir(), "triage.json")}
			if _, err := a.SetTriageDetailed("saved", "save"); err != nil {
				t.Fatal(err)
			}
			if got := a.GetTransferReview().Candidates; len(got) != 0 {
				t.Fatalf("unsafe transfer candidates: %+v", got)
			}
		})
	}
}

func TestLegacyDeleteCannotBypassDiscardPreview(t *testing.T) {
	for _, method := range []MoveMethod{MoveMethodZip, MoveMethodBoth} {
		t.Run(string(method), func(t *testing.T) {
			root := t.TempDir()
			zip := filepath.Join(root, "x.zip")
			folder := filepath.Join(root, "x")
			if err := testutil.WriteZip(zip, []testutil.Entry{{Name: "x/a.txt", Content: "a"}}); err != nil {
				t.Fatal(err)
			}
			if err := testutil.WriteFile(folder, "a.txt", "a"); err != nil {
				t.Fatal(err)
			}
			a := newTestApp(t)
			if _, err := a.Scan([]string{root}); err != nil {
				t.Fatal(err)
			}
			ids := a.CandidateIDs(RowQuery{})
			if len(ids) != 1 {
				t.Fatal(ids)
			}
			if _, err := a.SetTriageDetailed(ids[0], "save"); err != nil {
				t.Fatal(err)
			}
			_, _ = a.DeleteSelected([]MoveRequest{{ID: ids[0], Method: method}}, true)
			if _, err := os.Stat(zip); err != nil {
				t.Fatal("saved archive moved without discard preview", err)
			}
		})
	}
}

func TestTriageWriteFailsClosedWhenStoreLocked(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "x.zip")
	if err := os.WriteFile(p, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(t.TempDir(), "triage.json")
	if err := os.Mkdir(db+".lock", 0700); err != nil {
		t.Fatal(err)
	}
	a := &App{items: []model.Item{{ID: "x", ZipPath: p}}, index: map[string]int{"x": 0}, roots: []string{root}, triagePath: db}
	if _, err := a.SetTriageDetailed("x", "save"); err == nil {
		t.Fatal("write ignored competing store lock")
	}
}
