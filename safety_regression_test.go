package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/testutil"
)

func safetyPair(t *testing.T) (*App, string, string, string) {
	t.Helper()
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
	ids := a.CandidateIDs(RowQuery{Filter: "candidate"})
	if len(ids) != 1 {
		t.Fatal(ids)
	}
	return a, ids[0], zip, folder
}
func TestLegacyFolderRequiresPresentArchiveAfterPartialDiscard(t *testing.T) {
	a, id, zip, folder := safetyPair(t)
	if err := os.Remove(zip); err != nil {
		t.Fatal(err)
	}
	a.items[a.index[id]].ZipMoved = true
	_, _ = a.DeleteSelected([]MoveRequest{{ID: id, Method: MoveMethodFolder}}, true)
	if _, err := os.Stat(folder); err != nil {
		t.Fatalf("last copy removed: %v", err)
	}
}
func TestLegacyFolderRejectsOtherScannedPaths(t *testing.T) {
	a, id, _, folder := safetyPair(t)
	a.items = append(a.items, model.Item{ID: "other", ZipPath: filepath.Join(folder, "a.txt")})
	a.index["other"] = len(a.items) - 1
	_, _ = a.DeleteSelected([]MoveRequest{{ID: id, Method: MoveMethodFolder}}, true)
	if _, err := os.Stat(folder); err != nil {
		t.Fatalf("overlapping folder removed: %v", err)
	}
}
func TestTransferLaunchFailsWhenTriageLocked(t *testing.T) {
	a, id, zip, _ := safetyPair(t)
	if err := a.SetTriage(id, "save"); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockTriage(a.triageFile())
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	_, err = a.LaunchMultiDriveCopy([]string{zip})
	if err == nil {
		t.Fatal("launch ignored triage lock")
	}
}
func TestSavedTriageAvailableBeforeRescan(t *testing.T) {
	a, id, zip, _ := safetyPair(t)
	if err := a.SetTriage(id, "save"); err != nil {
		t.Fatal(err)
	}
	r, err := a.DeleteSelected([]MoveRequest{{ID: id, Method: MoveMethodFolder}}, true)
	if err != nil || r.Succeeded != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	for _, c := range a.GetTransferReview().Candidates {
		if c.Path == zip {
			return
		}
	}
	t.Fatal("saved archive unavailable before rescan")
}
