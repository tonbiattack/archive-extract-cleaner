package verify

import (
	"context"
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

type fixture struct {
	root   string
	zip    string
	folder string
}

func newFixture(t *testing.T, entries []testutil.Entry, files map[string]string) fixture {
	t.Helper()
	root := t.TempDir()
	zipPath := filepath.Join(root, "photos.zip")
	folder := filepath.Join(root, "photos")
	if err := testutil.WriteZip(zipPath, entries); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		if err := testutil.WriteFile(folder, rel, content); err != nil {
			t.Fatal(err)
		}
	}
	return fixture{root: root, zip: zipPath, folder: folder}
}

func (f fixture) verify(t *testing.T, opts Options) Result {
	t.Helper()
	if opts.Limits.MaxEntries == 0 {
		opts.Limits = testLimits()
	}
	if opts.Roots == nil {
		opts.Roots = []string{f.root}
	}
	return Verify(context.Background(), f.zip, f.folder, opts)
}

func TestVerifyExactMatch(t *testing.T) {
	f := newFixture(t,
		[]testutil.Entry{{Name: "a.txt", Content: "alpha"}, {Name: "sub/b.txt", Content: "beta"}},
		map[string]string{"a.txt": "alpha", "sub/b.txt": "beta"})
	result := f.verify(t, Options{})
	if result.Verdict != model.VerdictDeleteCandidate {
		t.Fatalf("verdict = %s: %s %v", result.Verdict, result.Reason, result.Details)
	}
	if result.MatchMode != model.MatchDirect {
		t.Fatalf("mode = %s", result.MatchMode)
	}
}

func TestVerifyStripTopDirectory(t *testing.T) {
	f := newFixture(t,
		[]testutil.Entry{{Name: "photos/a.txt", Content: "alpha"}, {Name: "photos/sub/b.txt", Content: "beta"}},
		map[string]string{"a.txt": "alpha", "sub/b.txt": "beta"})
	result := f.verify(t, Options{})
	if result.Verdict != model.VerdictDeleteCandidate {
		t.Fatalf("verdict = %s: %s %v", result.Verdict, result.Reason, result.Details)
	}
	if result.MatchMode != model.MatchStrip {
		t.Fatalf("mode = %s", result.MatchMode)
	}
}

func TestVerifyStripTopDirectoryCaseInsensitive(t *testing.T) {
	f := newFixture(t,
		[]testutil.Entry{{Name: "Photos/a.txt", Content: "alpha"}, {Name: "Photos/SUB/b.txt", Content: "beta"}},
		map[string]string{"a.txt": "alpha", "sub/b.txt": "beta"})
	result := f.verify(t, Options{})
	if result.Verdict != model.VerdictDeleteCandidate {
		t.Fatalf("verdict = %s: %s %v", result.Verdict, result.Reason, result.Details)
	}
	if result.MatchMode != model.MatchStrip {
		t.Fatalf("mode = %s", result.MatchMode)
	}
}

func TestVerifyStripDifferentTopDirectoryName(t *testing.T) {
	f := newFixture(t,
		[]testutil.Entry{{Name: "album/a.txt", Content: "alpha"}},
		map[string]string{"a.txt": "alpha"})
	result := f.verify(t, Options{})
	if result.Verdict != model.VerdictDeleteCandidate {
		t.Fatalf("verdict = %s: %s %v", result.Verdict, result.Reason, result.Details)
	}
	if result.MatchMode != model.MatchStrip {
		t.Fatalf("mode = %s", result.MatchMode)
	}
}

func TestVerifyMismatches(t *testing.T) {
	cases := []struct {
		name    string
		entries []testutil.Entry
		files   map[string]string
	}{
		{"extra-file", []testutil.Entry{{Name: "a.txt", Content: "a"}}, map[string]string{"a.txt": "a", "extra.txt": "x"}},
		{"missing-file", []testutil.Entry{{Name: "a.txt", Content: "a"}, {Name: "b.txt", Content: "b"}}, map[string]string{"a.txt": "a"}},
		{"changed-content", []testutil.Entry{{Name: "a.txt", Content: "abcd"}}, map[string]string{"a.txt": "abce"}},
		{"extra-directory", []testutil.Entry{{Name: "a.txt", Content: "a"}}, map[string]string{"a.txt": "a", "empty/.keep": ""}},
		{"missing-directory", []testutil.Entry{{Name: "a.txt", Content: "a"}, {Name: "empty/", Content: ""}}, map[string]string{"a.txt": "a"}},
		{"type-conflict", []testutil.Entry{{Name: "a", Content: "file"}}, map[string]string{"a/b.txt": "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, tc.entries, tc.files)
			result := f.verify(t, Options{})
			if result.Verdict != model.VerdictMismatch {
				t.Fatalf("verdict = %s: %s", result.Verdict, result.Reason)
			}
			if len(result.Details) == 0 {
				t.Fatal("expected diff details")
			}
		})
	}
}

