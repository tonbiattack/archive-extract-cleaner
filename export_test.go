package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/report"
	"github.com/tonbiattack/archive-extract-cleaner/internal/testutil"
)

func TestEnsureExtension(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		format report.Format
		want   string
	}{
		{name: "no extension", input: `C:\out\report`, format: report.FormatCSV, want: `C:\out\report.csv`},
		{name: "same extension", input: `C:\out\report.csv`, format: report.FormatCSV, want: `C:\out\report.csv`},
		{name: "uppercase extension", input: `C:\out\REPORT.CSV`, format: report.FormatCSV, want: `C:\out\REPORT.CSV`},
		{name: "wrong extension", input: `C:\out\report.json`, format: report.FormatCSV, want: `C:\out\report.csv`},
		{name: "json", input: `C:\out\report.txt`, format: report.FormatJSON, want: `C:\out\report.json`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ensureExtension(tc.input, tc.format)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWriteReportCSVAndJSON(t *testing.T) {
	dir := t.TempDir()
	items := []model.Item{
		{
			ID:         "x1",
			Verdict:    model.VerdictDeleteCandidate,
			Status:     model.StatusReady,
			Name:       "photos",
			ZipPath:    `C:\dl\photos.zip`,
			FolderPath: `C:\dl\photos`,
			ParentPath: `C:\dl`,
			Reason:     "完全一致",
		},
	}
	summary := model.Summarize(items)

	csvPath := filepath.Join(dir, "out.csv")
	if err := writeReport(csvPath, report.FormatCSV, items, summary, []string{`C:\dl`}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("csv is empty")
	}
	if head := string(data[:len("id,verdict")]); head != "id,verdict" {
		t.Fatalf("csv head = %q", head)
	}

	jsonPath := filepath.Join(dir, "out.json")
	if err := writeReport(jsonPath, report.FormatJSON, items, summary, []string{`C:\dl`}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("json is empty")
	}
}

func TestWriteReportReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	items := []model.Item{{ID: "a", Verdict: model.VerdictKeepArchive, Reason: "なし"}}
	if err := writeReport(path, report.FormatCSV, items, model.Summarize(items), nil); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "out.csv" {
			t.Fatalf("temporary file left behind: %s", entry.Name())
		}
	}
}

func TestExportReportRequiresScan(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.ExportReport("csv"); err == nil {
		t.Fatal("export without a scan must fail")
	}
}

func TestExportReportRejectsUnknownFormat(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.ExportReport("xlsx"); err == nil {
		t.Fatal("unknown format must fail")
	}
}

func TestScanRecordsNameVariant(t *testing.T) {
	root := t.TempDir()
	if err := testutil.WriteZip(filepath.Join(root, "photos.zip"), []testutil.Entry{{Name: "a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos (1)"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	app := newTestApp(t)
	if _, err := app.Scan([]string{root}); err != nil {
		t.Fatal(err)
	}
	rows := app.GetRows(RowQuery{Filter: "candidate"})
	if len(rows.Rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	if !rows.Rows[0].NameVariant {
		t.Fatal("name variant must be exposed to the UI")
	}
	if !rows.Rows[0].Deletable {
		t.Fatal("name variant with exact content should be deletable")
	}
}
