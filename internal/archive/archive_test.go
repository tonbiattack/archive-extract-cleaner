package archive

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/testutil"
)

func testLimits() model.Limits {
	l := model.DefaultLimits()
	l.MaxEntries = 1000
	l.MaxTotalBytes = 1 << 30
	l.MaxFileBytes = 1 << 30
	return l
}

func TestParseSevenZipListing(t *testing.T) {
	output := `7-Zip 25.01 (x64) : Copyright (c) 1999-2025 Igor Pavlov : 2025-08-03

Scanning the drive for archives:
1 file, 200 bytes (1 KiB)

Listing archive: C:\tmp\photos.7z

--
Path = C:\tmp\photos.7z
Type = 7z
Physical Size = 200
Headers Size = 187

----------
Path = emptydir
Size = 0
Packed Size = 0
Modified = 2026-11-15 16:46:50.4544794
Attributes = D
CRC = 
Encrypted = -
Method = 

Path = sub
Size = 0
Packed Size = 0
Modified = 2026-11-15 16:46:50.4482133
Attributes = D
CRC = 
Encrypted = -

Path = a.txt
Size = 5
Packed Size = 13
Modified = 2026-11-15 16:46:50.4534791
Attributes = A
CRC = D0E0396A
Encrypted = -

Path = sub\b.txt
Size = 4
Packed Size = 
Modified = 2026-11-15 16:46:50.4544794
Attributes = A
CRC = 8F910463
Encrypted = -
`
	entries, total, err := parseSevenZipListing([]byte(output), testLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("entries = %d: %+v", len(entries), entries)
	}
	if total != 9 {
		t.Fatalf("total = %d, want 9", total)
	}

	byPath := map[string]Entry{}
	for _, entry := range entries {
		byPath[entry.Path] = entry
	}
	emptydir, ok := byPath["emptydir"]
	if !ok || !emptydir.Dir {
		t.Fatalf("emptydir = %+v", emptydir)
	}
	a, ok := byPath["a.txt"]
	if !ok {
		t.Fatalf("a.txt missing: %+v", byPath)
	}
	if a.Size != 5 || a.CRC32 != 0xD0E0396A || a.Dir {
		t.Fatalf("a.txt = %+v", a)
	}
	// バックスラッシュはスラッシュへ正規化される。
	b, ok := byPath["sub/b.txt"]
	if !ok || b.Key != "sub/b.txt" {
		t.Fatalf("sub/b.txt = %+v", b)
	}
}

func TestParseSevenZipListingRejectsTraversal(t *testing.T) {
	output := `----------
Path = ..\evil.txt
Size = 1
Attributes = A
`
	if _, _, err := parseSevenZipListing([]byte(output), testLimits()); err == nil {
		t.Fatal("path traversal must be rejected")
	}
}

func TestParseSevenZipListingRejectsAbsolute(t *testing.T) {
	output := `----------
Path = C:\windows\system32\evil.txt
Size = 1
Attributes = A
`
	if _, _, err := parseSevenZipListing([]byte(output), testLimits()); err == nil {
		t.Fatal("absolute path must be rejected")
	}
}

func TestParseSevenZipListingRejectsDuplicate(t *testing.T) {
	output := `----------
Path = a.txt
Size = 1
Attributes = A

Path = a.txt
Size = 2
Attributes = A
`
	if _, _, err := parseSevenZipListing([]byte(output), testLimits()); err == nil {
		t.Fatal("duplicate path must be rejected")
	}
}

func TestParseSevenZipListingRejectsEncrypted(t *testing.T) {
	output := `----------
Path = a.txt
Size = 1
Attributes = A
Encrypted = +
`
	if _, _, err := parseSevenZipListing([]byte(output), testLimits()); err == nil {
		t.Fatal("encrypted archive must be rejected")
	}
	if _, _, err := parseSevenZipListing([]byte(output), testLimits()); err != ErrEncrypted && err == nil {
		t.Fatal("expected ErrEncrypted")
	}
}

func TestParseSevenZipListingEnforcesMaxEntries(t *testing.T) {
	limits := testLimits()
	limits.MaxEntries = 2
	output := `----------
Path = a.txt
Size = 1
Attributes = A

Path = b.txt
Size = 1
Attributes = A

Path = c.txt
Size = 1
Attributes = A
`
	if _, _, err := parseSevenZipListing([]byte(output), limits); err == nil {
		t.Fatal("entry limit must be enforced")
	}
}

func TestParseSevenZipListingNoEntries(t *testing.T) {
	output := `Listing archive: empty.7z

----------
`
	entries, total, err := parseSevenZipListing([]byte(output), testLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 || total != 0 {
		t.Fatalf("entries=%d total=%d", len(entries), total)
	}
}

// sevenZipFixture は 7z が使える場合だけアーカイブを作る。
func sevenZipFixture(t *testing.T, format string) string {
	t.Helper()
	if !ToolAvailable() {
		t.Skip("7-Zip CLI is not available")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "photos")
	if err := testutil.WriteFile(source, "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(source, "sub/b.txt", "beta"); err != nil {
		t.Fatal(err)
	}
	if err := testutil.MakeDir(source, "emptydir"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "photos."+format)
	tool := ToolPath()
	cmd := commandFor(tool, "a", "-t"+format, target, filepath.Join(source, "*"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot create %s archive: %v: %s", format, err, out)
	}
	return target
}

func TestOpen7zArchive(t *testing.T) {
	path := sevenZipFixture(t, "7z")
	archive, err := Open(path, testLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()

	// 内容は emptydir / sub / a.txt / sub\b.txt の4件。
	if archive.EntryCount() != 4 {
		t.Fatalf("entry count = %d", archive.EntryCount())
	}
	files := archive.Variants()[0].Files
	if len(files) != 2 {
		t.Fatalf("files = %+v", files)
	}
	entry, ok := files["a.txt"]
	if !ok {
		t.Fatalf("a.txt missing: %+v", files)
	}
	reader, err := archive.Open(entry)
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if string(content) != "alpha" {
		t.Fatalf("content = %q", content)
	}
}

func TestOpen7zMatchesFolderVerification(t *testing.T) {
	path := sevenZipFixture(t, "7z")
	folder := filepath.Join(filepath.Dir(path), "photos")
	if _, err := os.Stat(folder); err != nil {
		t.Fatal(err)
	}
	archive, err := Open(path, testLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()

	// 直接比較の variant でファイル集合が一致することを確認する。
	direct := archive.Variants()[0]
	if direct.FileCount != 2 {
		t.Fatalf("files = %d", direct.FileCount)
	}
	if _, ok := direct.Dirs["emptydir"]; !ok {
		t.Fatalf("dirs = %+v", direct.Dirs)
	}
}

func TestOpenUnsupportedFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.tar")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, testLimits()); err == nil {
		t.Fatal("unsupported format must fail")
	}
}

func TestFormatOf(t *testing.T) {
	cases := map[string]Format{
		"a.ZIP": FormatZIP,
		"a.7z":  Format7z,
		"a.RAR": FormatRAR,
		"a.tar": "",
	}
	for path, want := range cases {
		if got := FormatOf(path); got != want {
			t.Errorf("FormatOf(%q) = %q, want %q", path, got, want)
		}
	}
}
