package transfer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"unicode/utf16"
)

const maxCommandLineLength = 30000

type manifestFile struct {
	Paths  []string `json:"paths"`
	Preset string   `json:"preset,omitempty"`
}

func Launch(paths []string, scriptPath, preset string) (LaunchResult, error) {
	if len(paths) == 0 {
		return LaunchResult{}, errors.New("転送対象が選択されていません")
	}
	if !ScriptAvailable(scriptPath) {
		return LaunchResult{}, fmt.Errorf("multi-drive-copy のスクリプトが見つかりません: %s", scriptPath)
	}
	powershell, err := powershellPath()
	if err != nil {
		return LaunchResult{}, err
	}
	scriptPath, err = filepath.Abs(scriptPath)
	if err != nil {
		return LaunchResult{}, err
	}

	base := []string{"-NoProfile", "-STA", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-File", scriptPath}
	if preset != "" {
		base = append(base, "-Preset", preset)
	}
	direct := append(append([]string(nil), base...), paths...)

	result := LaunchResult{Launched: true, PathCount: len(paths), ScriptPath: scriptPath, Preset: preset}
	args := direct
	result.Mode = "args"
	if commandLineLength(powershell, direct) > maxCommandLineLength {
		dir, err := WorkDir()
		if err != nil {
			return LaunchResult{}, err
		}
		wrapperPath, err := ensureWrapper(dir)
		if err != nil {
			return LaunchResult{}, err
		}
		manifestPath, err := writeManifest(dir, paths, preset)
		if err != nil {
			return LaunchResult{}, err
		}
		args = []string{
			"-NoProfile", "-STA", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden",
			"-File", wrapperPath,
			"-Manifest", manifestPath,
			"-TargetScript", scriptPath,
		}
		result.Mode = "manifest"
		result.ManifestPath = manifestPath
	}

	command := exec.Command(powershell, args...)
	command.Dir = filepath.Dir(scriptPath)
	if err := command.Start(); err != nil {
		return LaunchResult{}, fmt.Errorf("multi-drive-copy を起動できません: %w", err)
	}
	return result, nil
}

func powershellPath() (string, error) {
	if path, err := exec.LookPath("powershell.exe"); err == nil {
		return path, nil
	}
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	path := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	return "", errors.New("powershell.exe が見つかりません")
}

func commandLineLength(executable string, args []string) int {
	total := utf16Length(executable)
	for _, arg := range args {
		total += 1 + utf16Length(arg) + 2
	}
	return total
}

func utf16Length(value string) int {
	return len(utf16.Encode([]rune(value)))
}

func WorkDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "archive-extract-cleaner", "transfer")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func writeManifest(dir string, paths []string, preset string) (string, error) {
	file, err := os.CreateTemp(dir, "manifest-*.json")
	if err != nil {
		return "", err
	}
	path := file.Name()
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(manifestFile{Paths: paths, Preset: preset}); err != nil {
		file.Close()
		os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

func ensureWrapper(dir string) (string, error) {
	path := filepath.Join(dir, "run-multi-drive-copy.ps1")
	script := `param(
    [Parameter(Mandatory = $true)][string]$Manifest,
    [Parameter(Mandatory = $true)][string]$TargetScript
)
$ErrorActionPreference = 'Stop'
$data = Get-Content -LiteralPath $Manifest -Raw -Encoding UTF8 | ConvertFrom-Json
$sources = @($data.paths | ForEach-Object { [string]$_ })
if ($data.preset) {
    & $TargetScript -Preset ([string]$data.preset) -Sources $sources
} else {
    & $TargetScript -Sources $sources
}
`
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
