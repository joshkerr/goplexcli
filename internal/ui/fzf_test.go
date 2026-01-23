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
