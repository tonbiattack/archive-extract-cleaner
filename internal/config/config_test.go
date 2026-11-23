package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
)

func TestLoadMissingReturnsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg, gotPath, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != path {
		t.Fatalf("path = %s", gotPath)
	}
	if cfg.Limits != model.DefaultLimits() {
		t.Fatalf("limits = %+v", cfg.Limits)
	}
}

func TestWriteDefaultAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	if err := WriteDefault(path); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Limits != model.DefaultLimits() {
		t.Fatalf("limits = %+v", cfg.Limits)
	}
}

func TestLoadNormalizesZeroValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"limits":{"maxEntries":0,"maxTotalBytes":0,"maxFileBytes":0,"maxCompressionRatio":0}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Limits != model.DefaultLimits() {
		t.Fatalf("limits = %+v", cfg.Limits)
	}
}

func TestLoadMultiDriveCopySettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	data := `{"multiDriveCopy":{"scriptPath":"  C:\\Tools\\MultiDriveCopy.ps1  ","preset":"  Backup  "}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MultiDriveCopy.ScriptPath != `C:\Tools\MultiDriveCopy.ps1` {
		t.Fatalf("script = %q", cfg.MultiDriveCopy.ScriptPath)
	}
	if cfg.MultiDriveCopy.Preset != "Backup" {
		t.Fatalf("preset = %q", cfg.MultiDriveCopy.Preset)
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(path)
	if err == nil {
		t.Fatal("expected error")
	}
	if cfg.Limits != model.DefaultLimits() {
		t.Fatalf("limits = %+v", cfg.Limits)
	}
}

func TestLoadNormalizesWorkers(t *testing.T) {
	cases := []struct {
		name string
		data string
		want int
	}{
		{name: "auto", data: `{}`, want: 0},
		{name: "explicit", data: `{"workers":4}`, want: 4},
		{name: "negative", data: `{"workers":-3}`, want: 0},
		{name: "too large", data: `{"workers":99}`, want: 16},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tc.data), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, _, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Workers != tc.want {
				t.Fatalf("workers = %d, want %d", cfg.Workers, tc.want)
			}
		})
	}
}

// E/F/G の既定運用を固定する。設定を変えていない利用者は、追加操作なしで
// 従来どおり E/F/G を検索できること（Issue #69 の完了条件）。
func TestArchiveSearchDefaultsKeepEFGWithoutExtraSteps(t *testing.T) {
	cfg := Default()
	if got, want := fmt.Sprint(cfg.ArchiveSearch.Drives), "[E F G]"; got != want {
		t.Fatalf("default drives = %s, want %s", got, want)
	}
	if cfg.ArchiveSearch.RelativePath != "" || cfg.ArchiveSearch.Recursive {
		t.Fatalf("defaults must stay simple: %+v", cfg.ArchiveSearch)
	}

	// 設定ファイルが旧形式でも、読み込み後に E/F/G が補完されること。
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"workers":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := fmt.Sprint(loaded.ArchiveSearch.Drives), "[E F G]"; got != want {
		t.Fatalf("loaded drives = %s, want %s", got, want)
	}
}

// 利用者が明示したドライブ設定は保持されること（後方互換）。
func TestArchiveSearchKeepsExplicitDrives(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	data := `{"archiveSearch":{"enabled":true,"drives":["d","x"],"relativePath":"save","recursive":true}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := fmt.Sprint(cfg.ArchiveSearch.Drives), "[D X]"; got != want {
		t.Fatalf("drives = %s, want %s", got, want)
	}
	if cfg.ArchiveSearch.RelativePath != "save" || !cfg.ArchiveSearch.Recursive {
		t.Fatalf("explicit settings lost: %+v", cfg.ArchiveSearch)
	}
}
