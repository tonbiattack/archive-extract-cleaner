package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tonbiattack/archive-extract-cleaner/internal/cache"
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
		if err := os.WriteFile(path, []byte("bravo"), 0o644); err != nil {
			t.Errorf("write: %v", err)
			return
		}
		stamp := time.Now().Add(2 * time.Second)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Errorf("chtimes: %v", err)
		}
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

func TestVerifyIgnoresMacOSXArtifacts(t *testing.T) {
	f := newFixture(t,
		[]testutil.Entry{
			{Name: "photos/a.txt", Content: "alpha"},
			{Name: "photos/__MACOSX/._a.txt", Content: "resource-fork"},
			{Name: "photos/.DS_Store", Content: "mac-metadata"},
		},
		map[string]string{"a.txt": "alpha"})
	result := f.verify(t, Options{})
	if result.Verdict != model.VerdictDeleteCandidateIgnorable {
		t.Fatalf("verdict = %s: %s %v", result.Verdict, result.Reason, result.Details)
	}
	if result.ComparedBytes == 0 {
		t.Fatal("common files were not compared")
	}
}

func TestVerifyIgnoresThumbsDbAndDesktopIni(t *testing.T) {
	f := newFixture(t,
		[]testutil.Entry{
			{Name: "a.txt", Content: "alpha"},
			{Name: "sub/Thumbs.db", Content: "thumb"},
			{Name: "desktop.ini", Content: "ini"},
		},
		map[string]string{"a.txt": "alpha", "sub/.keep": ""})
	// フォルダ側に sub ディレクトリがあり、ZIP側にだけ Thumbs.db がある形。
	if err := testutil.WriteFile(f.folder, "sub/.keep", ""); err != nil {
		t.Fatal(err)
	}
	result := f.verify(t, Options{})
	if result.Verdict != model.VerdictMismatch && result.Verdict != model.VerdictDeleteCandidateIgnorable {
		t.Fatalf("verdict = %s: %s %v", result.Verdict, result.Reason, result.Details)
	}
	if result.Verdict == model.VerdictDeleteCandidateIgnorable && result.ComparedBytes == 0 {
		t.Fatal("common files were not compared")
	}
}

func TestVerifyIgnoresOnlyZipSideArtifacts(t *testing.T) {
	// フォルダ側にしかない補助ファイルは削除対象にしない。
	f := newFixture(t,
		[]testutil.Entry{{Name: "a.txt", Content: "alpha"}},
		map[string]string{"a.txt": "alpha", ".DS_Store": "mac-metadata"})
	result := f.verify(t, Options{})
	if result.Verdict != model.VerdictMismatch {
		t.Fatalf("verdict = %s: %s", result.Verdict, result.Reason)
	}
}

func TestVerifyArtifactWithChangedContentIsMismatch(t *testing.T) {
	// 補助ファイル以外の内容が違えば、補助差分として扱わない。
	f := newFixture(t,
		[]testutil.Entry{
			{Name: "a.txt", Content: "alpha"},
			{Name: ".DS_Store", Content: "mac-metadata"},
		},
		map[string]string{"a.txt": "changed"})
	result := f.verify(t, Options{})
	if result.Verdict != model.VerdictMismatch {
		t.Fatalf("verdict = %s: %s %v", result.Verdict, result.Reason, result.Details)
	}
}

func TestShouldIgnoreZipOnly(t *testing.T) {
	ignored := []string{
		".DS_Store",
		"sub/Thumbs.db",
		"desktop.ini",
		"__MACOSX/._a.jpg",
		"a/b/._resource",
		".Spotlight-V100/store.db",
		"download.crdownload",
		"file.tmp",
		"partial.part",
	}
	for _, path := range ignored {
		if !shouldIgnoreZipOnly(path) {
			t.Errorf("%q should be ignored", path)
		}
	}
	kept := []string{
		"a.txt",
		"photos/image.jpg",
		"notes.txt",
		"image.png",
		"archive/TEMP",
	}
	for _, path := range kept {
		if shouldIgnoreZipOnly(path) {
			t.Errorf("%q should not be ignored", path)
		}
	}
}

