package transfer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

const maxCommandLineLength = 30000

const manifestMaxAge = 24 * time.Hour

type sourceListFile struct {
	Sources []string `json:"sources"`
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
		cleanupOldManifests(dir, manifestMaxAge)
		manifestPath, err := writeManifest(dir, paths)
		if err != nil {
			return LaunchResult{}, err
		}
		if err := validateSourceList(scriptPath, manifestPath, len(paths)); err != nil {
			os.Remove(manifestPath)
			return LaunchResult{}, err
		}
		args = []string{
			"-NoProfile", "-STA", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden",
			"-File", scriptPath,
			"-SourceListFile", manifestPath,
		}
		result.Mode = "manifest"
		result.ManifestPath = manifestPath
		if preset != "" {
			result.PresetSkipped = true
			result.Warning = "multi-drive-copy は -Preset と -SourceListFile を併用できません。大量パスのためプリセット「" + preset + "」を適用せず、レビュー画面を開きます。"
		}
	}

	command := exec.Command(powershell, args...)
	command.Dir = filepath.Dir(scriptPath)
	if err := command.Start(); err != nil {
		return LaunchResult{}, fmt.Errorf("multi-drive-copy を起動できません: %w", err)
	}
	return result, nil
}

func validateSourceList(scriptPath, manifestPath string, expected int) error {
	corePath := filepath.Join(filepath.Dir(scriptPath), "MultiDriveCopy.Core.ps1")
	coreData, err := os.ReadFile(corePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("multi-drive-copy のCoreスクリプトを確認できません: %w", err)
	}
	if !strings.Contains(string(coreData), "function Read-SourceListFile") {
		return errors.New("multi-drive-copy が -SourceListFile に対応していません。multi-drive-copy を更新してください")
	}
	powershell, err := powershellPath()
	if err != nil {
		return err
	}
	dir, err := WorkDir()
	if err != nil {
		return err
	}
	runner, err := os.CreateTemp(dir, "validate-source-list-*.ps1")
	if err != nil {
		return err
	}
	runnerPath := runner.Name()
	defer os.Remove(runnerPath)
	script := `param(
    [Parameter(Mandatory = $true)][string]$Core,
    [Parameter(Mandatory = $true)][string]$Manifest
)
$ErrorActionPreference = 'Stop'
. $Core
$paths = @(Read-SourceListFile $Manifest)
[Console]::Out.Write($paths.Count)
`
	if _, err := runner.WriteString(script); err != nil {
		runner.Close()
		return err
	}
	if err := runner.Close(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, powershell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-File", runnerPath, "-Core", corePath, "-Manifest", manifestPath)
	command.Dir = filepath.Dir(corePath)
	output, err := command.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return errors.New("multi-drive-copy のソースリスト読込検証がタイムアウトしました")
	}
	if err != nil {
		return fmt.Errorf("multi-drive-copy がソースリストを読み込めません: %v: %s", err, strings.TrimSpace(string(output)))
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		return fmt.Errorf("multi-drive-copy のソースリスト読込結果を解釈できません: %q", strings.TrimSpace(string(output)))
	}
	if count != expected {
		return fmt.Errorf("multi-drive-copy のソースリスト読込件数が一致しません: %d / %d", count, expected)
	}
	return nil
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

func writeManifest(dir string, paths []string) (string, error) {
	file, err := os.CreateTemp(dir, "manifest-*.json")
	if err != nil {
		return "", err
	}
	path := file.Name()
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(sourceListFile{Sources: paths}); err != nil {
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

func cleanupOldManifests(dir string, maxAge time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "manifest-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil || now.Sub(info.ModTime()) <= maxAge {
			continue
		}
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
}
