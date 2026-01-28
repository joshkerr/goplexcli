package ui

import (
	"testing"

	"github.com/joshkerr/goplexcli/internal/plex"
)

func TestSelectQueueItemsForRemoval_EmptyQueue(t *testing.T) {
	var queue []*plex.MediaItem
	_, err := SelectQueueItemsForRemoval(queue, "fzf")
	if err == nil {
		t.Error("Expected error for empty queue, got nil")
	}
	if err.Error() != "queue is empty" {
		t.Errorf("Expected 'queue is empty' error, got: %s", err.Error())
	}
}

func TestSelectQueueItemsForRemoval_FzfNotFound(t *testing.T) {
	queue := []*plex.MediaItem{
		{Key: "/library/1", Title: "Test Movie"},
	}
	_, err := SelectQueueItemsForRemoval(queue, "nonexistent-fzf-binary-12345")
	if err == nil {
		t.Error("Expected error for missing fzf, got nil")
	}
	expectedMsg := "fzf not found in PATH. Please install fzf or specify the path in config"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message %q, got %q", expectedMsg, err.Error())
	}
}

func TestPluralizeItems(t *testing.T) {
	tests := []struct {
		count    int
		expected string
	}{
		{0, "0 items"},
		{1, "1 item"},
		{2, "2 items"},
		{10, "10 items"},
	}

	for _, tt := range tests {
		result := PluralizeItems(tt.count)
		if result != tt.expected {
			t.Errorf("PluralizeItems(%d) = %q, expected %q", tt.count, result, tt.expected)
		}
	}
}

func TestIsAvailable(t *testing.T) {
	// Test with non-existent binary
	if IsAvailable("nonexistent-binary-12345") {
		t.Error("Expected false for non-existent binary")
	}

	// Test with empty path (should check for "fzf" in PATH)
	// This test just verifies the function doesn't panic
	_ = IsAvailable("")
}

func TestGetUniqueTVShows(t *testing.T) {
	tests := []struct {
		name     string
		episodes []plex.MediaItem
		expected []string
	}{
		{
			name:     "empty input",
			episodes: []plex.MediaItem{},
			expected: []string{},
		},
		{
			name: "no episodes (only movies)",
			episodes: []plex.MediaItem{
				{Type: "movie", Title: "Movie 1"},
				{Type: "movie", Title: "Movie 2"},
			},
			expected: []string{},
		},
		{
			name: "single show",
			episodes: []plex.MediaItem{
				{Type: "episode", Title: "Ep 1", ParentTitle: "Show A"},
				{Type: "episode", Title: "Ep 2", ParentTitle: "Show A"},
			},
			expected: []string{"Show A"},
		},
		{
			name: "multiple shows sorted alphabetically",
			episodes: []plex.MediaItem{
				{Type: "episode", Title: "Ep 1", ParentTitle: "Zebra Show"},
				{Type: "episode", Title: "Ep 1", ParentTitle: "Alpha Show"},
				{Type: "episode", Title: "Ep 2", ParentTitle: "Beta Show"},
			},
			expected: []string{"Alpha Show", "Beta Show", "Zebra Show"},
		},
		{
			name: "mixed movies and episodes",
			episodes: []plex.MediaItem{
				{Type: "movie", Title: "Movie 1"},
				{Type: "episode", Title: "Ep 1", ParentTitle: "Show A"},
				{Type: "episode", Title: "Ep 2", ParentTitle: "Show B"},
			},
			expected: []string{"Show A", "Show B"},
		},
		{
			name: "episode with empty ParentTitle ignored",
			episodes: []plex.MediaItem{
				{Type: "episode", Title: "Ep 1", ParentTitle: ""},
				{Type: "episode", Title: "Ep 2", ParentTitle: "Show A"},
			},
			expected: []string{"Show A"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetUniqueTVShows(tt.episodes)
			if len(result) != len(tt.expected) {
				t.Errorf("GetUniqueTVShows() got %d shows, want %d", len(result), len(tt.expected))
				return
			}
			for i, show := range result {
				if show != tt.expected[i] {
					t.Errorf("GetUniqueTVShows()[%d] = %q, want %q", i, show, tt.expected[i])
				}
			}
		})
	}
}

