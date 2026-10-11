package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/joshkerr/goplexcli/internal/cache"
	"github.com/joshkerr/goplexcli/internal/config"
	"github.com/joshkerr/goplexcli/internal/library"
	"github.com/joshkerr/goplexcli/internal/libserver"
	"github.com/joshkerr/goplexcli/internal/plex"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// MatchCandidateDTO is one TMDB title offered in the "Fix match" panel.
type MatchCandidateDTO struct {
	ID        int     `json:"id"`
	Kind      string  `json:"kind"`
	Title     string  `json:"title"`
	Year      int     `json:"year"`
	Overview  string  `json:"overview"`
	Rating    float64 `json:"rating"`
	PosterURL string  `json:"posterURL"`
}

// MatchResultDTO summarizes an applied fix.
type MatchResultDTO struct {
	Title   string `json:"title"`   // new movie title or show name
	Key     string `json:"key"`     // card key to reopen (movie key or "show:<name>")
	Rebuilt int    `json:"rebuilt"` // items rebuilt
}

// matchTarget resolves a card key (movie key or "show:<name>") to a locally
// indexed item that identifies the movie or show on its library server.
func (a *App) matchTarget(key string) (*plex.MediaItem, error) {
	c := a.media()
	if c == nil {
		return nil, fmt.Errorf("no library loaded")
	}
	if strings.HasPrefix(key, "show:") {
		name := strings.TrimPrefix(key, "show:")
		for i := range c.Media {
			it := &c.Media[i]
			if it.Type == "episode" && it.ParentTitle == name && libserver.IsLocalKey(it.Key) {
				return it, nil
			}
		}
		return nil, fmt.Errorf("%q is not a locally indexed show; matches can only be fixed for local libraries", name)
	}
	for i := range c.Media {
		if c.Media[i].Key == key {
			if !libserver.IsLocalKey(key) {
				return nil, fmt.Errorf("this title comes from Plex; matches can only be fixed for local libraries")
			}
			return &c.Media[i], nil
		}
	}
	return nil, fmt.Errorf("item not found")
}

// isOwnLibrary reports whether an item belongs to this machine's library.
func (a *App) isOwnLibrary(it *plex.MediaItem) bool {
	return a.lib != nil && it.ServerName == library.ServerName(a.config())
}

// SearchMatches looks up TMDB candidates for fixing an item's match. kind is
// "movie" or "tv"; year narrows the search when > 0. The search runs on
// the library server that owns the item, so no TMDB key is needed here.
func (a *App) SearchMatches(key, kind, query string, year int) ([]MatchCandidateDTO, error) {
	it, err := a.matchTarget(key)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg := a.config()
	var results []libserver.Candidate
	posterBase := it.ServerURL
	if a.isOwnLibrary(it) {
		cacheDir, err := config.GetCacheDir()
		if err != nil {
			return nil, err
		}
		client, err := library.NewTMDBClient(cfg, cacheDir)
		if err != nil {
			return nil, err
		}
		results, err = library.SearchTitles(ctx, client, kind, query, year)
		if err != nil {
			return nil, err
		}
		posterBase = fmt.Sprintf("http://127.0.0.1:%d", a.lib.Port())
	} else {
		results, err = libserver.SearchRemote(ctx, it.ServerURL, cfg.TokenForURL(it.ServerURL), kind, query, year)
		if err != nil {
			return nil, err
		}
	}
	out := make([]MatchCandidateDTO, 0, len(results))
	for _, c := range results {
		dto := MatchCandidateDTO{ID: c.ID, Kind: c.Kind, Title: c.Title, Year: c.Year, Overview: c.Overview, Rating: c.Rating}
		if c.Poster != "" && posterBase != "" {
			dto.PosterURL = a.posters.register(posterSource{
				ServerURL: strings.TrimRight(posterBase, "/"),
				ThumbPath: c.Poster,
				Token:     cfg.TokenForURL(it.ServerURL),
				Width:     92,
				Height:    138,
			})
		}
		out = append(out, dto)
	}
	return out, nil
}

