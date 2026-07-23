package main

import (
	"testing"

	"github.com/joshkerr/goplexcli/internal/cache"
	"github.com/joshkerr/goplexcli/internal/plex"
)

// TestFavoritesToggleAndPersist checks that toggling round-trips through
// favorites.json: favorites survive an app restart and un-favoriting removes
// the key.
func TestFavoritesToggleAndPersist(t *testing.T) {
	isolateHistory(t)

	a := NewApp()
	if fav, err := a.ToggleFavorite("m1"); err != nil || !fav {
		t.Fatalf("ToggleFavorite(m1) = %v, %v; want true, nil", fav, err)
	}
	if fav, err := a.ToggleFavorite("show:Show A"); err != nil || !fav {
		t.Fatalf("ToggleFavorite(show:Show A) = %v, %v; want true, nil", fav, err)
	}
	if _, err := a.ToggleFavorite(""); err == nil {
		t.Error("ToggleFavorite(\"\") should fail")
	}

	// A fresh App reloads the persisted set from disk.
	b := NewApp()
	keys := b.ListFavoriteKeys()
	if len(keys) != 2 || keys[0] != "m1" || keys[1] != "show:Show A" {
		t.Fatalf("reloaded keys = %v; want [m1 show:Show A]", keys)
	}

	if fav, err := b.ToggleFavorite("m1"); err != nil || fav {
		t.Fatalf("ToggleFavorite(m1) again = %v, %v; want false, nil", fav, err)
	}
	keys = NewApp().ListFavoriteKeys()
	if len(keys) != 1 || keys[0] != "show:Show A" {
		t.Errorf("keys after unfavorite = %v; want [show:Show A]", keys)
	}
}

// TestListCategoryFavorites checks that the favorites categories return only
// favorited movies/shows and honor the sort options.
func TestListCategoryFavorites(t *testing.T) {
	isolateHistory(t)

	a := NewApp()
	a.setMedia(&cache.Cache{Media: []plex.MediaItem{
		{Key: "m1", Type: "movie", Title: "Beta", Year: 2001, Genre: "Action"},
		{Key: "m2", Type: "movie", Title: "Alpha", Year: 2010, Genre: "Comedy"},
		{Key: "m3", Type: "movie", Title: "Gamma", Year: 1999, Genre: "Action"},
		{Key: "e1", Type: "episode", Title: "Pilot", ParentTitle: "Show A", Year: 2015, AddedAt: 100},
		{Key: "e2", Type: "episode", Title: "Pilot", ParentTitle: "Show B", Year: 2005, AddedAt: 300},
		{Key: "e3", Type: "episode", Title: "Pilot", ParentTitle: "Show C", Year: 2020, AddedAt: 200},
	}})
	for _, key := range []string{"m1", "m2", "show:Show A", "show:Show B"} {
		if _, err := a.ToggleFavorite(key); err != nil {
			t.Fatalf("ToggleFavorite(%s): %v", key, err)
		}
	}

	// Movies: only favorites, default A-Z.
	got := a.ListCategory("favorites-movies", BrowseOptions{})
	if len(got) != 2 || got[0].Key != "m2" || got[1].Key != "m1" {
		t.Fatalf("favorites-movies = %v; want [m2 m1]", cardKeys(got))
	}

	// Movies: sort + genre filter still apply.
	got = a.ListCategory("favorites-movies", BrowseOptions{SortField: "year", Desc: true})
	if len(got) != 2 || got[0].Key != "m2" || got[1].Key != "m1" {
		t.Errorf("favorites-movies by year desc = %v; want [m2 m1]", cardKeys(got))
	}
	got = a.ListCategory("favorites-movies", BrowseOptions{Genre: "Action"})
	if len(got) != 1 || got[0].Key != "m1" {
		t.Errorf("favorites-movies Action = %v; want [m1]", cardKeys(got))
	}

	// TV: only favorited shows; default A-Z by title.
	got = a.ListCategory("favorites-tv", BrowseOptions{})
	if len(got) != 2 || got[0].Key != "show:Show A" || got[1].Key != "show:Show B" {
		t.Fatalf("favorites-tv = %v; want [show:Show A show:Show B]", cardKeys(got))
	}

	// TV: "added" keeps newest-episode-first for desc, oldest-first for asc.
	got = a.ListCategory("favorites-tv", BrowseOptions{SortField: "added", Desc: true})
	if got[0].Key != "show:Show B" || got[1].Key != "show:Show A" {
		t.Errorf("favorites-tv added desc = %v; want [show:Show B show:Show A]", cardKeys(got))
	}
	got = a.ListCategory("favorites-tv", BrowseOptions{SortField: "added"})
	if got[0].Key != "show:Show A" || got[1].Key != "show:Show B" {
		t.Errorf("favorites-tv added asc = %v; want [show:Show A show:Show B]", cardKeys(got))
	}

	// TV: year sorting; unsupported fields fall back to title.
	got = a.ListCategory("favorites-tv", BrowseOptions{SortField: "year"})
	if got[0].Key != "show:Show B" || got[1].Key != "show:Show A" {
		t.Errorf("favorites-tv year asc = %v; want [show:Show B show:Show A]", cardKeys(got))
	}
	got = a.ListCategory("favorites-tv", BrowseOptions{SortField: "rating", Desc: true})
	if got[0].Key != "show:Show B" || got[1].Key != "show:Show A" {
		t.Errorf("favorites-tv rating fallback = %v; want title desc [show:Show B show:Show A]", cardKeys(got))
	}
}

func cardKeys(cards []MediaCardDTO) []string {
	keys := make([]string, len(cards))
	for i, c := range cards {
		keys[i] = c.Key
	}
	return keys
}
