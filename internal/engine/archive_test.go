package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/archive"
	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/testutil"
	"github.com/tonbiattack/archive-extract-cleaner/internal/verify"
)

// makeSevenZip は 7-Zip でアーカイブを作る。ツールが無ければテストをスキップする。
func makeSevenZip(t *testing.T, root, name, format string) string {
	t.Helper()
	if !archive.ToolAvailable() {
		t.Skip("7-Zip CLI is not available")
	}
	source := filepath.Join(root, name)
	if err := testutil.WriteFile(source, "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(source, "sub/b.txt", "beta"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, name+"."+format)
	cmd := exec.Command(archive.ToolPath(), "a", "-t"+format, target, filepath.Join(source, "*"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot create %s: %v: %s", format, err, out)
	}
	return target
}

func TestScanSevenZipExactMatch(t *testing.T) {
	root := t.TempDir()
	makeSevenZip(t, root, "photos", "7z")

	result := scanRoot(t, root)
	item := findItem(t, result, "photos")
	if item.Verdict != model.VerdictDeleteCandidate {
		t.Fatalf("verdict = %s: %s %v", item.Verdict, item.Reason, item.Details)
	}
	if item.ZipPath == "" {
		t.Fatal("archive path must be set")
	}
	if !item.Deletable() {
		t.Fatal("exact 7z match should be deletable")
	}
}

func TestScanSevenZipMismatch(t *testing.T) {
	root := t.TempDir()
	makeSevenZip(t, root, "photos", "7z")
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

func TestScanSevenZipDetectsExtraFileInFolder(t *testing.T) {
	root := t.TempDir()
	makeSevenZip(t, root, "photos", "7z")
	if err := testutil.WriteFile(filepath.Join(root, "photos"), "extra.txt", "extra"); err != nil {
		t.Fatal(err)
	}
	result := scanRoot(t, root)
	item := findItem(t, result, "photos")
	if item.Verdict != model.VerdictMismatch {
		t.Fatalf("verdict = %s: %s", item.Verdict, item.Reason)
	}
}

func TestScanSevenZipWithoutToolIsUnverified(t *testing.T) {
	root := t.TempDir()
	// 中身は不正でもよい。ツールが無い場合は読み取り前に失敗する。
	path := filepath.Join(root, "photos.7z")
	if err := os.WriteFile(path, []byte("not an archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "photos"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}

	// 存在しないパスを設定して「未インストール」を再現する。
	archive.ConfigureTool(filepath.Join(t.TempDir(), "missing-7z.exe"))
	defer archive.ConfigureTool("")

	result := scanRoot(t, root)
	item := findItem(t, result, "photos")
	if item.Verdict != model.VerdictUnverified {
		t.Fatalf("verdict = %s: %s", item.Verdict, item.Reason)
	}
	if item.Deletable() {
		t.Fatal("unverified archive must not be deletable")
	}
	if item.Reason == "" {
		t.Fatal("reason must explain the missing tool")
	}
}

func TestScanDetectsSupportedExtensions(t *testing.T) {
	if !archive.IsSupported("a.zip") || !archive.IsSupported("a.7z") || !archive.IsSupported("a.RAR") {
		t.Fatal("zip/7z/rar must be supported")
	}
	if archive.IsSupported("a.tar") || archive.IsSupported("a.gz") {
		t.Fatal("tar/gz are not supported")
	}
}

func TestScanIgnoresUnsupportedArchives(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "photos.tar"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Scan(context.Background(), Options{
		Roots: []string{root}, Recursive: true, Limits: model.DefaultLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range result.Items {
		if filepath.Ext(item.ZipPath) == ".tar" {
			t.Fatalf("tar must not be scanned: %+v", item)
		}
	}
}

func TestScanSevenZipRejectsChangedFolderBeforeMove(t *testing.T) {
	// 7z でも、スキャン後の改ざんは移動直前の再照合で検出される。
	root := t.TempDir()
	makeSevenZip(t, root, "photos", "7z")
	result := scanRoot(t, root)
	item := findItem(t, result, "photos")
	if !item.Deletable() {
		t.Fatalf("precondition failed: %s", item.Verdict)
	}

	if err := testutil.WriteFile(filepath.Join(root, "photos"), "a.txt", "tampered"); err != nil {
		t.Fatal(err)
	}
	verification := verify.Verify(context.Background(), item.ZipPath, item.FolderPath, verify.Options{
		Limits: model.DefaultLimits(),
		Roots:  []string{root},
	})
	if verification.Verdict.IsDeleteCandidate() {
		t.Fatal("changed folder must not remain a delete candidate")
	}
}
