package main

import (
	"testing"

	"github.com/joshkerr/goplexcli/internal/cache"
	"github.com/joshkerr/goplexcli/internal/config"
	"github.com/joshkerr/goplexcli/internal/plex"
)

func TestProgressPct(t *testing.T) {
	cases := []struct {
		name   string
		item   plex.MediaItem
		want   int
		inProg bool
	}{
		{"unwatched", plex.MediaItem{Duration: 1000}, 0, false},
		{"halfway", plex.MediaItem{Duration: 1000, ViewOffset: 500}, 50, true},
		{"nearly done", plex.MediaItem{Duration: 1000, ViewOffset: 960}, 96, false},
		{"no duration", plex.MediaItem{ViewOffset: 500}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := progressPct(&tc.item); got != tc.want {
				t.Errorf("progressPct = %d, want %d", got, tc.want)
			}
			if got := isInProgress(&tc.item); got != tc.inProg {
				t.Errorf("isInProgress = %v, want %v", got, tc.inProg)
			}
		})
	}
}

func TestGroupShows(t *testing.T) {
	a := NewApp()
	c := &cache.Cache{Media: []plex.MediaItem{
		{Key: "m1", Type: "movie", Title: "A Movie"},
		{Key: "e1", Type: "episode", Title: "Pilot", ParentTitle: "Show Z", ParentIndex: 1, Index: 1},
		{Key: "e2", Type: "episode", Title: "Ep2", ParentTitle: "Show Z", ParentIndex: 1, Index: 2},
		{Key: "e3", Type: "episode", Title: "Ep1", ParentTitle: "Show A", ParentIndex: 2, Index: 1},
	}}

	shows := a.groupShowCards(c)
	if len(shows) != 2 {
		t.Fatalf("expected 2 shows, got %d", len(shows))
	}
	// Sorted alphabetically: "Show A" before "Show Z".
	if shows[0].Title != "Show A" || shows[1].Title != "Show Z" {
		t.Errorf("shows not sorted: %q, %q", shows[0].Title, shows[1].Title)
	}
	if shows[1].EpisodeCount != 2 {
		t.Errorf("Show Z episode count = %d, want 2", shows[1].EpisodeCount)
	}
	if shows[0].Type != "show" || shows[0].Key != "show:Show A" {
		t.Errorf("unexpected show row: type=%q key=%q", shows[0].Type, shows[0].Key)
	}
}

func TestRecentlyAdded(t *testing.T) {
	a := NewApp()
	c := &cache.Cache{Media: []plex.MediaItem{
		{Key: "old", Type: "movie", Title: "Old", AddedAt: 100},
		{Key: "new", Type: "movie", Title: "New", AddedAt: 300},
		{Key: "mid", Type: "movie", Title: "Mid", AddedAt: 200},
		{Key: "ep", Type: "episode", Title: "Ep", AddedAt: 999},
	}}

	got := recentlyAddedCards(a, c, "movie", 2)
	if len(got) != 2 {
		t.Fatalf("expected 2 results (limited), got %d", len(got))
	}
	if got[0].Key != "new" || got[1].Key != "mid" {
		t.Errorf("recentlyAdded order = %q, %q; want new, mid", got[0].Key, got[1].Key)
	}
}

func TestGetItem(t *testing.T) {
	a := NewApp()
	a.setMedia(&cache.Cache{Media: []plex.MediaItem{
		{Key: "m1", Type: "movie", Title: "A Movie", Summary: "summary"},
		{Key: "e1", Type: "episode", Title: "Pilot", ParentTitle: "Show Z", Summary: "ep summary", ParentIndex: 1, Index: 1},
		{Key: "e2", Type: "episode", Title: "Ep2", ParentTitle: "Show Z", ParentIndex: 1, Index: 2},
	}})

	movie, err := a.GetItem("m1")
	if err != nil || movie.Summary != "summary" {
		t.Fatalf("GetItem(movie) = %+v, err=%v", movie, err)
	}

	show, err := a.GetItem("show:Show Z")
	if err != nil {
		t.Fatalf("GetItem(show) error: %v", err)
	}
	if show.Type != "show" || show.EpisodeCount != 2 || show.Summary != "ep summary" {
		t.Errorf("unexpected show DTO: %+v", show)
	}

	if _, err := a.GetItem("missing"); err == nil {
		t.Error("expected error for missing key")
	}
}

func TestBuildServerConfigs(t *testing.T) {
	// Multi-server.
	cfg := &config.Config{
		PlexToken: "tok",
		Servers: []config.PlexServer{
			{Name: "S1", URL: "http://a", Enabled: true},
			{Name: "S2", URL: "http://b", Enabled: false},
		},
	}
	got, err := buildServerConfigs(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "S1" || got[0].Token != "tok" {
		t.Errorf("expected only enabled S1 with token, got %+v", got)
	}

	// Legacy single-server fallback.
	legacy := &config.Config{PlexToken: "t2", PlexURL: "http://legacy"}
	got, err = buildServerConfigs(legacy)
	if err != nil || len(got) != 1 || got[0].URL != "http://legacy" {
		t.Errorf("legacy fallback failed: got=%+v err=%v", got, err)
	}

	// No servers.
	if _, err := buildServerConfigs(&config.Config{}); err == nil {
		t.Error("expected error when no servers configured")
	}
}
