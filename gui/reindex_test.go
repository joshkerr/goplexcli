package main

import (
	"testing"

	"github.com/joshkerr/goplexcli/internal/plex"
)

func TestMergeMediaDedupesByMachineID(t *testing.T) {
	// Regression test: a server re-indexed under a different ServerName (e.g.
	// after a rename, or a legacy cache indexed before multi-server naming
	// existed) must not be treated as a second library when it shares the
	// same permanent Plex machineIdentifier.
	existing := []plex.MediaItem{
		{ServerName: "old-label", ServerMachineID: "machine-1", Key: "/library/metadata/1", Title: "Movie A"},
	}
	fetched := []plex.MediaItem{
		{ServerName: "new-label", ServerMachineID: "machine-1", Key: "/library/metadata/1", Title: "Movie A (refreshed)"},
	}

	merged, added := mergeMedia(existing, fetched)

	if added != 0 {
		t.Fatalf("added = %d, want 0 (same machine, renamed server should update in place)", added)
	}
	if len(merged) != 1 {
		t.Fatalf("len(merged) = %d, want 1", len(merged))
	}
	if merged[0].Title != "Movie A (refreshed)" {
		t.Errorf("merged[0].Title = %q, want the refreshed title (existing entry should be replaced)", merged[0].Title)
	}
}

func TestMergeMediaFallsBackToServerName(t *testing.T) {
	// Cache entries written before ServerMachineID existed have it empty;
	// merging must still dedupe those by ServerName so upgrading doesn't
	// itself cause a one-time duplication.
	existing := []plex.MediaItem{
		{ServerName: "cypher-joshkerr", ServerMachineID: "", Key: "/library/metadata/1", Title: "Movie A"},
	}
	fetched := []plex.MediaItem{
		{ServerName: "cypher-joshkerr", ServerMachineID: "machine-1", Key: "/library/metadata/1", Title: "Movie A"},
	}

	merged, added := mergeMedia(existing, fetched)

	if added != 0 {
		t.Fatalf("added = %d, want 0", added)
	}
	if len(merged) != 1 {
		t.Fatalf("len(merged) = %d, want 1", len(merged))
	}
}

func TestMergeMediaDistinctServersNotMerged(t *testing.T) {
	existing := []plex.MediaItem{
		{ServerName: "server-a", ServerMachineID: "machine-a", Key: "/library/metadata/1", Title: "Movie A"},
	}
	fetched := []plex.MediaItem{
		{ServerName: "server-b", ServerMachineID: "machine-b", Key: "/library/metadata/1", Title: "Movie A"},
	}

	merged, added := mergeMedia(existing, fetched)

	if added != 1 {
		t.Fatalf("added = %d, want 1 (genuinely different servers must not be merged)", added)
	}
	if len(merged) != 2 {
		t.Fatalf("len(merged) = %d, want 2", len(merged))
	}
}
