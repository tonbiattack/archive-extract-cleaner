package preview

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
	"github.com/tonbiattack/archive-extract-cleaner/internal/ziparchive"
)

const MaxEntries = 20000

type Source struct {
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	Available bool   `json:"available"`
}

type Entry struct {
	Path     string    `json:"path"`
	Name     string    `json:"name"`
	Kind     string    `json:"kind"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Depth    int       `json:"depth"`
}

type Response struct {
	ID        string   `json:"id"`
	Source    string   `json:"source"`
	Sources   []Source `json:"sources"`
	Entries   []Entry  `json:"entries"`
	Truncated bool     `json:"truncated"`
	Error     string   `json:"error,omitempty"`
}

func ListZIP(path string, limits model.Limits) ([]Entry, bool, error) {
	archive, err := ziparchive.Open(path, limits)
	if err != nil {
		return nil, false, err
	}
	defer archive.Close()
	direct := archive.Variants[0]
	entries := make([]Entry, 0, direct.FileCount+direct.DirCount)
	for _, display := range direct.DirPaths {
		entries = append(entries, Entry{
			Path:  display,
			Name:  filepath.Base(display),
			Kind:  "directory",
			Depth: depth(display),
		})
	}
	for _, file := range direct.Files {
		modified := time.Time{}
		if file.File.Modified != (time.Time{}) {
			modified = file.File.Modified
		}
		entries = append(entries, Entry{
			Path:     file.Path,
			Name:     filepath.Base(file.Path),
			Kind:     "file",
			Size:     int64(file.File.UncompressedSize64),
			Modified: modified,
			Depth:    depth(file.Path),
		})
	}
	sortEntries(entries)
	truncated := false
	if len(entries) > MaxEntries {
		entries = entries[:MaxEntries]
		truncated = true
	}
	return entries, truncated, nil
}

func ListFolder(ctx context.Context, path string, maxEntries int) ([]Entry, bool, error) {
	if maxEntries <= 0 || maxEntries > MaxEntries {
		maxEntries = MaxEntries
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, false, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return nil, false, err
	}
	if !info.IsDir() {
		return nil, false, fmt.Errorf("%s is not a directory", abs)
	}
	reparse, err := winpath.IsReparsePoint(abs)
	if err != nil {
		return nil, false, err
	}
	if reparse {
		return nil, false, fmt.Errorf("%s is a link or reparse point", abs)
	}
	state := &folderState{ctx: ctx, max: maxEntries}
	state.walk(abs, "", 0)
	sortEntries(state.entries)
	return state.entries, state.truncated, nil
}

type folderState struct {
	ctx       context.Context
	max       int
	entries   []Entry
	truncated bool
}

func (s *folderState) walk(absDir, relDir string, depthValue int) {
	if s.truncated || s.ctx.Err() != nil {
		return
	}
	entries, err := os.ReadDir(absDir)
	if err != nil {
		s.add(Entry{
			Path:  relDir,
			Name:  filepath.Base(absDir),
			Kind:  "error",
			Depth: depthValue,
		})
		return
	}
	for _, entry := range entries {
		if s.truncated || s.ctx.Err() != nil {
			return
		}
		name := entry.Name()
		rel := name
		if relDir != "" {
			rel = relDir + "/" + name
		}
		full := filepath.Join(absDir, name)
		reparse, err := winpath.IsReparsePoint(full)
		if err != nil {
			s.add(Entry{Path: rel, Name: name, Kind: "error", Depth: depthValue})
			continue
		}
		if reparse || entry.Type()&os.ModeSymlink != 0 {
			s.add(Entry{Path: rel, Name: name, Kind: "link", Depth: depthValue})
			continue
		}
		if entry.IsDir() {
			s.add(Entry{Path: rel, Name: name, Kind: "directory", Depth: depthValue})
			s.walk(full, rel, depthValue+1)
			continue
		}
		info, err := entry.Info()
		if err != nil {
			s.add(Entry{Path: rel, Name: name, Kind: "error", Depth: depthValue})
			continue
		}
		kind := "file"
		if !info.Mode().IsRegular() {
			kind = "other"
		}
		s.add(Entry{
			Path:     rel,
			Name:     name,
			Kind:     kind,
			Size:     info.Size(),
			Modified: info.ModTime(),
			Depth:    depthValue,
		})
	}
}

func (s *folderState) add(entry Entry) {
	if len(s.entries) >= s.max {
		s.truncated = true
		return
	}
	s.entries = append(s.entries, entry)
}

func sortEntries(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := strings.ToLower(entries[i].Path), strings.ToLower(entries[j].Path)
		if a != b {
			return a < b
		}
		return entries[i].Kind < entries[j].Kind
	})
}

func depth(path string) int {
	return strings.Count(strings.Trim(path, "/"), "/")
}
