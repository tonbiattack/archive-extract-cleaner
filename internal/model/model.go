package model

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Verdict string

const (
	VerdictKeepArchive     Verdict = "KEEP_ARCHIVE"
	VerdictKeepDirectory   Verdict = "KEEP_DIRECTORY"
	VerdictDeleteCandidate Verdict = "DELETE_CANDIDATE"
	// VerdictDeleteCandidateIgnorable は、差分がOSやアーカイバが自動生成する
	// 補助ファイル（__MACOSX/、.DS_Store など）だけである組。
	// 共通部分の内容は SHA-256 で完全一致を確認済みであることを前提とする。
	VerdictDeleteCandidateIgnorable Verdict = "DELETE_CANDIDATE_IGNORABLE"
	VerdictMismatch                 Verdict = "MISMATCH"
	VerdictAmbiguous                Verdict = "AMBIGUOUS"
	VerdictUnverified               Verdict = "UNVERIFIED"
)

func (v Verdict) IsKeep() bool {
	return v == VerdictKeepArchive || v == VerdictKeepDirectory
}

func (v Verdict) IsProblem() bool {
	return v == VerdictMismatch || v == VerdictAmbiguous || v == VerdictUnverified
}

// IsDeleteCandidate は削除候補として扱える判定かを返す。
func (v Verdict) IsDeleteCandidate() bool {
	return v == VerdictDeleteCandidate || v == VerdictDeleteCandidateIgnorable
}

type Status string

const (
	StatusReady   Status = "ready"
	StatusDeleted Status = "deleted"
	StatusError   Status = "error"
)

type MatchMode string

const (
	MatchDirect MatchMode = "direct"
	MatchStrip  MatchMode = "strip-top"
)

type Item struct {
	ID             string    `json:"id"`
	Verdict        Verdict   `json:"verdict"`
	Status         Status    `json:"status"`
	ZipPath        string    `json:"zipPath"`
	FolderPath     string    `json:"folderPath"`
	ParentPath     string    `json:"parentPath"`
	Name           string    `json:"name"`
	ZipSize        int64     `json:"zipSize"`
	FolderSize     int64     `json:"folderSize"`
	Size           int64     `json:"size"`
	Reason         string    `json:"reason"`
	Details        []string  `json:"details,omitempty"`
	RelatedFolders []string  `json:"relatedFolders,omitempty"`
	MatchMode      MatchMode `json:"matchMode,omitempty"`
	// NameVariant は ZIP名とフォルダ名が完全一致せず、重複名の接尾辞を
	// 除いた正規化名で対応したことを示す。
	NameVariant   bool      `json:"nameVariant,omitempty"`
	FileCount     int       `json:"fileCount"`
	DirCount      int       `json:"dirCount"`
	ZipEntries    int       `json:"zipEntries"`
	ComparedBytes int64     `json:"comparedBytes"`
	CheckedAt     time.Time `json:"checkedAt"`
	DeletedAt     time.Time `json:"deletedAt,omitempty"`
	DeleteError   string    `json:"deleteError,omitempty"`
	ZipMoved      bool      `json:"zipMoved"`
	FolderMoved   bool      `json:"folderMoved"`
}

func (i Item) Deletable() bool {
	if !i.Verdict.IsDeleteCandidate() || i.Status == StatusDeleted {
		return false
	}
	return (i.ZipPath != "" && !i.ZipMoved) || (i.FolderPath != "" && !i.FolderMoved)
}

func (i Item) DisplayName() string {
	if i.Name != "" {
		return i.Name
	}
	if i.FolderPath != "" {
		return filepath.Base(i.FolderPath)
	}
	if i.ZipPath != "" {
		return filepath.Base(i.ZipPath)
	}
	return i.ID
}

func MakeID(zipPath, folderPath string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(zipPath)) + "\x00" + strings.ToLower(filepath.Clean(folderPath))))
	return hex.EncodeToString(sum[:12])
}

type Summary struct {
	Total          int             `json:"total"`
	ByVerdict      map[Verdict]int `json:"byVerdict"`
	CandidateCount int             `json:"candidateCount"`
	CandidateBytes int64           `json:"candidateBytes"`
	ArchiveCount   int             `json:"archiveCount"`
	DirectoryCount int             `json:"directoryCount"`
	DeletedCount   int             `json:"deletedCount"`
	ScannedAt      time.Time       `json:"scannedAt"`
	DurationMS     int64           `json:"durationMs"`
	// BytesByVerdict は判定ごとの対象サイズ。
	// 削除候補はフォルダサイズ、ZIP保持はZIPサイズ、それ以外はフォルダ側のサイズ。
	BytesByVerdict map[Verdict]int64 `json:"bytesByVerdict"`
	// ZipBytes / FolderBytes は種別ごとの合計。二重計上を避けるため分けて持つ。
	ZipBytes    int64 `json:"zipBytes"`
	FolderBytes int64 `json:"folderBytes"`
}

