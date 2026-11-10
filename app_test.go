package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/oplog"
	"github.com/tonbiattack/archive-extract-cleaner/internal/testutil"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	app := NewApp()
	app.log = oplog.New(filepath.Join(t.TempDir(), "operations.jsonl"))
	app.logPath = app.log.Path()
	app.limits = model.DefaultLimits()
	app.mover = func(path string) error { return os.RemoveAll(path) }
	return app
}

func TestAppScanRowsAndDelete(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "photos.zip")
	folder := filepath.Join(root, "photos")
	if err := testutil.WriteZip(zipPath, []testutil.Entry{{Name: "photos/a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(folder, "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteZip(filepath.Join(root, "docs.zip"), []testutil.Entry{{Name: "docs/a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "music"), "song.txt", "music"); err != nil {
		t.Fatal(err)
	}

	app := newTestApp(t)
	info, err := app.Scan([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if info.Summary.CandidateCount != 1 {
		t.Fatalf("candidates = %d", info.Summary.CandidateCount)
	}

	rows := app.GetRows(RowQuery{Filter: "candidate"})
	if rows.Total != 1 || len(rows.Rows) != 1 {
		t.Fatalf("candidate rows = %+v", rows)
	}
	if !rows.Rows[0].Deletable {
		t.Fatal("row should be deletable")
	}
	if rows.Rows[0].FolderPath != folder {
		t.Fatalf("folder = %s", rows.Rows[0].FolderPath)
	}

	ids := app.CandidateIDs(RowQuery{Filter: "candidate"})
	if len(ids) != 1 {
		t.Fatalf("candidate ids = %v", ids)
	}
	preview := app.PreviewDelete(ids)
	if preview.Count != 1 || preview.Bytes <= 0 {
		t.Fatalf("preview = %+v", preview)
	}
	if _, err := app.DeleteSelected(ids, false); err == nil {
		t.Fatal("delete without confirmation should fail")
	}
	if _, err := os.Stat(folder); err != nil {
		t.Fatal("folder was removed without confirmation")
	}

	response, err := app.DeleteSelected(ids, true)
	if err != nil {
		t.Fatal(err)
	}
	if response.Deleted != 1 || response.Failed != 0 {
		t.Fatalf("response = %+v", response)
	}
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Fatalf("folder still exists: %v", err)
	}
	if _, err := os.Stat(zipPath); err != nil {
		t.Fatalf("zip was removed: %v", err)
	}
	if after := app.GetRows(RowQuery{Filter: "candidate"}); after.Total != 0 {
		t.Fatalf("candidate rows after delete = %d", after.Total)
	}
	state := app.GetState()
	if state.Summary.CandidateCount != 0 || state.Summary.DeletedCount != 1 {
		t.Fatalf("summary = %+v", state.Summary)
	}
	if after := app.GetRows(RowQuery{Filter: "keep"}); after.Total < 2 {
		t.Fatalf("keep rows = %d", after.Total)
	}

	logData, err := os.ReadFile(app.log.Path())
	if err != nil {
		t.Fatal(err)
	}
	firstLine := strings.SplitN(string(logData), "\n", 2)[0]
	var entry oplog.Entry
	if err := json.Unmarshal([]byte(firstLine), &entry); err != nil {
		t.Fatal(err)
	}
	if !entry.Success || entry.Verdict != string(model.VerdictDeleteCandidate) {
		t.Fatalf("log entry = %+v", entry)
	}
	if entry.Action != "recycle" {
		t.Fatalf("log action = %q", entry.Action)
	}
	if entry.ZipPath != zipPath || entry.FolderPath != folder {
		t.Fatalf("log paths = %+v", entry)
	}
}

func TestAppDeleteSkipsChangedTarget(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "photos.zip")
	folder := filepath.Join(root, "photos")
	if err := testutil.WriteZip(zipPath, []testutil.Entry{{Name: "photos/a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(folder, "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	app := newTestApp(t)
	if _, err := app.Scan([]string{root}); err != nil {
		t.Fatal(err)
	}
	ids := app.CandidateIDs(RowQuery{Filter: "candidate"})
	if len(ids) != 1 {
		t.Fatalf("ids = %v", ids)
	}
	if err := os.WriteFile(filepath.Join(folder, "a.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	response, err := app.DeleteSelected(ids, true)
	if err != nil {
		t.Fatal(err)
	}
	if response.Deleted != 0 || response.Skipped != 1 || response.Failed != 0 {
		t.Fatalf("response = %+v", response)
	}
	if _, err := os.Stat(folder); err != nil {
		t.Fatalf("changed folder was removed: %v", err)
	}
	if _, err := os.Stat(zipPath); err != nil {
		t.Fatalf("zip was removed: %v", err)
	}
	row := app.GetRows(RowQuery{Filter: "all"}).Rows[0]
	if row.Verdict != string(model.VerdictMismatch) || row.Status != string(model.StatusError) {
		t.Fatalf("row = %+v", row)
	}
}

func TestAppTransferReview(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "photos.zip")
	folder := filepath.Join(root, "photos")
	if err := testutil.WriteZip(zipPath, []testutil.Entry{{Name: "photos/a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(folder, "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	app := newTestApp(t)
	if _, err := app.Scan([]string{root}); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "MultiDriveCopy.ps1")
	if err := os.WriteFile(script, []byte("# test"), 0o600); err != nil {
		t.Fatal(err)
	}
	app.multiDriveCopyScript = script
	app.multiDriveCopyPreset = "Backup"

	review := app.GetTransferReview()
	if !review.ScriptAvailable || review.Preset != "Backup" {
		t.Fatalf("review = %+v", review)
	}
	found := false
	for _, candidate := range review.Candidates {
		if candidate.Path == zipPath {
			found = true
			if !candidate.Selected || candidate.Review || candidate.Kind != "zip" {
				t.Fatalf("zip candidate = %+v", candidate)
			}
		}
		if candidate.Path == folder {
			t.Fatalf("folder should be omitted when the matching ZIP exists: %+v", candidate)
		}
	}
	if !found {
		t.Fatalf("zip candidate not found: %+v", review.Candidates)
	}

	outside := filepath.Join(t.TempDir(), "outside.zip")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.LaunchMultiDriveCopy([]string{outside}); err == nil {
		t.Fatal("unscanned path must be rejected")
	}

	if err := os.RemoveAll(folder); err != nil {
		t.Fatal(err)
	}
	review = app.GetTransferReview()
	if review.MissingCount != 1 {
		t.Fatalf("missing count = %d, review = %+v", review.MissingCount, review.Candidates)
	}
	for _, candidate := range review.Candidates {
		if candidate.Path == folder && (candidate.Exists || candidate.Selected || !candidate.Review) {
			t.Fatalf("missing folder candidate = %+v", candidate)
		}
	}
}

func TestCopyTextRequiresInitializedContext(t *testing.T) {
	app := NewApp()
	if err := app.CopyText("hello"); err == nil {
		t.Fatal("CopyText should fail before Wails startup")
	}
}

func TestFilterAndSortRows(t *testing.T) {
	items := []model.Item{
		{ID: "1", Verdict: model.VerdictDeleteCandidate, Status: model.StatusReady, Name: "b-candidate", ZipPath: "C:\\x\\b.zip", FolderPath: "C:\\x\\b", ParentPath: "C:\\x", Size: 30},
		{ID: "2", Verdict: model.VerdictMismatch, Status: model.StatusReady, Name: "a-mismatch", ZipPath: "C:\\x\\a.zip", FolderPath: "C:\\x\\a", ParentPath: "C:\\x", Size: 10},
		{ID: "3", Verdict: model.VerdictKeepArchive, Status: model.StatusReady, Name: "c-keep", ZipPath: "C:\\x\\c.zip", ParentPath: "C:\\x", Size: 20},
		{ID: "4", Verdict: model.VerdictKeepDirectory, Status: model.StatusReady, Name: "d-folder", FolderPath: "C:\\x\\d", ParentPath: "C:\\x", Size: 40},
	}
	filtered := filterItems(items, RowQuery{Filter: "candidate"})
	if len(filtered) != 1 || filtered[0].ID != "1" {
		t.Fatalf("filtered = %+v", filtered)
	}
	filtered = filterItems(items, RowQuery{Filter: "keep"})
	if len(filtered) != 2 {
		t.Fatalf("keep filtered = %+v", filtered)
	}
	filtered = filterItems(items, RowQuery{Search: "mismatch"})
	if len(filtered) != 1 || filtered[0].ID != "2" {
		t.Fatalf("search filtered = %+v", filtered)
	}
	sortItems(items, "size", false)
	if items[0].ID != "2" || items[3].ID != "4" {
		t.Fatalf("ascending sort = %+v", items)
	}
	sortItems(items, "size", true)
	if items[0].ID != "4" || items[3].ID != "2" {
		t.Fatalf("descending sort = %+v", items)
	}
	sortItems(items, "verdict", false)
	if items[0].ID != "1" || items[1].ID != "2" {
		t.Fatalf("verdict sort = %+v", items)
	}
}
