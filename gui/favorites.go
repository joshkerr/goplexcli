package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/joshkerr/goplexcli/internal/config"
)

// Favorites are stored as a set of card keys: a movie's Plex metadata key, or a
// show's synthetic "show:<title>" key (shows have no MediaItem of their own —
// they're grouped from episodes, see groupShowCards). Keys of items that later
// leave the library are harmless: they simply match nothing when listing.

// favoritesPath returns the JSON file holding the favorite keys, alongside the
// media cache.
func favoritesPath() (string, error) {
	dir, err := config.GetCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "favorites.json"), nil
}

// favoritesLocked returns the favorites set, loading favorites.json on first
// use. Callers must hold favMu. A missing or unreadable file yields an empty
// set — favoriting starts fresh rather than failing.
func (a *App) favoritesLocked() map[string]bool {
	if a.favorites != nil {
		return a.favorites
	}
	a.favorites = map[string]bool{}
	path, err := favoritesPath()
	if err != nil {
		return a.favorites
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return a.favorites
	}
	var keys []string
	if err := json.Unmarshal(data, &keys); err != nil {
		return a.favorites
	}
	for _, k := range keys {
		if k != "" {
			a.favorites[k] = true
		}
	}
	return a.favorites
}

// saveFavoritesLocked persists the favorites set. Callers must hold favMu.
// Keys are written sorted so the file is stable across saves.
func (a *App) saveFavoritesLocked() error {
	keys := make([]string, 0, len(a.favorites))
	for k := range a.favorites {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	path, err := favoritesPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(keys, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// favSnapshot returns a copy of the favorites set for lock-free reads while
// building category lists.
func (a *App) favSnapshot() map[string]bool {
	a.favMu.Lock()
	defer a.favMu.Unlock()
	fav := a.favoritesLocked()
	out := make(map[string]bool, len(fav))
	for k := range fav {
		out[k] = true
	}
	return out
}

// ToggleFavorite adds or removes a card key from the favorites set and
// persists the change. Returns the new state: true if the item is now a
// favorite.
func (a *App) ToggleFavorite(key string) (bool, error) {
	if key == "" {
		return false, fmt.Errorf("empty key")
	}
	a.favMu.Lock()
	defer a.favMu.Unlock()
	fav := a.favoritesLocked()
	now := !fav[key]
	if now {
		fav[key] = true
	} else {
		delete(fav, key)
	}
	if err := a.saveFavoritesLocked(); err != nil {
		return now, fmt.Errorf("failed to save favorites: %w", err)
	}
	return now, nil
}

// ListFavoriteKeys returns every favorited card key, sorted, so the frontend
// can mark stars across grids without a per-item round trip.
func (a *App) ListFavoriteKeys() []string {
	a.favMu.Lock()
	defer a.favMu.Unlock()
	fav := a.favoritesLocked()
	keys := make([]string, 0, len(fav))
	for k := range fav {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
