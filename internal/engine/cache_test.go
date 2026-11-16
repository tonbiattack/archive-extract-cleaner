package engine

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

// scanWithCache は前回結果と ZIP ハッシュキャッシュを渡してスキャンする。
func scanWithCache(t *testing.T, root string, previous *cache.RootRecord, hashes *cache.ArchiveHashes, full bool) *Result {
	t.Helper()
	result, err := Scan(context.Background(), Options{
		Roots:     []string{root},
		Recursive: true,
		Limits:    model.DefaultLimits(),
		Hashes:    hashes,
		Previous:  previous,
		Full:      full,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func recordFor(t *testing.T, result *Result, root string) *cache.RootRecord {
	t.Helper()
	record := cache.NewRootRecord(root, true, result.Records, result.Summary.ScannedAt)
	return &record
}

func TestCachedScanDetectsSameSizeRestoredTimeFolderChange(t *testing.T) {
	root := t.TempDir()
	zip := filepath.Join(root, "photos.zip")
	file := filepath.Join(root, "photos", "a.txt")
	if err := testutil.WriteZip(zip, []testutil.Entry{{Name: "a.txt", Content: "alpha", Store: true}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Dir(file), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	hashes := cache.NewArchiveHashes("")
	first := scanWithCache(t, root, nil, hashes, false)
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("bravo"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	for _, full := range []bool{false, true} {
		result := scanWithCache(t, root, recordFor(t, first, root), hashes, full)
		if got := findItem(t, result, "photos").Verdict; got != model.VerdictMismatch {
			t.Fatalf("full=%v: got %s", full, got)
		}
	}
}

func TestCachedScanDetectsCorruptArchiveWithUnchangedMetadata(t *testing.T) {
	root := t.TempDir()
	zip := filepath.Join(root, "photos.zip")
	if err := testutil.WriteZip(zip, []testutil.Entry{{Name: "a.txt", Content: "alpha", Store: true}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	hashes := cache.NewArchiveHashes("")
	first := scanWithCache(t, root, nil, hashes, false)
	info, err := os.Stat(zip)
	if err != nil {
		t.Fatal(err)
	}
	if err := testutil.CorruptFirstFileData(zip); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(zip, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	for _, full := range []bool{false, true} {
		result := scanWithCache(t, root, recordFor(t, first, root), hashes, full)
		if got := findItem(t, result, "photos").Verdict; got == model.VerdictDeleteCandidate {
			t.Fatalf("full=%v: corrupted archive was considered a candidate", full)
		}
	}
}

// TestDifferentialScanDoesNotReuseVerdicts は、判定の再利用を停止していることを固定する。
//
// フォルダ指紋は相対パス・サイズ・更新時刻から作るため、同じサイズ・同じ
// 更新時刻のまま中身だけ差し替えたフォルダを検出できない。判定を再利用すると
// 内容が違うフォルダが DELETE_CANDIDATE のまま残り、誤削除につながる。
// 再利用を再開するときは、このテストが示す攻撃を防げる条件を先に設計すること。
func TestDifferentialScanDoesNotReuseVerdicts(t *testing.T) {
	root := t.TempDir()
	manyPairs(t, root, 5)

	hashes := cache.NewArchiveHashes(filepath.Join(t.TempDir(), "hashes.json"))
	first := scanWithCache(t, root, nil, hashes, false)
	if first.Cache.ReusedItems != 0 {
		t.Fatalf("first scan must verify everything: %+v", first.Cache)
	}

	previous := recordFor(t, first, root)
	second := scanWithCache(t, root, previous, hashes, false)

	if second.Cache.ReusedItems != 0 {
		t.Fatalf("verdict reuse must stay disabled: %+v", second.Cache)
	}
	if second.Summary.CandidateCount != first.Summary.CandidateCount {
		t.Fatalf("candidates first=%d second=%d", first.Summary.CandidateCount, second.Summary.CandidateCount)
	}
}

// TestDifferentialScanDoesNotReuseArchiveHashes は、ZIP ハッシュの再利用を
// 停止していることを固定する。
//
// キャッシュのキーはセントラルディレクトリの構造指紋（CRC32 を含む）だが、
// CRC32 は圧縮データの破損を検出できない。破損した ZIP はセントラル
// ディレクトリが無傷のまま残るため指紋が一致し、破損 ZIP を「一致」と
// 誤表示する。展開を省くと破損を検出できないため、ハッシュは再利用しない。
func TestDifferentialScanDoesNotReuseArchiveHashes(t *testing.T) {
	root := t.TempDir()
	manyPairs(t, root, 5)

	hashes := cache.NewArchiveHashes(filepath.Join(t.TempDir(), "hashes.json"))
	first := scanWithCache(t, root, nil, hashes, false)
	if first.Cache.ComputedHashes == 0 {
		t.Fatal("first scan should compute ZIP hashes")
	}

	previous := recordFor(t, first, root)
	second := scanWithCache(t, root, previous, hashes, false)

	if second.Cache.CachedHashes != 0 {
		t.Fatalf("archive hash reuse must stay disabled: %+v", second.Cache)
	}
	if second.Cache.ComputedHashes == 0 {
		t.Fatalf("second scan must recompute hashes: %+v", second.Cache)
	}
}

func TestDifferentialScanMatchesFullScan(t *testing.T) {
	root := t.TempDir()
	manyPairs(t, root, 6)
	if err := os.WriteFile(filepath.Join(root, "broken.zip"), []byte("broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}

	hashes := cache.NewArchiveHashes("")
	first := scanWithCache(t, root, nil, hashes, false)
	previous := recordFor(t, first, root)

	differential := scanWithCache(t, root, previous, hashes, false)
	full := scanWithCache(t, root, previous, hashes, true)

	if len(differential.Items) != len(full.Items) {
		t.Fatalf("items differential=%d full=%d", len(differential.Items), len(full.Items))
	}
	fullByID := make(map[string]model.Item, len(full.Items))
	for _, item := range full.Items {
		fullByID[item.ID] = item
	}
	for _, item := range differential.Items {
		other, ok := fullByID[item.ID]
		if !ok {
			t.Fatalf("item %s missing from full scan", item.ID)
		}
		if item.Verdict != other.Verdict {
			t.Fatalf("item %s verdict differential=%s full=%s", item.ID, item.Verdict, other.Verdict)
		}
		if item.Deletable() != other.Deletable() {
			t.Fatalf("item %s deletable mismatch", item.ID)
		}
	}
	if differential.Summary.CandidateCount != full.Summary.CandidateCount {
		t.Fatalf("candidates differential=%d full=%d", differential.Summary.CandidateCount, full.Summary.CandidateCount)
	}
}

func TestDifferentialScanDetectsFolderChanges(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, folder string)
	}{
		{
			name: "content change",
			mutate: func(t *testing.T, folder string) {
				if err := testutil.WriteFile(folder, "a.txt", "changed"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "file added",
			mutate: func(t *testing.T, folder string) {
				if err := testutil.WriteFile(folder, "b.txt", "beta"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "empty directory added",
			mutate: func(t *testing.T, folder string) {
				if err := testutil.MakeDir(folder, "emptydir"); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := testutil.WriteZip(filepath.Join(root, "photos.zip"), []testutil.Entry{{Name: "a.txt", Content: "alpha"}}); err != nil {
				t.Fatal(err)
			}
			folder := filepath.Join(root, "photos")
			if err := testutil.WriteFile(folder, "a.txt", "alpha"); err != nil {
				t.Fatal(err)
			}

			hashes := cache.NewArchiveHashes("")
			first := scanWithCache(t, root, nil, hashes, false)
			if first.Cache.ReusedItems != 0 {
				t.Fatalf("first scan = %+v", first.Cache)
			}
			previous := recordFor(t, first, root)

			tc.mutate(t, folder)

			second := scanWithCache(t, root, previous, hashes, false)
			if second.Cache.ReusedItems != 0 {
				t.Fatalf("changed folder must be re-verified: %+v", second.Cache)
			}
			item := findItem(t, second, "photos")
			if item.Verdict == model.VerdictDeleteCandidate {
				t.Fatalf("changed folder must not stay a delete candidate: %s", item.Reason)
			}
		})
	}
}

func TestDifferentialScanDetectsZipSwap(t *testing.T) {
	// 同サイズ・同更新時刻で ZIP の中身だけを差し替えたケース。
	root := t.TempDir()
	zipPath := filepath.Join(root, "photos.zip")
	if err := testutil.WriteZip(zipPath, []testutil.Entry{{Name: "a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}

	hashes := cache.NewArchiveHashes("")
	first := scanWithCache(t, root, nil, hashes, false)
	previous := recordFor(t, first, root)

	info, err := os.Stat(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	// 同じ長さの別内容へ差し替え、サイズと更新時刻を元に戻す。
	if err := testutil.WriteZip(zipPath, []testutil.Entry{{Name: "a.txt", Content: "bravo"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(zipPath, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}

	second := scanWithCache(t, root, previous, hashes, false)
	item := findItem(t, second, "photos")
	if item.Verdict == model.VerdictDeleteCandidate {
		t.Fatal("swapped ZIP must not remain a delete candidate")
	}
}

func TestDifferentialScanSkipsUnverifiedRecords(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "broken.zip"), []byte("broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "broken"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}

	hashes := cache.NewArchiveHashes("")
	first := scanWithCache(t, root, nil, hashes, false)
	previous := recordFor(t, first, root)

	for _, record := range previous.Items {
		if record.Verdict == string(model.VerdictUnverified) && record.Cacheable {
			t.Fatal("UNVERIFIED items must not be cacheable")
		}
	}

	second := scanWithCache(t, root, previous, hashes, false)
	item := findItem(t, second, "broken")
	if item.Verdict != model.VerdictUnverified {
		t.Fatalf("verdict = %s", item.Verdict)
	}
}

func TestDifferentialScanRecoversFromMissingCache(t *testing.T) {
	root := t.TempDir()
	manyPairs(t, root, 3)

	hashes := cache.NewArchiveHashes(filepath.Join(t.TempDir(), "hashes.json"))
	first := scanWithCache(t, root, nil, hashes, false)
	previous := recordFor(t, first, root)

	// キャッシュファイルを消しても、完全スキャンとして正しく動く。
	missing := cache.NewArchiveHashes(filepath.Join(t.TempDir(), "gone.json"))
	second := scanWithCache(t, root, previous, missing, false)
	if second.Summary.CandidateCount != first.Summary.CandidateCount {
		t.Fatalf("candidates = %d, want %d", second.Summary.CandidateCount, first.Summary.CandidateCount)
	}
}

func TestCacheFlushAndReloadKeepsResults(t *testing.T) {
	root := t.TempDir()
	manyPairs(t, root, 4)

	dir := t.TempDir()
	hashPath := filepath.Join(dir, "hashes.json")
	scanPath := filepath.Join(dir, "scan.json")

	hashes := cache.NewArchiveHashes(hashPath)
	scans := cache.NewScanStore(scanPath)

	first := scanWithCache(t, root, nil, hashes, false)
	if err := hashes.Flush(); err != nil {
		t.Fatal(err)
	}
	scans.Put(*recordFor(t, first, root))
	if err := scans.Flush(); err != nil {
		t.Fatal(err)
	}

	// 再起動相当。ディスクから読み直して2回目を実行する。
	reloadedHashes := cache.NewArchiveHashes(hashPath)
	reloadedScans := cache.NewScanStore(scanPath)
	previous, ok := reloadedScans.Lookup(root, true, cache.ArchiveEpoch())
	if !ok {
		t.Fatal("scan cache should be found")
	}
	second := scanWithCache(t, root, &previous, reloadedHashes, false)

	if second.Cache.ReusedItems != 0 {
		t.Fatalf("unsafe cache reuse: %d", second.Cache.ReusedItems)
	}
}

func TestDifferentialScanBenchmark(t *testing.T) {
	if testing.Short() {
		t.Skip("skip in short mode")
	}
	root := t.TempDir()
	manyPairs(t, root, 40)
	hashes := cache.NewArchiveHashes("")

	start := time.Now()
	first := scanWithCache(t, root, nil, hashes, false)
	firstDuration := time.Since(start)

	previous := recordFor(t, first, root)
	start = time.Now()
	second := scanWithCache(t, root, previous, hashes, false)
	secondDuration := time.Since(start)

	t.Logf("full scan: %v, differential scan: %v", firstDuration, secondDuration)
	if second.Cache.ReusedItems != 0 {
		t.Fatal("unsafe cache reuse")
	}
}
