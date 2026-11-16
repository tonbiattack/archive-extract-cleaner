package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/report"
)

// CSV / JSON の書き出し。
//
// 出力はスキャン結果のスナップショットで、移動の可否を決めるものではない。
// 既定のファイル名は日時から作り、保存先はユーザーに選ばせる。

// ExportResult は書き出し結果。
type ExportResult struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
	Empty bool   `json:"empty"`
}

// ExportReport は保存ダイアログを出して CSV または JSON を書き出す。
// format が空なら拡張子や既定値から決める。
func (a *App) ExportReport(format string) (ExportResult, error) {
	parsed, err := report.ParseFormat(format)
	if err != nil {
		return ExportResult{}, err
	}
	a.mu.RLock()
	items := append([]model.Item(nil), a.items...)
	summary := a.summary
	roots := append([]string(nil), a.roots...)
	ctx := a.ctx
	a.mu.RUnlock()
	if ctx == nil {
		return ExportResult{}, errors.New("アプリケーションが初期化されていません")
	}
	if len(items) == 0 {
		return ExportResult{}, errors.New("書き出せるスキャン結果がありません。先にスキャンしてください")
	}

	defaultName := defaultExportName(parsed)
	selected, err := wailsruntime.SaveFileDialog(ctx, wailsruntime.SaveDialogOptions{
		Title:           "スキャン結果を書き出す",
		DefaultFilename: defaultName,
		Filters:         exportFilters(parsed),
	})
	if err != nil {
		return ExportResult{}, err
	}
	if strings.TrimSpace(selected) == "" {
		return ExportResult{}, errors.New("保存先が選択されていません")
	}
	path, err := ensureExtension(selected, parsed)
	if err != nil {
		return ExportResult{}, err
	}

	// 書き出し順は安定させる。画面のソート状態には依存しない。
	sorted := report.SortForExport(items)
	if err := writeReport(path, parsed, sorted, summary, roots); err != nil {
		return ExportResult{}, err
	}
	return ExportResult{Path: path, Count: len(sorted), Empty: len(sorted) == 0}, nil
}

func defaultExportName(format report.Format) string {
	stamp := time.Now().Format("20060102-150405")
	return fmt.Sprintf("archive-extract-cleaner-%s.%s", stamp, format)
}

func exportFilters(format report.Format) []wailsruntime.FileFilter {
	switch format {
	case report.FormatJSON:
		return []wailsruntime.FileFilter{{DisplayName: "JSON (*.json)", Pattern: "*.json"}}
	default:
		return []wailsruntime.FileFilter{{DisplayName: "CSV (*.csv)", Pattern: "*.csv"}}
	}
}

// ensureExtension は拡張子が無い場合に付与する。
func ensureExtension(path string, format report.Format) (string, error) {
	ext := filepath.Ext(path)
	want := "." + string(format)
	if strings.EqualFold(ext, want) {
		return path, nil
	}
	if ext == "" {
		return path + want, nil
	}
	// 別の拡張子が付いている場合は上書きせず、指定形式に合わせて付け替える。
	return strings.TrimSuffix(path, ext) + want, nil
}

func writeReport(path string, format report.Format, items []model.Item, summary model.Summary, roots []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// 一時ファイルへ書いてから置き換え、途中失敗で壊れたファイルを残さない。
	temp, err := os.CreateTemp(filepath.Dir(path), ".export-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() {
		if temp != nil {
			temp.Close()
			os.Remove(tempPath)
		}
	}()

	writer := bufio.NewWriter(temp)
	switch format {
	case report.FormatJSON:
		err = report.WriteJSON(writer, items, summary, roots)
	default:
		err = report.WriteCSV(writer, items)
	}
	if err != nil {
		return err
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	temp = nil
	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath)
		return err
	}
	return nil
}
