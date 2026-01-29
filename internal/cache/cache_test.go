package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshkerr/goplexcli/internal/plex"
)

func TestIsStale(t *testing.T) {
	tests := []struct {
		name        string
		lastUpdated time.Time
		maxAge      time.Duration
		want        bool
	}{
		{
			name:        "zero time is stale",
			lastUpdated: time.Time{},
			maxAge:      time.Hour,
			want:        true,
		},
		{
			name:        "recent update is not stale",
			lastUpdated: time.Now().Add(-30 * time.Minute),
			maxAge:      time.Hour,
			want:        false,
		},
		{
			name:        "old update is stale",
			lastUpdated: time.Now().Add(-2 * time.Hour),
			maxAge:      time.Hour,
			want:        true,
		},
		{
			name:        "exactly at max age boundary",
			lastUpdated: time.Now().Add(-time.Hour - time.Second),
			maxAge:      time.Hour,
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Cache{LastUpdated: tt.lastUpdated}
			if got := c.IsStale(tt.maxAge); got != tt.want {
				t.Errorf("IsStale() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetMediaByTitle(t *testing.T) {
	c := &Cache{
		Media: []plex.MediaItem{
			{Key: "/library/1", Title: "The Matrix", Type: "movie"},
			{Key: "/library/2", Title: "The Matrix Reloaded", Type: "movie"},
			{Key: "/library/3", Title: "Inception", Type: "movie"},
		},
	}

	// Search for exact match
	results := c.GetMediaByTitle("The Matrix")
	if len(results) != 1 {
		t.Errorf("GetMediaByTitle('The Matrix') = %d results, want 1", len(results))
	}

	// Search for non-existent
	results = c.GetMediaByTitle("Avatar")
	if len(results) != 0 {
		t.Errorf("GetMediaByTitle('Avatar') = %d results, want 0", len(results))
	}
}

func TestGetMediaByIndex(t *testing.T) {
	c := &Cache{
		Media: []plex.MediaItem{
			{Key: "/library/1", Title: "Movie 1", Type: "movie"},
			{Key: "/library/2", Title: "Movie 2", Type: "movie"},
		},
	}

	// Valid index
	item, err := c.GetMediaByIndex(0)
	if err != nil {
		t.Errorf("GetMediaByIndex(0) unexpected error: %v", err)
	}
	if item.Title != "Movie 1" {
		t.Errorf("GetMediaByIndex(0) = %q, want 'Movie 1'", item.Title)
	}

	// Negative index
	_, err = c.GetMediaByIndex(-1)
	if err == nil {
		t.Error("GetMediaByIndex(-1) expected error, got nil")
	}

	// Out of bounds
	_, err = c.GetMediaByIndex(100)
	if err == nil {
		t.Error("GetMediaByIndex(100) expected error, got nil")
	}
}

func TestFormatForFzf(t *testing.T) {
	c := &Cache{
		Media: []plex.MediaItem{
			{Key: "/library/1", Title: "The Matrix", Year: 1999, Type: "movie"},
			{Key: "/library/2", Title: "Pilot", Type: "episode", ParentTitle: "Breaking Bad", ParentIndex: 1, Index: 1},
		},
	}

	items := c.FormatForFzf()
	if len(items) != 2 {
		t.Errorf("FormatForFzf() = %d items, want 2", len(items))
	}

	// Check that formatting is applied
	if items[0] != "The Matrix (1999)" {
		t.Errorf("FormatForFzf()[0] = %q, want 'The Matrix (1999)'", items[0])
	}
}

func TestGetMediaByFormattedTitle(t *testing.T) {
	c := &Cache{
		Media: []plex.MediaItem{
			{Key: "/library/1", Title: "The Matrix", Year: 1999, Type: "movie"},
		},
	}

	// Find by formatted title
	item, err := c.GetMediaByFormattedTitle("The Matrix (1999)")
	if err != nil {
		t.Errorf("GetMediaByFormattedTitle() unexpected error: %v", err)
	}
	if item.Key != "/library/1" {
		t.Errorf("GetMediaByFormattedTitle() key = %q, want '/library/1'", item.Key)
	}

	// Non-existent title
	_, err = c.GetMediaByFormattedTitle("Non Existent Movie (2000)")
	if err == nil {
		t.Error("GetMediaByFormattedTitle() expected error for non-existent title, got nil")
	}
}

func TestSaveLoad(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "goplexcli-cache-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create cache file directly
	cachePath := filepath.Join(tmpDir, "media.json")

	originalCache := &Cache{
		Media: []plex.MediaItem{
			{Key: "/library/1", Title: "Test Movie", Year: 2023, Type: "movie"},
			{Key: "/library/2", Title: "Test Episode", Type: "episode", ParentTitle: "Test Show"},
		},
		LastUpdated: time.Now().Truncate(time.Second), // Truncate for JSON roundtrip
	}

	// Save to temp file
	data, err := json.MarshalIndent(originalCache, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal cache: %v", err)
	}

	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		t.Fatalf("Failed to write cache: %v", err)
	}

	// Read it back
	data, err = os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("Failed to read cache: %v", err)
	}

	var loaded Cache
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("Failed to unmarshal cache: %v", err)
	}

	// Verify
	if len(loaded.Media) != 2 {
		t.Errorf("Media count = %d, want 2", len(loaded.Media))
	}
	if loaded.Media[0].Title != "Test Movie" {
		t.Errorf("Media[0].Title = %q, want 'Test Movie'", loaded.Media[0].Title)
	}
}

func TestEmptyCache(t *testing.T) {
	c := &Cache{}

	// FormatForFzf should return empty/nil slice (length 0)
	items := c.FormatForFzf()
	if len(items) != 0 {
		t.Errorf("FormatForFzf() = %d items, want 0", len(items))
	}

	// GetMediaByTitle should return empty slice
	results := c.GetMediaByTitle("anything")
	if len(results) != 0 {
		t.Errorf("GetMediaByTitle() = %d results, want 0", len(results))
	}
}
