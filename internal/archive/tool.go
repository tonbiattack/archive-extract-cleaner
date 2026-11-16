package archive

import (
	"os"
	"path/filepath"
	"sync"
)

// 外部ツール（7-Zip CLI）の場所を保持する。
// 設定ファイルの値はアプリ起動時に ConfigureTool で渡す。

var (
	toolMu         sync.RWMutex
	toolConfigured string
	toolDetected   string
	toolOnce       sync.Once
)

// ConfigureTool は設定ファイルで指定された 7z.exe のパスを設定する。
// 空文字なら自動検出に戻す。
func ConfigureTool(path string) {
	toolMu.Lock()
	defer toolMu.Unlock()
	toolConfigured = path
	toolOnce = sync.Once{}
	toolDetected = ""
}

// findSevenZip は 7z.exe の場所を返す。設定値 → PATH → 既定のインストール先の順。
func findSevenZip() (string, bool) {
	toolMu.RLock()
	configured := toolConfigured
	detected := toolDetected
	toolMu.RUnlock()

	if configured != "" {
		if info, err := os.Stat(configured); err == nil && info.Mode().IsRegular() {
			return configured, true
		}
		return "", false
	}
	if detected != "" {
		return detected, true
	}

	toolOnce.Do(func() {
		found := ""
		if path, err := lookPath("7z"); err == nil {
			found = path
		} else {
			for _, candidate := range defaultToolCandidates() {
				if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
					found = candidate
					break
				}
			}
		}
		toolMu.Lock()
		toolDetected = found
		toolMu.Unlock()
	})

	toolMu.RLock()
	defer toolMu.RUnlock()
	return toolDetected, toolDetected != ""
}

// defaultToolCandidates は既定のインストール先を返す。
func defaultToolCandidates() []string {
	if runtimeIsWindows() {
		var candidates []string
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
			if base := os.Getenv(env); base != "" {
				candidates = append(candidates, filepath.Join(base, "7-Zip", "7z.exe"))
			}
		}
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			candidates = append(candidates, filepath.Join(local, "Programs", "7-Zip", "7z.exe"))
		}
		return candidates
	}
	return []string{"/usr/bin/7z", "/usr/local/bin/7z", "/opt/homebrew/bin/7z"}
}

// ToolAvailable は 7-Zip CLI が見つかるかを返す。
func ToolAvailable() bool {
	_, ok := findSevenZip()
	return ok
}

// ToolPath は検出した 7z.exe のパスを返す（見つからなければ空）。
func ToolPath() string {
	path, _ := findSevenZip()
	return path
}
