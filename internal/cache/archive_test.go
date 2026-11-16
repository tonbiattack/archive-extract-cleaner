package cache

import (
	"path/filepath"
	"testing"
	"time"
)

func sampleRecord(path, hash string, accessed time.Time) ArchiveRecord {
	return ArchiveRecord{
		Key: ArchiveKey{
			Path:          path,
			Size:          100,
			ModTimeUnixNS: 12345,
			StructureHash: hash,
			SchemaVersion: SchemaVersion,
		},
		Entries: []ArchiveEntry{
			{Path: "a.txt", SHA256: "aa", Size: 10, CRC32: 1},
		},
		AccessedAt: accessed,
	}
}

func TestArchiveHashesRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archives.json")
	cache := NewArchiveHashes(path)
	record := sampleRecord(`C:\dl\photos.zip`, "struct", time.Now())
	cache.Put(record)

	got, ok := cache.Lookup(record.Key)
	if !ok {
		t.Fatal("lookup failed")
	}
	if len(got.Entries) != 1 || got.Entries[0].Path != "a.txt" {
		t.Fatalf("entries = %+v", got.Entries)
	}
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}

	reloaded := NewArchiveHashes(path)
	again, ok := reloaded.Lookup(record.Key)
	if !ok {
		t.Fatal("lookup after reload failed")
	}
	if again.Key.StructureHash != "struct" {
		t.Fatalf("key = %+v", again.Key)
	}
}

func TestArchiveHashesRejectsChangedStructure(t *testing.T) {
	cache := NewArchiveHashes("")
	record := sampleRecord(`C:\dl\photos.zip`, "struct-a", time.Now())
	cache.Put(record)

	changed := record.Key
	changed.StructureHash = "struct-b"
	if _, ok := cache.Lookup(changed); ok {
		t.Fatal("changed structure hash must miss")
	}

	resized := record.Key
	resized.Size = 101
	if _, ok := cache.Lookup(resized); ok {
		t.Fatal("changed size must miss")
	}

	touched := record.Key
	touched.ModTimeUnixNS = 99999
	if _, ok := cache.Lookup(touched); ok {
		t.Fatal("changed mtime must miss")
	}
}

func TestArchiveHashesSameSizeAndMtimeButDifferentStructureMisses(t *testing.T) {
	// 同サイズ・同時刻で中身だけ差し替えられた ZIP を想定する。
	cache := NewArchiveHashes("")
	original := sampleRecord(`C:\dl\photos.zip`, "original-structure", time.Now())
	cache.Put(original)

	swapped := original.Key
	swapped.ModTimeUnixNS = original.Key.ModTimeUnixNS
	swapped.Size = original.Key.Size
	swapped.StructureHash = "swapped-structure"
	if _, ok := cache.Lookup(swapped); ok {
		t.Fatal("same size and mtime with a different structure must miss")
	}
}

func TestArchiveHashesCap(t *testing.T) {
	cache := NewArchiveHashes("")
	cache.SetLimit(2)
	base := time.Now()
	cache.Put(sampleRecord("a.zip", "h", base))
	cache.Put(sampleRecord("b.zip", "h", base.Add(time.Second)))
	cache.Put(sampleRecord("c.zip", "h", base.Add(2*time.Second)))

	if cache.Len() != 2 {
		t.Fatalf("len = %d", cache.Len())
	}
	if _, ok := cache.Lookup(sampleRecord("a.zip", "h", base).Key); ok {
		t.Fatal("oldest record should be evicted")
	}
}

func TestArchiveHashesInvalidate(t *testing.T) {
	cache := NewArchiveHashes("")
	record := sampleRecord(`C:\dl\photos.zip`, "h", time.Now())
	cache.Put(record)
	cache.Invalidate(`C:\dl\photos.zip`)
	if _, ok := cache.Lookup(record.Key); ok {
		t.Fatal("record should be invalidated")
	}
}

func TestArchiveHashesCorruptFileStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archives.json")
	if err := writeRaw(path, "{broken"); err != nil {
		t.Fatal(err)
	}
	cache := NewArchiveHashes(path)
	if cache.Len() != 0 {
		t.Fatalf("len = %d", cache.Len())
	}
	record := sampleRecord("x.zip", "h", time.Now())
	cache.Put(record)
	if _, ok := cache.Lookup(record.Key); !ok {
		t.Fatal("cache should still work after a corrupt load")
	}
}
