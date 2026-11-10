package transfer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

func TestWriteManifestUsesReceiverSchema(t *testing.T) {
	dir := t.TempDir()
	paths := []string{`C:\日本語 フォルダ\a&b #1.zip`, `C:\space folder\file (1).txt`}
	path, err := writeManifest(dir, paths)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Sources []string `json:"sources"`
		Paths   []string `json:"paths"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Paths != nil {
		t.Fatalf("legacy paths property must not be written: %s", data)
	}
	if len(decoded.Sources) != len(paths) {
		t.Fatalf("sources = %+v", decoded.Sources)
	}
	for index, expected := range paths {
		if decoded.Sources[index] != expected {
			t.Fatalf("source[%d] = %q, want %q", index, decoded.Sources[index], expected)
		}
	}
}

func TestCleanupOldManifests(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "manifest-old.json")
	current := filepath.Join(dir, "manifest-new.json")
	other := filepath.Join(dir, "keep.txt")
	for _, path := range []string{old, current, other} {
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	oldTime := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(old, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	cleanupOldManifests(dir, 24*time.Hour)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old manifest was not removed: %v", err)
	}
	if _, err := os.Stat(current); err != nil {
		t.Fatalf("current manifest was removed: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("non-manifest file was removed: %v", err)
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
