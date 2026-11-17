package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
)

type triageDecision struct {
	State         string `json:"state"`
	Fingerprint   string `json:"fingerprint"`
	RemovedFolder string `json:"removedFolder,omitempty"`
}

func (a *App) triageFile() string {
	if a.triagePath != "" {
		return a.triagePath
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "archive-extract-cleaner", "triage.json")
}

// lockTriage serializes read-modify-write transactions across processes. A crash
// leaves the directory in place and requires manual recovery, rather than risking
// a concurrent write or guessing whether a lock owner is alive.
func lockTriage(path string) (func(), error) {
	if path == "" {
		return nil, errors.New("triage storage unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	lock := path + ".lock"
	if err := os.Mkdir(lock, 0700); err != nil {
		return nil, fmt.Errorf("triage storage locked: %w", err)
	}
	return func() { _ = os.Remove(lock) }, nil
}

func readTriage(path string) (map[string]triageDecision, error) {
	if path == "" {
		return nil, errors.New("triage storage unavailable")
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]triageDecision{}, nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]triageDecision
	if err = json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errors.New("invalid triage storage")
	}
	return m, nil
}
func writeTriage(path string, m map[string]triageDecision) error {
	if path == "" {
		return errors.New("triage storage unavailable")
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "triage-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
func triageFingerprint(item model.Item, roots []string) (string, error) {
	fp, _, err := triageFingerprintSize(item, roots)
	return fp, err
}

func triageFingerprintSize(item model.Item, roots []string) (string, int64, error) {
	var total int64
	zipPath, folderPath := item.ZipPath, item.FolderPath
	if item.ZipMoved {
		zipPath = ""
	}
	if item.FolderMoved {
		folderPath = ""
	}
	paths := append([]string{zipPath, folderPath}, item.RelatedFolders...)
	h := sha256.New()
	fmt.Fprintf(h, "%q %q %q %q %t %t\n", item.ID, item.ZipPath, item.FolderPath, item.Verdict, item.ZipMoved, item.FolderMoved)
	for _, p := range paths {
		if p == "" {
			fmt.Fprintln(h, "absent")
			continue
		}
		abs, err := winpath.CleanAbs(p)
		if err != nil {
			return "", 0, err
		}
		safe := false
		for _, root := range roots {
			if !winpath.EqualPath(root, abs) && winpath.IsWithin(root, abs) {
				if err = winpath.CheckNoReparseAncestors(root, abs); err != nil {
					return "", 0, err
				}
				safe = true
				break
			}
		}
		if !safe {
			return "", 0, errors.New("path outside scan roots")
		}
		err = filepath.Walk(abs, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			reparse, err := winpath.IsReparsePoint(path)
			if err != nil {
				return err
			}
			if reparse {
				return errors.New("link or reparse point")
			}
			fmt.Fprintf(h, "%q %v %d %d\n", path, info.Mode(), info.Size(), info.ModTime().UnixNano())
			if info.IsDir() {
				return nil
			}
			if !info.Mode().IsRegular() {
				return errors.New("unsupported file type")
			}
			if info.Size() > int64(^uint64(0)>>1)-total {
				return errors.New("total size exceeds supported range")
			}
			total += info.Size()
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, err = io.Copy(h, f)
			closeErr := f.Close()
			if err != nil {
				return err
			}
			return closeErr
		})
		if err != nil {
			return "", 0, err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), total, nil
}
func triageState(item model.Item, roots []string, decisions map[string]triageDecision) string {
	d, ok := decisions[item.ID]
	if !ok {
		return "unjudged"
	}
	switch d.State {
	case "save", "discard", "hold":
	default:
		return "unjudged"
	}
	if d.RemovedFolder != "" {
		if _, err := os.Lstat(d.RemovedFolder); !errors.Is(err, os.ErrNotExist) {
			return "unjudged"
		}
	}
	fingerprint, err := triageFingerprint(item, roots)
	if err != nil || fingerprint != d.Fingerprint {
		return "unjudged"
	}
	return d.State
}

// SetTriage persists a decision without moving or deleting any files.
func (a *App) SetTriage(id, state string) error {
	switch state {
	case "save", "discard", "hold", "unjudged":
	default:
		return errors.New("invalid triage state")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.scanning || a.deleting {
		return errors.New("operation in progress")
	}
	a.discardPreview = nil
	a.triageDisplay = nil
	i, ok := a.index[id]
	if !ok {
		return errors.New("item not found")
	}
	path := a.triageFile()
	unlock, err := lockTriage(path)
	if err != nil {
		return err
	}
	defer unlock()
	m, err := readTriage(path)
	if err != nil {
		return err
	}
	if state == "unjudged" {
		delete(m, id)
	} else {
		fp, err := triageFingerprint(a.items[i], a.roots)
		if err != nil {
			return err
		}
		m[id] = triageDecision{State: state, Fingerprint: fp}
	}
	return writeTriage(path, m)
}

func (a *App) savedItemsLocked() []model.Item {
	decisions, err := readTriage(a.triageFile())
	if err != nil {
		return nil
	}
	var items []model.Item
	for _, item := range a.items {
		if triageState(item, a.roots, decisions) == "save" {
			items = append(items, item)
		}
	}
	return items
}

// Display cache is advisory only. Authorization always calls triageState directly.
// Root metadata catches common edits; nested changes may remain invisible until
// rescan or an authorization recheck.
type triageDisplayEntry struct{ key, state string }

func (a *App) displayTriageState(item model.Item, roots []string, decisions map[string]triageDecision) string {
	d := decisions[item.ID]
	key := fmt.Sprintf("%#v %#v %#v", item, roots, d)
	for _, p := range append([]string{item.ZipPath, item.FolderPath, d.RemovedFolder}, item.RelatedFolders...) {
		if p == "" {
			continue
		}
		info, err := os.Lstat(p)
		if err != nil {
			key += err.Error()
		} else {
			key += fmt.Sprintf("%v %d %d", info.Mode(), info.Size(), info.ModTime().UnixNano())
		}
	}
	if e, ok := a.triageDisplay[item.ID]; ok && e.key == key {
		return e.state
	}
	state := triageState(item, roots, decisions)
	if a.triageDisplay == nil {
		a.triageDisplay = map[string]triageDisplayEntry{}
	}
	a.triageDisplay[item.ID] = triageDisplayEntry{key, state}
	return state
}
