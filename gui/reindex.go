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
// event ({count} or {error}). It runs synchronously (the frontend calls it
// without awaiting completion for the UI, relying on events), but guards
// against concurrent runs.
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

	mappings := make([]plex.PathMapping, len(cfg.PathMappings))
	for i, m := range cfg.PathMappings {
		mappings[i] = plex.PathMapping{Prefix: m.Prefix, Remote: m.Remote}
	}

	cb := func(serverName, libraryName string, itemCount, totalItems, totalLibraries, currentLibrary, serverNum, totalServers int) {
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

	media, err := plex.GetAllMediaFromServers(context.Background(), serverConfigs, mappings, cb)
	if err != nil {
		a.emitReindexDone(0, err)
		return err
	}

	newCache := &cache.Cache{Media: media}
	if err := newCache.Save(); err != nil {
		a.emitReindexDone(0, err)
		return fmt.Errorf("failed to save cache: %w", err)
	}
	// Serve the freshly-indexed library from memory without re-reading disk.
	a.setMedia(newCache)

	a.emitReindexDone(len(media), nil)
	return nil
}

func (a *App) emitReindexDone(count int, err error) {
	if a.ctx == nil {
		return
	}
	payload := map[string]interface{}{"count": count}
	if err != nil {
		payload["error"] = err.Error()
	}
	wruntime.EventsEmit(a.ctx, "reindex:done", payload)
}

// buildServerConfigs assembles the per-server connection list from config,
// supporting both the multi-server format and the legacy single-URL field.
func buildServerConfigs(cfg *config.Config) ([]struct{ Name, URL, Token string }, error) {
	var out []struct{ Name, URL, Token string }
	for _, s := range cfg.GetEnabledServers() {
		out = append(out, struct{ Name, URL, Token string }{Name: s.Name, URL: s.URL, Token: cfg.PlexToken})
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
