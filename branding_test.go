//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/cache"
	"github.com/tonbiattack/archive-extract-cleaner/internal/config"
	"github.com/tonbiattack/archive-extract-cleaner/internal/oplog"
)

// A display-name change must not move an existing user's configuration or state.
func TestLegacyDataPathsRemainStable(t *testing.T) {
	configBase := t.TempDir()
	cacheBase := t.TempDir()
	t.Setenv("APPDATA", configBase)
	t.Setenv("LOCALAPPDATA", cacheBase)

	configPath, err := config.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	wantConfig := filepath.Join(configBase, "archive-extract-cleaner", "config.json")
	if configPath != wantConfig {
		t.Fatalf("config path = %q; want %q", configPath, wantConfig)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{"workers":3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := config.Load("")
	if err != nil || loaded.Workers != 3 {
		t.Fatalf("existing configuration not loaded: workers=%d err=%v", loaded.Workers, err)
	}
	if got := (&App{}).triageFile(); got != filepath.Join(configBase, "archive-extract-cleaner", "triage.json") {
		t.Fatalf("triage path = %q", got)
	}
	if got := cache.DefaultDir(); got != filepath.Join(cacheBase, "archive-extract-cleaner", "cache") {
		t.Fatalf("cache path = %q", got)
	}
	logPath, err := oplog.DefaultPath()
	if err != nil || logPath != filepath.Join(cacheBase, "archive-extract-cleaner", "operations.jsonl") {
		t.Fatalf("log path = %q, err=%v", logPath, err)
	}
}

func TestBrandingAndDistributionIdentity(t *testing.T) {
	data, err := os.ReadFile("wails.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Name           string `json:"name"`
		OutputFilename string `json:"outputfilename"`
		Info           struct {
			ProductName    string `json:"productName"`
			ProductVersion string `json:"productVersion"`
		} `json:"info"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Info.ProductName != "ZipSweep" || cfg.Name != "archive-extract-cleaner" || cfg.OutputFilename != "archive-extract-cleaner" {
		t.Fatalf("unexpected branding or changed internal identity: %+v", cfg)
	}
	if cfg.Info.ProductVersion != appVersion {
		t.Fatalf("Wails version %q differs from app version %q", cfg.Info.ProductVersion, appVersion)
	}
	html, err := os.ReadFile(filepath.Join("frontend", "dist", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"<title>ZipSweep</title>", "<h1>ZipSweep</h1>"} {
		if !strings.Contains(string(html), expected) {
			t.Errorf("missing %q", expected)
		}
	}
}
