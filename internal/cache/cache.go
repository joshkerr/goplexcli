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

	// Compact JSON: the cache is machine-read only, and for large libraries
	// indented output roughly doubles the file size and marshal time.
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}

	// Write to a temp file and rename into place so an interrupted index run
	// (crash, Ctrl-C, power loss) can never leave a truncated cache behind.
	tmp, err := os.CreateTemp(cacheDir, ".media-*.json.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(tmpPath, 0644); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpPath, cachePath); err != nil {
		cleanup()
		return err
	}

	// Best-effort freshness sidecar so LAN peers can report cache size/age
	// without parsing the (large) media.json. A failure here must not fail the
	// save — the sidecar is an optimization, not the source of truth.
	_ = SaveMeta(CacheMeta{Count: len(c.Media), LastUpdated: c.LastUpdated})
	return nil
}

// CacheMeta is a tiny freshness summary written alongside media.json (meta.json)
// so a process can report how big and how fresh its cache is without reading the
// whole file. It powers the LAN cache-sync freshness comparison.
type CacheMeta struct {
	Count       int       `json:"count"`
	LastUpdated time.Time `json:"last_updated"`
}

// GetMetaPath returns the path to the freshness sidecar file.
func GetMetaPath() (string, error) {
	cacheDir, err := config.GetCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "meta.json"), nil
}

// LoadMeta reads the freshness sidecar. A missing sidecar is not an error: it
// returns a zero CacheMeta (count 0, zero time), which compares as "older than
// anything" for sync purposes.
func LoadMeta() (CacheMeta, error) {
	path, err := GetMetaPath()
	if err != nil {
		return CacheMeta{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return CacheMeta{}, nil
		}
		return CacheMeta{}, err
	}
	var m CacheMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return CacheMeta{}, err
	}
	return m, nil
}

// SaveMeta atomically writes the freshness sidecar. It's called by Save and by
// the LAN sync pull (which writes media.json directly, bypassing Save) so the
// sidecar always matches the cache on disk — preserving the original
// LastUpdated stamp rather than resetting it.
func SaveMeta(m CacheMeta) error {
	cacheDir, err := config.GetCacheDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}
	path, err := GetMetaPath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(cacheDir, ".meta-*.json.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

// IsStale checks if the cache is older than the given duration
func (c *Cache) IsStale(maxAge time.Duration) bool {
	if c.LastUpdated.IsZero() {
		return true
	}
	return time.Since(c.LastUpdated) > maxAge
}

// ApplyOffsets writes playback positions (milliseconds, keyed by media key)
// into the matching cached items, updating ViewOffset and LastViewedAt. It is
// used after playback to flush progress into the local cache so items appear in
// "Continue Watching" immediately, without a full reindex. It returns true if
// any item was updated. Callers persist the change with Save().
func (c *Cache) ApplyOffsets(offsets map[string]int) bool {
	if len(offsets) == 0 {
		return false
	}
	now := time.Now().Unix()
	updated := false
	for i := range c.Media {
		if offsetMs, ok := offsets[c.Media[i].Key]; ok {
			c.Media[i].ViewOffset = offsetMs
			c.Media[i].LastViewedAt = now
			updated = true
		}
	}
	return updated
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
