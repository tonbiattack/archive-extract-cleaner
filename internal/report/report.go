// Package report はスキャン結果を CSV / JSON へ書き出す。
//
// 出力は読み取り専用のスキャン結果だけを対象とし、移動の可否判定には使わない。
// CSV は Excel での確認と、外部ツール（AIを含む）への受け渡しを想定する。
package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"time"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
)

// Format は出力形式。
type Format string

const (
	FormatCSV  Format = "csv"
	FormatJSON Format = "json"
)

// ParseFormat は文字列から Format を返す。
func ParseFormat(value string) (Format, error) {
	switch Format(value) {
	case FormatCSV, "":
		return FormatCSV, nil
	case FormatJSON:
		return FormatJSON, nil
	default:
		return "", fmt.Errorf("未対応の形式です: %q", value)
	}
}

// CSVHeader は CSV の列名。末尾の列は順序を変えないこと。
var CSVHeader = []string{
	"id",
	"verdict",
	"status",
	"name",
	"zipPath",
	"folderPath",
	"parentPath",
	"zipSize",
	"folderSize",
	"size",
	"fileCount",
	"dirCount",
	"zipEntries",
	"comparedBytes",
	"matchMode",
	"nameVariant",
	"deletable",
	"reason",
	"details",
	"checkedAt",
	"deletedAt",
	"deleteError",
	"zipMoved",
	"folderMoved",
}

// Record は1行分の出力。
type Record struct {
	Item model.Item
}

// WriteCSV は items を CSV として w へ書く。
// UTF-8 の BOM は付けない（Excel で文字化けする場合は JSON か UTF-8 対応の
// エディタを使ってください）。details は " | " で連結する。
func WriteCSV(w io.Writer, items []model.Item) error {
	writer := csv.NewWriter(w)
	if err := writer.Write(CSVHeader); err != nil {
		return err
	}
	for _, item := range items {
		if err := writer.Write(csvRow(item)); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func csvRow(item model.Item) []string {
	return []string{
		item.ID,
		string(item.Verdict),
		string(item.Status),
		item.DisplayName(),
		item.ZipPath,
		item.FolderPath,
		item.ParentPath,
		strconv.FormatInt(item.ZipSize, 10),
		strconv.FormatInt(item.FolderSize, 10),
		strconv.FormatInt(item.Size, 10),
		strconv.Itoa(item.FileCount),
		strconv.Itoa(item.DirCount),
		strconv.Itoa(item.ZipEntries),
		strconv.FormatInt(item.ComparedBytes, 10),
		string(item.MatchMode),
		strconv.FormatBool(item.NameVariant),
		strconv.FormatBool(item.Deletable()),
		item.Reason,
		joinDetails(item.Details),
		formatTime(item.CheckedAt),
		formatTime(item.DeletedAt),
		item.DeleteError,
		strconv.FormatBool(item.ZipMoved),
		strconv.FormatBool(item.FolderMoved),
	}
}

func joinDetails(details []string) string {
	if len(details) == 0 {
		return ""
	}
	result := details[0]
	for _, detail := range details[1:] {
		result += " | " + detail
	}
	return result
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}

// jsonItem は JSON 出力用の項目。model.Item に CSV 用の派生値を足す。
type jsonItem struct {
	ID            string   `json:"id"`
	Verdict       string   `json:"verdict"`
	Status        string   `json:"status"`
	Name          string   `json:"name"`
	ZipPath       string   `json:"zipPath"`
	FolderPath    string   `json:"folderPath"`
	ParentPath    string   `json:"parentPath"`
	ZipSize       int64    `json:"zipSize"`
	FolderSize    int64    `json:"folderSize"`
	Size          int64    `json:"size"`
	FileCount     int      `json:"fileCount"`
	DirCount      int      `json:"dirCount"`
	ZipEntries    int      `json:"zipEntries"`
	ComparedBytes int64    `json:"comparedBytes"`
	MatchMode     string   `json:"matchMode,omitempty"`
	NameVariant   bool     `json:"nameVariant,omitempty"`
	Deletable     bool     `json:"deletable"`
	Reason        string   `json:"reason"`
	Details       []string `json:"details,omitempty"`
	CheckedAt     string   `json:"checkedAt,omitempty"`
	DeletedAt     string   `json:"deletedAt,omitempty"`
	DeleteError   string   `json:"deleteError,omitempty"`
	ZipMoved      bool     `json:"zipMoved"`
	FolderMoved   bool     `json:"folderMoved"`
}

type jsonDocument struct {
	GeneratedAt string        `json:"generatedAt"`
	Summary     model.Summary `json:"summary"`
	Roots       []string      `json:"roots"`
	Items       []jsonItem    `json:"items"`
}

// WriteJSON は items と summary を JSON として w へ書く。
// 構造はフラットな配列とし、外部ツールから扱いやすくする。
func WriteJSON(w io.Writer, items []model.Item, summary model.Summary, roots []string) error {
	document := jsonDocument{
		GeneratedAt: time.Now().Format(time.RFC3339),
		Summary:     summary,
		Roots:       append([]string(nil), roots...),
		Items:       make([]jsonItem, 0, len(items)),
	}
	if document.Roots == nil {
		document.Roots = []string{}
	}
	for _, item := range items {
		document.Items = append(document.Items, jsonItem{
			ID:            item.ID,
			Verdict:       string(item.Verdict),
			Status:        string(item.Status),
			Name:          item.DisplayName(),
			ZipPath:       item.ZipPath,
			FolderPath:    item.FolderPath,
			ParentPath:    item.ParentPath,
			ZipSize:       item.ZipSize,
			FolderSize:    item.FolderSize,
			Size:          item.Size,
			FileCount:     item.FileCount,
			DirCount:      item.DirCount,
			ZipEntries:    item.ZipEntries,
			ComparedBytes: item.ComparedBytes,
			MatchMode:     string(item.MatchMode),
			NameVariant:   item.NameVariant,
			Deletable:     item.Deletable(),
			Reason:        item.Reason,
			Details:       item.Details,
			CheckedAt:     formatTime(item.CheckedAt),
			DeletedAt:     formatTime(item.DeletedAt),
			DeleteError:   item.DeleteError,
			ZipMoved:      item.ZipMoved,
			FolderMoved:   item.FolderMoved,
		})
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(document)
}

// SortForExport は出力順を安定させる。判定順、親パス、名前の順。
func SortForExport(items []model.Item) []model.Item {
	result := append([]model.Item(nil), items...)
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Verdict != result[j].Verdict {
			return result[i].Verdict < result[j].Verdict
		}
		if result[i].ParentPath != result[j].ParentPath {
			return result[i].ParentPath < result[j].ParentPath
		}
		return result[i].DisplayName() < result[j].DisplayName()
	})
	return result
}
