package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
)

func TestLegacyAPIsFailBeforeChangingState(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.zip")
	if err := os.WriteFile(path, []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	a := &App{items: []model.Item{{ID: "a", ZipPath: path}}, index: map[string]int{"a": 0}, roots: []string{root}, triagePath: filepath.Join(t.TempDir(), "triage.json"), logPath: filepath.Join(t.TempDir(), "audit.jsonl"), mover: func(p string) error { calls++; return os.Remove(p) }}
	if err := a.SetTriage("a", "discard"); err == nil || !strings.Contains(err.Error(), "SetTriageDetailed") {
		t.Fatalf("legacy triage error = %v", err)
	}
	if _, err := os.Stat(a.triageFile()); !os.IsNotExist(err) {
		t.Fatalf("legacy triage wrote state: %v", err)
	}
	if _, err := a.SetTriageDetailed("a", "discard"); err != nil {
		t.Fatal(err)
	}
	preview, err := a.PreviewDiscard([]string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.RecycleDiscard(preview.Token, true); err == nil || !strings.Contains(err.Error(), "RecycleDiscardDetailed") {
		t.Fatalf("legacy recycle error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("legacy recycle called mover %d times", calls)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	result, err := a.RecycleDiscardDetailed(preview.Token, true)
	if err != nil || len(result.Results) != 1 || !result.Results[0].Success {
		t.Fatalf("preview changed: result=%+v err=%v", result, err)
	}
}
