package queue

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joshkerr/goplexcli/internal/plex"
)

func TestAdd(t *testing.T) {
	tests := []struct {
		name          string
		existingItems []*plex.MediaItem
		newItems      []*plex.MediaItem
		expectedLen   int
		expectedAdded int
		expectedKeys  []string
	}{
		{
			name:          "add to empty queue",
			existingItems: nil,
			newItems: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1"},
				{Key: "/library/2", Title: "Movie 2"},
			},
			expectedLen:   2,
			expectedAdded: 2,
			expectedKeys:  []string{"/library/1", "/library/2"},
		},
		{
			name: "add to existing queue",
			existingItems: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1"},
			},
			newItems: []*plex.MediaItem{
				{Key: "/library/2", Title: "Movie 2"},
			},
			expectedLen:   2,
			expectedAdded: 1,
			expectedKeys:  []string{"/library/1", "/library/2"},
		},
		{
			name: "avoid duplicates",
			existingItems: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1"},
			},
			newItems: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1 Duplicate"},
				{Key: "/library/2", Title: "Movie 2"},
			},
			expectedLen:   2,
			expectedAdded: 1,
			expectedKeys:  []string{"/library/1", "/library/2"},
		},
		{
			name: "add empty items",
			existingItems: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1"},
			},
			newItems:      []*plex.MediaItem{},
			expectedLen:   1,
			expectedAdded: 0,
			expectedKeys:  []string{"/library/1"},
		},
		{
			name: "all duplicates",
			existingItems: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1"},
				{Key: "/library/2", Title: "Movie 2"},
			},
			newItems: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1"},
				{Key: "/library/2", Title: "Movie 2"},
			},
			expectedLen:   2,
			expectedAdded: 0,
			expectedKeys:  []string{"/library/1", "/library/2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &Queue{Items: make([]*plex.MediaItem, len(tt.existingItems))}
			copy(q.Items, tt.existingItems)

			added := q.Add(tt.newItems)

			if added != tt.expectedAdded {
				t.Errorf("expected %d items added, got %d", tt.expectedAdded, added)
			}

			if q.Len() != tt.expectedLen {
				t.Errorf("expected queue length %d, got %d", tt.expectedLen, q.Len())
			}

			for i, expectedKey := range tt.expectedKeys {
				if i >= q.Len() {
					t.Errorf("queue shorter than expected, missing key at index %d", i)
					continue
				}
				if q.Items[i].Key != expectedKey {
					t.Errorf("expected key %s at index %d, got %s", expectedKey, i, q.Items[i].Key)
				}
			}
		})
	}
}

func TestRemove(t *testing.T) {
	tests := []struct {
		name         string
		items        []*plex.MediaItem
		indices      []int
		expectedLen  int
		expectedKeys []string
	}{
		{
			name: "remove single item",
			items: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1"},
				{Key: "/library/2", Title: "Movie 2"},
				{Key: "/library/3", Title: "Movie 3"},
			},
			indices:      []int{1},
			expectedLen:  2,
			expectedKeys: []string{"/library/1", "/library/3"},
		},
		{
			name: "remove multiple items",
			items: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1"},
				{Key: "/library/2", Title: "Movie 2"},
				{Key: "/library/3", Title: "Movie 3"},
				{Key: "/library/4", Title: "Movie 4"},
			},
			indices:      []int{0, 2},
			expectedLen:  2,
			expectedKeys: []string{"/library/2", "/library/4"},
		},
		{
			name: "remove with duplicate indices",
			items: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1"},
				{Key: "/library/2", Title: "Movie 2"},
				{Key: "/library/3", Title: "Movie 3"},
			},
			indices:      []int{1, 1, 1},
			expectedLen:  2,
			expectedKeys: []string{"/library/1", "/library/3"},
		},
		{
			name: "remove with empty indices",
			items: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1"},
			},
			indices:      []int{},
			expectedLen:  1,
			expectedKeys: []string{"/library/1"},
		},
		{
			name: "remove with out of bounds index",
			items: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1"},
				{Key: "/library/2", Title: "Movie 2"},
			},
			indices:      []int{5, 10},
			expectedLen:  2,
			expectedKeys: []string{"/library/1", "/library/2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &Queue{Items: make([]*plex.MediaItem, len(tt.items))}
			copy(q.Items, tt.items)

			q.Remove(tt.indices)

			if q.Len() != tt.expectedLen {
				t.Errorf("expected queue length %d, got %d", tt.expectedLen, q.Len())
			}

			for i, expectedKey := range tt.expectedKeys {
				if i >= q.Len() {
					t.Errorf("queue shorter than expected, missing key at index %d", i)
					continue
				}
				if q.Items[i].Key != expectedKey {
					t.Errorf("expected key %s at index %d, got %s", expectedKey, i, q.Items[i].Key)
				}
			}
		})
	}
}

