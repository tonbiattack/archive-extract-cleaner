//go:build windows

package transfer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeScriptResult struct {
	Preset  string   `json:"preset"`
	Sources []string `json:"sources"`
}

func writeFakeScript(t *testing.T, outputPath string) string {
	t.Helper()
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "fake-multi-drive-copy.ps1")
	escapedOutput := strings.ReplaceAll(outputPath, "'", "''")
	script := `param(
    [string]$Preset,
    [Parameter(ValueFromRemainingArguments = $true)][string[]]$Sources
)
$result = [ordered]@{ preset = [string]$Preset; sources = @($Sources | ForEach-Object { [string]$_ }) }
$json = $result | ConvertTo-Json -Compress -Depth 4
[IO.File]::WriteAllText('` + escapedOutput + `', $json, (New-Object Text.UTF8Encoding($false)))
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	return scriptPath
}

func readFakeResult(t *testing.T, outputPath string) fakeScriptResult {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(outputPath)
		if err == nil {
			data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
			var result fakeScriptResult
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatalf("decode fake result: %v (%s)", err, data)
			}
			return result
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("fake script did not write %s", outputPath)
	return fakeScriptResult{}
}

func TestLaunchPassesSpecialCharactersAsArguments(t *testing.T) {
	output := filepath.Join(t.TempDir(), "result.json")
	script := writeFakeScript(t, output)
	paths := []string{
		`C:\日本語 フォルダ\a&b #1.zip`,
		`C:\space folder\file (1).txt`,
		`C:\quote ' folder\file.txt`,
	}
	result, err := Launch(paths, script, "Backup プリセット")
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != "args" {
		t.Fatalf("mode = %s", result.Mode)
	}
	got := readFakeResult(t, output)
	if got.Preset != "Backup プリセット" {
		t.Fatalf("preset = %q", got.Preset)
	}
	if len(got.Sources) != len(paths) {
		t.Fatalf("sources = %#v", got.Sources)
	}
	for index, expected := range paths {
		if got.Sources[index] != expected {
			t.Fatalf("source[%d] = %q, want %q", index, got.Sources[index], expected)
		}
	}
}

func TestLaunchUsesManifestForLongPathLists(t *testing.T) {
	output := filepath.Join(t.TempDir(), "result.json")
	script := writeFakeScript(t, output)
	paths := make([]string, 80)
	for index := range paths {
		paths[index] = fmt.Sprintf(`C:\長い パス\日本語 フォルダ %02d\%s.zip`, index, strings.Repeat("長い名前 ", 120))
	}
	result, err := Launch(paths, script, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != "manifest" || result.ManifestPath == "" {
		t.Fatalf("result = %+v", result)
	}
	defer os.Remove(result.ManifestPath)
	got := readFakeResult(t, output)
	if got.Preset != "" {
		t.Fatalf("preset = %q", got.Preset)
	}
	if len(got.Sources) != len(paths) {
		t.Fatalf("sources = %d, want %d", len(got.Sources), len(paths))
	}
	for index, expected := range paths {
		if got.Sources[index] != expected {
			t.Fatalf("source[%d] = %q, want %q", index, got.Sources[index], expected)
		}
	}
}
