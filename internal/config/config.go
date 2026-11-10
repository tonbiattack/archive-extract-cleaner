package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
)

type Config struct {
	Limits model.Limits `json:"limits"`
}

func Default() Config {
	return Config{Limits: model.DefaultLimits()}
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "archive-extract-cleaner", "config.json"), nil
}

func Load(path string) (Config, string, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return Default(), "", err
		}
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), path, nil
	}
	if err != nil {
		return Default(), path, err
	}
	cfg := Default()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), path, err
	}
	normalize(&cfg)
	return cfg, path, nil
}

func normalize(cfg *Config) {
	defaults := model.DefaultLimits()
	if cfg.Limits.MaxEntries <= 0 {
		cfg.Limits.MaxEntries = defaults.MaxEntries
	}
	if cfg.Limits.MaxTotalBytes == 0 {
		cfg.Limits.MaxTotalBytes = defaults.MaxTotalBytes
	}
	if cfg.Limits.MaxFileBytes == 0 {
		cfg.Limits.MaxFileBytes = defaults.MaxFileBytes
	}
	if cfg.Limits.MaxCompressionRatio == 0 {
		cfg.Limits.MaxCompressionRatio = defaults.MaxCompressionRatio
	}
}

func WriteDefault(path string) error {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(Default(), "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}
