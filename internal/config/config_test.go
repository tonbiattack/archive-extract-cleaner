package config

import (
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
