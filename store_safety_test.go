package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/testutil"
)

// A non-candidate verdict must never be promoted by the legacy move API,
// even if the files happen to match when a move is requested.
func TestMoveRejectsUnverifiedAndAmbiguousVerdicts(t *testing.T) {
	for _, verdict := range []model.Verdict{model.VerdictUnverified, model.VerdictAmbiguous, model.VerdictMismatch} {
		t.Run(string(verdict), func(t *testing.T) {
			a, id, zip, folder := safetyPair(t)
			a.items[a.index[id]].Verdict = verdict
			calls := 0
			a.mover = func(string) error { calls++; return nil }
			response, err := a.DeleteSelected([]MoveRequest{{ID: id, Method: MoveMethodFolder}}, true)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 0 || response.Succeeded != 0 {
				t.Fatalf("%s was moved: calls=%d response=%+v", verdict, calls, response)
			}
			for _, path := range []string{zip, folder} {
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("original disappeared: %s: %v", path, err)
				}
			}
		})
	}
}

// Revalidation must reject a changed archive even when the initial scan matched.
func TestMoveRejectsArchiveChangedAfterScan(t *testing.T) {
	a, id, zip, folder := safetyPair(t)
	if err := testutil.WriteZip(zip, []testutil.Entry{{Name: "x/a.txt", Content: "different"}}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	a.mover = func(string) error { calls++; return nil }
	response, err := a.DeleteSelected([]MoveRequest{{ID: id, Method: MoveMethodFolder}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || response.Succeeded != 0 || response.Skipped != 1 {
		t.Fatalf("changed archive authorized move: calls=%d response=%+v", calls, response)
	}
	if _, err := os.Stat(filepath.Join(folder, "a.txt")); err != nil {
		t.Fatalf("folder data missing: %v", err)
	}
}
