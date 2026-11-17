package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
)

func TestTriageUndoRevisionAndContent(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.zip")
	if err := os.WriteFile(p, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(t.TempDir(), "triage.json")
	makeApp := func() *App {
		return &App{items: []model.Item{{ID: "a", ZipPath: p}, {ID: "b", ZipPath: p}}, index: map[string]int{"a": 0, "b": 1}, roots: []string{root}, triagePath: db}
	}
	a := makeApp()
	set := func(s string) TriageResult {
		t.Helper()
		r, e := a.SetTriageDetailed("a", s)
		if e != nil || r.UndoToken == "" {
			t.Fatalf("set %s: %+v %v", s, r, e)
		}
		return r
	}
	first := set("discard")
	same := set("discard")
	if same.UndoToken == first.UndoToken || same.Previous != "discard" {
		t.Fatal("same-state operation did not advance revision")
	}
	if _, e := makeApp().RestoreTriage("a", first.UndoToken); e == nil {
		t.Fatal("same-state stale undo accepted")
	}
	set("save")
	set("discard")
	if _, e := makeApp().RestoreTriage("a", first.UndoToken); e == nil {
		t.Fatal("ABA undo accepted")
	}
	u := set("unjudged")
	if _, e := makeApp().RestoreTriage("b", u.UndoToken); e == nil {
		t.Fatal("cross-item token accepted")
	}
	if e := os.WriteFile(p, []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := makeApp().RestoreTriage("a", u.UndoToken); e == nil {
		t.Fatal("changed content accepted")
	}
	m, e := readTriage(db)
	if e != nil || triageState(a.items[0], a.roots, m) != "unjudged" {
		t.Fatalf("changed content authorized: %v %v", m, e)
	}
	latest := set("save")
	r, e := makeApp().RestoreTriage("a", latest.UndoToken)
	if e != nil || r.Previous != "save" || r.State != "unjudged" {
		t.Fatalf("restore %+v %v", r, e)
	}
	if _, e = makeApp().RestoreTriage("a", latest.UndoToken); e == nil {
		t.Fatal("token reused")
	}
}

func TestTriageUndoLegacyRecord(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.zip")
	if e := os.WriteFile(p, []byte("legacy"), 0600); e != nil {
		t.Fatal(e)
	}
	item := model.Item{ID: "a", ZipPath: p}
	db := filepath.Join(t.TempDir(), "triage.json")
	fp, e := triageFingerprint(item, []string{root})
	if e != nil {
		t.Fatal(e)
	}
	if e = writeTriage(db, map[string]triageDecision{"a": {State: "discard", Fingerprint: fp}}); e != nil {
		t.Fatal(e)
	}
	a := &App{items: []model.Item{item}, index: map[string]int{"a": 0}, roots: []string{root}, triagePath: db}
	r, e := a.SetTriageDetailed("a", "hold")
	if e != nil || r.Previous != "discard" {
		t.Fatalf("set %+v %v", r, e)
	}
	restored, e := a.RestoreTriage("a", r.UndoToken)
	if e != nil || restored.State != "discard" {
		t.Fatalf("restore %+v %v", restored, e)
	}
}
