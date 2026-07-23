package main

import (
	"testing"

	"github.com/joshkerr/goplexcli/internal/cache"
	"github.com/joshkerr/goplexcli/internal/plex"
)

// simTestApp builds an App over an in-memory media cache. Items carry no
// Thumb/ServerURL so no poster URLs are registered and no config is touched.
func simTestApp(t *testing.T, items []plex.MediaItem) *App {
	t.Helper()
	useTempConfigDir(t)
	app := NewApp()
	app.setMedia(&cache.Cache{Media: items})
	return app
}

func similarKeys(cards []MediaCardDTO) []string {
	keys := make([]string, len(cards))
	for i, c := range cards {
		keys[i] = c.Key
	}
	return keys
}

func TestSimilarItemsRanksBySummaryAndMetadata(t *testing.T) {
	app := simTestApp(t, []plex.MediaItem{
		{
			Key: "dasboot", Type: "movie", Title: "Das Boot", Year: 1981,
			Genre: "War, Drama", Director: "Wolfgang Petersen",
			Summary: "The claustrophobic world of a WWII German U-boat submarine; boredom, filth and sheer terror as the crew patrols the Atlantic.",
		},
		{
			Key: "u571", Type: "movie", Title: "U-571", Year: 2000,
			Genre: "War, Action", Director: "Jonathan Mostow",
			Summary: "A WWII German submarine is boarded by disguised American submariners trying to capture her Enigma cipher machine from the U-boat crew.",
		},
		{
			Key: "airforceone", Type: "movie", Title: "Air Force One", Year: 1997,
			Genre: "Action, Thriller", Director: "Wolfgang Petersen",
			Summary: "Communist radicals hijack the president's plane; he must fight to free the hostages.",
		},
		{
			Key: "clueless", Type: "movie", Title: "Clueless", Year: 1995,
			Genre: "Comedy, Romance", Director: "Amy Heckerling",
			Summary: "Shallow, rich and socially successful Cher is at the top of her Beverly Hills high school's pecking scale.",
		},
	})

	got := similarKeys(app.SimilarItems("dasboot"))
	if len(got) < 2 {
		t.Fatalf("SimilarItems = %v, want at least U-571 and Air Force One", got)
	}
	// U-571 shares the rare summary vocabulary (submarine, u-boat, wwii, crew)
	// plus a genre — it must beat the same-director-but-unrelated thriller.
	if got[0] != "u571" {
		t.Errorf("top result = %q, want u571 (got order %v)", got[0], got)
	}
	found := false
	for _, k := range got {
		if k == "airforceone" {
			found = true
		}
		if k == "dasboot" {
			t.Errorf("seed movie appears in its own results: %v", got)
		}
	}
	if !found {
		t.Errorf("same-director Air Force One missing from results %v", got)
	}
}

func TestSimilarItemsStayWithinSeedType(t *testing.T) {
	app := simTestApp(t, []plex.MediaItem{
		{
			Key: "dasboot", Type: "movie", Title: "Das Boot", Year: 1981,
			Genre: "War, Drama", Summary: "A WWII German U-boat submarine crew patrols the Atlantic.",
		},
		{
			Key: "ep1", Type: "episode", Title: "Pilot", ParentTitle: "Das Boot (Series)", Year: 2018,
			Genre: "War, Drama", Summary: "A German U-boat submarine crew sets out on patrol in WWII.",
		},
		{
			Key: "greyhound", Type: "movie", Title: "Greyhound", Year: 2020,
			Genre: "War, Action", Summary: "A navy commander escorts an Atlantic convoy hunted by German U-boat submarine wolfpacks in WWII.",
		},
	})

	for _, key := range similarKeys(app.SimilarItems("dasboot")) {
		if key == "show:Das Boot (Series)" {
			t.Errorf("show returned for a movie seed")
		}
	}
	// An episode seed resolves to its show; with only one show in the library
	// there is nothing similar, but the lookup must not error or cross types.
	if got := app.SimilarItems("ep1"); len(got) != 0 {
		t.Errorf("episode seed returned cross-type results: %v", similarKeys(got))
	}
}

func TestSimilarItemsExcludesUnrelated(t *testing.T) {
	app := simTestApp(t, []plex.MediaItem{
		{
			Key: "dasboot", Type: "movie", Title: "Das Boot", Year: 1981,
			Genre: "War, Drama", Director: "Wolfgang Petersen",
			Summary: "A WWII German U-boat submarine crew patrols the Atlantic.",
		},
		{
			Key: "nothing", Type: "movie", Title: "Totally Unrelated", Year: 0,
			Genre: "", Director: "", Summary: "",
		},
	})
	if got := app.SimilarItems("dasboot"); len(got) != 0 {
		t.Errorf("empty-metadata movie should score below the floor, got %v", similarKeys(got))
	}
}
