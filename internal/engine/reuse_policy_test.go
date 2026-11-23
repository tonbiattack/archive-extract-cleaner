package engine

import (
	"path/filepath"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/cache"
	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/testutil"
)

// 削除候補ではない判定は、指紋が変わらなければ再利用する（Issue #31）。
func TestDifferentialScanReusesNonCandidateVerdict(t *testing.T) {
	root := t.TempDir()
	if err := testutil.WriteZip(filepath.Join(root, "photos.zip"), []testutil.Entry{{Name: "a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	// 内容が異なるので MISMATCH。削除候補ではない。
	if err := testutil.WriteFile(filepath.Join(root, "photos"), "a.txt", "bravo"); err != nil {
		t.Fatal(err)
	}

	hashes := cache.NewArchiveHashes("")
	first := scanWithCache(t, root, nil, hashes, false)
	if first.Cache.ReusedItems != 0 {
		t.Fatalf("first scan reused %d items", first.Cache.ReusedItems)
	}
	item := findItem(t, first, "photos")
	if item.Verdict != model.VerdictMismatch {
		t.Fatalf("verdict = %s, want MISMATCH", item.Verdict)
	}

	second := scanWithCache(t, root, recordFor(t, first, root), hashes, false)
	if second.Cache.ReusedItems != 1 {
		t.Fatalf("non-candidate verdict should be reused: %+v", second.Cache)
	}
	reused := findItem(t, second, "photos")
	if reused.Verdict != model.VerdictMismatch {
		t.Fatalf("reused verdict = %s", reused.Verdict)
	}
	// 再利用した項目が削除候補になってはいけない。
	if reused.Deletable() || reused.Verdict.IsDeleteCandidate() {
		t.Fatal("reused item must never be deletable")
	}
}

// 前回が削除候補なら、指紋が同じでも必ず検証し直す。
func TestDifferentialScanNeverReusesDeleteCandidate(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "photos", "a.txt")
	if err := testutil.WriteZip(filepath.Join(root, "photos.zip"), []testutil.Entry{{Name: "a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}

	hashes := cache.NewArchiveHashes("")
	first := scanWithCache(t, root, nil, hashes, false)
	if got := findItem(t, first, "photos").Verdict; got != model.VerdictDeleteCandidate {
		t.Fatalf("verdict = %s, want DELETE_CANDIDATE", got)
	}

	second := scanWithCache(t, root, recordFor(t, first, root), hashes, false)
	if second.Cache.ReusedItems != 0 {
		t.Fatalf("delete candidate must never be reused: %+v", second.Cache)
	}
	item := findItem(t, second, "photos")
	if item.Verdict != model.VerdictDeleteCandidate {
		t.Fatalf("verdict = %s, want a fresh DELETE_CANDIDATE", item.Verdict)
	}
	// 実体を読み直しているので、変更があれば検出できる。
	if err := testutil.WriteFile(filepath.Dir(file), "a.txt", "changed"); err != nil {
		t.Fatal(err)
	}
	third := scanWithCache(t, root, recordFor(t, second, root), hashes, false)
	if got := findItem(t, third, "photos").Verdict; got == model.VerdictDeleteCandidate {
		t.Fatal("changed folder must not stay a delete candidate")
	}
}

// 完全再スキャンは前回結果を一切使わない。
func TestFullScanIgnoresPreviousResults(t *testing.T) {
	root := t.TempDir()
	if err := testutil.WriteZip(filepath.Join(root, "photos.zip"), []testutil.Entry{{Name: "a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos"), "a.txt", "bravo"); err != nil {
		t.Fatal(err)
	}
	hashes := cache.NewArchiveHashes("")
	first := scanWithCache(t, root, nil, hashes, false)
	full := scanWithCache(t, root, recordFor(t, first, root), hashes, true)
	if full.Cache.ReusedItems != 0 {
		t.Fatalf("full scan reused %d items", full.Cache.ReusedItems)
	}
}
