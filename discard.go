package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/removal"
	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
)

type DiscardPreview struct {
	Token string   `json:"token"`
	Paths []string `json:"paths"`
}
type discardSnapshot struct {
	token        string
	ids          []string
	paths        []string
	fingerprints map[string]string
	targetHashes map[string]string
	owners       map[string]string
}
type DiscardResult struct {
	Path    string `json:"path"`
	Success bool   `json:"success"`
	Reason  string `json:"reason"`
}

func (a *App) PreviewDiscard(ids []string) (DiscardPreview, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.discardPreview = nil
	if a.scanning || a.deleting {
		return DiscardPreview{}, errors.New("operation in progress")
	}
	if len(ids) == 0 {
		return DiscardPreview{}, errors.New("no items selected")
	}
	decisions, err := readTriage(a.triageFile())
	if err != nil {
		return DiscardPreview{}, err
	}
	snapshot := &discardSnapshot{fingerprints: map[string]string{}, targetHashes: map[string]string{}, owners: map[string]string{}}
	seen := map[string]bool{}
	pathSeen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return DiscardPreview{}, errors.New("duplicate item")
		}
		seen[id] = true
		idx, ok := a.index[id]
		if !ok {
			return DiscardPreview{}, errors.New("item not found")
		}
		item := a.items[idx]
		if len(item.RelatedFolders) > 0 {
			return DiscardPreview{}, errors.New("ambiguous item cannot be recycled")
		}
		if triageState(item, a.roots, decisions) != "discard" {
			return DiscardPreview{}, errors.New("item is not currently discarded")
		}
		snapshot.ids = append(snapshot.ids, id)
		snapshot.fingerprints[id] = decisions[id].Fingerprint
		zipPath, folderPath := item.ZipPath, item.FolderPath
		if item.ZipMoved {
			zipPath = ""
		}
		if item.FolderMoved {
			folderPath = ""
		}
		for _, p := range []string{zipPath, folderPath} {
			if p == "" {
				continue
			}
			abs, err := winpath.CleanAbs(p)
			if err != nil {
				return DiscardPreview{}, err
			}
			key := winpath.Key(abs)
			if pathSeen[key] {
				return DiscardPreview{}, errors.New("duplicate target path")
			}
			pathSeen[key] = true
			fp, err := triageFingerprint(model.Item{ID: "target", ZipPath: abs}, a.roots)
			if err != nil {
				return DiscardPreview{}, err
			}
			snapshot.targetHashes[abs] = fp
			snapshot.owners[abs] = id
			snapshot.paths = append(snapshot.paths, abs)
		}
	}
	if len(snapshot.paths) == 0 {
		return DiscardPreview{}, errors.New("no paths")
	}
	// Never recycle a directory that contains another scanned item, even when
	// that item is also selected: otherwise paths and per-target results lie.
	for _, target := range snapshot.paths {
		info, err := os.Lstat(target)
		if err != nil {
			return DiscardPreview{}, err
		}
		_ = info
		for _, item := range a.items {
			for _, other := range append([]string{item.ZipPath, item.FolderPath}, item.RelatedFolders...) {
				if other != "" && ((item.ID != snapshot.owners[target] && (winpath.EqualPath(target, other) || winpath.IsWithin(other, target))) || (!winpath.EqualPath(target, other) && winpath.IsWithin(target, other))) {
					return DiscardPreview{}, errors.New("folder contains another scanned item")
				}
			}
		}
	}
	token := make([]byte, 32)
	if _, err = rand.Read(token); err != nil {
		return DiscardPreview{}, err
	}
	snapshot.token = hex.EncodeToString(token)
	a.discardPreview = snapshot
	return DiscardPreview{Token: snapshot.token, Paths: append([]string(nil), snapshot.paths...)}, nil
}
func (a *App) RecycleDiscard(token string, confirmed bool) ([]DiscardResult, error) {
	if !confirmed {
		return nil, errors.New("explicit confirmation required")
	}
	a.mu.Lock()
	if a.scanning || a.deleting {
		a.mu.Unlock()
		return nil, errors.New("operation in progress")
	}
	snap := a.discardPreview
	a.discardPreview = nil
	if snap == nil || token == "" || snap.token != token {
		a.mu.Unlock()
		return nil, errors.New("preview missing or expired")
	}
	unlock, err := lockTriage(a.triageFile())
	if err != nil {
		a.mu.Unlock()
		return nil, err
	}
	defer unlock()
	decisions, err := readTriage(a.triageFile())
	if err != nil {
		a.mu.Unlock()
		return nil, err
	}
	for _, id := range snap.ids {
		idx, ok := a.index[id]
		if !ok || triageState(a.items[idx], a.roots, decisions) != "discard" || decisions[id].Fingerprint != snap.fingerprints[id] {
			a.mu.Unlock()
			return nil, errors.New("preview is stale; review again")
		}
	}
	roots := append([]string(nil), a.roots...)
	mover := a.mover
	a.deleting = true
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.deleting = false; a.mu.Unlock() }()
	results := make([]DiscardResult, 0, len(snap.paths))
	for _, p := range snap.paths {
		fp, err := triageFingerprint(model.Item{ID: "target", ZipPath: p}, roots)
		if err != nil || fp != snap.targetHashes[p] {
			results = append(results, DiscardResult{Path: p, Reason: "target changed since preview"})
			continue
		}
		info, err := os.Lstat(p)
		if err != nil {
			results = append(results, DiscardResult{Path: p, Reason: err.Error()})
			continue
		}
		kind := removal.KindFile
		if info.IsDir() {
			kind = removal.KindFolder
		}
		// The OS recycle API takes a path, not a verified file handle. An external
		// process can still replace it between this final check and the OS call.
		r := removal.Move(context.Background(), p, roots, kind, mover)
		results = append(results, DiscardResult{Path: p, Success: r.Success, Reason: r.Reason})
		if r.Success {
			a.mu.Lock()
			idx := a.index[snap.owners[p]]
			a.triageDisplay = nil
			item := &a.items[idx]
			if winpath.EqualPath(item.ZipPath, p) {
				item.ZipMoved = true
			}
			if winpath.EqualPath(item.FolderPath, p) {
				item.FolderMoved = true
			}
			item.Size = remainingSize(*item)
			// Preserve authorization only if the remaining targets still match the
			// preview; never bless changed content after a partial move.
			safe := true
			for _, remaining := range snap.paths {
				if snap.owners[remaining] != item.ID || winpath.EqualPath(remaining, p) {
					continue
				}
				if (item.ZipMoved && winpath.EqualPath(remaining, item.ZipPath)) || (item.FolderMoved && winpath.EqualPath(remaining, item.FolderPath)) {
					continue
				}
				fp, e := triageFingerprint(model.Item{ID: "target", ZipPath: remaining}, roots)
				if e != nil || fp != snap.targetHashes[remaining] {
					safe = false
					break
				}
			}
			if safe && ((!item.ZipMoved && item.ZipPath != "") || (!item.FolderMoved && item.FolderPath != "")) {
				fp, e := triageFingerprint(*item, roots)
				if e == nil {
					decisions[item.ID] = triageDecision{State: "discard", Fingerprint: fp}
				} else {
					delete(decisions, item.ID)
				}
			} else {
				delete(decisions, item.ID)
			}
			if e := writeTriage(a.triageFile(), decisions); e != nil {
				delete(decisions, item.ID)
				r.Reason = "moved, but could not save updated decision: " + e.Error()
				results[len(results)-1].Reason = r.Reason
			}
			a.mu.Unlock()
		}
	}
	return results, nil
}
