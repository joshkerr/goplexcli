package queue

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/joshkerr/goplexcli/internal/config"
	"github.com/joshkerr/goplexcli/internal/plex"
)

// Queue represents a persistent download queue
type Queue struct {
	Items       []*plex.MediaItem `json:"items"`
	LastUpdated time.Time         `json:"last_updated"`
}

// GetQueuePath returns the path to the queue file
func GetQueuePath() (string, error) {
	cacheDir, err := config.GetCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "queue.json"), nil
}

// Load reads the queue from disk
func Load() (*Queue, error) {
	queuePath, err := GetQueuePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(queuePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &Queue{Items: []*plex.MediaItem{}, LastUpdated: time.Time{}}, nil
		}
		return nil, err
	}

	var q Queue
	if err := json.Unmarshal(data, &q); err != nil {
		return nil, err
	}

	return &q, nil
}

// Save writes the queue to disk
func (q *Queue) Save() error {
	cacheDir, err := config.GetCacheDir()
	if err != nil {
		return err
	}

	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}

	queuePath, err := GetQueuePath()
	if err != nil {
		return err
	}

	q.LastUpdated = time.Now()

	data, err := json.MarshalIndent(q, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(queuePath, data, 0644)
}

// Clear removes all items from the queue and deletes the file
func (q *Queue) Clear() error {
	q.Items = []*plex.MediaItem{}
	q.LastUpdated = time.Now()

	queuePath, err := GetQueuePath()
	if err != nil {
		return err
	}

	// Remove the file if it exists
	if err := os.Remove(queuePath); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

// Add appends items to the queue, avoiding duplicates by Key
// Returns the number of items actually added (excluding duplicates)
func (q *Queue) Add(items []*plex.MediaItem) int {
	existing := make(map[string]bool)
	for _, item := range q.Items {
		existing[item.Key] = true
	}

	added := 0
	for _, item := range items {
		if !existing[item.Key] {
			q.Items = append(q.Items, item)
			existing[item.Key] = true
			added++
		}
	}
	return added
}

// Remove removes items at specified indices from the queue
func (q *Queue) Remove(indices []int) {
	if len(indices) == 0 {
		return
	}

	// Deduplicate indices
	seen := make(map[int]bool)
	var uniqueIndices []int
	for _, idx := range indices {
		if !seen[idx] {
			seen[idx] = true
			uniqueIndices = append(uniqueIndices, idx)
		}
	}

	// Sort indices in descending order to remove from end first
	for i := 0; i < len(uniqueIndices)-1; i++ {
		for j := i + 1; j < len(uniqueIndices); j++ {
			if uniqueIndices[i] < uniqueIndices[j] {
				uniqueIndices[i], uniqueIndices[j] = uniqueIndices[j], uniqueIndices[i]
			}
		}
	}

	for _, idx := range uniqueIndices {
		if idx >= 0 && idx < len(q.Items) {
			q.Items = append(q.Items[:idx], q.Items[idx+1:]...)
		}
	}
}

// Len returns the number of items in the queue
func (q *Queue) Len() int {
	return len(q.Items)
}

// IsEmpty returns true if the queue has no items
func (q *Queue) IsEmpty() bool {
	return len(q.Items) == 0
}
