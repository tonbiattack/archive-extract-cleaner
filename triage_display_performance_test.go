package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
)

func TestGetRowsTriageIsAdvisoryNotContentRevalidation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "item.zip")
	if err := os.WriteFile(path, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	item := model.Item{ID: "item", ZipPath: path, Verdict: model.VerdictKeepArchive}
	db := filepath.Join(t.TempDir(), "triage.json")
	if err := writeTriage(db, map[string]triageDecision{"item": {State: "save", Fingerprint: "stale"}}); err != nil {
		t.Fatal(err)
	}
	a := &App{items: []model.Item{item}, index: map[string]int{"item": 0}, roots: []string{root}, triagePath: db}
	if got := a.GetRows(RowQuery{}).Rows[0].Triage; got != "save" {
		t.Fatalf("display should be advisory, got %q", got)
	}
	if got := len(a.GetTransferReview().Candidates); got != 0 {
		t.Fatalf("stale decision authorized %d candidates", got)
	}
}
