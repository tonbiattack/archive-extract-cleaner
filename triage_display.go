package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
)

// advisoryTriageStates never reads file contents. Persisted decisions are hints,
// not authorization; transfer and recycle must freshly verify triageState.
// Top-level metadata detects common changes within a display-cache lifetime;
// nested changes and changes before the first display may not be detected.
func (a *App) advisoryTriageStates(items []model.Item, roots []string, decisions map[string]triageDecision) map[string]string {
	type observation struct{ id, identity, metadata, state string }
	observations := make([]observation, 0, len(items))
	states := make(map[string]string, len(items))
	for _, item := range items {
		d, ok := decisions[item.ID]
		if !ok || (d.State != "save" && d.State != "discard" && d.State != "hold") {
			states[item.ID] = "unjudged"
			continue
		}
		identity := fmt.Sprintf("%#v %#v %#v", item, roots, d)
		var metadata strings.Builder
		for _, p := range append([]string{item.ZipPath, item.FolderPath}, item.RelatedFolders...) {
			if p == "" {
				continue
			}
			info, err := os.Lstat(p)
			if err != nil {
				fmt.Fprintf(&metadata, "%q error:%v;", p, err)
				continue
			}
			fmt.Fprintf(&metadata, "%q %v %d %d;", p, info.Mode(), info.Size(), info.ModTime().UnixNano())
		}
		observations = append(observations, observation{item.ID, identity, metadata.String(), d.State})
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.triageDisplay == nil {
		a.triageDisplay = make(map[string]triageDisplayEntry)
	}
	for _, o := range observations {
		key := o.identity + "\x00" + o.metadata
		state := o.state
		if old, ok := a.triageDisplay[o.id]; ok {
			identity, _, _ := strings.Cut(old.key, "\x00")
			if identity == o.identity {
				if old.key != key || old.state == "unjudged" {
					state = "unjudged"
				}
			}
		}
		a.triageDisplay[o.id] = triageDisplayEntry{key, state}
		states[o.id] = state
	}
	return states
}
