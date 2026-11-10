package transfer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestWriteManifestPreservesUnicodeAndSpecialCharacters(t *testing.T) {
	dir := t.TempDir()
	paths := []string{`C:\日本語 フォルダ\a&b #1.zip`, `C:\space folder\file (1).txt`}
	path, err := writeManifest(dir, paths, "Backup プリセット")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded manifestFile
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Preset != "Backup プリセット" {
		t.Fatalf("preset = %q", decoded.Preset)
	}
	if len(decoded.Paths) != len(paths) {
		t.Fatalf("paths = %+v", decoded.Paths)
	}
	for index, expected := range paths {
		if decoded.Paths[index] != expected {
			t.Fatalf("path[%d] = %q, want %q", index, decoded.Paths[index], expected)
		}
	}
}

func TestEnsureWrapper(t *testing.T) {
	dir := t.TempDir()
	path, err := ensureWrapper(dir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "ConvertFrom-Json") || !strings.Contains(string(data), "$TargetScript") {
		t.Fatalf("wrapper = %s", data)
	}
}

func TestCommandLineLengthUsesUTF16Units(t *testing.T) {
	ascii := commandLineLength("powershell.exe", []string{"-File", `C:\a\b.ps1`})
	unicode := commandLineLength("powershell.exe", []string{"-File", `C:\日本語\b.ps1`})
	if unicode <= ascii {
		t.Fatalf("unicode length = %d, ascii = %d", unicode, ascii)
	}
	if got := len(utf16.Encode([]rune(`C:\日本語`))); got != 6 {
		t.Fatalf("utf16 length = %d", got)
	}
}

func TestLaunchRejectsMissingScript(t *testing.T) {
	_, err := Launch([]string{`C:\data\a.zip`}, filepath.Join(t.TempDir(), "missing.ps1"), "")
	if err == nil {
		t.Fatal("expected missing script error")
	}
	if !strings.Contains(err.Error(), "見つかりません") {
		t.Fatalf("error = %v", err)
	}
}

func TestDefaultScriptPath(t *testing.T) {
	path := DefaultScriptPath()
	if path == "" || !strings.HasSuffix(path, filepath.Join("MultiDriveCopy", "MultiDriveCopy.ps1")) {
		t.Fatalf("path = %q", path)
	}
}
