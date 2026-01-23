package main

import (
	"testing"

	"github.com/joshkerr/goplexcli/internal/plex"
)

func TestAddToQueue(t *testing.T) {
	tests := []struct {
		name          string
		existingQueue []*plex.MediaItem
		newItems      []*plex.MediaItem
		expectedLen   int
		expectedKeys  []string
	}{
		{
			name:          "add to empty queue",
			existingQueue: nil,
			newItems: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1"},
				{Key: "/library/2", Title: "Movie 2"},
			},
			expectedLen:  2,
			expectedKeys: []string{"/library/1", "/library/2"},
		},
		{
			name: "add to existing queue",
			existingQueue: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1"},
			},
			newItems: []*plex.MediaItem{
				{Key: "/library/2", Title: "Movie 2"},
			},
			expectedLen:  2,
			expectedKeys: []string{"/library/1", "/library/2"},
		},
		{
			name: "avoid duplicates",
			existingQueue: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1"},
			},
			newItems: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1 Duplicate"},
				{Key: "/library/2", Title: "Movie 2"},
			},
			expectedLen:  2,
			expectedKeys: []string{"/library/1", "/library/2"},
		},
		{
			name: "add empty items",
			existingQueue: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1"},
			},
			newItems:     []*plex.MediaItem{},
			expectedLen:  1,
			expectedKeys: []string{"/library/1"},
		},
		{
			name: "all duplicates",
			existingQueue: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1"},
				{Key: "/library/2", Title: "Movie 2"},
			},
			newItems: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1"},
				{Key: "/library/2", Title: "Movie 2"},
			},
			expectedLen:  2,
			expectedKeys: []string{"/library/1", "/library/2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queue := make([]*plex.MediaItem, len(tt.existingQueue))
			copy(queue, tt.existingQueue)

			addToQueue(&queue, tt.newItems)

			if len(queue) != tt.expectedLen {
				t.Errorf("expected queue length %d, got %d", tt.expectedLen, len(queue))
			}

			for i, expectedKey := range tt.expectedKeys {
				if i >= len(queue) {
					t.Errorf("queue shorter than expected, missing key at index %d", i)
					continue
				}
				if queue[i].Key != expectedKey {
					t.Errorf("expected key %s at index %d, got %s", expectedKey, i, queue[i].Key)
				}
			}
		})
	}
}

func TestRemoveFromQueue(t *testing.T) {
	tests := []struct {
		name          string
		queue         []*plex.MediaItem
		indices       []int
		expectedLen   int
		expectedKeys  []string
	}{
		{
			name: "remove single item",
			queue: []*plex.MediaItem{
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
			queue: []*plex.MediaItem{
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
			name: "remove last item",
			queue: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1"},
				{Key: "/library/2", Title: "Movie 2"},
			},
			indices:      []int{1},
			expectedLen:  1,
			expectedKeys: []string{"/library/1"},
		},
		{
			name: "remove first item",
			queue: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1"},
				{Key: "/library/2", Title: "Movie 2"},
			},
			indices:      []int{0},
			expectedLen:  1,
			expectedKeys: []string{"/library/2"},
		},
		{
			name: "remove all items",
			queue: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1"},
				{Key: "/library/2", Title: "Movie 2"},
			},
			indices:      []int{0, 1},
			expectedLen:  0,
			expectedKeys: []string{},
		},
		{
			name: "remove with empty indices",
			queue: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1"},
			},
			indices:      []int{},
			expectedLen:  1,
			expectedKeys: []string{"/library/1"},
		},
		{
			name: "remove with out of bounds index",
			queue: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1"},
				{Key: "/library/2", Title: "Movie 2"},
			},
			indices:      []int{5, 10},
			expectedLen:  2,
			expectedKeys: []string{"/library/1", "/library/2"},
		},
		{
			name: "remove with negative index",
			queue: []*plex.MediaItem{
				{Key: "/library/1", Title: "Movie 1"},
			},
			indices:      []int{-1},
			expectedLen:  1,
			expectedKeys: []string{"/library/1"},
		},
		{
			name: "remove from empty queue",
			queue: []*plex.MediaItem{},
			indices:      []int{0},
			expectedLen:  0,
			expectedKeys: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queue := make([]*plex.MediaItem, len(tt.queue))
			copy(queue, tt.queue)

			removeFromQueue(&queue, tt.indices)

			if len(queue) != tt.expectedLen {
				t.Errorf("expected queue length %d, got %d", tt.expectedLen, len(queue))
			}

			for i, expectedKey := range tt.expectedKeys {
				if i >= len(queue) {
					t.Errorf("queue shorter than expected, missing key at index %d", i)
					continue
				}
				if queue[i].Key != expectedKey {
					t.Errorf("expected key %s at index %d, got %s", expectedKey, i, queue[i].Key)
				}
			}
		})
	}
}

func TestRemoveFromQueue_UnsortedIndices(t *testing.T) {
	// Test that indices are properly handled regardless of order
	queue := []*plex.MediaItem{
		{Key: "/library/1", Title: "Movie 1"},
		{Key: "/library/2", Title: "Movie 2"},
		{Key: "/library/3", Title: "Movie 3"},
		{Key: "/library/4", Title: "Movie 4"},
		{Key: "/library/5", Title: "Movie 5"},
	}

	// Remove indices 1, 3 (in various orders should give same result)
	testCases := [][]int{
		{1, 3},
		{3, 1},
	}

	for _, indices := range testCases {
		q := make([]*plex.MediaItem, len(queue))
		copy(q, queue)

		removeFromQueue(&q, indices)

		if len(q) != 3 {
			t.Errorf("expected 3 items, got %d for indices %v", len(q), indices)
		}

		expectedKeys := []string{"/library/1", "/library/3", "/library/5"}
		for i, key := range expectedKeys {
			if q[i].Key != key {
				t.Errorf("expected key %s at %d, got %s for indices %v", key, i, q[i].Key, indices)
			}
		}
	}
}

func TestAddToQueue_PreservesOrder(t *testing.T) {
	queue := []*plex.MediaItem{
		{Key: "/library/1", Title: "First"},
	}

	newItems := []*plex.MediaItem{
		{Key: "/library/2", Title: "Second"},
		{Key: "/library/3", Title: "Third"},
	}

	addToQueue(&queue, newItems)

	if len(queue) != 3 {
		t.Fatalf("expected 3 items, got %d", len(queue))
	}

	expectedOrder := []string{"First", "Second", "Third"}
	for i, expected := range expectedOrder {
		if queue[i].Title != expected {
			t.Errorf("expected title %s at index %d, got %s", expected, i, queue[i].Title)
		}
	}
}
