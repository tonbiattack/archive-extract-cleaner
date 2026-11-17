package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/testutil"
)

func TestSavedTriageSurvivesVerifiedFolderCleanupRescan(t *testing.T) {
	root := t.TempDir()
	zip := filepath.Join(root, "photos.zip")
	folder := filepath.Join(root, "photos")
	if err := testutil.WriteZip(zip, []testutil.Entry{{Name: "photos/a.txt", Content: "alpha"}}); err != nil {
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
		t.Fatal(ids)
	}
	if _, err := app.SetTriageDetailed(ids[0], "save"); err != nil {
		t.Fatal(err)
	}
	res, err := app.DeleteSelected([]MoveRequest{{ID: ids[0], Method: MoveMethodFolder}}, true)
	if err != nil || res.Succeeded != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err = app.Scan([]string{root}); err != nil {
		t.Fatal(err)
	}
	if got := app.savedItemsLocked(); len(got) != 1 {
		t.Fatalf("saved after rescan: %+v", got)
	}
	if err = os.WriteFile(zip, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := app.savedItemsLocked(); len(got) != 0 {
		t.Fatalf("saved after replacement: %+v", got)
	}
}