// ApplyMatch pins a movie or show to a TMDB id (0 restores the automatic
// match), rebuilds its items on the owning library server, and merges them
// into this GUI's cache.
func (a *App) ApplyMatch(key string, tmdbID int) (MatchResultDTO, error) {
	it, err := a.matchTarget(key)
	if err != nil {
		return MatchResultDTO{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cfg := a.config()
	var rebuilt []plex.MediaItem
	if a.isOwnLibrary(it) {
		rebuilt, err = a.rematchLocal(ctx, it.Key, tmdbID)
	} else {
		rebuilt, err = libserver.MatchRemote(ctx, it.ServerURL, cfg.TokenForURL(it.ServerURL), it.Key, tmdbID)
		if err == nil {
			a.mergeItems(rebuilt)
		}
	}
	if err != nil {
		return MatchResultDTO{}, err
	}
	res := MatchResultDTO{Rebuilt: len(rebuilt)}
	if len(rebuilt) > 0 {
		if rebuilt[0].Type == "episode" {
			res.Title = rebuilt[0].ParentTitle
			res.Key = "show:" + rebuilt[0].ParentTitle
		} else {
			res.Title = rebuilt[0].Title
			res.Key = rebuilt[0].Key
		}
	}
	// A renamed show keeps its star: favorites key shows by name.
	if res.Key != "" && res.Key != key && strings.HasPrefix(key, "show:") {
		if moved, err := a.fav.Remap(map[string]string{key: res.Key}); err == nil && moved > 0 {
			a.emitFavoritesChanged()
		}
	}
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "media:changed", map[string]any{"key": res.Key, "previous": key})
	}
	return res, nil
}

// rematchLocal rebuilds a movie or show from this machine's own library and
// installs the result in the cache. It also backs the embedded library
// server's match endpoint for other devices.
func (a *App) rematchLocal(ctx context.Context, key string, tmdbID int) ([]plex.MediaItem, error) {
	if !a.busy.TryLock() {
		return nil, fmt.Errorf("an index operation is already running; try again when it finishes")
	}
	defer a.busy.Unlock()
	c := a.media()
	if c == nil {
		return nil, fmt.Errorf("no library loaded")
	}
	cacheDir, err := config.GetCacheDir()
	if err != nil {
		return nil, err
	}
	cfg := a.reloadConfig()
	a.mediaMu.RLock()
	snapshot := append([]plex.MediaItem(nil), c.Media...)
	a.mediaMu.RUnlock()
	rebuilt, err := library.Rematch(ctx, cfg, cacheDir, snapshot, key, tmdbID, func(format string, args ...any) {
		fmt.Printf("library: "+format+"\n", args...)
	})
	if err != nil {
		return nil, err
	}
	a.mergeItems(rebuilt)
	return rebuilt, nil
}

// mergeItems installs rebuilt items into the in-memory cache, saves it, and
// refreshes the embedded library server.
func (a *App) mergeItems(rebuilt []plex.MediaItem) {
	if len(rebuilt) == 0 {
		return
	}
	c := a.media()
	if c == nil {
		return
	}
	a.mediaMu.Lock()
	merged := &cache.Cache{Media: library.MergeByKey(c.Media, rebuilt), LastUpdated: c.LastUpdated}
	a.mediaCache = merged
	a.mediaMu.Unlock()
	if err := merged.Save(); err != nil {
		fmt.Printf("failed to save cache after match fix: %v\n", err)
	}
	a.refreshLibraryServer()
}

// searchLocalTitles backs the embedded library server's search endpoint.
func (a *App) searchLocalTitles(ctx context.Context, kind, query string, year int) ([]libserver.Candidate, error) {
	cacheDir, err := config.GetCacheDir()
	if err != nil {
		return nil, err
	}
	client, err := library.NewTMDBClient(a.config(), cacheDir)
	if err != nil {
		return nil, err
	}
	return library.SearchTitles(ctx, client, kind, query, year)
}
