package main

import (
	"os"
	"testing"
)

func TestLegacyCleanupFailsClosedWhenTriageLocked(t *testing.T) {
	a, id, zip, folder := safetyPair(t)
	called := false
	a.mover = func(string) error { called = true; return nil }
	unlock, err := lockTriage(a.triageFile())
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	_, err = a.DeleteSelected([]MoveRequest{{ID: id, Method: MoveMethodFolder}}, true)
	if err == nil {
		t.Error("cleanup ignored triage lock")
	}
	if called {
		t.Error("cleanup called mover without lock")
	}
	for _, path := range []string{zip, folder} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("lost %s: %v", path, err)
		}
	}
}

func TestLegacyCleanupExcludedWhileDiscardMovesArchive(t *testing.T) {
	legacy, id, zip, folder := safetyPair(t)
	discard := newTestApp(t)
	discard.triagePath = legacy.triagePath
	if _, err := discard.Scan(legacy.roots); err != nil {
		t.Fatal(err)
	}
	if err := discard.SetTriage(id, "discard"); err != nil {
		t.Fatal(err)
	}
	preview, err := discard.PreviewDiscard([]string{id})
	if err != nil {
		t.Fatal(err)
	}
	legacyCalled := false
	legacy.mover = func(string) error { legacyCalled = true; return nil }
	discard.mover = func(path string) error {
		if path == zip {
			_, err := legacy.DeleteSelected([]MoveRequest{{ID: id, Method: MoveMethodFolder}}, true)
			if err == nil {
				t.Error("cleanup bypassed lock during discard")
			}
			if legacyCalled {
				t.Error("cleanup moved folder during discard")
			}
		}
		return os.ErrPermission
	}
	if _, err := discard.RecycleDiscard(preview.Token, true); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{zip, folder} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("lost %s: %v", path, err)
		}
	}
}

func TestDiscardExcludedWhileLegacyCleanupMovesFolder(t *testing.T) {
	legacy, id, zip, folder := safetyPair(t)
	discard := newTestApp(t)
	discard.triagePath = legacy.triagePath
	if _, err := discard.Scan(legacy.roots); err != nil {
		t.Fatal(err)
	}
	if err := discard.SetTriage(id, "discard"); err != nil {
		t.Fatal(err)
	}
	preview, err := discard.PreviewDiscard([]string{id})
	if err != nil {
		t.Fatal(err)
	}
	discardCalled := false
	discard.mover = func(string) error { discardCalled = true; return nil }
	legacy.mover = func(string) error {
		_, err := discard.RecycleDiscard(preview.Token, true)
		if err == nil {
			t.Error("discard bypassed lock during cleanup")
		}
		if discardCalled {
			t.Error("discard moved archive during cleanup")
		}
		return os.ErrPermission
	}
	if _, err := legacy.DeleteSelected([]MoveRequest{{ID: id, Method: MoveMethodFolder}}, true); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{zip, folder} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("lost %s: %v", path, err)
		}
	}
}
