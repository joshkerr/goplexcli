package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	"github.com/joshkerr/goplexcli/internal/config"
	"github.com/joshkerr/goplexcli/internal/plex"
)

const (
	// lockTimeout is the maximum time to wait for acquiring a lock
	lockTimeout = 30 * time.Second
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

// GetLockPath returns the path to the queue lock file
func GetLockPath() (string, error) {
	cacheDir, err := config.GetCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "queue.lock"), nil
}

// withExclusiveLock executes a function while holding an exclusive lock on the queue
func withExclusiveLock(fn func() error) error {
	lockPath, err := GetLockPath()
	if err != nil {
		return fmt.Errorf("failed to get lock path: %w", err)
	}

	// Ensure the cache directory exists
	cacheDir, err := config.GetCacheDir()
	if err != nil {
		return fmt.Errorf("failed to get cache dir: %w", err)
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache dir: %w", err)
	}

	fileLock := flock.New(lockPath)
	ctx, cancel := context.WithTimeout(context.Background(), lockTimeout)
	defer cancel()

	locked, err := fileLock.TryLockContext(ctx, 100*time.Millisecond)
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	if !locked {
		return fmt.Errorf("could not acquire queue lock within %v (another instance may be using the queue)", lockTimeout)
	}
	defer func() {
		_ = fileLock.Unlock() // Error intentionally ignored - lock released on process exit regardless
	}()

	return fn()
}

// withSharedLock executes a function while holding a shared (read) lock on the queue
func withSharedLock(fn func() error) error {
	lockPath, err := GetLockPath()
	if err != nil {
		return fmt.Errorf("failed to get lock path: %w", err)
	}

	// Ensure the cache directory exists
	cacheDir, err := config.GetCacheDir()
	if err != nil {
		return fmt.Errorf("failed to get cache dir: %w", err)
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache dir: %w", err)
	}

	fileLock := flock.New(lockPath)
	ctx, cancel := context.WithTimeout(context.Background(), lockTimeout)
	defer cancel()

	locked, err := fileLock.TryRLockContext(ctx, 100*time.Millisecond)
	if err != nil {
		return fmt.Errorf("failed to acquire shared lock: %w", err)
	}
	if !locked {
		return fmt.Errorf("could not acquire queue lock within %v (another instance may be using the queue)", lockTimeout)
	}
	defer func() {
		_ = fileLock.Unlock() // Error intentionally ignored - lock released on process exit regardless
	}()

	return fn()
}

// Load reads the queue from disk with a shared lock for concurrent read safety
func Load() (*Queue, error) {
	var q *Queue
	var loadErr error

	err := withSharedLock(func() error {
		queuePath, err := GetQueuePath()
		if err != nil {
			loadErr = err
			return nil
		}

		data, err := os.ReadFile(queuePath)
		if err != nil {
			if os.IsNotExist(err) {
				q = &Queue{Items: []*plex.MediaItem{}, LastUpdated: time.Time{}}
				return nil
			}
			loadErr = err
			return nil
		}

		var loaded Queue
		if err := json.Unmarshal(data, &loaded); err != nil {
			loadErr = err
			return nil
		}

		q = &loaded
		return nil
	})

	if err != nil {
		return nil, err
	}
	if loadErr != nil {
		return nil, loadErr
	}

	return q, nil
}

// Save writes the queue to disk with exclusive lock and atomic write for concurrent safety
func (q *Queue) Save() error {
	return withExclusiveLock(func() error {
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

		// Atomic write: write to temp file then rename
		tempPath := queuePath + ".tmp"
		if err := os.WriteFile(tempPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write temp file: %w", err)
		}

		if err := os.Rename(tempPath, queuePath); err != nil {
			// Clean up temp file on rename failure
			os.Remove(tempPath)
			return fmt.Errorf("failed to rename temp file: %w", err)
		}

		return nil
	})
}

// Clear removes all items from the queue and deletes the file with exclusive lock
func (q *Queue) Clear() error {
	return withExclusiveLock(func() error {
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

		// Also clean up any stale temp file
		os.Remove(queuePath + ".tmp")

		return nil
	})
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

// RemoveByKeys removes items with matching keys from the persisted queue.
// This method reloads from disk, removes the specified items, and saves back.
// This ensures items added by other instances while processing are preserved.
// The in-memory queue (q) is also updated to reflect the new state.
func (q *Queue) RemoveByKeys(keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	return withExclusiveLock(func() error {
		queuePath, err := GetQueuePath()
		if err != nil {
			return err
		}

		// Reload queue from disk to get current state (including items added by other instances)
		data, err := os.ReadFile(queuePath)
		if err != nil {
			if os.IsNotExist(err) {
				// Queue file doesn't exist, nothing to remove
				q.Items = []*plex.MediaItem{}
				q.LastUpdated = time.Now()
				return nil
			}
			return err
		}

		var diskQueue Queue
		if err := json.Unmarshal(data, &diskQueue); err != nil {
			return err
		}

		// Build set of keys to remove
		keysToRemove := make(map[string]bool)
		for _, key := range keys {
			keysToRemove[key] = true
		}

		// Filter out items with matching keys
		var remaining []*plex.MediaItem
		for _, item := range diskQueue.Items {
			if !keysToRemove[item.Key] {
				remaining = append(remaining, item)
			}
		}

		// Update in-memory queue
		q.Items = remaining
		q.LastUpdated = time.Now()

		// If queue is empty, delete the file
		if len(remaining) == 0 {
			if err := os.Remove(queuePath); err != nil && !os.IsNotExist(err) {
				return err
			}
			os.Remove(queuePath + ".tmp")
			return nil
		}

		// Save remaining items back to disk with atomic write
		data, err = json.MarshalIndent(q, "", "  ")
		if err != nil {
			return err
		}

		tempPath := queuePath + ".tmp"
		if err := os.WriteFile(tempPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write temp file: %w", err)
		}

		if err := os.Rename(tempPath, queuePath); err != nil {
			os.Remove(tempPath)
			return fmt.Errorf("failed to rename temp file: %w", err)
		}

		return nil
	})
}
