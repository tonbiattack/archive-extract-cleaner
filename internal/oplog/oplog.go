package oplog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Entry struct {
	Time          time.Time `json:"time"`
	Action        string    `json:"action"`
	Method        string    `json:"method,omitempty"`
	ID            string    `json:"id"`
	ZipPath       string    `json:"zipPath,omitempty"`
	FolderPath    string    `json:"folderPath,omitempty"`
	Verdict       string    `json:"verdict,omitempty"`
	Success       bool      `json:"success"`
	Error         string    `json:"error,omitempty"`
	Reason        string    `json:"reason,omitempty"`
	MovedTargets  []string  `json:"movedTargets,omitempty"`
	FailedTargets []string  `json:"failedTargets,omitempty"`
	// AI推薦の記録。action が ai_suggestion のときだけ設定する。
	Decision   string  `json:"decision,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
	EvidenceID string  `json:"evidenceId,omitempty"`
}

type Logger struct {
	path string
	mu   sync.Mutex
}

func DefaultPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "archive-extract-cleaner", "operations.jsonl"), nil
}

func New(path string) *Logger {
	if path == "" {
		if fallback, err := DefaultPath(); err == nil {
			path = fallback
		}
	}
	return &Logger{path: path}
}

func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

func (l *Logger) Append(entry Entry) error {
	if l == nil || l.path == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = file.Write(data)
	return err
}
