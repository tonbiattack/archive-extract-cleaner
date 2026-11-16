package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tonbiattack/archive-extract-cleaner/internal/fswalk"
	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
)

func writeRaw(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

func sampleTree() *fswalk.Tree {
	return &fswalk.Tree{
		Files: map[string]fswalk.File{
			"a.txt": {Key: "a.txt", Path: "a.txt", Size: 5, ModTime: time.Unix(1000, 0)},
		},
		Dirs:      map[string]struct{}{"sub": {}},
		Size:      5,
		FileCount: 1,
		DirCount:  1,
	}
}

func TestFolderFingerprintStable(t *testing.T) {
	tree := sampleTree()
	first := FolderFingerprintFromTree(`C:\dl\photos`, tree)
	second := FolderFingerprintFromTree(`C:\dl\photos`, tree)
	if first.Hash != second.Hash {
		t.Fatal("fingerprint must be deterministic")
	}
	if !first.Same(second) {
		t.Fatal("identical trees must compare equal")
	}
}

func TestFolderFingerprintDetectsChanges(t *testing.T) {
	base := FolderFingerprintFromTree(`C:\dl\photos`, sampleTree())

	modified := sampleTree()
	modified.Files["a.txt"] = fswalk.File{Key: "a.txt", Path: "a.txt", Size: 6, ModTime: time.Unix(1000, 0)}
	if base.Same(FolderFingerprintFromTree(`C:\dl\photos`, modified)) {
		t.Fatal("size change must be detected")
	}

	touched := sampleTree()
	touched.Files["a.txt"] = fswalk.File{Key: "a.txt", Path: "a.txt", Size: 5, ModTime: time.Unix(2000, 0)}
	if base.Same(FolderFingerprintFromTree(`C:\dl\photos`, touched)) {
		t.Fatal("mtime change must be detected")
	}

	added := sampleTree()
	added.Files["b.txt"] = fswalk.File{Key: "b.txt", Path: "b.txt", Size: 1, ModTime: time.Unix(1000, 0)}
	added.FileCount = 2
	if base.Same(FolderFingerprintFromTree(`C:\dl\photos`, added)) {
		t.Fatal("added file must be detected")
	}

	renamed := sampleTree()
	delete(renamed.Files, "a.txt")
	renamed.Files["c.txt"] = fswalk.File{Key: "c.txt", Path: "c.txt", Size: 5, ModTime: time.Unix(1000, 0)}
	if base.Same(FolderFingerprintFromTree(`C:\dl\photos`, renamed)) {
		t.Fatal("rename must be detected")
	}

	emptyDir := sampleTree()
	emptyDir.Dirs = map[string]struct{}{}
	emptyDir.DirCount = 0
	if base.Same(FolderFingerprintFromTree(`C:\dl\photos`, emptyDir)) {
		t.Fatal("empty directory change must be detected")
	}
}

func TestScanStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scan.json")
	store := NewScanStore(path)
	record := NewRootRecord(`C:\dl`, true, []ItemRecord{
		{ID: "a", Verdict: "DELETE_CANDIDATE", Cacheable: true},
		{ID: "b", Verdict: "MISMATCH", Cacheable: true},
	}, time.Now())
	store.Put(record)

	got, ok := store.Lookup(`C:\dl`, true, ArchiveEpoch())
	if !ok {
		t.Fatal("lookup failed")
	}
	if len(got.Items) != 2 {
		t.Fatalf("items = %+v", got.Items)
	}
	if item, found := got.FindItem("b"); !found || item.Verdict != "MISMATCH" {
		t.Fatalf("item = %+v", item)
	}
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}
	reloaded := NewScanStore(path)
	if _, ok := reloaded.Lookup(`C:\dl`, true, ArchiveEpoch()); !ok {
		t.Fatal("lookup after reload failed")
	}
}

func TestScanStoreRejectsDifferentMode(t *testing.T) {
	store := NewScanStore("")
	store.Put(NewRootRecord(`C:\dl`, true, nil, time.Now()))
	if _, ok := store.Lookup(`C:\dl`, false, ArchiveEpoch()); ok {
		t.Fatal("recursive flag mismatch must miss")
	}
	if _, ok := store.Lookup(`C:\dl`, true, ArchiveEpoch()+1); ok {
		t.Fatal("archive epoch mismatch must miss")
	}
}

func TestScanStoreCap(t *testing.T) {
	store := NewScanStore("")
	store.SetLimit(1)
	store.Put(NewRootRecord("a", true, nil, time.Now()))
	store.Put(NewRootRecord("b", true, nil, time.Now()))
	if store.Len() != 1 {
		t.Fatalf("len = %d", store.Len())
	}
}

func TestScanStoreClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scan.json")
	store := NewScanStore(path)
	store.Put(NewRootRecord(`C:\dl`, true, nil, time.Now()))
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := store.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Lookup(`C:\dl`, true, ArchiveEpoch()); ok {
		t.Fatal("cleared store must miss")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("cache file should be removed")
	}
}

func TestFolderIndexAndZipIndex(t *testing.T) {
	key := ArchiveKey{Path: `C:\dl\photos.zip`, Size: 1, StructureHash: "h", SchemaVersion: SchemaVersion}
	record := NewRootRecord(`C:\dl`, true, []ItemRecord{
		{ID: "a", Folder: &FolderFingerprint{Path: `C:\dl\photos`, Hash: "h1"}, ZipKey: &key},
	}, time.Now())

	if _, ok := record.FolderIndex()[winpath.Key(`C:\dl\photos`)]; !ok {
		t.Fatal("folder index miss")
	}
	if _, ok := record.ZipIndex()[winpath.Key(`C:\dl\photos.zip`)]; !ok {
		t.Fatal("zip index miss")
	}
}
