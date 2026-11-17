package main

import (
	"errors"
	"github.com/tonbiattack/archive-extract-cleaner/internal/testutil"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupMigrationSafety(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mover    func(string, string) error
		reappear bool
	}{
		{"failed", func(p, z string) error { return errors.New("failed") }, false},
		{"archive changed during move", func(p, z string) error {
			if err := os.RemoveAll(p); err != nil {
				return err
			}
			return os.WriteFile(z, []byte("replaced"), 0600)
		}, false},
		{"folder returns", func(p, z string) error { return os.RemoveAll(p) }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			if err := app.SetTriage(ids[0], "save"); err != nil {
				t.Fatal(err)
			}
			app.mover = func(p string) error { return tc.mover(p, zip) }
			if _, err := app.DeleteSelected([]MoveRequest{{ID: ids[0], Method: MoveMethodFolder}}, true); err != nil {
				t.Fatal(err)
			}
			if tc.reappear {
				if err := testutil.WriteFile(folder, "a.txt", "alpha"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := app.Scan([]string{root}); err != nil {
				t.Fatal(err)
			}
			if tc.name != "failed" && len(app.savedItemsLocked()) != 0 {
				t.Fatal("unsafe migration")
			}
			if tc.name == "failed" {
				d, err := readTriage(app.triageFile())
				if err != nil {
					t.Fatal(err)
				}
				if len(d) != 1 {
					t.Fatalf("failed cleanup migrated: %+v", d)
				}
			}
		})
	}
}
