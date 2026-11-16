package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
)

func sampleItems() []model.Item {
	checked := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	return []model.Item{
		{
			ID:         "a1",
			Verdict:    model.VerdictDeleteCandidate,
			Status:     model.StatusReady,
			Name:       "photos",
			ZipPath:    `C:\dl\photos.zip`,
			FolderPath: `C:\dl\photos`,
			ParentPath: `C:\dl`,
			ZipSize:    100,
			FolderSize: 200,
			Size:       200,
			FileCount:  3,
			DirCount:   1,
			ZipEntries: 4,
			MatchMode:  model.MatchDirect,
			Reason:     "完全一致",
			Details:    []string{"a.txt", "b.txt"},
			CheckedAt:  checked,
		},
		{
			ID:         "b2",
			Verdict:    model.VerdictMismatch,
			Status:     model.StatusReady,
			Name:       "docs, with comma",
			ZipPath:    `C:\dl\docs.zip`,
			FolderPath: `C:\dl\docs`,
			ParentPath: `C:\dl`,
			Reason:     "内容が一致しません",
			Details:    []string{`ZIPにのみ存在: "quoted.txt"`},
			CheckedAt:  checked,
		},
	}
}

func TestWriteCSVRoundTrip(t *testing.T) {
	var buffer bytes.Buffer
	if err := WriteCSV(&buffer, sampleItems()); err != nil {
		t.Fatal(err)
	}
	reader := csv.NewReader(strings.NewReader(buffer.String()))
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d", len(rows))
	}
	if len(rows[0]) != len(CSVHeader) {
		t.Fatalf("header = %v", rows[0])
	}
	if rows[0][1] != "verdict" {
		t.Fatalf("header = %v", rows[0])
	}
	// カンマ・引用符を含む値が1セルに収まること。
	if rows[2][3] != "docs, with comma" {
		t.Fatalf("name cell = %q", rows[2][3])
	}
	if !strings.Contains(rows[2][18], "quoted.txt") {
		t.Fatalf("details cell = %q", rows[2][18])
	}
	if rows[1][16] != "true" {
		t.Fatalf("deletable = %q", rows[1][16])
	}
	if rows[1][19] != "2026-02-03T04:05:06Z" {
		t.Fatalf("checkedAt = %q", rows[1][19])
	}
}

func TestWriteCSVHeaderStable(t *testing.T) {
	var buffer bytes.Buffer
	if err := WriteCSV(&buffer, nil); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buffer.String(), "\r\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("lines = %v", lines)
	}
	header := strings.TrimSuffix(lines[0], "\r")
	if header != strings.Join(CSVHeader, ",") {
		t.Fatalf("header = %q", header)
	}
}

func TestWriteJSONShape(t *testing.T) {
	var buffer bytes.Buffer
	summary := model.Summarize(sampleItems())
	if err := WriteJSON(&buffer, sampleItems(), summary, []string{`C:\dl`}); err != nil {
		t.Fatal(err)
	}
	var document struct {
		GeneratedAt string           `json:"generatedAt"`
		Summary     map[string]any   `json:"summary"`
		Roots       []string         `json:"roots"`
		Items       []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(buffer.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if document.GeneratedAt == "" {
		t.Fatal("generatedAt must be set")
	}
	if len(document.Items) != 2 {
		t.Fatalf("items = %d", len(document.Items))
	}
	if document.Items[0]["deletable"] != true {
		t.Fatalf("item = %+v", document.Items[0])
	}
	if document.Items[1]["deletable"] != false {
		t.Fatalf("item = %+v", document.Items[1])
	}
	if len(document.Roots) != 1 {
		t.Fatalf("roots = %v", document.Roots)
	}
	// HTMLエスケープしない（パスに & が入っても読みやすい形で残す）。
	if strings.Contains(buffer.String(), "\\u0026") {
		t.Fatal("json must not escape HTML characters")
	}
}

func TestWriteJSONWithoutDetailsOmitsField(t *testing.T) {
	items := []model.Item{{ID: "x", Verdict: model.VerdictKeepArchive, Reason: "なし"}}
	var buffer bytes.Buffer
	if err := WriteJSON(&buffer, items, model.Summarize(items), nil); err != nil {
		t.Fatal(err)
	}
	var document struct {
		Items []map[string]any `json:"items"`
		Roots []string         `json:"roots"`
	}
	if err := json.Unmarshal(buffer.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if _, exists := document.Items[0]["details"]; exists {
		t.Fatalf("details should be omitted: %+v", document.Items[0])
	}
	if document.Roots == nil {
		t.Fatal("roots must be an empty array, not null")
	}
}

func TestParseFormat(t *testing.T) {
	if format, err := ParseFormat(""); err != nil || format != FormatCSV {
		t.Fatalf("format = %q err = %v", format, err)
	}
	if format, err := ParseFormat("json"); err != nil || format != FormatJSON {
		t.Fatalf("format = %q err = %v", format, err)
	}
	if _, err := ParseFormat("xlsx"); err == nil {
		t.Fatal("unsupported format must fail")
	}
}

func TestSortForExportIsStable(t *testing.T) {
	items := sampleItems()
	sorted := SortForExport(items)
	if sorted[0].Verdict != model.VerdictDeleteCandidate {
		t.Fatalf("first = %s", sorted[0].Verdict)
	}
	if items[0].ID != "a1" {
		t.Fatal("input must not be reordered")
	}
}