func Summarize(items []Item) Summary {
	s := Summary{
		ByVerdict:      map[Verdict]int{},
		BytesByVerdict: map[Verdict]int64{},
	}
	for _, item := range items {
		s.Total++
		s.ByVerdict[item.Verdict]++
		s.BytesByVerdict[item.Verdict] += itemSizeForVerdict(item)
		if item.ZipPath != "" {
			s.ZipBytes += item.ZipSize
		}
		if item.FolderPath != "" {
			s.FolderBytes += item.FolderSize
		}
		if item.Status == StatusDeleted {
			s.DeletedCount++
			continue
		}
		if item.Verdict.IsDeleteCandidate() {
			s.CandidateCount++
			s.CandidateBytes += item.Size
		}
		if item.ZipPath != "" {
			s.ArchiveCount++
		}
		if item.FolderPath != "" {
			s.DirectoryCount++
		}
	}
	return s
}

// itemSizeForVerdict は判定ごとの代表サイズを返す。
// 削除候補は実際に移動するフォルダのサイズ、ZIP保持はZIPのサイズを使う。
func itemSizeForVerdict(item Item) int64 {
	switch item.Verdict {
	case VerdictKeepArchive:
		if item.ZipSize > 0 {
			return item.ZipSize
		}
		return item.Size
	case VerdictDeleteCandidate, VerdictDeleteCandidateIgnorable:
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

type Phase string

const (
	PhaseIdle      Phase = "idle"
	PhaseWalking   Phase = "walking"
	PhaseVerifying Phase = "verifying"
	PhaseDone      Phase = "done"
	PhaseCancelled Phase = "cancelled"
)

type Progress struct {
	Phase      Phase  `json:"phase"`
	Current    string `json:"current"`
	WalkedDirs int    `json:"walkedDirs"`
	Archives   int    `json:"archives"`
	Folders    int    `json:"folders"`
	Completed  int    `json:"completed"`
	Total      int    `json:"total"`
	Bytes      int64  `json:"bytes"`
	ElapsedMS  int64  `json:"elapsedMs"`
	// ETAMS は照合フェーズの残り時間の推定値。算出できない場合は -1。
	ETAMS int64 `json:"etaMs"`
}

type Limits struct {
	MaxEntries          int    `json:"maxEntries"`
	MaxTotalBytes       uint64 `json:"maxTotalBytes"`
	MaxFileBytes        uint64 `json:"maxFileBytes"`
	MaxCompressionRatio uint64 `json:"maxCompressionRatio"`
}

func DefaultLimits() Limits {
	return Limits{
		MaxEntries:          200000,
		MaxTotalBytes:       64 << 30,
		MaxFileBytes:        16 << 30,
		MaxCompressionRatio: 1000,
	}
}

type DeletePreviewItem struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ZipPath    string `json:"zipPath"`
	FolderPath string `json:"folderPath"`
	Size       int64  `json:"size"`
}

type DeletePreview struct {
	Count int                 `json:"count"`
	Bytes int64               `json:"bytes"`
	Items []DeletePreviewItem `json:"items"`
}

type DeleteResult struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	ZipPath    string    `json:"zipPath"`
	FolderPath string    `json:"folderPath"`
	Success    bool      `json:"success"`
	Skipped    bool      `json:"skipped"`
	Reason     string    `json:"reason"`
	DeletedAt  time.Time `json:"deletedAt"`
	LogPath    string    `json:"logPath"`
}

func SortItems(items []Item) {
	rank := func(v Verdict) int {
		switch v {
		case VerdictDeleteCandidate:
			return 0
		case VerdictDeleteCandidateIgnorable:
			return 1
		case VerdictMismatch:
			return 2
		case VerdictAmbiguous:
			return 3
		case VerdictUnverified:
			return 4
		case VerdictKeepArchive:
			return 5
		case VerdictKeepDirectory:
			return 6
		default:
			return 7
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		ri, rj := rank(items[i].Verdict), rank(items[j].Verdict)
		if ri != rj {
			return ri < rj
		}
		if items[i].ParentPath != items[j].ParentPath {
			return strings.ToLower(items[i].ParentPath) < strings.ToLower(items[j].ParentPath)
		}
		return strings.ToLower(items[i].DisplayName()) < strings.ToLower(items[j].DisplayName())
	})
}
