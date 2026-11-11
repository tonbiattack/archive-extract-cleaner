package transfer

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
)

type Candidate struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Verdict  string `json:"verdict"`
	Status   string `json:"status"`
	Reason   string `json:"reason"`
	Review   bool   `json:"review"`
	Selected bool   `json:"selected"`
	Exists   bool   `json:"exists"`
}

type Review struct {
	Candidates       []Candidate `json:"candidates"`
	RecommendedCount int         `json:"recommendedCount"`
	ReviewCount      int         `json:"reviewCount"`
	MissingCount     int         `json:"missingCount"`
	ScriptPath       string      `json:"scriptPath"`
	ScriptAvailable  bool        `json:"scriptAvailable"`
	Preset           string      `json:"preset"`
	Busy             bool        `json:"busy"`
}

type LaunchResult struct {
	Launched      bool   `json:"launched"`
	Mode          string `json:"mode"`
	PathCount     int    `json:"pathCount"`
	ScriptPath    string `json:"scriptPath"`
	Preset        string `json:"preset"`
	PresetSkipped bool   `json:"presetSkipped"`
	Warning       string `json:"warning,omitempty"`
	ManifestPath  string `json:"manifestPath,omitempty"`
}

type reviewBuilder struct {
	byKey      map[string]int
	candidates []Candidate
}

func BuildReview(items []model.Item, scriptPath, preset string, busy bool) Review {
	builder := &reviewBuilder{byKey: map[string]int{}}
	for _, item := range items {
		addItem(builder, item)
	}
	sort.SliceStable(builder.candidates, func(i, j int) bool {
		a, b := builder.candidates[i], builder.candidates[j]
		if a.Review != b.Review {
			return !a.Review
		}
		if a.Exists != b.Exists {
			return a.Exists
		}
		return strings.ToLower(a.Path) < strings.ToLower(b.Path)
	})
	review := Review{
		Candidates: builder.candidates,
		ScriptPath: scriptPath,
		Preset:     preset,
		Busy:       busy,
	}
	for _, candidate := range review.Candidates {
		if candidate.Review {
			review.ReviewCount++
		} else {
			review.RecommendedCount++
		}
		if !candidate.Exists {
			review.MissingCount++
		}
	}
	review.ScriptAvailable = ScriptAvailable(scriptPath)
	return review
}

func addItem(builder *reviewBuilder, item model.Item) {
	if item.Status == model.StatusDeleted {
		if !item.ZipMoved {
			addCandidate(builder, item, item.ZipPath, "zip", false)
		}
		return
	}
	needsReview := item.Status == model.StatusError ||
		item.Verdict == model.VerdictMismatch ||
		item.Verdict == model.VerdictUnverified ||
		item.Verdict == model.VerdictAmbiguous

	switch item.Verdict {
	case model.VerdictDeleteCandidate:
		if !item.ZipMoved {
			addCandidate(builder, item, item.ZipPath, "zip", needsReview)
		}
		if !item.FolderMoved && item.FolderPath != "" {
			_, zipErr := os.Lstat(item.ZipPath)
			_, folderErr := os.Lstat(item.FolderPath)
			if item.ZipMoved || item.ZipPath == "" || zipErr != nil || folderErr != nil {
				addCandidate(builder, item, item.FolderPath, "folder", true)
			}
		}
	case model.VerdictKeepArchive:
		if !item.ZipMoved {
			addCandidate(builder, item, item.ZipPath, "zip", needsReview)
		}
	case model.VerdictKeepDirectory:
		if !item.FolderMoved {
			addCandidate(builder, item, item.FolderPath, "folder", needsReview)
		}
	case model.VerdictMismatch, model.VerdictUnverified:
		if !item.ZipMoved {
			addCandidate(builder, item, item.ZipPath, "zip", true)
		}
		if !item.FolderMoved {
			addCandidate(builder, item, item.FolderPath, "folder", true)
		}
	case model.VerdictAmbiguous:
		if !item.ZipMoved {
			addCandidate(builder, item, item.ZipPath, "zip", true)
		}
		if !item.FolderMoved {
			addCandidate(builder, item, item.FolderPath, "folder", true)
		}
		for _, folder := range item.RelatedFolders {
			addCandidate(builder, item, folder, "folder", true)
		}
	}
}

func addCandidate(builder *reviewBuilder, item model.Item, path, kind string, review bool) {
	if strings.TrimSpace(path) == "" {
		return
	}
	abs, err := winpath.CleanAbs(path)
	if err != nil {
		return
	}
	key := winpath.Key(abs)
	exists := true
	info, err := os.Lstat(abs)
	if err != nil {
		exists = false
	}
	if exists && kind == "folder" && !info.IsDir() {
		kind = "zip"
	}
	reason := item.Reason
	if !exists {
		review = true
		reason = strings.TrimSpace(reason + "（パスが見つかりません）")
	}
	if index, ok := builder.byKey[key]; ok {
		existing := &builder.candidates[index]
		if review {
			existing.Review = true
			existing.Selected = false
		}
		if !exists {
			existing.Exists = false
			existing.Review = true
			existing.Selected = false
		}
		if reason != "" && !strings.Contains(existing.Reason, reason) {
			if existing.Reason == "" {
				existing.Reason = reason
			} else {
				existing.Reason += " / " + reason
			}
		}
		return
	}
	builder.byKey[key] = len(builder.candidates)
	builder.candidates = append(builder.candidates, Candidate{
		ID:       model.MakeID("transfer", abs),
		Path:     abs,
		Name:     filepath.Base(abs),
		Kind:     kind,
		Verdict:  string(item.Verdict),
		Status:   string(item.Status),
		Reason:   reason,
		Review:   review,
		Selected: !review && exists,
		Exists:   exists,
	})
}

func ScriptAvailable(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func DefaultScriptPath() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		if cache, err := os.UserCacheDir(); err == nil {
			base = cache
		}
	}
	if base == "" {
		return ""
	}
	return filepath.Join(base, "Programs", "MultiDriveCopy", "MultiDriveCopy.ps1")
}
