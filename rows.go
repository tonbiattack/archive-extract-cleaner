package main

import (
	"sort"
	"strings"
	"time"

	"github.com/tonbiattack/archive-extract-cleaner/internal/dashboard"
	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
)

type RowQuery struct {
	Search string `json:"search"`
	Filter string `json:"filter"`
	Sort   string `json:"sort"`
	Desc   bool   `json:"desc"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

type Row struct {
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
}

type RowPage struct {
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
	filtered := make([]model.Item, 0, len(items))
	for _, item := range items {
		if matchesFilter(item, query.Filter) && matchesSearch(item, query.Search) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func sortItems(items []model.Item, sortKey string, desc bool) {
	less := func(a, b model.Item) bool {
		switch sortKey {
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
