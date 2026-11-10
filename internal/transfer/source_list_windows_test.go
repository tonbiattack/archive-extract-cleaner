//go:build windows

package transfer

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func installedCorePath() string {
	if path := os.Getenv("MULTI_DRIVE_COPY_CORE"); path != "" {
		return path
	}
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return ""
	}
	return filepath.Join(base, "Programs", "MultiDriveCopy", "MultiDriveCopy.Core.ps1")
}

func readSourceListWithCore(t *testing.T, core string) []string {
	t.Helper()
	core, err := filepath.Abs(core)
	if err != nil {
		t.Fatal(err)
	}
	powershell, err := powershellPath()
	if err != nil {
		t.Skip(err)
	}

	root := t.TempDir()
	manifest, err := writeManifest(root, []string{
		makeTestFile(t, filepath.Join(root, "日本語 フォルダ", "a&b #1.zip")),
		makeTestFile(t, filepath.Join(root, "space folder", "file (1).txt")),
	})
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "read-result.txt")
	runner := filepath.Join(root, "read-source-list.ps1")
	script := `param(
    [Parameter(Mandatory = $true)][string]$Core,
    [Parameter(Mandatory = $true)][string]$Manifest,
    [Parameter(Mandatory = $true)][string]$Output
)
$ErrorActionPreference = 'Stop'
. $Core
$paths = @(Read-SourceListFile $Manifest)
[IO.File]::WriteAllLines($Output, [string[]]$paths, (New-Object Text.UTF8Encoding($false)))
`
	if err := os.WriteFile(runner, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(powershell, "-NoProfile", "-STA", "-ExecutionPolicy", "Bypass", "-File", runner, "-Core", core, "-Manifest", manifest, "-Output", output)
	command.Dir = filepath.Dir(core)
	if combined, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Read-SourceListFile failed: %v\n%s", err, combined)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line != "" {
			paths = append(paths, line)
		}
	}
	if len(paths) != 2 {
		t.Fatalf("paths = %#v", paths)
	}
	if !strings.Contains(paths[0], "日本語 フォルダ") || !strings.Contains(paths[1], "space folder") {
		t.Fatalf("paths = %#v", paths)
	}
	return paths
}

func TestSourceListCompatibilityFixture(t *testing.T) {
	readSourceListWithCore(t, filepath.Join("testdata", "read-source-list-fixture.ps1"))
}

func TestInstalledMultiDriveCopyReadsOurSourceList(t *testing.T) {
	core := installedCorePath()
	if core == "" {
		t.Skip("MULTI_DRIVE_COPY_CORE is not set")
	}
	if _, err := os.Stat(core); err != nil {
		t.Skipf("multi-drive-copy Core.ps1 is not installed: %v", err)
	}
	coreData, err := os.ReadFile(core)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(coreData, []byte("function Read-SourceListFile")) {
		t.Skip("installed multi-drive-copy does not support -SourceListFile yet")
	}
	readSourceListWithCore(t, core)
}

func TestValidateSourceListWithFixture(t *testing.T) {
	root := t.TempDir()
	coreData, err := os.ReadFile(filepath.Join("testdata", "read-source-list-fixture.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "MultiDriveCopy.Core.ps1"), coreData, 0o600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "MultiDriveCopy.ps1")
	if err := os.WriteFile(script, []byte("# test"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := writeManifest(root, []string{
		makeTestFile(t, filepath.Join(root, "日本語 フォルダ", "a.zip")),
		makeTestFile(t, filepath.Join(root, "space folder", "b.txt")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSourceList(script, manifest, 2); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSourceListRejectsLegacySchema(t *testing.T) {
	root := t.TempDir()
	coreData, err := os.ReadFile(filepath.Join("testdata", "read-source-list-fixture.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "MultiDriveCopy.Core.ps1"), coreData, 0o600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "MultiDriveCopy.ps1")
	if err := os.WriteFile(script, []byte("# test"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "legacy.json")
	if err := os.WriteFile(manifest, []byte(`{"paths":["C:\\x"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateSourceList(script, manifest, 1); err == nil {
		t.Fatal("legacy schema must be rejected")
	}
}

func TestValidateSourceListRejectsOldReceiver(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "MultiDriveCopy.Core.ps1"), []byte("# old receiver"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "MultiDriveCopy.ps1")
	if err := os.WriteFile(script, []byte("# test"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "manifest.json")
	if err := os.WriteFile(manifest, []byte(`{"sources":["C:\\x"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateSourceList(script, manifest, 1); err == nil {
		t.Fatal("old receiver must be rejected")
	}
}

func makeTestFile(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
