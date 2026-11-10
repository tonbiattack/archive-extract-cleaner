package ziparchive

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/testutil"
)

func limits() model.Limits {
	l := model.DefaultLimits()
	l.MaxEntries = 100
	l.MaxTotalBytes = 1 << 30
	l.MaxFileBytes = 1 << 30
	return l
}

func TestOpenVariantsWithTopDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "photos.zip")
	err := testutil.WriteZip(path, []testutil.Entry{
		{Name: "photos/"},
		{Name: "photos/a.jpg", Content: "a"},
		{Name: "photos/empty/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := Open(path, limits())
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if len(archive.Variants) != 2 {
		t.Fatalf("variants = %d, want 2", len(archive.Variants))
	}
	direct := archive.Variants[0]
	if _, ok := direct.Files["photos/a.jpg"]; !ok {
		t.Fatalf("direct files = %v", direct.Files)
	}
	stripped := archive.Variants[1]
	if stripped.Mode != "strip-top:photos" {
		t.Fatalf("mode = %q", stripped.Mode)
	}
	if _, ok := stripped.Files["a.jpg"]; !ok {
		t.Fatalf("stripped files = %v", stripped.Files)
	}
	if _, ok := stripped.Dirs["empty"]; !ok {
		t.Fatalf("stripped dirs = %v", stripped.Dirs)
	}
}

func TestOpenVariantsPreserveDirectoryDisplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "photos.zip")
	err := testutil.WriteZip(path, []testutil.Entry{
		{Name: "PHOTOS/"},
		{Name: "PHOTOS/SUB/a.jpg", Content: "a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := Open(path, limits())
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if len(archive.Variants) != 2 {
		t.Fatalf("variants = %d", len(archive.Variants))
	}
	stripped := archive.Variants[1]
	if stripped.Mode != "strip-top:photos" {
		t.Fatalf("mode = %q", stripped.Mode)
	}
	if _, ok := stripped.Files["sub/a.jpg"]; !ok {
		t.Fatalf("files = %+v", stripped.Files)
	}
	if stripped.DirPaths["sub"] != "SUB" {
		t.Fatalf("dir paths = %+v", stripped.DirPaths)
	}
}

func TestOpenRejectsUnsafeEntries(t *testing.T) {
	cases := map[string][]testutil.Entry{
		"zip-slip":       {{Name: "../evil.txt", Content: "x"}},
		"absolute":       {{Name: "/evil.txt", Content: "x"}},
		"duplicate":      {{Name: "a.txt", Content: "1"}, {Name: "a.txt", Content: "2"}},
		"case-collision": {{Name: "A.txt", Content: "1"}, {Name: "a.txt", Content: "2"}},
		"reserved":       {{Name: "CON.txt", Content: "x"}},
		"trailing-dot":   {{Name: "foo./bar.txt", Content: "x"}},
		"bad-separator":  {{Name: "a//b.txt", Content: "x"}},
		"symlink":        {{Name: "link", Content: "target", Mode: os.ModeSymlink | 0o777}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.zip")
			if err := testutil.WriteZip(path, entries); err != nil {
				t.Fatal(err)
			}
			archive, err := Open(path, limits())
			if err == nil {
				archive.Close()
				t.Fatal("expected error")
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestOpenRejectsEncrypted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "encrypted.zip")
	if err := testutil.WriteZip(path, []testutil.Entry{{Name: "a.txt", Content: "secret"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.SetEncrypted(path); err != nil {
		t.Fatal(err)
	}
	_, err := Open(path, limits())
	if !errors.Is(err, ErrEncrypted) {
		t.Fatalf("error = %v, want ErrEncrypted", err)
	}
}

func TestOpenRejectsUnsupportedMethod(t *testing.T) {
	path := filepath.Join(t.TempDir(), "method.zip")
	if err := testutil.WriteZip(path, []testutil.Entry{{Name: "a.txt", Content: "content"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.SetMethod(path, 99); err != nil {
		t.Fatal(err)
	}
	_, err := Open(path, limits())
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("error = %v, want ErrUnsupported", err)
	}
}

func TestOpenRejectsInvalidUTF8(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.zip")
	if err := testutil.WriteZip(path, []testutil.Entry{{Name: string([]byte{0xff, 0xfe}) + ".txt", Content: "x"}}); err != nil {
		t.Fatal(err)
	}
	_, err := Open(path, limits())
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
}

func TestOpenEnforcesEntryLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "many.zip")
	if err := testutil.WriteZip(path, []testutil.Entry{
		{Name: "a.txt", Content: "a"},
		{Name: "b.txt", Content: "b"},
	}); err != nil {
		t.Fatal(err)
	}
	small := limits()
	small.MaxEntries = 1
	_, err := Open(path, small)
	if !errors.Is(err, ErrLimit) {
		t.Fatalf("error = %v, want ErrLimit", err)
	}
}

func TestOpenEmptyArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.zip")
	if err := testutil.WriteZip(path, nil); err != nil {
		t.Fatal(err)
	}
	archive, err := Open(path, limits())
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if len(archive.Variants) != 1 || archive.Variants[0].FileCount != 0 || archive.Variants[0].DirCount != 0 {
		t.Fatalf("variants = %+v", archive.Variants)
	}
}
