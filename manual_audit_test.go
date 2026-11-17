package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
)

func TestManualTriageAuditFailureIsReported(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.zip")
	if err := os.WriteFile(p, []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(t.TempDir(), "audit")
	if err := os.Mkdir(bad, 0700); err != nil {
		t.Fatal(err)
	}
	a := &App{items: []model.Item{{ID: "a", ZipPath: p}}, index: map[string]int{"a": 0}, roots: []string{root}, triagePath: filepath.Join(t.TempDir(), "triage.json"), logPath: bad}
	if result, err := a.SetTriageDetailed("a", "discard"); err != nil || !strings.Contains(result.Warning, "decision saved, but audit failed") {
		t.Fatalf("result %+v error %v", result, err)
	}
	decisions, err := readTriage(a.triageFile())
	if err != nil || decisions["a"].State != "discard" {
		t.Fatalf("decision=%v err=%v", decisions, err)
	}
}

func TestManualAuditFailureReportsMovedTargetAndConsumesPreview(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.zip")
	if err := os.WriteFile(p, []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "audit.jsonl")
	calls := 0
	a := &App{items: []model.Item{{ID: "a", ZipPath: p}}, index: map[string]int{"a": 0}, roots: []string{root}, triagePath: filepath.Join(t.TempDir(), "triage.json"), logPath: log, mover: func(path string) error { calls++; return os.Remove(path) }}
	if _, err := a.SetTriageDetailed("a", "discard"); err != nil {
		t.Fatal(err)
	}
	preview, err := a.PreviewDiscard([]string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(log); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(log, 0700); err != nil {
		t.Fatal(err)
	}
	results, err := a.RecycleDiscardDetailed(preview.Token, true)
	if err != nil || !strings.Contains(results.Warning, "audit failed") || len(results.Results) != 1 || !results.Results[0].Success {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	if _, err := a.RecycleDiscardDetailed(preview.Token, true); err == nil || calls != 1 {
		t.Fatalf("preview reused: calls=%d err=%v", calls, err)
	}
}

func TestManualAuditRecordsDecisionAndPartialRecycle(t *testing.T) {
	root := t.TempDir()
	one, two := filepath.Join(root, "a.zip"), filepath.Join(root, "b.zip")
	for _, p := range []string{one, two} {
		if err := os.WriteFile(p, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	audit := filepath.Join(t.TempDir(), "operations.jsonl")
	a := &App{items: []model.Item{{ID: "a", ZipPath: one}, {ID: "b", ZipPath: two}}, index: map[string]int{"a": 0, "b": 1}, roots: []string{root}, triagePath: filepath.Join(t.TempDir(), "triage.json"), logPath: audit, mover: func(p string) error {
		if p == two {
			return os.ErrPermission
		}
		return os.Remove(p)
	}}
	for _, id := range []string{"a", "b"} {
		if _, err := a.SetTriageDetailed(id, "discard"); err != nil {
			t.Fatal(err)
		}
	}
	p, err := a.PreviewDiscard([]string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	results, err := a.RecycleDiscardDetailed(p.Token, true)
	if err != nil || results.Warning != "" || len(results.Results) != 2 || !results.Results[0].Success || results.Results[1].Success {
		t.Fatalf("%+v %v", results, err)
	}
	data, err := os.ReadFile(audit)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 4 {
		t.Fatalf("audit: %s", data)
	}
	var records []map[string]any
	for _, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatal(err)
		}
		records = append(records, m)
	}
	if records[0]["action"] != "triage" || records[0]["oldState"] != "unjudged" || records[0]["newState"] != "discard" || records[0]["id"] != "a" || records[0]["time"] == nil {
		t.Fatalf("triage audit: %v", records[0])
	}
	for i, p := range []string{one, two} {
		m := records[i+2]
		if m["action"] != "recycle" || m["path"] != p || m["reason"] != "manual_discard" || m["success"] != (i == 0) || m["time"] == nil {
			t.Fatalf("recycle audit: %v", m)
		}
	}
}
