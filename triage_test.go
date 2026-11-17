package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
)

func TestTriagePersistsAndInvalidatesChangedFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "one.zip")
	if err := os.WriteFile(path, []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	item := model.Item{ID: "one", ZipPath: path, Verdict: model.VerdictKeepArchive}
	db := filepath.Join(t.TempDir(), "triage.json")
	a := &App{items: []model.Item{item}, index: map[string]int{"one": 0}, roots: []string{root}, triagePath: db}
	if _, err := a.SetTriageDetailed("one", "save"); err != nil {
		t.Fatal(err)
	}
	b := &App{items: []model.Item{item}, index: map[string]int{"one": 0}, roots: []string{root}, triagePath: db}
	if got := b.GetRows(RowQuery{}).Rows[0].Triage; got != "save" {
		t.Fatalf("got %q", got)
	}
	if err := os.WriteFile(path, []byte("two"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := b.GetRows(RowQuery{}).Rows[0].Triage; got != "unjudged" {
		t.Fatalf("changed file retained decision %q", got)
	}
}

func TestTransferOnlySavedItems(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "one.zip")
	if err := os.WriteFile(path, []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	item := model.Item{ID: "one", ZipPath: path, Verdict: model.VerdictKeepArchive}
	a := &App{items: []model.Item{item}, index: map[string]int{"one": 0}, roots: []string{root}, triagePath: filepath.Join(t.TempDir(), "triage.json")}
	if len(a.GetTransferReview().Candidates) != 0 {
		t.Fatal("unjudged path offered for transfer")
	}
	if _, err := a.SetTriageDetailed("one", "save"); err != nil {
		t.Fatal(err)
	}
	if len(a.GetTransferReview().Candidates) != 1 {
		t.Fatal("saved zip missing")
	}
	if err := os.WriteFile(path, []byte("two"), 0600); err != nil {
		t.Fatal(err)
	}
	if len(a.GetTransferReview().Candidates) != 0 {
		t.Fatal("stale path offered for transfer")
	}
}

func TestTriageFilteringAndNoImplicitRecycle(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "x.zip")
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	count := 0
	a := &App{items: []model.Item{{ID: "x", ZipPath: path}}, index: map[string]int{"x": 0}, roots: []string{root}, triagePath: filepath.Join(t.TempDir(), "triage.json"), mover: func(string) error { count++; return nil }}
	for _, state := range []string{"save", "discard", "hold", "unjudged"} {
		if _, err := a.SetTriageDetailed("x", state); err != nil {
			t.Fatal(err)
		}
		if page := a.GetRows(RowQuery{Triage: state}); page.Total != 1 || page.Rows[0].Triage != state {
			t.Fatalf("filter %s: %+v", state, page)
		}
	}
	if count != 0 {
		t.Fatal("triage moved files")
	}
}

func TestDiscardRejectsFolderContainingOtherItem(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "dir")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(folder, "nested.zip")
	if err := os.WriteFile(nested, []byte("saved"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &App{items: []model.Item{{ID: "folder", FolderPath: folder}, {ID: "nested", ZipPath: nested}}, index: map[string]int{"folder": 0, "nested": 1}, roots: []string{root}, triagePath: filepath.Join(t.TempDir(), "triage.json")}
	if _, err := a.SetTriageDetailed("folder", "discard"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PreviewDiscard([]string{"folder"}); err == nil {
		t.Fatal("unreviewed nested item would be recycled")
	}
}

func TestDiscardRechecksEachTarget(t *testing.T) {
	root := t.TempDir()
	p1 := filepath.Join(root, "a.zip")
	p2 := filepath.Join(root, "b.zip")
	for _, p := range []string{p1, p2} {
		if err := os.WriteFile(p, []byte("old"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a := &App{items: []model.Item{{ID: "a", ZipPath: p1}, {ID: "b", ZipPath: p2}}, index: map[string]int{"a": 0, "b": 1}, roots: []string{root}, triagePath: filepath.Join(t.TempDir(), "triage.json")}
	a.mover = func(p string) error {
		if p == p1 {
			if err := os.WriteFile(p2, []byte("new"), 0600); err != nil {
				return err
			}
		}
		return os.Remove(p)
	}
	for _, id := range []string{"a", "b"} {
		if _, err := a.SetTriageDetailed(id, "discard"); err != nil {
			t.Fatal(err)
		}
	}
	preview, err := a.PreviewDiscard([]string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	results, err := a.RecycleDiscardDetailed(preview.Token, true)
	if err != nil {
		t.Fatal(err)
	}
	if results.Warning != "" || len(results.Results) != 2 || !results.Results[0].Success || results.Results[1].Success {
		t.Fatalf("results: %+v", results)
	}
	if _, err = os.Stat(p2); err != nil {
		t.Fatal("changed target removed", err)
	}
}

func TestDiscardRequiresReviewedUnchangedPreview(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "one.zip")
	if err := os.WriteFile(path, []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	item := model.Item{ID: "one", ZipPath: path, Verdict: model.VerdictKeepArchive}
	moved := 0
	a := &App{items: []model.Item{item}, index: map[string]int{"one": 0}, roots: []string{root}, triagePath: filepath.Join(t.TempDir(), "triage.json"), mover: func(p string) error { moved++; return os.Remove(p) }}
	if _, err := a.SetTriageDetailed("one", "discard"); err != nil {
		t.Fatal(err)
	}
	p, err := a.PreviewDiscard([]string{"one"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Paths) != 1 || p.Paths[0] != path {
		t.Fatalf("paths: %#v", p.Paths)
	}
	if _, err = a.RecycleDiscardDetailed(p.Token, false); err == nil {
		t.Fatal("confirmation required")
	}
	if err = os.WriteFile(path, []byte("two"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = a.RecycleDiscardDetailed(p.Token, true); err == nil {
		t.Fatal("stale preview accepted")
	}
	if moved != 0 {
		t.Fatal("stale preview moved files")
	}
}

func TestDiscardRejectsSharedPath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "x.zip")
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &App{items: []model.Item{{ID: "a", ZipPath: path}, {ID: "b", ZipPath: path}}, index: map[string]int{"a": 0, "b": 1}, roots: []string{root}, triagePath: filepath.Join(t.TempDir(), "triage.json")}
	if _, err := a.SetTriageDetailed("a", "discard"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PreviewDiscard([]string{"a"}); err == nil {
		t.Fatal("shared path accepted")
	}
}

func TestDiscardPartialRetry(t *testing.T) {
	root := t.TempDir()
	zip := filepath.Join(root, "x.zip")
	folder := filepath.Join(root, "x")
	os.WriteFile(zip, []byte("x"), 0600)
	os.Mkdir(folder, 0700)
	a := &App{items: []model.Item{{ID: "a", ZipPath: zip, FolderPath: folder}}, index: map[string]int{"a": 0}, roots: []string{root}, triagePath: filepath.Join(t.TempDir(), "triage.json")}
	fail := true
	a.mover = func(p string) error {
		if p == folder && fail {
			return os.ErrPermission
		}
		return os.Remove(p)
	}
	if _, err := a.SetTriageDetailed("a", "discard"); err != nil {
		t.Fatal(err)
	}
	p, err := a.PreviewDiscard([]string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := a.RecycleDiscardDetailed(p.Token, true)
	if err != nil || r.Warning != "" || len(r.Results) != 2 || !r.Results[0].Success || r.Results[1].Success {
		t.Fatalf("results %v %v", r, err)
	}
	fail = false
	p, err = a.PreviewDiscard([]string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Paths) != 1 || p.Paths[0] != folder {
		t.Fatalf("retry paths %v", p.Paths)
	}
	r, err = a.RecycleDiscardDetailed(p.Token, true)
	if err != nil || r.Warning != "" || len(r.Results) != 1 || !r.Results[0].Success {
		t.Fatalf("retry %v %v", r, err)
	}
}

// The row cache is advisory: unchanged metadata may hide content changes,
// but authorization must always hash the file again.
func TestRowsCacheDoesNotAuthorizeChangedContent(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "x.zip")
	os.WriteFile(p, []byte("abc"), 0600)
	a := &App{items: []model.Item{{ID: "x", ZipPath: p}}, index: map[string]int{"x": 0}, roots: []string{root}, triagePath: filepath.Join(t.TempDir(), "triage.json")}
	if _, err := a.SetTriageDetailed("x", "save"); err != nil {
		t.Fatal(err)
	}
	if got := a.GetRows(RowQuery{}).Rows[0].Triage; got != "save" {
		t.Fatal(got)
	}
	info, _ := os.Stat(p)
	os.WriteFile(p, []byte("xyz"), 0600)
	os.Chtimes(p, info.ModTime(), info.ModTime())
	if got := a.GetRows(RowQuery{}).Rows[0].Triage; got != "save" {
		t.Fatalf("expected cached display, got %s", got)
	}
	if len(a.GetTransferReview().Candidates) != 0 {
		t.Fatal("cached state authorized changed file")
	}
}

func TestDiscardRejectsTargetInsideOtherScannedFolder(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "dir")
	os.Mkdir(folder, 0700)
	p := filepath.Join(folder, "x.zip")
	os.WriteFile(p, []byte("x"), 0600)
	a := &App{items: []model.Item{{ID: "a", ZipPath: p}, {ID: "b", FolderPath: folder}}, index: map[string]int{"a": 0, "b": 1}, roots: []string{root}, triagePath: filepath.Join(t.TempDir(), "triage.json")}
	if _, err := a.SetTriageDetailed("a", "discard"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PreviewDiscard([]string{"a"}); err == nil {
		t.Fatal("target belongs to another scanned folder")
	}
}
