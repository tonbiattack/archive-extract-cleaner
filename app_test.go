package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/oplog"
	"github.com/tonbiattack/archive-extract-cleaner/internal/preview"
	"github.com/tonbiattack/archive-extract-cleaner/internal/testutil"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	app := NewApp()
	app.triagePath = filepath.Join(t.TempDir(), "triage.json")
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
	options := app.GetMoveOptions(ids)
	if len(options) != 1 || !options[0].CanFolder || !options[0].CanZip || !options[0].CanBoth {
		t.Fatalf("options = %+v", options)
	}
	requests := []MoveRequest{{ID: ids[0], Method: MoveMethodFolder}}
	preview, err := app.PreviewMove(requests)
	if err != nil {
		t.Fatal(err)
	}
	if preview.TargetCount != 1 || preview.Bytes <= 0 {
		t.Fatalf("preview = %+v", preview)
	}
	if _, err := app.DeleteSelected(requests, false); err == nil {
		t.Fatal("delete without confirmation should fail")
	}
	if _, err := os.Stat(folder); err != nil {
		t.Fatal("folder was removed without confirmation")
	}

	response, err := app.DeleteSelected(requests, true)
	if err != nil {
		t.Fatal(err)
	}
	if response.Succeeded != 1 || response.Failed != 0 {
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
	if entry.Method != string(MoveMethodFolder) || len(entry.MovedTargets) != 1 {
		t.Fatalf("log method/targets = %+v", entry)
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
	response, err := app.DeleteSelected([]MoveRequest{{ID: ids[0], Method: MoveMethodFolder}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if response.Succeeded != 0 || response.Skipped != 1 || response.Failed != 0 {
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

func TestOpenInExplorerMissingPath(t *testing.T) {
	app := newTestApp(t)
	if err := app.OpenInExplorer(filepath.Join(t.TempDir(), "missing.zip")); err == nil {
		t.Fatal("missing path must fail")
	}
}

func TestAppPreviewZipAndFolder(t *testing.T) {
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
	zipPreview := app.GetPreview(ids[0], "")
	if zipPreview.Error != "" || zipPreview.Source != "zip" || len(zipPreview.Sources) != 2 {
		t.Fatalf("zip preview = %+v", zipPreview)
	}
	if !previewHasPath(zipPreview.Entries, "photos/a.txt") {
		t.Fatalf("zip entries = %+v", zipPreview.Entries)
	}
	folderPreview := app.GetPreview(ids[0], "folder")
	if folderPreview.Error != "" || folderPreview.Source != "folder" {
		t.Fatalf("folder preview = %+v", folderPreview)
	}
	if !previewHasPath(folderPreview.Entries, "a.txt") {
		t.Fatalf("folder entries = %+v", folderPreview.Entries)
	}

	if _, err := app.DeleteSelected([]MoveRequest{{ID: ids[0], Method: MoveMethodFolder}}, true); err != nil {
		t.Fatal(err)
	}
	unavailable := app.GetPreview(ids[0], "folder")
	if unavailable.Error == "" || unavailable.Source != "folder" {
		t.Fatalf("unavailable preview = %+v", unavailable)
	}
	remaining := app.GetPreview(ids[0], "")
	if remaining.Error != "" || remaining.Source != "zip" {
		t.Fatalf("remaining preview = %+v", remaining)
	}
}

func previewHasPath(entries []preview.Entry, path string) bool {
	for _, entry := range entries {
		if entry.Path == path {
			return true
		}
	}
	return false
}

func TestLegacyZipAndBothDeletionRejected(t *testing.T) {
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
	for _, method := range []MoveMethod{MoveMethodZip, MoveMethodBoth} {
		requests := []MoveRequest{{ID: ids[0], Method: method}}
		if _, err := app.PreviewMove(requests); err != nil {
			t.Fatal(err)
		}
		if _, err := app.DeleteSelected(requests, true); err == nil {
			t.Fatalf("legacy %s accepted", method)
		}
		if _, err := os.Stat(zipPath); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(folder); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAppFolderFailureAndRetry(t *testing.T) {
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
	requests := []MoveRequest{{ID: ids[0], Method: MoveMethodFolder}}
	app.mover = func(string) error { return errors.New("folder locked") }
	response, err := app.DeleteSelected(requests, true)
	if err != nil || response.Failed != 1 {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	if _, err := os.Stat(folder); err != nil {
		t.Fatal(err)
	}
	app.mover = func(path string) error { return os.RemoveAll(path) }
	response, err = app.DeleteSelected(requests, true)
	if err != nil || response.Succeeded != 1 {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Fatalf("folder remains: %v", err)
	}
	if _, err := os.Stat(zipPath); err != nil {
		t.Fatal(err)
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
	if _, err := app.SetTriageDetailed(app.items[0].ID, "save"); err != nil {
		t.Fatal(err)
	}

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
	if len(review.Candidates) != 0 {
		t.Fatalf("stale saved item offered: %+v", review.Candidates)
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

func TestSuggestForItemRefusesDeletableCandidate(t *testing.T) {
	root := t.TempDir()
	if err := testutil.WriteZip(filepath.Join(root, "photos.zip"), []testutil.Entry{{Name: "photos/a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	app := newTestApp(t)
	if _, err := app.Scan([]string{root}); err != nil {
		t.Fatal(err)
	}
	ids := app.CandidateIDs(RowQuery{Filter: "candidate"})
	if len(ids) != 1 {
		t.Fatalf("candidates = %v", ids)
	}
	if _, err := app.SuggestForItem(ids[0]); err == nil {
		t.Fatal("AI must not be asked about an already-decided candidate")
	}
}

func TestSuggestForItemRefusesUnverified(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "broken.zip"), []byte("broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "broken"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	app := newTestApp(t)
	if _, err := app.Scan([]string{root}); err != nil {
		t.Fatal(err)
	}
	rows := app.GetRows(RowQuery{Filter: "unverified"})
	if len(rows.Rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	if _, err := app.SuggestForItem(rows.Rows[0].ID); err == nil {
		t.Fatal("UNVERIFIED items must not be sent to AI")
	}
}

func TestSuggestForItemDisabledByDefault(t *testing.T) {
	root := t.TempDir()
	if err := testutil.WriteZip(filepath.Join(root, "photos.zip"), []testutil.Entry{{Name: "photos/a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos"), "a.txt", "changed"); err != nil {
		t.Fatal(err)
	}
	app := newTestApp(t)
	if _, err := app.Scan([]string{root}); err != nil {
		t.Fatal(err)
	}
	rows := app.GetRows(RowQuery{Filter: "mismatch"})
	if len(rows.Rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	info, err := app.SuggestForItem(rows.Rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Error == "" {
		t.Fatal("disabled AI should report that it is disabled")
	}
	// AIの提案があっても削除候補にはならない。
	if len(app.CandidateIDs(RowQuery{Filter: "candidate"})) != 0 {
		t.Fatal("AI suggestion must not create delete candidates")
	}
}

func TestAISuggestionDoesNotEnableDeletion(t *testing.T) {
	root := t.TempDir()
	if err := testutil.WriteZip(filepath.Join(root, "photos.zip"), []testutil.Entry{{Name: "photos/a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos"), "a.txt", "changed"); err != nil {
		t.Fatal(err)
	}
	app := newTestApp(t)
	if _, err := app.Scan([]string{root}); err != nil {
		t.Fatal(err)
	}
	rows := app.GetRows(RowQuery{Filter: "mismatch"})
	if len(rows.Rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	id := rows.Rows[0].ID
	app.suggestions.set(id, SuggestionInfo{ID: id, Decision: "delete_folder", Confidence: 0.99})

	detail, err := app.GetItemDetail(id)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Deletable() {
		t.Fatal("AI suggestion must not make an item deletable")
	}
	options := app.GetMoveOptions([]string{id})
	if len(options) != 1 {
		t.Fatalf("options = %+v", options)
	}
	if options[0].CanFolder || options[0].CanZip || options[0].CanBoth {
		t.Fatalf("AI suggestion must not allow moves: %+v", options[0])
	}
}
