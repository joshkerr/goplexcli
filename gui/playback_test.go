package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/joshkerr/goplexcli/internal/cache"
	"github.com/joshkerr/goplexcli/internal/player"
	"github.com/joshkerr/goplexcli/internal/plex"
)

func TestResolveItemsReportsMissingKeys(t *testing.T) {
	c := &cache.Cache{Media: []plex.MediaItem{
		{Key: "/library/metadata/1", Title: "One"},
		{Key: "/library/metadata/2", Title: "Two"},
	}}

	items, missing, err := resolveItems(c, []string{
		"/library/metadata/2",
		"/library/metadata/404",
		"/library/metadata/1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 || items[0].Title != "Two" || items[1].Title != "One" {
		t.Errorf("items: got %d, want the 2 cached items in requested order", len(items))
	}
	if len(missing) != 1 || missing[0] != "/library/metadata/404" {
		t.Errorf("missing: got %v, want the one unknown key", missing)
	}
}

func TestSilentExitWarning(t *testing.T) {
	quit := &player.PlayOutcome{ExitCode: 0, ErrorLine: "Failed to open https://example.com/v."}
	tests := []struct {
		name     string
		tracked  bool
		ran      time.Duration
		outcome  *player.PlayOutcome
		wantSubs string // "" means no warning expected
	}{
		{"quick death without IPC warns with detail", false, 2 * time.Second, quit, "Failed to open"},
		{"playback that tracked is fine", true, 2 * time.Second, quit, ""},
		{"long run without IPC is fine", false, time.Minute, quit, ""},
		{"nil outcome is fine", false, 2 * time.Second, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := silentExitWarning(tt.tracked, tt.ran, tt.outcome)
			if tt.wantSubs == "" && got != "" {
				t.Errorf("got %q, want no warning", got)
			}
			if tt.wantSubs != "" && !strings.Contains(got, tt.wantSubs) {
				t.Errorf("got %q, want it to contain %q", got, tt.wantSubs)
			}
		})
	}
}

func TestResolveItemsAllMissingIsError(t *testing.T) {
	c := &cache.Cache{Media: []plex.MediaItem{{Key: "/library/metadata/1"}}}
	if _, _, err := resolveItems(c, []string{"/library/metadata/404"}); err == nil {
		t.Error("want error when no requested items are in the cache")
	}
}

func TestRefreshWatchedStatus(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"MediaContainer": map[string]any{
				"Metadata": []map[string]any{
					{"viewOffset": 0, "viewCount": 1, "lastViewedAt": 1700000000},
				},
			},
		})
	}))
	defer ts.Close()

	client, err := plex.NewWithName(ts.URL, "tok", "test")
	if err != nil {
		t.Fatalf("NewWithName: %v", err)
	}

	seed := &cache.Cache{Media: []plex.MediaItem{
		{Key: "/library/metadata/1", Title: "Movie A", ViewCount: 0},
		{Key: "/library/metadata/2", Title: "Movie B", ViewCount: 0},
	}}
	if err := seed.Save(); err != nil {
		t.Fatalf("seed.Save: %v", err)
	}

	// Only "1" was played; "2" must stay untouched.
	refreshWatchedStatus(context.Background(), client, map[string]int{"/library/metadata/1": 0})

	got, err := cache.Load()
	if err != nil {
		t.Fatalf("cache.Load: %v", err)
	}
	if got.Media[0].ViewCount != 1 {
		t.Errorf("Media[0].ViewCount = %d, want 1", got.Media[0].ViewCount)
	}
	if got.Media[0].LastViewedAt != 1700000000 {
		t.Errorf("Media[0].LastViewedAt = %d, want 1700000000", got.Media[0].LastViewedAt)
	}
	if got.Media[1].ViewCount != 0 {
		t.Errorf("Media[1].ViewCount = %d, want untouched (0)", got.Media[1].ViewCount)
	}
}

func TestRefreshWatchedStatusNoOffsetsIsNoop(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// No Plex client call should happen, and cache.Load() should never be hit
	// (which would error since no cache exists yet) — passing a nil client
	// would panic if refreshWatchedStatus tried to use it.
	refreshWatchedStatus(context.Background(), nil, nil)
}
