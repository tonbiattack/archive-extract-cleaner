package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/cache"
	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/testutil"
)

// newCachedTestApp はキャッシュを一時ディレクトリへ向けたテスト用 App を返す。
func newCachedTestApp(t *testing.T) *App {
	t.Helper()
	app := newTestApp(t)
	dir := t.TempDir()
	app.archiveHashes = cache.NewArchiveHashes(filepath.Join(dir, "archives.json"))
	app.scanStore = cache.NewScanStore(filepath.Join(dir, "scan.json"))
	return app
}

func TestAppScanReusesCache(t *testing.T) {
	root := t.TempDir()
	manyPairsForApp(t, root, 4)

	app := newCachedTestApp(t)
	first, err := app.Scan([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if first.Cache.ReusedItems != 0 {
		t.Fatalf("first scan = %+v", first.Cache)
	}

	second, err := app.Scan([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if second.Cache.ReusedItems != 0 || second.Cache.CachedHashes != 0 {
		t.Fatalf("unsafe cache reuse: %+v", second.Cache)
	}
	if second.Summary.CandidateCount != first.Summary.CandidateCount {
		t.Fatalf("candidates first=%d second=%d", first.Summary.CandidateCount, second.Summary.CandidateCount)
	}
}

func TestAppRescanIgnoresCache(t *testing.T) {
	root := t.TempDir()
	manyPairsForApp(t, root, 3)

	app := newCachedTestApp(t)
	if _, err := app.Scan([]string{root}); err != nil {
		t.Fatal(err)
	}
	full, err := app.Rescan([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if full.Cache.ReusedItems != 0 {
		t.Fatalf("rescan must not reuse cache: %+v", full.Cache)
	}
	if full.Cache.VerifiedItems == 0 {
		t.Fatal("rescan must verify items")
	}
}

func TestAppClearCache(t *testing.T) {
	root := t.TempDir()
	manyPairsForApp(t, root, 2)

	app := newCachedTestApp(t)
	if _, err := app.Scan([]string{root}); err != nil {
		t.Fatal(err)
	}
	state := app.GetCacheState()
	if state.Roots == 0 {
		t.Fatalf("cache state = %+v", state)
	}

	if err := app.ClearCache(); err != nil {
		t.Fatal(err)
	}
	cleared := app.GetCacheState()
	if cleared.Archives != 0 || cleared.Roots != 0 {
		t.Fatalf("cleared state = %+v", cleared)
	}

	// 破棄後もスキャンは完全スキャンとして正しく動く。
	after, err := app.Scan([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if after.Cache.ReusedItems != 0 {
		t.Fatalf("cache should be empty: %+v", after.Cache)
	}
	if after.Summary.CandidateCount != 2 {
		t.Fatalf("candidates = %d", after.Summary.CandidateCount)
	}
}

func TestAppCacheDoesNotBypassMoveVerification(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "photos.zip")
	folder := filepath.Join(root, "photos")
	if err := testutil.WriteZip(zipPath, []testutil.Entry{{Name: "a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(folder, "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}

	app := newCachedTestApp(t)
	if _, err := app.Scan([]string{root}); err != nil {
		t.Fatal(err)
	}
	ids := app.CandidateIDs(RowQuery{Filter: "candidate"})
	if len(ids) != 1 {
		t.Fatalf("candidates = %v", ids)
	}

	// スキャン後にフォルダを書き換える。差分スキャン用キャッシュが
	// あっても、移動直前の再照合で必ず検出されなければならない。
	if err := testutil.WriteFile(folder, "a.txt", "tampered"); err != nil {
		t.Fatal(err)
	}
	response, err := app.DeleteSelected([]MoveRequest{{ID: ids[0], Method: MoveMethodFolder}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if response.Skipped == 0 {
		t.Fatalf("changed folder must be skipped: %+v", response)
	}
	if _, err := os.Lstat(folder); err != nil {
		t.Fatal("folder must not be removed")
	}
}

func TestAppCacheStateReportsLastScan(t *testing.T) {
	root := t.TempDir()
	manyPairsForApp(t, root, 2)

	app := newCachedTestApp(t)
	if _, err := app.Scan([]string{root}); err != nil {
		t.Fatal(err)
	}
	state := app.GetCacheState()
	if !state.Enabled {
		t.Fatal("cache should be enabled")
	}
	if state.HashPath == "" || state.ScanPath == "" {
		t.Fatalf("paths = %+v", state)
	}
	if state.LastScan.VerifiedItems == 0 {
		t.Fatalf("last scan = %+v", state.LastScan)
	}
}

func TestAppScanWithoutCacheStillWorks(t *testing.T) {
	root := t.TempDir()
	manyPairsForApp(t, root, 2)

	app := newTestApp(t)
	app.archiveHashes = nil
	app.scanStore = nil
	info, err := app.Scan([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if info.Summary.CandidateCount != 2 {
		t.Fatalf("candidates = %d", info.Summary.CandidateCount)
	}
	if state := app.GetCacheState(); state.Enabled {
		t.Fatalf("cache should be disabled: %+v", state)
	}
}

func manyPairsForApp(t *testing.T, root string, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		name := "set" + string(rune('a'+i))
		if err := testutil.WriteZip(filepath.Join(root, name+".zip"), []testutil.Entry{{Name: name + "/a.txt", Content: name}}); err != nil {
			t.Fatal(err)
		}
		if err := testutil.WriteFile(filepath.Join(root, name), "a.txt", name); err != nil {
			t.Fatal(err)
		}
	}
}

var _ = model.DefaultLimits

func TestDashboardExcludesMismatchFromReclaimable(t *testing.T) {
	root := t.TempDir()
	if err := testutil.WriteZip(filepath.Join(root, "photos.zip"), []testutil.Entry{{Name: "a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos"), "a.txt", "changed"); err != nil {
		t.Fatal(err)
	}
	app := newTestApp(t)
	if _, err := app.Scan([]string{root}); err != nil {
		t.Fatal(err)
	}
	page := app.GetRows(RowQuery{Filter: "all"})
	if page.Dashboard.Scan.Reclaimable.Count != 0 {
		t.Fatalf("mismatch must not be reclaimable: %+v", page.Dashboard.Scan.Reclaimable)
	}
	if page.Dashboard.Scan.Mismatch.Count != 1 {
		t.Fatalf("mismatch count = %d", page.Dashboard.Scan.Mismatch.Count)
	}
}

func TestDashboardUpdatesAfterMove(t *testing.T) {
	root := t.TempDir()
	if err := testutil.WriteZip(filepath.Join(root, "photos.zip"), []testutil.Entry{{Name: "a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	app := newTestApp(t)
	if _, err := app.Scan([]string{root}); err != nil {
		t.Fatal(err)
	}
	before := app.GetRows(RowQuery{Filter: "all"})
	if before.Dashboard.Scan.Reclaimable.Count != 1 {
		t.Fatalf("reclaimable = %+v", before.Dashboard.Scan.Reclaimable)
	}
	if before.Dashboard.Moves.Succeeded.Count != 0 {
		t.Fatalf("moves before = %+v", before.Dashboard.Moves)
	}

	ids := app.CandidateIDs(RowQuery{Filter: "candidate"})
	response, err := app.DeleteSelected([]MoveRequest{{ID: ids[0], Method: MoveMethodFolder}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if response.Moves.MovedBytes == 0 {
		t.Fatalf("moved bytes should be recorded: %+v", response.Moves)
	}

	after := app.GetRows(RowQuery{Filter: "all"})
	if after.Dashboard.Scan.Reclaimable.Count != 0 {
		t.Fatalf("moved item must leave reclaimable: %+v", after.Dashboard.Scan.Reclaimable)
	}
	if after.Dashboard.Scan.Recycled.Count != 1 {
		t.Fatalf("recycled = %+v", after.Dashboard.Scan.Recycled)
	}
	if after.Dashboard.Moves.Succeeded.Count != 1 {
		t.Fatalf("moves after = %+v", after.Dashboard.Moves)
	}
	if after.Dashboard.Moves.MovedBytes != response.Moves.MovedBytes {
		t.Fatalf("moved bytes after=%d response=%d", after.Dashboard.Moves.MovedBytes, response.Moves.MovedBytes)
	}
	// ごみ箱へ移した容量と回収見込みを取り違えないこと。
	if after.Dashboard.Scan.Reclaimable.Bytes != 0 {
		t.Fatalf("reclaimable bytes must be 0 after move: %d", after.Dashboard.Scan.Reclaimable.Bytes)
	}
}

func TestDashboardScopeIsScanWide(t *testing.T) {
	root := t.TempDir()
	if err := testutil.WriteZip(filepath.Join(root, "photos.zip"), []testutil.Entry{{Name: "a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "only-folder"), "b.txt", "beta"); err != nil {
		t.Fatal(err)
	}
	app := newTestApp(t)
	if _, err := app.Scan([]string{root}); err != nil {
		t.Fatal(err)
	}

	all := app.GetRows(RowQuery{Filter: "all"})
	filtered := app.GetRows(RowQuery{Filter: "candidate"})
	if all.Dashboard.Filtered {
		t.Fatal("unfiltered view must not be marked filtered")
	}
	if !filtered.Dashboard.Filtered {
		t.Fatal("filtered view must be marked filtered")
	}
	// 絞り込んでもスキャン全体の集計は変わらない。
	if filtered.Dashboard.Scan.Total.Count != all.Dashboard.Scan.Total.Count {
		t.Fatalf("scan total changed: all=%d filtered=%d",
			all.Dashboard.Scan.Total.Count, filtered.Dashboard.Scan.Total.Count)
	}
	if filtered.Dashboard.FilteredCount != filtered.Total {
		t.Fatalf("filtered count = %d, total = %d", filtered.Dashboard.FilteredCount, filtered.Total)
	}
}

func TestDashboardAcceptsIgnorableCandidates(t *testing.T) {
	root := t.TempDir()
	if err := testutil.WriteZip(filepath.Join(root, "photos.zip"), []testutil.Entry{
		{Name: "a.txt", Content: "alpha"},
		{Name: ".DS_Store", Content: "meta"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	app := newTestApp(t)
	if _, err := app.Scan([]string{root}); err != nil {
		t.Fatal(err)
	}
	page := app.GetRows(RowQuery{Filter: "all"})
	if page.Dashboard.Scan.Reclaimable.Count != 1 {
		t.Fatalf("ignorable candidate should be reclaimable: %+v", page.Dashboard.Scan.Reclaimable)
	}
}
