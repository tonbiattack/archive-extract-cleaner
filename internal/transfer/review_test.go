package transfer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
)

func writeTestFile(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func makeTestDir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func findCandidate(t *testing.T, review Review, path string) Candidate {
	t.Helper()
	key := winpath.Key(path)
	for _, candidate := range review.Candidates {
		if winpath.Key(candidate.Path) == key {
			return candidate
		}
	}
	t.Fatalf("candidate not found: %s", path)
	return Candidate{}
}

func TestBuildReviewPrefersZIPAndKeepsReviewItems(t *testing.T) {
	root := t.TempDir()
	script := writeTestFile(t, filepath.Join(root, "MultiDriveCopy.ps1"))
	zipKeep := writeTestFile(t, filepath.Join(root, "keep.zip"))
	folderKeep := makeTestDir(t, filepath.Join(root, "folder-only"))
	candidateZip := writeTestFile(t, filepath.Join(root, "candidate.zip"))
	candidateFolder := makeTestDir(t, filepath.Join(root, "candidate"))
	mismatchZip := writeTestFile(t, filepath.Join(root, "mismatch.zip"))
	mismatchFolder := makeTestDir(t, filepath.Join(root, "mismatch"))
	ambiguousZip := writeTestFile(t, filepath.Join(root, "ambiguous.zip"))
	ambiguousOne := makeTestDir(t, filepath.Join(root, "ambiguous-one"))
	ambiguousTwo := makeTestDir(t, filepath.Join(root, "ambiguous-two"))
	unverifiedZip := writeTestFile(t, filepath.Join(root, "unverified.zip"))
	unverifiedFolder := makeTestDir(t, filepath.Join(root, "unverified"))
	deletedZip := writeTestFile(t, filepath.Join(root, "deleted.zip"))
	deletedFolder := makeTestDir(t, filepath.Join(root, "deleted"))
	missingZip := filepath.Join(root, "missing.zip")

	items := []model.Item{
		{Verdict: model.VerdictKeepArchive, Status: model.StatusReady, ZipPath: zipKeep, Reason: "zip only"},
		{Verdict: model.VerdictKeepDirectory, Status: model.StatusReady, FolderPath: folderKeep, Reason: "folder only"},
		{Verdict: model.VerdictDeleteCandidate, Status: model.StatusReady, ZipPath: candidateZip, FolderPath: candidateFolder, Reason: "exact"},
		{Verdict: model.VerdictMismatch, Status: model.StatusReady, ZipPath: mismatchZip, FolderPath: mismatchFolder, Reason: "mismatch"},
		{Verdict: model.VerdictAmbiguous, Status: model.StatusReady, ZipPath: ambiguousZip, RelatedFolders: []string{ambiguousOne, ambiguousTwo}, Reason: "ambiguous"},
		{Verdict: model.VerdictUnverified, Status: model.StatusReady, ZipPath: unverifiedZip, FolderPath: unverifiedFolder, Reason: "unverified"},
		{Verdict: model.VerdictKeepArchive, Status: model.StatusDeleted, ZipPath: deletedZip, FolderPath: deletedFolder, Reason: "moved"},
		{Verdict: model.VerdictKeepArchive, Status: model.StatusReady, ZipPath: missingZip, Reason: "missing"},
	}
	review := BuildReview(items, script, "Backup", false)

	if !review.ScriptAvailable || review.Preset != "Backup" {
		t.Fatalf("review metadata = %+v", review)
	}
	if review.RecommendedCount != 4 {
		t.Fatalf("recommended = %d, want 4", review.RecommendedCount)
	}
	if review.ReviewCount != 8 {
		t.Fatalf("review = %d, want 8", review.ReviewCount)
	}
	if review.MissingCount != 1 {
		t.Fatalf("missing = %d, want 1", review.MissingCount)
	}

	if candidate := findCandidate(t, review, candidateZip); !candidate.Selected || candidate.Review {
		t.Fatalf("candidate zip = %+v", candidate)
	}
	for _, path := range []string{candidateFolder} {
		for _, candidate := range review.Candidates {
			if winpath.Key(candidate.Path) == winpath.Key(path) {
				t.Fatalf("candidate folder must not be included when ZIP exists: %+v", candidate)
			}
		}
	}
	for _, path := range []string{mismatchZip, mismatchFolder, ambiguousOne, ambiguousTwo, unverifiedZip} {
		candidate := findCandidate(t, review, path)
		if !candidate.Review || candidate.Selected {
			t.Fatalf("review candidate %s = %+v", path, candidate)
		}
	}
	if candidate := findCandidate(t, review, deletedZip); !candidate.Selected || candidate.Review {
		t.Fatalf("deleted zip = %+v", candidate)
	}
	if candidate := findCandidate(t, review, missingZip); candidate.Exists || candidate.Selected || !candidate.Review {
		t.Fatalf("missing zip = %+v", candidate)
	}
	for index, candidate := range review.Candidates {
		if candidate.Review && index < review.RecommendedCount {
			t.Fatalf("recommended candidates must sort before review candidates: %+v", review.Candidates)
		}
	}
}

func TestBuildReviewMergesDuplicatePathsConservatively(t *testing.T) {
	root := t.TempDir()
	zip := writeTestFile(t, filepath.Join(root, "same.zip"))
	items := []model.Item{
		{Verdict: model.VerdictKeepArchive, Status: model.StatusReady, ZipPath: zip, Reason: "keep"},
		{Verdict: model.VerdictMismatch, Status: model.StatusReady, ZipPath: zip, Reason: "mismatch"},
	}
	review := BuildReview(items, "", "", false)
	if len(review.Candidates) != 1 {
		t.Fatalf("candidates = %+v", review.Candidates)
	}
	candidate := review.Candidates[0]
	if !candidate.Review || candidate.Selected {
		t.Fatalf("duplicate candidate = %+v", candidate)
	}
}

func TestBuildReviewEmpty(t *testing.T) {
	review := BuildReview(nil, "", "", true)
	if len(review.Candidates) != 0 || !review.Busy || review.ScriptAvailable {
		t.Fatalf("review = %+v", review)
	}
}

// 連携スクリプトが使えない理由を、UI が案内に使える区分で返すこと。
// 「未導入」と「設定したパスの誤り」を混同しない。
func TestDescribeScriptDistinguishesReasons(t *testing.T) {
	root := t.TempDir()
	installed := writeTestFile(t, filepath.Join(root, "Programs", "MultiDriveCopy", "MultiDriveCopy.ps1"))
	directory := makeTestDir(t, filepath.Join(root, "not-a-file"))

	cases := []struct {
		name        string
		path        string
		defaultPath string
		wantOK      bool
		wantReason  string
	}{
		{name: "installed", path: installed, defaultPath: installed, wantOK: true, wantReason: ""},
		{name: "unconfigured", path: "", defaultPath: installed, wantOK: false, wantReason: ScriptReasonUnconfigured},
		{name: "not installed at default path", path: filepath.Join(root, "missing", "MultiDriveCopy.ps1"), defaultPath: filepath.Join(root, "missing", "MultiDriveCopy.ps1"), wantOK: false, wantReason: ScriptReasonNotInstalled},
		{name: "configured path missing", path: filepath.Join(root, "elsewhere", "MultiDriveCopy.ps1"), defaultPath: installed, wantOK: false, wantReason: ScriptReasonNotFound},
		{name: "path is a directory", path: directory, defaultPath: installed, wantOK: false, wantReason: ScriptReasonNotFile},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, reason := DescribeScript(tc.path, tc.defaultPath)
			if ok != tc.wantOK || reason != tc.wantReason {
				t.Fatalf("DescribeScript = (%v, %q), want (%v, %q)", ok, reason, tc.wantOK, tc.wantReason)
			}
		})
	}
}

// 未導入でも review 自体は失敗せず、候補と理由を返すこと。
func TestBuildReviewWithoutInstalledScriptStillListsCandidates(t *testing.T) {
	root := t.TempDir()
	zip := writeTestFile(t, filepath.Join(root, "photos.zip"))
	missing := filepath.Join(root, "missing", "MultiDriveCopy.ps1")

	review := BuildReview([]model.Item{
		{ID: "a", Verdict: model.VerdictKeepArchive, Status: model.StatusReady, ZipPath: zip},
	}, missing, "", false)

	if review.ScriptAvailable {
		t.Fatal("script should be unavailable")
	}
	if review.ScriptReason == "" {
		t.Fatal("reason must explain why the script is unusable")
	}
	if len(review.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(review.Candidates))
	}
}