func TestGetSeasonsForShow(t *testing.T) {
	tests := []struct {
		name     string
		episodes []plex.MediaItem
		showName string
		expected []int
	}{
		{
			name:     "empty input",
			episodes: []plex.MediaItem{},
			showName: "Show A",
			expected: []int{},
		},
		{
			name: "show not found",
			episodes: []plex.MediaItem{
				{Type: "episode", ParentTitle: "Show B", ParentIndex: 1},
			},
			showName: "Show A",
			expected: []int{},
		},
		{
			name: "single season",
			episodes: []plex.MediaItem{
				{Type: "episode", ParentTitle: "Show A", ParentIndex: 1},
				{Type: "episode", ParentTitle: "Show A", ParentIndex: 1},
			},
			showName: "Show A",
			expected: []int{1},
		},
		{
			name: "multiple seasons sorted numerically",
			episodes: []plex.MediaItem{
				{Type: "episode", ParentTitle: "Show A", ParentIndex: 3},
				{Type: "episode", ParentTitle: "Show A", ParentIndex: 1},
				{Type: "episode", ParentTitle: "Show A", ParentIndex: 2},
			},
			showName: "Show A",
			expected: []int{1, 2, 3},
		},
		{
			name: "specials (Season 0) placed at end",
			episodes: []plex.MediaItem{
				{Type: "episode", ParentTitle: "Show A", ParentIndex: 0}, // Specials
				{Type: "episode", ParentTitle: "Show A", ParentIndex: 1},
				{Type: "episode", ParentTitle: "Show A", ParentIndex: 2},
			},
			showName: "Show A",
			expected: []int{1, 2, 0}, // Specials at end
		},
		{
			name: "filters by show name",
			episodes: []plex.MediaItem{
				{Type: "episode", ParentTitle: "Show A", ParentIndex: 1},
				{Type: "episode", ParentTitle: "Show B", ParentIndex: 5},
				{Type: "episode", ParentTitle: "Show A", ParentIndex: 2},
			},
			showName: "Show A",
			expected: []int{1, 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetSeasonsForShow(tt.episodes, tt.showName)
			if len(result) != len(tt.expected) {
				t.Errorf("GetSeasonsForShow() got %d seasons %v, want %d seasons %v",
					len(result), result, len(tt.expected), tt.expected)
				return
			}
			for i, season := range result {
				if season != tt.expected[i] {
					t.Errorf("GetSeasonsForShow()[%d] = %d, want %d", i, season, tt.expected[i])
				}
			}
		})
	}
}

func TestGetEpisodesForSeason(t *testing.T) {
	tests := []struct {
		name      string
		episodes  []plex.MediaItem
		showName  string
		seasonNum int
		expected  []string // Expected episode titles in order
	}{
		{
			name:      "empty input",
			episodes:  []plex.MediaItem{},
			showName:  "Show A",
			seasonNum: 1,
			expected:  []string{},
		},
		{
			name: "no matching episodes",
			episodes: []plex.MediaItem{
				{Type: "episode", Title: "Ep 1", ParentTitle: "Show A", ParentIndex: 2, Index: 1},
			},
			showName:  "Show A",
			seasonNum: 1,
			expected:  []string{},
		},
		{
			name: "episodes sorted by episode number",
			episodes: []plex.MediaItem{
				{Type: "episode", Title: "Ep 3", ParentTitle: "Show A", ParentIndex: 1, Index: 3},
				{Type: "episode", Title: "Ep 1", ParentTitle: "Show A", ParentIndex: 1, Index: 1},
				{Type: "episode", Title: "Ep 2", ParentTitle: "Show A", ParentIndex: 1, Index: 2},
			},
			showName:  "Show A",
			seasonNum: 1,
			expected:  []string{"Ep 1", "Ep 2", "Ep 3"},
		},
		{
			name: "filters by show and season",
			episodes: []plex.MediaItem{
				{Type: "episode", Title: "A-S1-E1", ParentTitle: "Show A", ParentIndex: 1, Index: 1},
				{Type: "episode", Title: "A-S2-E1", ParentTitle: "Show A", ParentIndex: 2, Index: 1},
				{Type: "episode", Title: "B-S1-E1", ParentTitle: "Show B", ParentIndex: 1, Index: 1},
			},
			showName:  "Show A",
			seasonNum: 1,
			expected:  []string{"A-S1-E1"},
		},
		{
			name: "get specials (Season 0)",
			episodes: []plex.MediaItem{
				{Type: "episode", Title: "Special 1", ParentTitle: "Show A", ParentIndex: 0, Index: 1},
				{Type: "episode", Title: "Ep 1", ParentTitle: "Show A", ParentIndex: 1, Index: 1},
				{Type: "episode", Title: "Special 2", ParentTitle: "Show A", ParentIndex: 0, Index: 2},
			},
			showName:  "Show A",
			seasonNum: 0,
			expected:  []string{"Special 1", "Special 2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetEpisodesForSeason(tt.episodes, tt.showName, tt.seasonNum)
			if len(result) != len(tt.expected) {
				t.Errorf("GetEpisodesForSeason() got %d episodes, want %d", len(result), len(tt.expected))
				return
			}
			for i, ep := range result {
				if ep.Title != tt.expected[i] {
					t.Errorf("GetEpisodesForSeason()[%d].Title = %q, want %q", i, ep.Title, tt.expected[i])
				}
			}
		})
	}
}

func TestSelectTVShow_EmptyList(t *testing.T) {
	_, err := SelectTVShow([]string{}, "fzf")
	if err == nil {
		t.Error("Expected error for empty show list, got nil")
	}
	if err.Error() != "no shows to select from" {
		t.Errorf("Expected 'no shows to select from' error, got: %s", err.Error())
	}
}

func TestSelectSeason_EmptyList(t *testing.T) {
	_, err := SelectSeason([]int{}, "Show A", "fzf")
	if err == nil {
		t.Error("Expected error for empty season list, got nil")
	}
	if err.Error() != "no seasons to select from" {
		t.Errorf("Expected 'no seasons to select from' error, got: %s", err.Error())
	}
}
