package main

import (
	"testing"

	"github.com/joshkerr/goplexcli/internal/plex"
)

func TestBuildContinueWatching(t *testing.T) {
	media := []plex.MediaItem{
		{Title: "Watched", Duration: 1000, ViewOffset: 990, LastViewedAt: 100},      // >=95% -> excluded
		{Title: "InProgressOld", Duration: 1000, ViewOffset: 200, LastViewedAt: 50}, // included
		{Title: "NotStarted", Duration: 1000, ViewOffset: 0, LastViewedAt: 0},       // excluded
		{Title: "InProgressNew", Duration: 1000, ViewOffset: 300, LastViewedAt: 80}, // included
	}

	got := buildContinueWatching(media)

	if len(got) != 2 {
		t.Fatalf("expected 2 resumable items, got %d", len(got))
	}
	// Most recently viewed first.
	if got[0].Title != "InProgressNew" || got[1].Title != "InProgressOld" {
		t.Errorf("wrong order: got %q then %q", got[0].Title, got[1].Title)
	}
}

func TestBuildRecentlyAdded(t *testing.T) {
	media := []plex.MediaItem{
		{Title: "Old", AddedAt: 10},
		{Title: "Newest", AddedAt: 30},
		{Title: "Mid", AddedAt: 20},
	}

	got := buildRecentlyAdded(media, 2)

	if len(got) != 2 {
		t.Fatalf("expected limit of 2, got %d", len(got))
	}
	if got[0].Title != "Newest" || got[1].Title != "Mid" {
		t.Errorf("wrong order/limit: got %q then %q", got[0].Title, got[1].Title)
	}

	// A zero limit means no cap.
	if all := buildRecentlyAdded(media, 0); len(all) != 3 {
		t.Errorf("zero limit should return all, got %d", len(all))
	}

	// The source slice must not be mutated.
	if media[0].Title != "Old" {
		t.Errorf("source slice was reordered")
	}
}
