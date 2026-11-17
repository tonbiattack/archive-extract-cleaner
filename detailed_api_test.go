package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
)

func TestDetailedTriageAuditAndCompareRestore(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.zip")
	if err := os.WriteFile(p, []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	audit := filepath.Join(t.TempDir(), "audit")
	if err := os.Mkdir(audit, 0700); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(t.TempDir(), "triage.json")
	makeApp := func() *App {
		return &App{items: []model.Item{{ID: "a", ZipPath: p}}, index: map[string]int{"a": 0}, roots: []string{root}, triagePath: db, logPath: audit}
	}
	a, b := makeApp(), makeApp()
	first, err := a.SetTriageDetailed("a", "save")
	if err != nil || first.Previous != "unjudged" || first.State != "save" || !strings.Contains(first.Warning, "audit failed") {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := b.SetTriageDetailed("a", "discard")
	if err != nil || second.Previous != "save" {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if _, err := a.RestoreTriage("a", first.UndoToken); err == nil {
		t.Fatal("stale undo succeeded")
	}
	restored, err := b.RestoreTriage("a", second.UndoToken)
	if err != nil || restored.Previous != "discard" || restored.State != "save" || restored.Warning == "" {
		t.Fatalf("restore=%+v err=%v", restored, err)
	}
	m, err := readTriage(db)
	if err != nil || triageState(a.items[0], a.roots, m) != "save" {
		t.Fatalf("state=%v err=%v", m, err)
	}
}

func TestDetailedRecyclePreservesResultsOnAuditFailure(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.zip")
	if err := os.WriteFile(p, []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	audit := filepath.Join(t.TempDir(), "audit.jsonl")
	calls := 0
	a := &App{items: []model.Item{{ID: "a", ZipPath: p}}, index: map[string]int{"a": 0}, roots: []string{root}, triagePath: filepath.Join(t.TempDir(), "triage.json"), logPath: audit, mover: func(p string) error { calls++; return os.Remove(p) }}
	if _, err := a.SetTriageDetailed("a", "discard"); err != nil {
		t.Fatal(err)
	}
	preview, err := a.PreviewDiscard([]string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(audit); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(audit, 0700); err != nil {
		t.Fatal(err)
	}
	result, err := a.RecycleDiscardDetailed(preview.Token, true)
	if err != nil || len(result.Results) != 1 || !result.Results[0].Success || !strings.Contains(result.Warning, "audit failed") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := a.RecycleDiscardDetailed(preview.Token, true); err == nil || calls != 1 {
		t.Fatalf("reused preview calls=%d err=%v", calls, err)
	}
}
