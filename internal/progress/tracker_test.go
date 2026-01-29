package progress

import (
	"testing"

	"github.com/joshkerr/goplexcli/internal/plex"
)

func TestTrackerState(t *testing.T) {
	items := []*plex.MediaItem{
		{Key: "/library/metadata/1", Title: "Movie 1", Duration: 7200000},
		{Key: "/library/metadata/2", Title: "Movie 2", Duration: 5400000},
	}

	tracker := NewTracker(items, nil, nil)

	// Initially at position 0
	if tracker.CurrentIndex() != 0 {
		t.Errorf("expected index 0, got %d", tracker.CurrentIndex())
	}

	// Get current media
	current := tracker.CurrentMedia()
	if current.Key != "/library/metadata/1" {
		t.Errorf("expected key /library/metadata/1, got %s", current.Key)
	}

	// Advance to next
	tracker.SetIndex(1)
	if tracker.CurrentIndex() != 1 {
		t.Errorf("expected index 1, got %d", tracker.CurrentIndex())
	}

	current = tracker.CurrentMedia()
	if current.Key != "/library/metadata/2" {
		t.Errorf("expected key /library/metadata/2, got %s", current.Key)
	}
}

func TestExtractRatingKey(t *testing.T) {
	tests := []struct {
		key      string
		expected string
	}{
		{"/library/metadata/12345", "12345"},
		{"/library/metadata/1", "1"},
		{"/library/metadata/999999", "999999"},
	}

	for _, tt := range tests {
		result := extractRatingKey(tt.key)
		if result != tt.expected {
			t.Errorf("extractRatingKey(%s) = %s, want %s", tt.key, result, tt.expected)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		ms   int
		want string
	}{
		{0, "0:00"},
		{60000, "1:00"},
		{3661000, "1:01:01"},
		{7200000, "2:00:00"},
	}

	for _, tt := range tests {
		got := FormatDuration(tt.ms)
		if got != tt.want {
			t.Errorf("FormatDuration(%d) = %q, want %q", tt.ms, got, tt.want)
		}
	}
}
