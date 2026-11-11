package main

import (
	"context"
	"os"
	"time"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/preview"
	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
)

const previewCacheLimit = 8

type cachedPreview struct {
	path      string
	modTime   time.Time
	size      int64
	entries   []preview.Entry
	truncated bool
	loadedAt  time.Time
}

func (a *App) GetPreview(id string, source string) preview.Response {
	a.mu.RLock()
	index, ok := a.index[id]
	var item model.Item
	if ok {
		item = a.items[index]
	}
	limits := a.limits
	base := a.ctx
	a.mu.RUnlock()
	if !ok {
		return preview.Response{ID: id, Error: "項目が見つかりません"}
	}
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithTimeout(base, 30*time.Second)
	defer cancel()

	sources := buildPreviewSources(item)
	response := preview.Response{ID: id, Sources: sources}
	if source == "" {
		for _, candidate := range sources {
			if candidate.Available {
				source = candidate.Kind
				break
			}
		}
	}
	response.Source = source
	if source == "" {
		response.Error = "表示できるZIPまたはフォルダがありません"
		return response
	}
	var selected *preview.Source
	for i := range sources {
		if sources[i].Kind == source {
			selected = &sources[i]
			break
		}
	}
	if selected == nil {
		response.Error = "指定されたプレビュー対象がありません"
		return response
	}
	if !selected.Available {
		if source == "zip" {
			response.Error = "ZIPは移動済み、または見つかりません"
		} else {
			response.Error = "解凍済みフォルダは移動済み、または見つかりません"
		}
		return response
	}

	info, err := os.Lstat(selected.Path)
	if err != nil {
		response.Error = "対象を確認できません: " + err.Error()
		return response
	}
	cacheKey := id + "|" + source
	a.previewMu.Lock()
	if cached, ok := a.previewCache[cacheKey]; ok &&
		cached.path == selected.Path &&
		cached.modTime.Equal(info.ModTime()) &&
		cached.size == info.Size() {
		response.Entries = cached.entries
		response.Truncated = cached.truncated
		a.previewMu.Unlock()
		return response
	}
	var entries []preview.Entry
	var truncated bool
	var loadErr error
	if source == "zip" {
		entries, truncated, loadErr = preview.ListZIP(selected.Path, limits)
	} else {
		entries, truncated, loadErr = preview.ListFolder(ctx, selected.Path, preview.MaxEntries)
	}
	if loadErr != nil {
		a.previewMu.Unlock()
		response.Error = loadErr.Error()
		return response
	}
	if len(a.previewCache) >= previewCacheLimit {
		oldestKey := ""
		var oldest time.Time
		for key, cached := range a.previewCache {
			if oldestKey == "" || cached.loadedAt.Before(oldest) {
				oldestKey = key
				oldest = cached.loadedAt
			}
		}
		delete(a.previewCache, oldestKey)
	}
	a.previewCache[cacheKey] = cachedPreview{
		path:      selected.Path,
		modTime:   info.ModTime(),
		size:      info.Size(),
		entries:   entries,
		truncated: truncated,
		loadedAt:  time.Now(),
	}
	a.previewMu.Unlock()
	response.Entries = entries
	response.Truncated = truncated
	return response
}

func buildPreviewSources(item model.Item) []preview.Source {
	var sources []preview.Source
	if item.ZipPath != "" {
		sources = append(sources, preview.Source{
			Kind:      "zip",
			Path:      item.ZipPath,
			Available: !item.ZipMoved && previewTargetAvailable(item.ZipPath, false),
		})
	}
	if item.FolderPath != "" {
		sources = append(sources, preview.Source{
			Kind:      "folder",
			Path:      item.FolderPath,
			Available: !item.FolderMoved && previewTargetAvailable(item.FolderPath, true),
		})
	}
	return sources
}

func previewTargetAvailable(path string, folder bool) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	reparse, err := winpath.IsReparsePoint(path)
	if err != nil || reparse {
		return false
	}
	if folder {
		return info.IsDir()
	}
	return info.Mode().IsRegular()
}

func (a *App) clearPreviewCache() {
	a.previewMu.Lock()
	a.previewCache = map[string]cachedPreview{}
	a.previewMu.Unlock()
}
