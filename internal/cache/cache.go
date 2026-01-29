// Package cache provides persistent storage for Plex media library data.
// It caches media items locally for fast offline browsing without requiring
// repeated API calls to the Plex server. The cache is stored as JSON in the
// user's config directory.
package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/joshkerr/goplexcli/internal/config"
	"github.com/joshkerr/goplexcli/internal/plex"
)

// Cache stores media items and metadata about when the cache was last updated.
// Use Load() to read from disk and Save() to persist changes.
type Cache struct {
	// Media contains all cached media items from the Plex library
	Media []plex.MediaItem `json:"media"`
	// LastUpdated tracks when the cache was last refreshed from Plex
	LastUpdated time.Time `json:"last_updated"`
}

// GetCachePath returns the path to the cache file
func GetCachePath() (string, error) {
	cacheDir, err := config.GetCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "media.json"), nil
}

// Load reads the cache from disk
func Load() (*Cache, error) {
	cachePath, err := GetCachePath()
	if err != nil {
		return nil, err
	}
	
	data, err := os.ReadFile(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &Cache{Media: []plex.MediaItem{}, LastUpdated: time.Time{}}, nil
		}
		return nil, err
	}
	
	var cache Cache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	
	return &cache, nil
}

// Save writes the cache to disk
func (c *Cache) Save() error {
	cacheDir, err := config.GetCacheDir()
	if err != nil {
		return err
	}
	
	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}
	
	cachePath, err := GetCachePath()
	if err != nil {
		return err
	}
	
	c.LastUpdated = time.Now()
	
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	
	return os.WriteFile(cachePath, data, 0644)
}

// IsStale checks if the cache is older than the given duration
func (c *Cache) IsStale(maxAge time.Duration) bool {
	if c.LastUpdated.IsZero() {
		return true
	}
	return time.Since(c.LastUpdated) > maxAge
}

// GetMediaByTitle returns media items that match the given title
func (c *Cache) GetMediaByTitle(title string) []plex.MediaItem {
	var results []plex.MediaItem
	for _, item := range c.Media {
		if item.Title == title {
			results = append(results, item)
		}
	}
	return results
}

// FormatForFzf returns a slice of formatted strings for fzf
func (c *Cache) FormatForFzf() []string {
	var items []string
	for _, media := range c.Media {
		items = append(items, media.FormatMediaTitle())
	}
	return items
}

// GetMediaByIndex returns the media item at the given index
func (c *Cache) GetMediaByIndex(index int) (*plex.MediaItem, error) {
	if index < 0 || index >= len(c.Media) {
		return nil, fmt.Errorf("index out of range")
	}
	return &c.Media[index], nil
}

// GetMediaByFormattedTitle returns the media item matching the formatted title
func (c *Cache) GetMediaByFormattedTitle(formattedTitle string) (*plex.MediaItem, error) {
	for _, item := range c.Media {
		if item.FormatMediaTitle() == formattedTitle {
			return &item, nil
		}
	}
	return nil, fmt.Errorf("media not found")
}
