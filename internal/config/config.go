package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
)

type MultiDriveCopy struct {
	ScriptPath string `json:"scriptPath"`
	Preset     string `json:"preset"`
}

// AI はクラウド推論の設定。既定は無効。
// APIキーはここに書かず、環境変数から読む。
type AI struct {
	Enabled       bool   `json:"enabled"`
	Endpoint      string `json:"endpoint"`
	Model         string `json:"model"`
	APIKeyEnv     string `json:"apiKeyEnv"`
	AllowInsecure bool   `json:"allowInsecure"`
}

// ArchiveSearch は E/F/G の同名アーカイブ検索の設定。
//
// 保存先は multi-drive-copy と同じ規則（ドライブ直下、または RelativePath を
// 結合したフォルダ）で解決する。同名検知は「同じ内容」の判定ではないため、
// 削除の可否には使わない。
type ArchiveSearch struct {
	// Enabled は同名アーカイブの横断チェックを行うか。
	Enabled bool `json:"enabled"`
	// Drives は検索するドライブ。既定は E / F / G。
	Drives []string `json:"drives"`
	// RelativePath は各ドライブ内の保存先フォルダ。空ならドライブ直下。
	RelativePath string `json:"relativePath"`
	// Recursive は保存先の配下サブフォルダまで検索するか。
	Recursive bool `json:"recursive"`
	// MaxDepth は Recursive 時の深さ上限。0 は既定（8）。
	MaxDepth int `json:"maxDepth"`
	// MaxEntries は1ドライブあたりに索引化するエントリ数の上限。0 は既定（200000）。
	MaxEntries int `json:"maxEntries"`
}

type Config struct {
	Limits         model.Limits   `json:"limits"`
	MultiDriveCopy MultiDriveCopy `json:"multiDriveCopy"`
	AI             AI             `json:"ai"`
	ArchiveSearch  ArchiveSearch  `json:"archiveSearch"`
	// Workers は照合の並列度。0 は自動（CPU数、上限8）。
	Workers int `json:"workers"`
	// ArchiveTool は 7z / RAR の検証に使う 7z.exe のパス。
	// 空欄なら PATH と既定のインストール先から自動検出する。
	ArchiveTool string `json:"archiveTool"`
}

func Default() Config {
	return Config{
		Limits: model.DefaultLimits(),
		ArchiveSearch: ArchiveSearch{
			Drives:     []string{"E", "F", "G"},
			MaxDepth:   8,
			MaxEntries: 200000,
		},
	}
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
	cfg.MultiDriveCopy.ScriptPath = strings.TrimSpace(cfg.MultiDriveCopy.ScriptPath)
	cfg.MultiDriveCopy.Preset = strings.TrimSpace(cfg.MultiDriveCopy.Preset)
	if cfg.Workers < 0 {
		cfg.Workers = 0
	}
	if cfg.Workers > 16 {
		cfg.Workers = 16
	}
	cfg.AI.Endpoint = strings.TrimSpace(cfg.AI.Endpoint)
	cfg.AI.Model = strings.TrimSpace(cfg.AI.Model)
	cfg.AI.APIKeyEnv = strings.TrimSpace(cfg.AI.APIKeyEnv)
	if cfg.AI.APIKeyEnv == "" {
		cfg.AI.APIKeyEnv = "ARCHIVE_CLEANER_AI_KEY"
	}
	if cfg.AI.Model == "" {
		cfg.AI.Model = "gpt-4o-mini"
	}
	if !cfg.AI.Enabled {
		// 無効時は接続先を空にして、誤って送信しないようにする。
		cfg.AI.Endpoint = ""
	}
	cfg.ArchiveTool = strings.TrimSpace(cfg.ArchiveTool)
	normalizeArchiveSearch(&cfg.ArchiveSearch)
}

// normalizeArchiveSearch は同名アーカイブ検索の設定を整える。
func normalizeArchiveSearch(search *ArchiveSearch) {
	drives := make([]string, 0, len(search.Drives))
	seen := map[string]bool{}
	for _, drive := range search.Drives {
		letter := strings.ToUpper(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(drive), ":")))
		if letter == "" || seen[letter] {
			continue
		}
		// ドライブレター1文字だけを受け付ける。パスを書かれても解釈しない。
		if len(letter) != 1 || letter[0] < 'A' || letter[0] > 'Z' {
			continue
		}
		seen[letter] = true
		drives = append(drives, letter)
	}
	if len(drives) == 0 {
		drives = []string{"E", "F", "G"}
	}
	search.Drives = drives
	search.RelativePath = strings.TrimSpace(search.RelativePath)
	if search.MaxDepth <= 0 {
		search.MaxDepth = 8
	}
	if search.MaxDepth > 32 {
		search.MaxDepth = 32
	}
	if search.MaxEntries <= 0 {
		search.MaxEntries = 200000
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
