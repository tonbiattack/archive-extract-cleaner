package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/oplog"
	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
)

type triageDecision struct {
	State             string `json:"state"`
	Fingerprint       string `json:"fingerprint"`
	RemovedFolder     string `json:"removedFolder,omitempty"`
	Revision          string `json:"revision,omitempty"`
	UndoPrevious      string `json:"undoPrevious,omitempty"`
	UndoRemovedFolder string `json:"undoRemovedFolder,omitempty"`
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

func (a *App) appendManualAudit(entry oplog.Entry) error {
	logger := a.log
	if logger == nil {
		path := a.logPath
		if path == "" {
			path = a.triageFile() + ".audit.jsonl"
		}
		logger = oplog.New(path)
	}
	if logger.Path() == "" {
		return errors.New("audit storage unavailable")
	}
	entry.Time = time.Now().UTC()
	return logger.Append(entry)
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

type TriageResult struct {
	Previous  string `json:"previous"`
	State     string `json:"state"`
	Warning   string `json:"warning"`
	UndoToken string `json:"undoToken,omitempty"`
}

// SetTriage is deprecated: use SetTriageDetailed to receive committed outcomes.
func (a *App) SetTriage(id, state string) error {
	return errors.New("SetTriage is deprecated; use SetTriageDetailed")
}

// SetTriageDetailed reports the previous effective state from the transaction.
func (a *App) SetTriageDetailed(id, state string) (TriageResult, error) {
	return a.updateTriage(id, state, nil)
}

// RestoreTriage consumes the token only for its exact, unchanged operation.
func (a *App) RestoreTriage(id, undoToken string) (TriageResult, error) {
	if undoToken == "" {
		return TriageResult{}, errors.New("invalid undo token")
	}
	return a.updateTriage(id, "", &undoToken)
}

func validTriageState(state string) bool {
	switch state {
	case "save", "discard", "hold", "unjudged":
		return true
	}
	return false
}

func (a *App) updateTriage(id, state string, undoToken *string) (TriageResult, error) {
	if undoToken == nil && !validTriageState(state) {
		return TriageResult{}, errors.New("invalid triage state")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.scanning || a.deleting {
		return TriageResult{}, errors.New("operation in progress")
	}
	a.discardPreview = nil
	a.triageDisplay = nil
	i, ok := a.index[id]
	if !ok {
		return TriageResult{}, errors.New("item not found")
	}
	path := a.triageFile()
	unlock, err := lockTriage(path)
	if err != nil {
		return TriageResult{}, err
	}
	defer unlock()
	m, err := readTriage(path)
	if err != nil {
		return TriageResult{}, err
	}
	fp, err := triageFingerprint(a.items[i], a.roots)
	if err != nil {
		return TriageResult{}, err
	}
	current := m[id]
	oldState := "unjudged"
	if (current.State == "save" || current.State == "discard" || current.State == "hold") && current.Fingerprint == fp {
		if current.RemovedFolder == "" {
			oldState = current.State
		} else if _, err := os.Lstat(current.RemovedFolder); errors.Is(err, os.ErrNotExist) {
			oldState = current.State
		}
	}
	removedFolder := ""
	if undoToken != nil {
		if current.Revision == "" || current.Revision != *undoToken || current.Fingerprint != fp || !validTriageState(current.UndoPrevious) || oldState != current.State {
			return TriageResult{}, errors.New("triage undo conflict: operation or content changed")
		}
		state = current.UndoPrevious
		removedFolder = current.UndoRemovedFolder
		if removedFolder != "" {
			if _, err := os.Lstat(removedFolder); !errors.Is(err, os.ErrNotExist) {
				return TriageResult{}, errors.New("triage undo conflict: removed folder changed")
			}
		}
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return TriageResult{}, err
	}
	revision := hex.EncodeToString(random[:])
	next := triageDecision{State: state, Fingerprint: fp, RemovedFolder: removedFolder, Revision: revision}
	if undoToken == nil {
		next.UndoPrevious = oldState
		if oldState != "unjudged" {
			next.UndoRemovedFolder = current.RemovedFolder
		}
	}
	m[id] = next
	if err := writeTriage(path, m); err != nil {
		return TriageResult{}, err
	}
	result := TriageResult{Previous: oldState, State: state}
	if undoToken == nil {
		result.UndoToken = revision
	}
	if err := a.appendManualAudit(oplog.Entry{Action: "triage", ID: id, OldState: oldState, NewState: state, Reason: "manual_triage", Success: true}); err != nil {
		result.Warning = fmt.Sprintf("decision saved, but audit failed: %v", err)
	}
	return result, nil
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
