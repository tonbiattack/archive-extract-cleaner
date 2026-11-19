package main

import (
	"sort"
	"strings"
	"time"

	"github.com/tonbiattack/archive-extract-cleaner/internal/dashboard"
	"github.com/tonbiattack/archive-extract-cleaner/internal/duplicates"
	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
)

type RowQuery struct {
	Triage string `json:"triage"`
	Search string `json:"search"`
	Filter string `json:"filter"`
	Sort   string `json:"sort"`
	Desc   bool   `json:"desc"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

type Row struct {
	Triage      string    `json:"triage"`
	ID          string    `json:"id"`
	Verdict     string    `json:"verdict"`
	Status      string    `json:"status"`
	Name        string    `json:"name"`
	ZipPath     string    `json:"zipPath"`
	FolderPath  string    `json:"folderPath"`
	ParentPath  string    `json:"parentPath"`
	Size        int64     `json:"size"`
	FolderSize  int64     `json:"folderSize"`
	ZipSize     int64     `json:"zipSize"`
	Reason      string    `json:"reason"`
	MatchMode   string    `json:"matchMode"`
	NameVariant bool      `json:"nameVariant"`
	FileCount   int       `json:"fileCount"`
	DirCount    int       `json:"dirCount"`
	ZipEntries  int       `json:"zipEntries"`
	CheckedAt   time.Time `json:"checkedAt"`
	DeletedAt   time.Time `json:"deletedAt"`
	DeleteError string    `json:"deleteError"`
	Deletable   bool      `json:"deletable"`
	ZipMoved    bool      `json:"zipMoved"`
	FolderMoved bool      `json:"folderMoved"`
	// Duplicate は E/F/G の保存先にある同名アーカイブの検索結果。
	// 未検索なら nil。同名は同一内容を意味しないため、削除の可否には使わない。
	Duplicate *duplicates.Result `json:"duplicate,omitempty"`
}

type RowPage struct {
	TriageError  string              `json:"triageError,omitempty"`
	Rows         []Row               `json:"rows"`
	Total        int                 `json:"total"`
	Offset       int                 `json:"offset"`
	Summary      model.Summary       `json:"summary"`
	Dashboard    dashboard.Dashboard `json:"dashboard"`
	Warnings     []string            `json:"warnings"`
	WarningTotal int                 `json:"warningTotal"`
	Roots        []string            `json:"roots"`
}

func verdictRank(v model.Verdict) int {
	switch v {
	case model.VerdictDeleteCandidate:
		return 0
	case model.VerdictDeleteCandidateIgnorable:
		return 1
	case model.VerdictMismatch:
		return 2
	case model.VerdictAmbiguous:
		return 3
	case model.VerdictUnverified:
		return 4
	case model.VerdictKeepArchive:
		return 5
	case model.VerdictKeepDirectory:
		return 6
	}
	return 7
}

func matchesFilter(item model.Item, filter string) bool {
	switch filter {
	case "candidate":
		return item.Deletable()
	case "ignorable":
		return item.Verdict == model.VerdictDeleteCandidateIgnorable && item.Status != model.StatusDeleted
	case "mismatch":
		return item.Verdict == model.VerdictMismatch
	case "unverified":
		return item.Verdict == model.VerdictUnverified || item.Verdict == model.VerdictAmbiguous
	case "keep":
		return item.Verdict.IsKeep()
	default:
		return true
	}
}

func matchesSearch(item model.Item, search string) bool {
	if search == "" {
		return true
	}
	search = strings.ToLower(search)
	values := []string{item.DisplayName(), item.ZipPath, item.FolderPath, item.ParentPath, item.Name}
	for _, value := range values {
		if value != "" && strings.Contains(strings.ToLower(value), search) {
			return true
		}
	}
	return false
}

func filterItems(items []model.Item, query RowQuery) []model.Item {
	return filterItemsWithDuplicates(items, query, nil)
}

// filterItemsWithDuplicates は同名アーカイブの検索結果を考慮して絞り込む。
// duplicates が nil なら重複フィルタは何もしない（未検索と同じ扱い）。
func filterItemsWithDuplicates(items []model.Item, query RowQuery, results map[string]duplicates.Result) []model.Item {
	filtered := make([]model.Item, 0, len(items))
	for _, item := range items {
		if !matchesFilter(item, query.Filter) || !matchesSearch(item, query.Search) {
			continue
		}
		if !matchesDuplicateFilter(item, query.Filter, results) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

// matchesDuplicateFilter は同名アーカイブに関するフィルタを判定する。
//
// "duplicate" は同名が1件以上見つかった項目、"duplicate-same-size" は
// 同名かつ同サイズの項目に限る。どちらも「内容が同じ」意味ではない。
func matchesDuplicateFilter(item model.Item, filter string, results map[string]duplicates.Result) bool {
	switch filter {
	case "duplicate", "duplicate-same-size":
	default:
		return true
	}
	result, ok := results[item.ID]
	if !ok {
		return false
	}
	if filter == "duplicate" {
		return len(result.Hits) > 0
	}
	for _, hit := range result.Hits {
		if hit.SameSize {
			return true
		}
	}
	return false
}

func sortItems(items []model.Item, sortKey string, desc bool) {
	sortItemsWithDuplicates(items, sortKey, desc, nil)
}

// sortItemsWithDuplicates は同名アーカイブの検索結果を考慮して並べ替える。
// "duplicate" は同名ありを先に、"duplicate-size" は同サイズありを先にする。
func sortItemsWithDuplicates(items []model.Item, sortKey string, desc bool, results map[string]duplicates.Result) {
	duplicateRank := func(item model.Item) int {
		result, ok := results[item.ID]
		if !ok || len(result.Hits) == 0 {
			return 1
		}
		return 0
	}
	sameSizeRank := func(item model.Item) int {
		result, ok := results[item.ID]
		if !ok {
			return 1
		}
		for _, hit := range result.Hits {
			if hit.SameSize {
				return 0
			}
		}
		return 1
	}
	less := func(a, b model.Item) bool {
		switch sortKey {
		case "duplicate":
			if ra, rb := duplicateRank(a), duplicateRank(b); ra != rb {
				return ra < rb
			}
		case "duplicate-size":
			if ra, rb := sameSizeRank(a), sameSizeRank(b); ra != rb {
				return ra < rb
			}
		case "name":
			return strings.ToLower(a.DisplayName()) < strings.ToLower(b.DisplayName())
		case "zip":
			return strings.ToLower(a.ZipPath) < strings.ToLower(b.ZipPath)
		case "folder":
			return strings.ToLower(a.FolderPath) < strings.ToLower(b.FolderPath)
		case "parent":
			return strings.ToLower(a.ParentPath) < strings.ToLower(b.ParentPath)
		case "size":
			if a.Size != b.Size {
				return a.Size < b.Size
			}
		case "reason":
			return strings.ToLower(a.Reason) < strings.ToLower(b.Reason)
		}
		ra, rb := verdictRank(a.Verdict), verdictRank(b.Verdict)
		if ra != rb {
			return ra < rb
		}
		if !strings.EqualFold(a.ParentPath, b.ParentPath) {
			return strings.ToLower(a.ParentPath) < strings.ToLower(b.ParentPath)
		}
		return strings.ToLower(a.DisplayName()) < strings.ToLower(b.DisplayName())
	}
	if desc {
		sort.SliceStable(items, func(i, j int) bool { return less(items[j], items[i]) })
		return
	}
	sort.SliceStable(items, func(i, j int) bool { return less(items[i], items[j]) })
}

func toRow(item model.Item) Row {
	return Row{
		ID:          item.ID,
		Verdict:     string(item.Verdict),
		Status:      string(item.Status),
		Name:        item.DisplayName(),
		ZipPath:     item.ZipPath,
		FolderPath:  item.FolderPath,
		ParentPath:  item.ParentPath,
		Size:        item.Size,
		FolderSize:  item.FolderSize,
		ZipSize:     item.ZipSize,
		Reason:      item.Reason,
		MatchMode:   string(item.MatchMode),
		NameVariant: item.NameVariant,
		FileCount:   item.FileCount,
		DirCount:    item.DirCount,
		ZipEntries:  item.ZipEntries,
		CheckedAt:   item.CheckedAt,
		DeletedAt:   item.DeletedAt,
		DeleteError: item.DeleteError,
		Deletable:   item.Deletable(),
		ZipMoved:    item.ZipMoved,
		FolderMoved: item.FolderMoved,
	}
}
