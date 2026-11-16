package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/testutil"
)

func scanRoot(t *testing.T, root string) *Result {
	t.Helper()
	result, err := Scan(context.Background(), Options{Roots: []string{root}, Recursive: true, Limits: model.DefaultLimits()})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func findItem(t *testing.T, result *Result, name string) model.Item {
	t.Helper()
	for _, item := range result.Items {
		if item.Name == name {
			return item
		}
	}
	t.Fatalf("item %q not found in %+v", name, result.Items)
	return model.Item{}
}

func TestScanVerdicts(t *testing.T) {
	root := t.TempDir()
	if err := testutil.WriteZip(filepath.Join(root, "photos.zip"), []testutil.Entry{{Name: "photos/a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteZip(filepath.Join(root, "docs.zip"), []testutil.Entry{{Name: "docs/a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "music"), "song.txt", "music"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "broken.zip"), []byte("broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteZip(filepath.Join(root, "mismatch.zip"), []testutil.Entry{{Name: "a.txt", Content: "different"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "mismatch"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteZip(filepath.Join(root, "nested", "album.zip"), []testutil.Entry{{Name: "album/a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "nested", "album"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}

	result := scanRoot(t, root)

	if got := findItem(t, result, "photos").Verdict; got != model.VerdictDeleteCandidate {
		t.Fatalf("photos = %s", got)
	}
	if got := findItem(t, result, "docs").Verdict; got != model.VerdictKeepArchive {
		t.Fatalf("docs = %s", got)
	}
	if got := findItem(t, result, "music").Verdict; got != model.VerdictKeepDirectory {
		t.Fatalf("music = %s", got)
	}
	if got := findItem(t, result, "broken").Verdict; got != model.VerdictUnverified {
		t.Fatalf("broken = %s", got)
	}
	if got := findItem(t, result, "mismatch").Verdict; got != model.VerdictMismatch {
		t.Fatalf("mismatch = %s", got)
	}
	if got := findItem(t, result, "album").Verdict; got != model.VerdictDeleteCandidate {
		t.Fatalf("album = %s", got)
	}
	if result.Summary.CandidateCount != 2 {
		t.Fatalf("candidates = %d, want 2", result.Summary.CandidateCount)
	}
	if result.Summary.CandidateBytes <= 0 {
		t.Fatalf("candidate bytes = %d", result.Summary.CandidateBytes)
	}
}

func TestScanMultipleRoots(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	if err := testutil.WriteZip(filepath.Join(rootA, "a.zip"), []testutil.Entry{{Name: "a.txt", Content: "a"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(rootA, "a"), "a.txt", "a"); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteZip(filepath.Join(rootB, "b.zip"), []testutil.Entry{{Name: "b.txt", Content: "b"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(rootB, "b"), "b.txt", "b"); err != nil {
		t.Fatal(err)
	}
	result, err := Scan(context.Background(), Options{Roots: []string{rootA, rootB}, Recursive: true, Limits: model.DefaultLimits()})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.CandidateCount != 2 {
		t.Fatalf("candidates = %d", result.Summary.CandidateCount)
	}
	if findItem(t, result, "a").Verdict != model.VerdictDeleteCandidate || findItem(t, result, "b").Verdict != model.VerdictDeleteCandidate {
		t.Fatalf("items = %+v", result.Items)
	}
}

func TestScanIsReadOnly(t *testing.T) {
	root := t.TempDir()
	if err := testutil.WriteZip(filepath.Join(root, "photos.zip"), []testutil.Entry{{Name: "photos/a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	before, err := testutil.Snapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	scanRoot(t, root)
	after, err := testutil.Snapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if !testutil.EqualSnapshot(before, after) {
		t.Fatal("scan modified files")
	}
}

func TestScanJapaneseAndSpacePaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ダウンロード 2026")
	if err := testutil.WriteZip(filepath.Join(root, "写真 データ.zip"), []testutil.Entry{{Name: "写真 データ/画像 1.txt", Content: "日本語"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "写真 データ"), "画像 1.txt", "日本語"); err != nil {
		t.Fatal(err)
	}
	result := scanRoot(t, root)
	if got := findItem(t, result, "写真 データ").Verdict; got != model.VerdictDeleteCandidate {
		t.Fatalf("verdict = %s", got)
	}
}

func TestScanCancel(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Scan(ctx, Options{Roots: []string{root}, Recursive: true, Limits: model.DefaultLimits()})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestScanNonRecursive(t *testing.T) {
	root := t.TempDir()
	if err := testutil.WriteZip(filepath.Join(root, "nested", "album.zip"), []testutil.Entry{{Name: "album/a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	result, err := Scan(context.Background(), Options{Roots: []string{root}, Recursive: false, Limits: model.DefaultLimits()})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range result.Items {
		if filepath.Base(item.ZipPath) == "album.zip" {
			t.Fatalf("nested archive should not be scanned: %+v", item)
		}
	}
}

func TestScanSkipsJunction(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := testutil.WriteZip(filepath.Join(outside, "photos.zip"), []testutil.Entry{{Name: "photos/a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(outside, "photos"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := testutil.Junction(filepath.Join(root, "link"), outside); err != nil {
		t.Skipf("cannot create junction: %v", err)
	}
	result := scanRoot(t, root)
	if len(result.Items) != 0 {
		t.Fatalf("junction was followed: %+v", result.Items)
	}
	if result.WarningTotal == 0 {
		t.Fatal("expected a warning")
	}
}

func TestScanProgress(t *testing.T) {
	root := t.TempDir()
	if err := testutil.WriteZip(filepath.Join(root, "photos.zip"), []testutil.Entry{{Name: "photos/a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	var phases []model.Phase
	result, err := Scan(context.Background(), Options{
		Roots:     []string{root},
		Recursive: true,
		Limits:    model.DefaultLimits(),
		Progress: func(p model.Progress) {
			phases = append(phases, p.Phase)
			if p.Phase == model.PhaseDone && p.Completed != p.Total {
				t.Errorf("completed = %d total = %d", p.Completed, p.Total)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(phases) == 0 || phases[0] != model.PhaseWalking || phases[len(phases)-1] != model.PhaseDone {
		t.Fatalf("phases = %v", phases)
	}
	if result.Summary.Total == 0 {
		t.Fatal("expected items")
	}
}

// manyPairs は照合対象の ZIP/フォルダ組を count 個作る。
func manyPairs(t *testing.T, root string, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("set%03d", i)
		content := fmt.Sprintf("content-%03d", i)
		if err := testutil.WriteZip(filepath.Join(root, name+".zip"), []testutil.Entry{{Name: name + "/a.txt", Content: content}}); err != nil {
			t.Fatal(err)
		}
		if err := testutil.WriteFile(filepath.Join(root, name), "a.txt", content); err != nil {
			t.Fatal(err)
		}
	}
}

func scanWithWorkers(t *testing.T, root string, workers int) *Result {
	t.Helper()
	result, err := Scan(context.Background(), Options{
		Roots:     []string{root},
		Recursive: true,
		Limits:    model.DefaultLimits(),
		Workers:   workers,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestScanParallelMatchesSequential(t *testing.T) {
	root := t.TempDir()
	manyPairs(t, root, 40)
	if err := os.WriteFile(filepath.Join(root, "broken.zip"), []byte("broken"), 0o644); err != nil {
		t.Fatal(err)
	}

	sequential := scanWithWorkers(t, root, 1)
	parallel := scanWithWorkers(t, root, 8)

	if sequential.Summary.CandidateCount != parallel.Summary.CandidateCount {
		t.Fatalf("candidates sequential=%d parallel=%d", sequential.Summary.CandidateCount, parallel.Summary.CandidateCount)
	}
	if sequential.Summary.CandidateCount != 40 {
		t.Fatalf("candidates = %d, want 40", sequential.Summary.CandidateCount)
	}
	if len(sequential.Items) != len(parallel.Items) {
		t.Fatalf("items sequential=%d parallel=%d", len(sequential.Items), len(parallel.Items))
	}
	for i := range sequential.Items {
		a, b := sequential.Items[i], parallel.Items[i]
		if a.ID != b.ID || a.Verdict != b.Verdict || a.Reason != b.Reason {
			t.Fatalf("item %d differs: %+v vs %+v", i, a, b)
		}
		if a.ComparedBytes != b.ComparedBytes {
			t.Fatalf("item %d compared bytes differ: %d vs %d", i, a.ComparedBytes, b.ComparedBytes)
		}
	}
}

func TestScanParallelProgressCounts(t *testing.T) {
	root := t.TempDir()
	manyPairs(t, root, 24)

	var last model.Progress
	_, err := Scan(context.Background(), Options{
		Roots:     []string{root},
		Recursive: true,
		Limits:    model.DefaultLimits(),
		Workers:   4,
		Progress: func(p model.Progress) {
			if p.Phase == model.PhaseVerifying {
				last = p
				if p.Completed > p.Total {
					t.Errorf("completed = %d total = %d", p.Completed, p.Total)
				}
				if p.ETAMS < -1 {
					t.Errorf("eta = %d", p.ETAMS)
				}
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if last.Completed != last.Total || last.Total != 24 {
		t.Fatalf("final verifying progress = %+v", last)
	}
}

func BenchmarkScanVerification(b *testing.B) {
	root := b.TempDir()
	for i := 0; i < 60; i++ {
		name := fmt.Sprintf("set%03d", i)
		content := fmt.Sprintf("content-%03d", i)
		if err := testutil.WriteZip(filepath.Join(root, name+".zip"), []testutil.Entry{{Name: name + "/a.txt", Content: content}}); err != nil {
			b.Fatal(err)
		}
		if err := testutil.WriteFile(filepath.Join(root, name), "a.txt", content); err != nil {
			b.Fatal(err)
		}
	}
	for _, workers := range []int{1, 0} {
		b.Run(fmt.Sprintf("workers=%d", workers), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, err := Scan(context.Background(), Options{
					Roots:     []string{root},
					Recursive: true,
					Limits:    model.DefaultLimits(),
					Workers:   workers,
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestScanIgnorableArtifactsOnly(t *testing.T) {
	root := t.TempDir()
	if err := testutil.WriteZip(filepath.Join(root, "photos.zip"), []testutil.Entry{
		{Name: "photos/a.txt", Content: "alpha"},
		{Name: "photos/__MACOSX/._a.txt", Content: "resource"},
		{Name: "photos/.DS_Store", Content: "meta"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	result := scanRoot(t, root)
	item := findItem(t, result, "photos")
	if item.Verdict != model.VerdictDeleteCandidateIgnorable {
		t.Fatalf("verdict = %s: %s", item.Verdict, item.Reason)
	}
	if !item.Deletable() {
		t.Fatal("ignorable item should be deletable")
	}
	if result.Summary.CandidateCount != 1 {
		t.Fatalf("candidates = %d", result.Summary.CandidateCount)
	}
	if result.Summary.ByVerdict[model.VerdictDeleteCandidateIgnorable] != 1 {
		t.Fatalf("byVerdict = %+v", result.Summary.ByVerdict)
	}
}

func TestScanIgnorableRequiresCommonContentMatch(t *testing.T) {
	root := t.TempDir()
	if err := testutil.WriteZip(filepath.Join(root, "photos.zip"), []testutil.Entry{
		{Name: "photos/a.txt", Content: "alpha"},
		{Name: "photos/.DS_Store", Content: "meta"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos"), "a.txt", "changed"); err != nil {
		t.Fatal(err)
	}
	result := scanRoot(t, root)
	item := findItem(t, result, "photos")
	if item.Verdict != model.VerdictMismatch {
		t.Fatalf("verdict = %s: %s", item.Verdict, item.Reason)
	}
	if item.Deletable() {
		t.Fatal("mismatch must not be deletable")
	}
}

func TestScanMatchesDuplicateNameVariants(t *testing.T) {
	cases := []struct {
		name   string
		folder string
	}{
		{name: "photos", folder: "photos (1)"},
		{name: "photos", folder: "photos (2)"},
		{name: "photos", folder: "photos - コピー"},
		{name: "photos", folder: "photos - Copy"},
		{name: "photos", folder: "photos_2"},
		{name: "photos", folder: "photos のコピー"},
	}
	for _, tc := range cases {
		t.Run(tc.folder, func(t *testing.T) {
			root := t.TempDir()
			if err := testutil.WriteZip(filepath.Join(root, tc.name+".zip"), []testutil.Entry{{Name: "a.txt", Content: "alpha"}}); err != nil {
				t.Fatal(err)
			}
			if err := testutil.WriteFile(filepath.Join(root, tc.folder), "a.txt", "alpha"); err != nil {
				t.Fatal(err)
			}
			result := scanRoot(t, root)
			item := findItem(t, result, tc.name)
			if item.Verdict != model.VerdictDeleteCandidate {
				t.Fatalf("verdict = %s: %s", item.Verdict, item.Reason)
			}
			if !item.NameVariant {
				t.Fatal("name variant must be recorded")
			}
			if item.FolderPath != filepath.Join(root, tc.folder) {
				t.Fatalf("folder = %s", item.FolderPath)
			}
		})
	}
}

func TestScanStillVerifiesContentForNameVariants(t *testing.T) {
	root := t.TempDir()
	if err := testutil.WriteZip(filepath.Join(root, "photos.zip"), []testutil.Entry{{Name: "a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos (1)"), "a.txt", "changed"); err != nil {
		t.Fatal(err)
	}
	result := scanRoot(t, root)
	item := findItem(t, result, "photos")
	if item.Verdict != model.VerdictMismatch {
		t.Fatalf("verdict = %s: %s", item.Verdict, item.Reason)
	}
	if item.NameVariant {
		t.Fatal("mismatch must not be flagged as a name variant match")
	}
}

func TestScanPrefersExactNameOverVariant(t *testing.T) {
	root := t.TempDir()
	if err := testutil.WriteZip(filepath.Join(root, "photos.zip"), []testutil.Entry{{Name: "a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos (1)"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	result := scanRoot(t, root)
	item := findItem(t, result, "photos")
	if item.Verdict != model.VerdictDeleteCandidate {
		t.Fatalf("verdict = %s: %s", item.Verdict, item.Reason)
	}
	if item.NameVariant {
		t.Fatal("exact match must win over a variant")
	}
	if item.FolderPath != filepath.Join(root, "photos") {
		t.Fatalf("folder = %s", item.FolderPath)
	}
}

func TestScanAmbiguousWhenMultipleVariants(t *testing.T) {
	root := t.TempDir()
	if err := testutil.WriteZip(filepath.Join(root, "photos.zip"), []testutil.Entry{{Name: "a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos (1)"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos (2)"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	result := scanRoot(t, root)
	item := findItem(t, result, "photos")
	if item.Verdict != model.VerdictAmbiguous {
		t.Fatalf("verdict = %s: %s", item.Verdict, item.Reason)
	}
	if item.Deletable() {
		t.Fatal("ambiguous item must not be deletable")
	}
}

func TestNormalizeDuplicateName(t *testing.T) {
	cases := map[string]string{
		"photos (1)":     "photos",
		"photos(2)":      "photos",
		"photos - コピー":   "photos",
		"photos - Copy":  "photos",
		"photos_3":       "photos",
		"photos のコピー":    "photos",
		"photos (1) (2)": "photos",
		"photos":         "photos",
		"写真 (1)":         "写真",
		"(1)":            "(1)",
	}
	for input, want := range cases {
		if got := normalizeDuplicateName(input); got != want {
			t.Errorf("normalizeDuplicateName(%q) = %q, want %q", input, got, want)
		}
	}
}