func TestVerifyEmptyDirectoryMatch(t *testing.T) {
	f := newFixture(t, []testutil.Entry{{Name: "empty/", Content: ""}}, map[string]string{})
	if err := testutil.MakeDir(f.folder, "empty"); err != nil {
		t.Fatal(err)
	}
	result := f.verify(t, Options{})
	if result.Verdict != model.VerdictDeleteCandidate {
		t.Fatalf("verdict = %s: %s %v", result.Verdict, result.Reason, result.Details)
	}
}

func TestVerifyEmptyArchiveAndFolder(t *testing.T) {
	f := newFixture(t, nil, map[string]string{})
	result := f.verify(t, Options{})
	if result.Verdict != model.VerdictDeleteCandidate {
		t.Fatalf("verdict = %s: %s %v", result.Verdict, result.Reason, result.Details)
	}
}

func TestVerifyBrokenArchive(t *testing.T) {
	f := newFixture(t, []testutil.Entry{{Name: "a.txt", Content: "alpha"}}, map[string]string{"a.txt": "alpha"})
	data, err := os.ReadFile(f.zip)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.zip, data[:len(data)/2], 0o644); err != nil {
		t.Fatal(err)
	}
	result := f.verify(t, Options{})
	if result.Verdict != model.VerdictUnverified {
		t.Fatalf("verdict = %s: %s", result.Verdict, result.Reason)
	}
}

func TestVerifyEncryptedArchive(t *testing.T) {
	f := newFixture(t, []testutil.Entry{{Name: "a.txt", Content: "alpha"}}, map[string]string{"a.txt": "alpha"})
	if err := testutil.SetEncrypted(f.zip); err != nil {
		t.Fatal(err)
	}
	result := f.verify(t, Options{})
	if result.Verdict != model.VerdictUnverified {
		t.Fatalf("verdict = %s: %s", result.Verdict, result.Reason)
	}
}

func TestVerifyChecksumError(t *testing.T) {
	f := newFixture(t, []testutil.Entry{{Name: "a.txt", Content: "alpha", Store: true}}, map[string]string{"a.txt": "alpha"})
	if err := testutil.CorruptFirstFileData(f.zip); err != nil {
		t.Fatal(err)
	}
	result := f.verify(t, Options{})
	if result.Verdict != model.VerdictUnverified {
		t.Fatalf("verdict = %s: %s", result.Verdict, result.Reason)
	}
}

func TestVerifyDetectsChangeDuringVerification(t *testing.T) {
	f := newFixture(t, []testutil.Entry{{Name: "a.txt", Content: "alpha"}}, map[string]string{"a.txt": "alpha"})
	result := f.verify(t, Options{BeforeRead: func(path string) {
		_ = os.WriteFile(path, []byte("bravo"), 0o644)
	}})
	if result.Verdict != model.VerdictUnverified {
		t.Fatalf("verdict = %s: %s", result.Verdict, result.Reason)
	}
}

func TestVerifyLimitExceeded(t *testing.T) {
	f := newFixture(t, []testutil.Entry{{Name: "a.txt", Content: "alpha"}}, map[string]string{"a.txt": "alpha"})
	limits := testLimits()
	limits.MaxFileBytes = 2
	result := f.verify(t, Options{Limits: limits})
	if result.Verdict != model.VerdictUnverified {
		t.Fatalf("verdict = %s: %s", result.Verdict, result.Reason)
	}
}

func TestVerifyRefusesRoot(t *testing.T) {
	f := newFixture(t, []testutil.Entry{{Name: "photos/a.txt", Content: "alpha"}}, map[string]string{})
	result := f.verify(t, Options{Roots: []string{f.folder}})
	if result.Verdict != model.VerdictUnverified {
		t.Fatalf("verdict = %s: %s", result.Verdict, result.Reason)
	}
}

func TestVerifyMissingFolder(t *testing.T) {
	f := newFixture(t, []testutil.Entry{{Name: "a.txt", Content: "alpha"}}, map[string]string{"a.txt": "alpha"})
	if err := os.RemoveAll(f.folder); err != nil {
		t.Fatal(err)
	}
	result := f.verify(t, Options{})
	if result.Verdict != model.VerdictUnverified {
		t.Fatalf("verdict = %s: %s", result.Verdict, result.Reason)
	}
}

func TestVerifyRejectsJunction(t *testing.T) {
	f := newFixture(t, []testutil.Entry{{Name: "a.txt", Content: "alpha"}}, map[string]string{"a.txt": "alpha"})
	outside := filepath.Join(f.root, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(f.folder, "link")
	if err := testutil.Junction(link, outside); err != nil {
		t.Skipf("cannot create junction: %v", err)
	}
	result := f.verify(t, Options{})
	if result.Verdict != model.VerdictUnverified {
		t.Fatalf("verdict = %s: %s", result.Verdict, result.Reason)
	}
}
