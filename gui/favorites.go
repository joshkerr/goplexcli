package main

// Favorites are stored as a set of card keys: a movie's Plex metadata key, or a
// show's synthetic "show:<title>" key (shows have no MediaItem of their own —
// they're grouped from episodes, see groupShowCards). Keys of items that later
// leave the library are harmless: they simply match nothing when listing.
//
// The set lives in internal/favorites (shared with the CLI sync commands) so
// it merges across machines via LAN sync; see gui/lansync.go for the wiring.

// ToggleFavorite adds or removes a card key from the favorites set and
// persists the change. Returns the new state: true if the item is now a
// favorite.
func (a *App) ToggleFavorite(key string) (bool, error) {
	return a.fav.Toggle(key)
}

// ListFavoriteKeys returns every favorited card key, sorted, so the frontend
// can mark stars across grids without a per-item round trip.
func (a *App) ListFavoriteKeys() []string {
	keys, err := a.fav.Keys()
	if err != nil || keys == nil {
		return []string{}
	}
	return keys
}

// favSnapshot returns the favorited keys as a set for building category lists.
func (a *App) favSnapshot() map[string]bool {
	set, err := a.fav.Snapshot()
	if err != nil || set == nil {
		return map[string]bool{}
	}
	return set
}
