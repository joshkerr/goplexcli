package main

import (
	"context"
	"fmt"

	"github.com/joshkerr/goplexcli/internal/cache"
	"github.com/joshkerr/goplexcli/internal/config"
	"github.com/joshkerr/goplexcli/internal/plex"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ReindexProgress is emitted on the "reindex:progress" event as the library is
// fetched, so the frontend can show a live indexing overlay.
type ReindexProgress struct {
	Server    string `json:"server"`
	Library   string `json:"library"`
	Items     int    `json:"items"`
	Total     int    `json:"total"`
	ServerNum int    `json:"serverNum"`
	Servers   int    `json:"servers"`
	LibNum    int    `json:"libNum"`
	Libraries int    `json:"libraries"`
}

// Reindex rebuilds the local media cache from all enabled Plex servers,
// emitting "reindex:progress" events throughout and a final "reindex:done"
// event ({mode, count, added} or {error}). It runs synchronously (the frontend
// calls it without awaiting completion for the UI, relying on events), but
// guards against concurrent runs.
func (a *App) Reindex() error {
	if !a.busy.TryLock() {
		return fmt.Errorf("an index operation is already running")
	}
	defer a.busy.Unlock()

	cfg := a.reloadConfig()
	if err := cfg.Validate(); err != nil {
		return err
	}

	serverConfigs, err := buildServerConfigs(cfg)
	if err != nil {
		return err
	}

	mappings := pathMappings(cfg)

	media, err := plex.GetAllMediaFromServers(context.Background(), serverConfigs, mappings, a.reindexProgress())
	if err != nil {
		a.emitReindexDone("reindex", 0, 0, err)
		return err
	}

	newCache := &cache.Cache{Media: media}
	if err := newCache.Save(); err != nil {
		a.emitReindexDone("reindex", 0, 0, err)
		return fmt.Errorf("failed to save cache: %w", err)
	}
	// Serve the freshly-indexed library from memory without re-reading disk.
	a.setMedia(newCache)

	a.emitReindexDone("reindex", len(media), len(media), nil)
	return nil
}

// Update refreshes the local media cache incrementally: it fetches only items
// added to Plex since the newest item already cached and merges them in, which
// is far quicker than a full Reindex for large libraries. It reuses the same
// "reindex:progress"/"reindex:done" events as Reindex (with mode "update"), so
// the UI can share the indexing overlay. If the cache is empty there is nothing
// to update against, so it falls back to fetching everything.
func (a *App) Update() error {
	if !a.busy.TryLock() {
		return fmt.Errorf("an index operation is already running")
	}
	defer a.busy.Unlock()

	cfg := a.reloadConfig()
	if err := cfg.Validate(); err != nil {
		return err
	}

	existing, err := cache.Load()
	if err != nil {
		a.emitReindexDone("update", 0, 0, err)
		return fmt.Errorf("failed to load existing cache: %w", err)
	}
	incremental := len(existing.Media) > 0

	serverConfigs, err := buildServerConfigs(cfg)
	if err != nil {
		return err
	}

	mappings := pathMappings(cfg)
	cb := a.reindexProgress()

	var media []plex.MediaItem
	if incremental {
		media, err = plex.GetNewMediaFromServers(context.Background(), serverConfigs, mappings, newestAddedFunc(existing.Media), cb)
	} else {
		media, err = plex.GetAllMediaFromServers(context.Background(), serverConfigs, mappings, cb)
	}
	if err != nil {
		a.emitReindexDone("update", 0, 0, err)
		return err
	}

	finalMedia := media
	added := len(media)
	if incremental {
		finalMedia, added = mergeMedia(existing.Media, media)
	}

	newCache := &cache.Cache{Media: finalMedia}
	if err := newCache.Save(); err != nil {
		a.emitReindexDone("update", 0, 0, err)
		return fmt.Errorf("failed to save cache: %w", err)
	}
	a.setMedia(newCache)

	a.emitReindexDone("update", len(finalMedia), added, nil)
	return nil
}

// reindexProgress builds the callback that relays fetch progress to the
// frontend as "reindex:progress" events, shared by Reindex and Update.
func (a *App) reindexProgress() func(serverName, libraryName string, itemCount, totalItems, totalLibraries, currentLibrary, serverNum, totalServers int) {
	return func(serverName, libraryName string, itemCount, totalItems, totalLibraries, currentLibrary, serverNum, totalServers int) {
		if a.ctx == nil {
			return
		}
		wruntime.EventsEmit(a.ctx, "reindex:progress", ReindexProgress{
			Server:    serverName,
			Library:   libraryName,
			Items:     itemCount,
			Total:     totalItems,
			ServerNum: serverNum,
			Servers:   totalServers,
			LibNum:    currentLibrary,
			Libraries: totalLibraries,
		})
	}
}

func (a *App) emitReindexDone(mode string, count, added int, err error) {
	if a.ctx == nil {
		return
	}
	payload := map[string]interface{}{"mode": mode, "count": count, "added": added}
	if err != nil {
		payload["error"] = err.Error()
	}
	wruntime.EventsEmit(a.ctx, "reindex:done", payload)
}

// pathMappings converts the config's path mappings to the plex package's form.
func pathMappings(cfg *config.Config) []plex.PathMapping {
	mappings := make([]plex.PathMapping, len(cfg.PathMappings))
	for i, m := range cfg.PathMappings {
		mappings[i] = plex.PathMapping{Prefix: m.Prefix, Remote: m.Remote}
	}
	return mappings
}

// newestAddedFunc returns a lookup of the newest AddedAt already cached, keyed
// by server name and library type ("movie"/"show"), so an incremental fetch can
// ask Plex only for items newer than what we already have.
func newestAddedFunc(existing []plex.MediaItem) func(serverName, libType string) int64 {
	// maxAdded[serverName][itemType] = newest AddedAt seen for that item type.
	maxAdded := map[string]map[string]int64{}
	for _, item := range existing {
		byType := maxAdded[item.ServerName]
		if byType == nil {
			byType = map[string]int64{}
			maxAdded[item.ServerName] = byType
		}
		if item.AddedAt > byType[item.Type] {
			byType[item.Type] = item.AddedAt
		}
	}
	return func(serverName, libType string) int64 {
		itemType := "movie"
		if libType == "show" {
			itemType = "episode"
		}
		if byType, ok := maxAdded[serverName]; ok {
			return byType[itemType]
		}
		return 0
	}
}

// mergeMedia combines newly fetched items into the existing cached items,
// deduplicating by server name and key. Items present in both are replaced with
// the freshly fetched version (picking up metadata changes). It returns the
// merged slice and the number of items that were newly added.
func mergeMedia(existing, fetched []plex.MediaItem) ([]plex.MediaItem, int) {
	keyOf := func(m plex.MediaItem) string { return m.ServerName + "\x00" + m.Key }

	merged := make([]plex.MediaItem, len(existing))
	copy(merged, existing)

	index := make(map[string]int, len(merged))
	for i := range merged {
		index[keyOf(merged[i])] = i
	}

	added := 0
	for _, item := range fetched {
		k := keyOf(item)
		if i, ok := index[k]; ok {
			merged[i] = item
			continue
		}
		index[k] = len(merged)
		merged = append(merged, item)
		added++
	}

	return merged, added
}

// buildServerConfigs assembles the per-server connection list from config,
// supporting both the multi-server format and the legacy single-URL field.
func buildServerConfigs(cfg *config.Config) ([]struct{ Name, URL, Token string }, error) {
	var out []struct{ Name, URL, Token string }
	for _, s := range cfg.GetEnabledServers() {
		out = append(out, struct{ Name, URL, Token string }{Name: s.Name, URL: s.URL, Token: cfg.TokenForServer(s)})
	}
	// Legacy single-server fallback.
	if len(out) == 0 && cfg.PlexURL != "" {
		out = append(out, struct{ Name, URL, Token string }{Name: "Default Server", URL: cfg.PlexURL, Token: cfg.PlexToken})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no enabled Plex servers configured")
	}
	return out, nil
}
