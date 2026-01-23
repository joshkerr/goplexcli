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
		result := pluralizeItems(tt.count)
		if result != tt.expected {
			t.Errorf("pluralizeItems(%d) = %q, expected %q", tt.count, result, tt.expected)
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