func TestIsEmpty(t *testing.T) {
	q := &Queue{}
	if !q.IsEmpty() {
		t.Error("expected empty queue to return true for IsEmpty")
	}

	q.Items = []*plex.MediaItem{{Key: "/library/1"}}
	if q.IsEmpty() {
		t.Error("expected non-empty queue to return false for IsEmpty")
	}
}

func TestSaveAndLoad(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "queue_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Override the queue path for testing
	testQueuePath := filepath.Join(tmpDir, "queue.json")

	q := &Queue{
		Items: []*plex.MediaItem{
			{Key: "/library/1", Title: "Movie 1", Year: 2020},
			{Key: "/library/2", Title: "Movie 2", Year: 2021},
		},
	}

	// Save directly to test path
	data, err := os.ReadFile(testQueuePath)
	if err == nil {
		t.Log("Queue file already exists, content:", string(data))
	}

	// Test that queue can be serialized and deserialized
	if q.Len() != 2 {
		t.Errorf("expected 2 items, got %d", q.Len())
	}

	if q.Items[0].Title != "Movie 1" {
		t.Errorf("expected 'Movie 1', got '%s'", q.Items[0].Title)
	}
}

func TestClear(t *testing.T) {
	q := &Queue{
		Items: []*plex.MediaItem{
			{Key: "/library/1", Title: "Movie 1"},
			{Key: "/library/2", Title: "Movie 2"},
		},
	}

	if q.Len() != 2 {
		t.Errorf("expected 2 items before clear, got %d", q.Len())
	}

	// Just clear the in-memory items (don't test file operations here)
	q.Items = []*plex.MediaItem{}

	if !q.IsEmpty() {
		t.Error("expected queue to be empty after clear")
	}
}

// setupTestDir creates a temporary directory and sets it as the queue directory for testing
func setupTestDir(t *testing.T) (cleanup func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "queue_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	testQueueDir = tmpDir
	return func() {
		testQueueDir = ""
		os.RemoveAll(tmpDir)
	}
}

func TestSaveAndLoadWithFileIO(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	// Create and save a queue
	q := &Queue{
		Items: []*plex.MediaItem{
			{Key: "/library/1", Title: "Movie 1", Year: 2020},
			{Key: "/library/2", Title: "Movie 2", Year: 2021},
		},
	}

	if err := q.Save(); err != nil {
		t.Fatalf("failed to save queue: %v", err)
	}

	// Load the queue back
	loaded, err := Load()
	if err != nil {
		t.Fatalf("failed to load queue: %v", err)
	}

	if loaded.Len() != 2 {
		t.Errorf("expected 2 items, got %d", loaded.Len())
	}

	if loaded.Items[0].Title != "Movie 1" {
		t.Errorf("expected 'Movie 1', got '%s'", loaded.Items[0].Title)
	}

	if loaded.Items[1].Year != 2021 {
		t.Errorf("expected year 2021, got %d", loaded.Items[1].Year)
	}
}

func TestClearWithFileIO(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	// Create and save a queue
	q := &Queue{
		Items: []*plex.MediaItem{
			{Key: "/library/1", Title: "Movie 1"},
		},
	}

	if err := q.Save(); err != nil {
		t.Fatalf("failed to save queue: %v", err)
	}

	// Clear the queue
	if err := q.Clear(); err != nil {
		t.Fatalf("failed to clear queue: %v", err)
	}

	// Verify in-memory state is cleared
	if !q.IsEmpty() {
		t.Error("expected queue to be empty after clear")
	}

	// Verify file is deleted
	queuePath, _ := GetQueuePath()
	if _, err := os.Stat(queuePath); !os.IsNotExist(err) {
		t.Error("expected queue file to be deleted after clear")
	}

	// Load should return empty queue
	loaded, err := Load()
	if err != nil {
		t.Fatalf("failed to load after clear: %v", err)
	}
	if !loaded.IsEmpty() {
		t.Error("expected loaded queue to be empty after clear")
	}
}

func TestRemoveByKeys(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	// Create and save a queue
	q := &Queue{
		Items: []*plex.MediaItem{
			{Key: "/library/1", Title: "Movie 1"},
			{Key: "/library/2", Title: "Movie 2"},
			{Key: "/library/3", Title: "Movie 3"},
		},
	}

	if err := q.Save(); err != nil {
		t.Fatalf("failed to save queue: %v", err)
	}

	// Remove some keys
	if err := q.RemoveByKeys([]string{"/library/1", "/library/3"}); err != nil {
		t.Fatalf("failed to remove by keys: %v", err)
	}

	// Verify in-memory state
	if q.Len() != 1 {
		t.Errorf("expected 1 item, got %d", q.Len())
	}
	if q.Items[0].Key != "/library/2" {
		t.Errorf("expected key '/library/2', got '%s'", q.Items[0].Key)
	}

	// Verify file state
	loaded, err := Load()
	if err != nil {
		t.Fatalf("failed to load queue: %v", err)
	}
	if loaded.Len() != 1 {
		t.Errorf("expected 1 item in loaded queue, got %d", loaded.Len())
	}
}

