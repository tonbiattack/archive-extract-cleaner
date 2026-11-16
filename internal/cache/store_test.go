package cache

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type samplePayload struct {
	Value string `json:"value"`
}

func TestStoreMissingReturnsErrMiss(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "missing.json"))
	var payload samplePayload
	if err := store.Load("kind", &payload); !errors.Is(err, ErrMiss) {
		t.Fatalf("err = %v, want ErrMiss", err)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	store := NewStore(path)
	if err := store.Save("kind", samplePayload{Value: "hello"}); err != nil {
		t.Fatal(err)
	}
	var payload samplePayload
	if err := store.Load("kind", &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Value != "hello" {
		t.Fatalf("value = %q", payload.Value)
	}
}

func TestStoreWrongKindIsMiss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	store := NewStore(path)
	if err := store.Save("kind-a", samplePayload{Value: "a"}); err != nil {
		t.Fatal(err)
	}
	var payload samplePayload
	if err := store.Load("kind-b", &payload); !errors.Is(err, ErrMiss) {
		t.Fatalf("err = %v, want ErrMiss", err)
	}
}

func TestStoreCorruptIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	var payload samplePayload
	err := store.Load("kind", &payload)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
}

func TestStoreSchemaMismatchIsMiss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	if err := os.WriteFile(path, []byte(`{"version":9999,"kind":"kind","payload":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	var payload samplePayload
	if err := store.Load("kind", &payload); !errors.Is(err, ErrMiss) {
		t.Fatalf("err = %v, want ErrMiss", err)
	}
}

func TestStoreSaveLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")
	store := NewStore(path)
	for i := 0; i < 3; i++ {
		if err := store.Save("kind", samplePayload{Value: "v"}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "cache.json" {
			t.Fatalf("unexpected file: %s", entry.Name())
		}
	}
}

func TestStoreOverwritesAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	store := NewStore(path)
	if err := store.Save("kind", samplePayload{Value: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("kind", samplePayload{Value: "second"}); err != nil {
		t.Fatal(err)
	}
	var payload samplePayload
	if err := store.Load("kind", &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Value != "second" {
		t.Fatalf("value = %q", payload.Value)
	}
}

func TestStoreRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	store := NewStore(path)
	if err := store.Save("kind", samplePayload{Value: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove(); err != nil {
		t.Fatal(err)
	}
	var payload samplePayload
	if err := store.Load("kind", &payload); !errors.Is(err, ErrMiss) {
		t.Fatalf("err = %v, want ErrMiss", err)
	}
}

func TestCapMapKeepsNewest(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	entries := map[string]time.Time{
		"old":   base,
		"mid":   base.Add(time.Hour),
		"newer": base.Add(2 * time.Hour),
		"new":   base.Add(3 * time.Hour),
	}
	CapMap(entries, 2, func(v time.Time) time.Time { return v }, func(key string) {
		delete(entries, key)
	})
	if len(entries) != 2 {
		t.Fatalf("entries = %v", entries)
	}
	if _, ok := entries["old"]; ok {
		t.Fatal("oldest entry should be removed")
	}
	if _, ok := entries["new"]; !ok {
		t.Fatal("newest entry should be kept")
	}
}
