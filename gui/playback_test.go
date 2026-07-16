package main

import (
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
