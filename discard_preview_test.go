package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
)

func TestDiscardPreviewReportsCountsAndBytes(t *testing.T) {
	root := t.TempDir()
	zip := filepath.Join(root, "a.zip")
	folder := filepath.Join(root, "a")
	if err := os.MkdirAll(filepath.Join(folder, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string]string{zip: "123", filepath.Join(folder, "nested", "b"): "12345"} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a := &App{items: []model.Item{{ID: "a", ZipPath: zip, FolderPath: folder}}, index: map[string]int{"a": 0}, roots: []string{root}, triagePath: filepath.Join(t.TempDir(), "triage.json")}
	if _, err := a.SetTriageDetailed("a", "discard"); err != nil {
		t.Fatal(err)
	}
	p, err := a.PreviewDiscard([]string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err = json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["itemCount"] != float64(1) || fields["targetCount"] != float64(2) || fields["bytes"] != float64(8) {
		t.Fatalf("incorrect preview: %s", b)
	}
	if fields["fileCount"] != float64(1) || fields["folderCount"] != float64(1) {
		t.Fatalf("incorrect typed counts: %s", b)
	}
	targets, ok := fields["targets"].([]any)
	if !ok || len(targets) != 2 {
		t.Fatalf("incorrect targets: %s", b)
	}
	for i, want := range []struct{ path, kind string }{{zip, "file"}, {folder, "folder"}} {
		target, ok := targets[i].(map[string]any)
		if !ok || target["path"] != want.path || target["kind"] != want.kind || target["reason"] != "manual_discard" || target["itemId"] != "a" {
			t.Fatalf("incorrect target: %s", b)
		}
	}
	if len(p.Paths) != 2 || p.Paths[0] != zip || p.Paths[1] != folder || p.Token == "" {
		t.Fatalf("unexpected targets: %+v", p)
	}
}

func TestDiscardPreviewActualKindsAcrossReviewClasses(t *testing.T) {
	for _, tc := range []struct {
		name              string
		verdict           model.Verdict
		fileInFolderField bool
		folderInZipField  bool
	}{
		{name: "zip-only", verdict: model.VerdictKeepArchive},
		{name: "folder-only", verdict: model.VerdictKeepDirectory, folderInZipField: true},
		{name: "mismatch", verdict: model.VerdictMismatch, fileInFolderField: true},
		{name: "unverified", verdict: model.VerdictUnverified, folderInZipField: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, "file.zip")
			folder := filepath.Join(root, "folder")
			if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(folder, 0700); err != nil {
				t.Fatal(err)
			}
			item := model.Item{ID: "x", Verdict: tc.verdict}
			path, kind := file, "file"
			if tc.folderInZipField {
				item.ZipPath = folder
				path, kind = folder, "folder"
			} else if tc.fileInFolderField {
				item.FolderPath = file
			} else {
				item.ZipPath = file
			}
			a := &App{items: []model.Item{item}, index: map[string]int{"x": 0}, roots: []string{root}, triagePath: filepath.Join(t.TempDir(), "triage.json")}
			if _, err := a.PreviewDiscard([]string{"x"}); err == nil {
				t.Fatal("unjudged accepted")
			}
			if _, err := a.SetTriageDetailed("x", "discard"); err != nil {
				t.Fatal(err)
			}
			preview, err := a.PreviewDiscard([]string{"x"})
			if err != nil {
				t.Fatal(err)
			}
			if len(preview.Targets) != 1 || preview.Targets[0].Path != path || preview.Targets[0].Kind != kind || preview.FileCount+preview.FolderCount != 1 {
				t.Fatalf("%+v", preview)
			}
		})
	}
}

func TestDiscardPreviewMixedSelectionErrorIdentifiesItem(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.zip")
	if err := os.WriteFile(p, []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &App{items: []model.Item{{ID: "a", ZipPath: p}, {ID: "b", ZipPath: filepath.Join(root, "b.zip")}}, index: map[string]int{"a": 0, "b": 1}, roots: []string{root}, triagePath: filepath.Join(t.TempDir(), "triage.json")}
	if _, err := a.SetTriageDetailed("a", "discard"); err != nil {
		t.Fatal(err)
	}
	preview, err := a.PreviewDiscard([]string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "b") || !strings.Contains(err.Error(), "discard") {
		t.Fatalf("not actionable: preview=%+v err=%v", preview, err)
	}
	if preview.Token != "" || a.discardPreview != nil {
		t.Fatal("invalid selection produced authorization")
	}
}

func TestDiscardPreviewRejectsAmbiguousWithPaths(t *testing.T) {
	root := t.TempDir()
	zip := filepath.Join(root, "a.zip")
	related := filepath.Join(root, "candidate")
	a := &App{items: []model.Item{{ID: "ambiguous", ZipPath: zip, RelatedFolders: []string{related}}}, index: map[string]int{"ambiguous": 0}, roots: []string{root}, triagePath: filepath.Join(t.TempDir(), "triage.json")}
	p, err := a.PreviewDiscard([]string{"ambiguous"})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") || !strings.Contains(err.Error(), related) || p.Token != "" {
		t.Fatalf("error=%v preview=%+v", err, p)
	}
}

func TestDiscardPreviewRejectsNestedLink(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "folder")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(folder, "file")
	if err := os.WriteFile(file, []byte("123"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &App{items: []model.Item{{ID: "a", FolderPath: folder}}, index: map[string]int{"a": 0}, roots: []string{root}, triagePath: filepath.Join(t.TempDir(), "triage.json")}
	if _, err := a.SetTriageDetailed("a", "discard"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(file, filepath.Join(folder, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	preview, err := a.PreviewDiscard([]string{"a"})
	if err == nil || preview.Token != "" || a.discardPreview != nil {
		t.Fatalf("link accepted: %+v, %v", preview, err)
	}
}
