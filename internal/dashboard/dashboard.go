// Package dashboard は整理前後の容量を集計する。
//
// 重要な前提:
//   - 「回収見込み」は削除候補（内容一致済み）の容量だけを指す。
//     MISMATCH / AMBIGUOUS / UNVERIFIED は含めない。
//   - 「移動した容量」はごみ箱へ移したバイト数であり、ディスクの空き容量が
//     そのぶん増えたことを意味しない。ごみ箱を空にするまで物理領域は残る。
//   - 集計は表示専用で、削除の可否判定には使わない。
package dashboard

import (
	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
)

// Category は件数とサイズの組。
type Category struct {
	Count int   `json:"count"`
	Bytes int64 `json:"bytes"`
}

// Add は件数とサイズを加算する。
func (c *Category) Add(count int, bytes int64) {
	c.Count += count
	c.Bytes += bytes
}

// Scan はスキャン結果の内訳。スキャン全体の値であり、フィルタ後の値ではない。
type Scan struct {
	Total       Category `json:"total"`
	Reclaimable Category `json:"reclaimable"`
	KeepArchive Category `json:"keepArchive"`
	KeepFolder  Category `json:"keepFolder"`
	Mismatch    Category `json:"mismatch"`
	Ambiguous   Category `json:"ambiguous"`
	Unverified  Category `json:"unverified"`
	// Recycled は移動済みの項目数。Total や他の分類とは独立した情報で、
	// 内訳の合計には含めない（移動後は判定が KEEP_* に変わるため）。
	Recycled Category `json:"recycled"`
}

// Moves は実際の移動結果の集計。
type Moves struct {
	Succeeded Category `json:"succeeded"`
	Partial   Category `json:"partial"`
	Failed    Category `json:"failed"`
	Skipped   Category `json:"skipped"`
	// MovedBytes はごみ箱へ移したバイト数（成功＋部分成功の成功分）。
	MovedBytes int64 `json:"movedBytes"`
	// AllSucceeded は1件以上の移動がすべて成功したか。
	AllSucceeded bool `json:"allSucceeded"`
}

// Dashboard は画面へ渡す集計。
type Dashboard struct {
	Scan  Scan  `json:"scan"`
	Moves Moves `json:"moves"`
	// Filtered は一覧が絞り込まれているかを示す。
	// Scan はスキャン全体、FilteredCount は表示中の件数。
	Filtered      bool `json:"filtered"`
	FilteredCount int  `json:"filteredCount"`
}

// BuildScan は項目一覧からスキャン内訳を作る。
func BuildScan(items []model.Item) Scan {
	var scan Scan
	for _, item := range items {
		size := itemSizeForVerdict(item)
		scan.Total.Add(1, size)

		// 移動済みは判定ではなく状態。内訳とは別に数え、二重計上しない。
		if item.Status == model.StatusDeleted {
			scan.Recycled.Add(1, size)
		}

		switch item.Verdict {
		case model.VerdictDeleteCandidate, model.VerdictDeleteCandidateIgnorable:
			if item.Status == model.StatusDeleted {
				continue
			}
			scan.Reclaimable.Add(1, size)
		case model.VerdictKeepArchive:
			scan.KeepArchive.Add(1, size)
		case model.VerdictKeepDirectory:
			scan.KeepFolder.Add(1, size)
		case model.VerdictMismatch:
			scan.Mismatch.Add(1, size)
		case model.VerdictAmbiguous:
			scan.Ambiguous.Add(1, size)
		case model.VerdictUnverified:
			scan.Unverified.Add(1, size)
		}
	}
	return scan
}

// itemSizeForVerdict は判定ごとの代表サイズを返す。
// 削除候補は実際に移動するフォルダのサイズ、ZIP保持はZIPのサイズを使う。
func itemSizeForVerdict(item model.Item) int64 {
	switch item.Verdict {
	case model.VerdictKeepArchive:
		if item.ZipSize > 0 {
			return item.ZipSize
		}
		return item.Size
	case model.VerdictDeleteCandidate, model.VerdictDeleteCandidateIgnorable:
		if item.Size > 0 {
			return item.Size
		}
		return item.FolderSize
	default:
		if item.FolderSize > 0 {
			return item.FolderSize
		}
		return item.Size
	}
}

// AddMoveResult は移動結果を集計へ加算する。
// 成功・部分成功・失敗・スキップをサイズ付きで数える。
func (m *Moves) AddMoveResult(result MoveOutcome) {
	switch {
	case result.Skipped:
		m.Skipped.Add(1, result.Bytes)
	case result.Partial:
		m.Partial.Add(1, result.Bytes)
		m.MovedBytes += result.MovedBytes
	case result.Success:
		m.Succeeded.Add(1, result.Bytes)
		m.MovedBytes += result.MovedBytes
	default:
		m.Failed.Add(1, result.Bytes)
	}
}

// MoveOutcome は移動1件分の結果。moves.go の型に依存しないよう独立させる。
type MoveOutcome struct {
	Success bool
	Partial bool
	Skipped bool
	// Bytes は対象としていた容量。
	Bytes int64
	// MovedBytes は実際にごみ箱へ移せた容量。
	MovedBytes int64
}

// Build は項目一覧と移動集計からダッシュボードを組み立てる。
func Build(items []model.Item, moves Moves, filtered bool, filteredCount int) Dashboard {
	return Dashboard{
		Scan:          BuildScan(items),
		Moves:         moves,
		Filtered:      filtered,
		FilteredCount: filteredCount,
	}
}

// RecycleNote はごみ箱の性質を説明する文言を返す。
// 「空き容量が増えた」と誤解させないために必ず表示する。
func RecycleNote() string {
	return "ごみ箱へ移動した容量は、ごみ箱を空にするまでディスクの空き容量として戻りません。"
}
