package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/config"
	"github.com/tonbiattack/archive-extract-cleaner/internal/duplicates"
	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/testutil"
)

// duplicateTestApp は同名検索を有効にした App を返す。
func duplicateTestApp(t *testing.T, drives []string, relative string, recursive bool) *App {
	t.Helper()
	app := newTestApp(t)
	app.duplicateSearch = config.ArchiveSearch{
		Enabled:      true,
		Drives:       drives,
		RelativePath: relative,
		Recursive:    recursive,
		MaxDepth:     8,
		MaxEntries:   20000,
	}
	app.duplicateResults = map[string]duplicates.Result{}
	return app
}

// 同名・同サイズを検出し、一覧の Row に載ること。
func TestSearchDuplicatesFindsSameNameSameSize(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "photos.zip")
	if err := testutil.WriteZip(zipPath, []testutil.Entry{{Name: "a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}

	// 保存先に見つかる同名アーカイブを用意する。
	dest := t.TempDir()
	same := filepath.Join(dest, "photos.zip")
	if err := os.WriteFile(same, []byte("different-content"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := duplicateTestApp(t, []string{"X"}, "", false)
	a.duplicateRoots = map[string]string{"X": dest}
	if _, err := a.Scan([]string{root}); err != nil {
		t.Fatal(err)
	}
	state, err := a.SearchDuplicates()
	if err != nil {
		t.Fatal(err)
	}
	if state.Searched == 0 {
		t.Fatal("nothing searched")
	}

	page := a.GetRows(RowQuery{Filter: "all", Limit: 100})
	found := false
	for _, row := range page.Rows {
		if row.Duplicate == nil {
			continue
		}
		if len(row.Duplicate.Hits) == 0 {
			continue
		}
		found = true
		hit := row.Duplicate.Hits[0]
		if hit.Drive != "X" {
			t.Fatalf("drive = %s", hit.Drive)
		}
		if hit.Path != same {
			t.Fatalf("path = %s, want %s", hit.Path, same)
		}
	}
	if !found {
		t.Fatal("duplicate hit not attached to any row")
	}
}

// 同名が無ければ none になること。
func TestSearchDuplicatesReportsNone(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "photos.zip")
	if err := testutil.WriteZip(zipPath, []testutil.Entry{{Name: "a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()

	a := duplicateTestApp(t, []string{"X"}, "", false)
	a.duplicateRoots = map[string]string{"X": dest}
	if _, err := a.Scan([]string{root}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SearchDuplicates(); err != nil {
		t.Fatal(err)
	}
	page := a.GetRows(RowQuery{Filter: "all", Limit: 100})
	for _, row := range page.Rows {
		if row.Duplicate == nil {
			continue
		}
		if row.Duplicate.Status != duplicates.StatusNone {
			t.Fatalf("status = %s, want none", row.Duplicate.Status)
		}
	}
}

// 保存先が存在しない場合は「なし」ではなく「未確認」になること。
func TestSearchDuplicatesMissingDriveIsUnknown(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "photos.zip")
	if err := testutil.WriteZip(zipPath, []testutil.Entry{{Name: "a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}

	a := duplicateTestApp(t, []string{"X"}, "", false)
	a.duplicateRoots = map[string]string{"X": filepath.Join(t.TempDir(), "missing")}
	if _, err := a.Scan([]string{root}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SearchDuplicates(); err != nil {
		t.Fatal(err)
	}
	page := a.GetRows(RowQuery{Filter: "all", Limit: 100})
	sawUnknown := false
	for _, row := range page.Rows {
		if row.Duplicate != nil && row.Duplicate.Status == duplicates.StatusUnknown {
			sawUnknown = true
		}
	}
	if !sawUnknown {
		t.Fatal("missing drive must be reported as unknown, not none")
	}
}

// 重複フィルタが同名ありの項目だけを返すこと。
func TestDuplicateFilter(t *testing.T) {
	items := []model.Item{
		{ID: "a", ZipPath: `C:\x\a.zip`},
		{ID: "b", ZipPath: `C:\x\b.zip`},
	}
	results := map[string]duplicates.Result{
		"a": {Status: duplicates.StatusSameSize, Hits: []duplicates.Hit{{Drive: "E", SameSize: true}}},
		"b": {Status: duplicates.StatusNone},
	}
	got := filterItemsWithDuplicates(items, RowQuery{Filter: "duplicate"}, results)
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("duplicate filter returned %+v", got)
	}
	got = filterItemsWithDuplicates(items, RowQuery{Filter: "duplicate-same-size"}, results)
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("same-size filter returned %+v", got)
	}
}

// 重複ソートが同名ありを先にすること。
func TestDuplicateSort(t *testing.T) {
	items := []model.Item{
		{ID: "b", ZipPath: `C:\x\b.zip`},
		{ID: "a", ZipPath: `C:\x\a.zip`},
	}
	results := map[string]duplicates.Result{
		"a": {Status: duplicates.StatusSameSize, Hits: []duplicates.Hit{{Drive: "E", SameSize: true}}},
		"b": {Status: duplicates.StatusNone},
	}
	sortItemsWithDuplicates(items, "duplicate", false, results)
	if items[0].ID != "a" {
		t.Fatalf("duplicate sort put %s first", items[0].ID)
	}
}

// 検索が無効なら実行しないこと。
func TestSearchDuplicatesDisabled(t *testing.T) {
	a := newTestApp(t)
	a.duplicateSearch = config.ArchiveSearch{Enabled: false}
	if _, err := a.SearchDuplicates(); err == nil {
		t.Fatal("disabled search must fail")
	}
}

// 検索結果を破棄できること。
func TestClearDuplicateResults(t *testing.T) {
	a := duplicateTestApp(t, []string{"X"}, "", false)
	a.duplicateResults = map[string]duplicates.Result{"a": {Status: duplicates.StatusSameSize}}
	state := a.ClearDuplicateResults()
	if state.Searched != 0 {
		t.Fatalf("searched = %d after clear", state.Searched)
	}
	if len(a.duplicateResults) != 0 {
		t.Fatal("results not cleared")
	}
}