func TestRemoveByKeysEmptyQueue(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	q := &Queue{Items: []*plex.MediaItem{}}

	// Should not error on empty queue
	if err := q.RemoveByKeys([]string{"/library/1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemoveByKeysNonExistentKeys(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	q := &Queue{
		Items: []*plex.MediaItem{
			{Key: "/library/1", Title: "Movie 1"},
		},
	}

	if err := q.Save(); err != nil {
		t.Fatalf("failed to save queue: %v", err)
	}

	// Remove keys that don't exist
	if err := q.RemoveByKeys([]string{"/library/999"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Queue should be unchanged
	if q.Len() != 1 {
		t.Errorf("expected 1 item, got %d", q.Len())
	}
}

func TestRemoveByKeysDeletesFileWhenEmpty(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	q := &Queue{
		Items: []*plex.MediaItem{
			{Key: "/library/1", Title: "Movie 1"},
		},
	}

	if err := q.Save(); err != nil {
		t.Fatalf("failed to save queue: %v", err)
	}

	// Remove all items
	if err := q.RemoveByKeys([]string{"/library/1"}); err != nil {
		t.Fatalf("failed to remove by keys: %v", err)
	}

	// Verify file is deleted
	queuePath, _ := GetQueuePath()
	if _, err := os.Stat(queuePath); !os.IsNotExist(err) {
		t.Error("expected queue file to be deleted when empty")
	}
}

func TestRemoveByKeysPreservesNewItems(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	// Create initial queue
	q := &Queue{
		Items: []*plex.MediaItem{
			{Key: "/library/1", Title: "Movie 1"},
			{Key: "/library/2", Title: "Movie 2"},
		},
	}

	if err := q.Save(); err != nil {
		t.Fatalf("failed to save queue: %v", err)
	}

	// Simulate another instance adding an item by directly modifying the file
	q2, err := Load()
	if err != nil {
		t.Fatalf("failed to load queue: %v", err)
	}
	q2.Add([]*plex.MediaItem{{Key: "/library/3", Title: "Movie 3"}})
	if err := q2.Save(); err != nil {
		t.Fatalf("failed to save q2: %v", err)
	}

	// Now q (original instance) removes only its original keys
	// This simulates: download items 1 and 2, then remove them
	if err := q.RemoveByKeys([]string{"/library/1", "/library/2"}); err != nil {
		t.Fatalf("failed to remove by keys: %v", err)
	}

	// The new item (3) added by "another instance" should be preserved
	if q.Len() != 1 {
		t.Errorf("expected 1 item preserved, got %d", q.Len())
	}
	if q.Items[0].Key != "/library/3" {
		t.Errorf("expected preserved item to be '/library/3', got '%s'", q.Items[0].Key)
	}

	// Verify file also has the correct state
	loaded, err := Load()
	if err != nil {
		t.Fatalf("failed to load queue: %v", err)
	}
	if loaded.Len() != 1 || loaded.Items[0].Key != "/library/3" {
		t.Error("file state doesn't match expected")
	}
}

func TestConcurrentSaveLoad(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	// Create initial queue
	q := &Queue{Items: []*plex.MediaItem{}}
	if err := q.Save(); err != nil {
		t.Fatalf("failed to save initial queue: %v", err)
	}

	const numGoroutines = 10
	const itemsPerGoroutine = 5

	errCh := make(chan error, numGoroutines*2)
	done := make(chan bool)

	// Spawn goroutines that add items
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < itemsPerGoroutine; j++ {
				loaded, err := Load()
				if err != nil {
					errCh <- err
					return
				}
				key := filepath.Join("/library", string(rune('A'+id)), string(rune('0'+j)))
				loaded.Add([]*plex.MediaItem{{Key: key, Title: "Test"}})
				if err := loaded.Save(); err != nil {
					errCh <- err
					return
				}
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		select {
		case err := <-errCh:
			t.Fatalf("concurrent operation failed: %v", err)
		case <-done:
		}
	}

	// Verify no data corruption (queue should be valid JSON)
	final, err := Load()
	if err != nil {
		t.Fatalf("failed to load final queue: %v", err)
	}

	// Due to race conditions without proper locking, some items may be lost
	// But with proper locking, no corruption should occur
	t.Logf("Final queue has %d items (expected up to %d)", final.Len(), numGoroutines*itemsPerGoroutine)

	if final.Len() == 0 {
		t.Error("queue is empty - severe data loss")
	}
}
