package plex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetWatchStatus(t *testing.T) {
	var gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"MediaContainer": map[string]any{
				"Metadata": []map[string]any{
					{"viewOffset": 0, "viewCount": 1, "lastViewedAt": 1700000000},
				},
			},
		})
	}))
	defer ts.Close()

	// itemKey is the full MediaItem.Key path; GetWatchStatus should extract
	// just the trailing ratingKey for the request URL.
	viewOffset, viewCount, lastViewedAt, err := testPlexClient(ts.URL).GetWatchStatus(context.Background(), "/library/metadata/12345")
	if err != nil {
		t.Fatalf("GetWatchStatus: %v", err)
	}
	if gotPath != "/library/metadata/12345" {
		t.Errorf("request path = %q, want /library/metadata/12345", gotPath)
	}
	if viewOffset != 0 || viewCount != 1 || lastViewedAt != 1700000000 {
		t.Errorf("got (%d, %d, %d), want (0, 1, 1700000000)", viewOffset, viewCount, lastViewedAt)
	}
}

func TestGetWatchStatusNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"MediaContainer": map[string]any{"Metadata": []map[string]any{}},
		})
	}))
	defer ts.Close()

	if _, _, _, err := testPlexClient(ts.URL).GetWatchStatus(context.Background(), "/library/metadata/1"); err == nil {
		t.Fatal("expected error when Plex returns no Metadata, got nil")
	}
}

func TestGetWatchStatusEmptyKey(t *testing.T) {
	if _, _, _, err := testPlexClient("http://unused").GetWatchStatus(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty item key, got nil")
	}
}