// countingCache はテスト用のハッシュキャッシュ。
type countingCache struct {
	records map[string]cache.ArchiveRecord
	lookups int
	puts    int
}

func (c *countingCache) Lookup(key cache.ArchiveKey) (cache.ArchiveRecord, bool) {
	c.lookups++
	record, ok := c.records[filepath.Clean(key.Path)]
	if !ok {
		return cache.ArchiveRecord{}, false
	}
	if record.Key != key {
		return cache.ArchiveRecord{}, false
	}
	return record, true
}

func (c *countingCache) Put(record cache.ArchiveRecord) {
	c.puts++
	if c.records == nil {
		c.records = map[string]cache.ArchiveRecord{}
	}
	c.records[filepath.Clean(record.Key.Path)] = record
}

func TestVerifyWithCacheMatchesWithoutCache(t *testing.T) {
	f := newFixture(t,
		[]testutil.Entry{{Name: "a.txt", Content: "alpha"}, {Name: "sub/b.txt", Content: "beta"}},
		map[string]string{"a.txt": "alpha", "sub/b.txt": "beta"})

	plain := f.verify(t, Options{})
	cache := &countingCache{}
	first := f.verify(t, Options{Hashes: cache})
	second := f.verify(t, Options{Hashes: cache})

	if plain.Verdict != first.Verdict || plain.Verdict != second.Verdict {
		t.Fatalf("verdicts plain=%s first=%s second=%s", plain.Verdict, first.Verdict, second.Verdict)
	}
	if first.ComputedHashes == 0 {
		t.Fatal("first run should compute hashes")
	}
	if second.CachedHashes == 0 {
		t.Fatal("second run should use cached hashes")
	}
	if second.ComputedHashes != 0 {
		t.Fatalf("second run should not compute: %+v", second)
	}
	if cache.puts == 0 {
		t.Fatal("hashes should be stored")
	}
}

func TestVerifyCacheDoesNotMaskContentChange(t *testing.T) {
	f := newFixture(t,
		[]testutil.Entry{{Name: "a.txt", Content: "alpha"}},
		map[string]string{"a.txt": "alpha"})
	cache := &countingCache{}
	if got := f.verify(t, Options{Hashes: cache}); got.Verdict != model.VerdictDeleteCandidate {
		t.Fatalf("verdict = %s", got.Verdict)
	}

	// ZIP の中身を差し替える。構造指紋が変わるのでキャッシュは無効になる。
	if err := testutil.WriteZip(f.zip, []testutil.Entry{{Name: "a.txt", Content: "bravo"}}); err != nil {
		t.Fatal(err)
	}
	changed := f.verify(t, Options{Hashes: cache})
	if changed.Verdict == model.VerdictDeleteCandidate {
		t.Fatal("changed ZIP content must not stay a delete candidate")
	}
}

func TestVerifyCacheIgnoresCorruptedDigest(t *testing.T) {
	f := newFixture(t,
		[]testutil.Entry{{Name: "a.txt", Content: "alpha"}},
		map[string]string{"a.txt": "alpha"})
	broken := &brokenDigestCache{}
	result := f.verify(t, Options{Hashes: broken})
	// 壊れたキャッシュは使わず、実計算で正しい判定になる。
	if result.Verdict != model.VerdictDeleteCandidate {
		t.Fatalf("verdict = %s: %s", result.Verdict, result.Reason)
	}
	if result.ComputedHashes == 0 {
		t.Fatal("should fall back to computing hashes")
	}
}

type brokenDigestCache struct{}

func (brokenDigestCache) Lookup(key cache.ArchiveKey) (cache.ArchiveRecord, bool) {
	return cache.ArchiveRecord{
		Key: key,
		Entries: []cache.ArchiveEntry{
			{Path: "a.txt", SHA256: "not-hex", Size: 5, CRC32: 0},
		},
	}, true
}

func (brokenDigestCache) Put(cache.ArchiveRecord) {}
