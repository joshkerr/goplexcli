package plex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fastRetries shrinks the retry pause for the duration of a test.
func fastRetries(t *testing.T) {
	t.Helper()
	old := pageRetryDelay
	pageRetryDelay = time.Millisecond
	t.Cleanup(func() { pageRetryDelay = old })
}

// makeMovies generates n movie metadata entries with descending addedAt
// timestamps (newest first), mirroring a sort=addedAt:desc response.
func makeMovies(n int, newestAddedAt int64) []map[string]any {
	items := make([]map[string]any, n)
	for i := range items {
		items[i] = map[string]any{
			"key":     fmt.Sprintf("/library/metadata/%d", i),
			"title":   fmt.Sprintf("Movie %d", i),
			"addedAt": newestAddedAt - int64(i),
		}
	}
	return items
}

// writeContainerPage slices items according to the request's container
// pagination params and writes a MediaContainer JSON response.
func writeContainerPage(w http.ResponseWriter, r *http.Request, items []map[string]any) {
	start, _ := strconv.Atoi(r.URL.Query().Get("X-Plex-Container-Start"))
	size, _ := strconv.Atoi(r.URL.Query().Get("X-Plex-Container-Size"))
	if start > len(items) {
		start = len(items)
	}
	end := min(start+size, len(items))
	page := items[start:end]
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"MediaContainer": map[string]any{
			"totalSize": len(items),
			"size":      len(page),
			"Metadata":  page,
		},
	})
}

// newSectionServer serves pages of items using the X-Plex-Container-Start/Size
// protocol. If hook is non-nil it runs first and may fully handle the request
// by returning true.
func newSectionServer(items []map[string]any, hook func(w http.ResponseWriter, r *http.Request) bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hook != nil && hook(w, r) {
			return
		}
		writeContainerPage(w, r, items)
	}))
}

func testPlexClient(url string) *Client {
	return &Client{serverURL: url, serverName: "test", token: "tok"}
}

func TestGetMediaFromSectionPaginates(t *testing.T) {
	items := makeMovies(450, 1000000)
	ts := newSectionServer(items, nil)
	defer ts.Close()

	got, err := testPlexClient(ts.URL).getMediaFromSection(context.Background(), "1", "movie", 0, nil)
	if err != nil {
		t.Fatalf("getMediaFromSection: %v", err)
	}
	if len(got) != len(items) {
		t.Fatalf("got %d items, want %d", len(got), len(items))
	}
	for i, item := range got {
		if want := fmt.Sprintf("Movie %d", i); item.Title != want {
			t.Fatalf("item %d out of order: got %q, want %q", i, item.Title, want)
		}
	}
}

func TestGetMediaFromSectionShrinksPageSizeOn500(t *testing.T) {
	fastRetries(t)
	items := makeMovies(120, 1000000)
	// Emulate the Plex response-size bug: any page request above 50 items 500s.
	ts := newSectionServer(items, func(w http.ResponseWriter, r *http.Request) bool {
		size, _ := strconv.Atoi(r.URL.Query().Get("X-Plex-Container-Size"))
		if size > 50 {
			w.WriteHeader(http.StatusInternalServerError)
			return true
		}
		return false
	})
	defer ts.Close()

	got, err := testPlexClient(ts.URL).getMediaFromSection(context.Background(), "1", "movie", 0, nil)
	if err != nil {
		t.Fatalf("getMediaFromSection: %v", err)
	}
	if len(got) != len(items) {
		t.Fatalf("got %d items, want %d", len(got), len(items))
	}
}

func TestGetMediaFromSectionRetriesTransientNetworkError(t *testing.T) {
	fastRetries(t)
	items := makeMovies(10, 1000000)
	var calls atomic.Int32
	// Drop the first connection mid-request to simulate a transient network
	// failure; every later request succeeds.
	ts := newSectionServer(items, func(w http.ResponseWriter, r *http.Request) bool {
		if calls.Add(1) == 1 {
			hj, ok := w.(http.Hijacker)
			if !ok {
				panic("test server does not support hijacking")
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				panic(err)
			}
			conn.Close()
			return true
		}
		return false
	})
	defer ts.Close()

	got, err := testPlexClient(ts.URL).getMediaFromSection(context.Background(), "1", "movie", 0, nil)
	if err != nil {
		t.Fatalf("getMediaFromSection: %v", err)
	}
	if len(got) != len(items) {
		t.Fatalf("got %d items, want %d", len(got), len(items))
	}
	if calls.Load() != 2 {
		t.Fatalf("got %d requests, want 2 (one dropped connection + one retry)", calls.Load())
	}
}

func TestGetMediaFromSectionDoesNotRetryAuthFailure(t *testing.T) {
	fastRetries(t)
	var calls atomic.Int32
	ts := newSectionServer(nil, func(w http.ResponseWriter, r *http.Request) bool {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		return true
	})
	defer ts.Close()

	_, err := testPlexClient(ts.URL).getMediaFromSection(context.Background(), "1", "movie", 0, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("got %d requests, want 1 (auth errors must not be retried)", calls.Load())
	}
}

func TestGetMediaFromSectionIncrementalStopsAtThreshold(t *testing.T) {
	const newest = int64(1000000)
	items := makeMovies(500, newest) // addedAt: newest, newest-1, ..., newest-499
	var sawSort atomic.Bool
	ts := newSectionServer(items, func(w http.ResponseWriter, r *http.Request) bool {
		if strings.Contains(r.URL.RawQuery, "sort=addedAt:desc") {
			sawSort.Store(true)
		}
		return false
	})
	defer ts.Close()

	// Threshold sits inside the first page: items 0..49 have addedAt >= since
	// (boundary item included), everything older must be skipped.
	since := newest - 49
	got, err := testPlexClient(ts.URL).getMediaFromSection(context.Background(), "1", "movie", since, nil)
	if err != nil {
		t.Fatalf("getMediaFromSection: %v", err)
	}
	if len(got) != 50 {
		t.Fatalf("got %d items, want 50", len(got))
	}
	for _, item := range got {
		if item.AddedAt < since {
			t.Fatalf("item %q has addedAt %d older than threshold %d", item.Title, item.AddedAt, since)
		}
	}
	if !sawSort.Load() {
		t.Fatal("incremental fetch did not request sort=addedAt:desc")
	}
}

func TestGetMediaFetchesSectionsInParallelPreservingOrder(t *testing.T) {
	// Three movie libraries whose items carry their library key in the title;
	// results must come back grouped in library order even though sections are
	// fetched concurrently.
	libs := []string{"1", "2", "3"}
	libItems := map[string][]map[string]any{}
	for li, key := range libs {
		items := make([]map[string]any, 250)
		for i := range items {
			items[i] = map[string]any{
				"key":     fmt.Sprintf("/library/metadata/%s-%d", key, i),
				"title":   fmt.Sprintf("Lib%s Movie %d", key, i),
				"addedAt": int64(1000000 - li*1000 - i),
			}
		}
		libItems[key] = items
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/library/sections" {
			dirs := make([]map[string]any, len(libs))
			for i, key := range libs {
				dirs[i] = map[string]any{"key": key, "title": "Library " + key, "type": "movie"}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"MediaContainer": map[string]any{"Directory": dirs},
			})
			return
		}
		for _, key := range libs {
			if r.URL.Path == "/library/sections/"+key+"/all" {
				writeContainerPage(w, r, libItems[key])
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	got, err := testPlexClient(ts.URL).getMedia(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("getMedia: %v", err)
	}
	if want := 3 * 250; len(got) != want {
		t.Fatalf("got %d items, want %d", len(got), want)
	}
	idx := 0
	for _, key := range libs {
		for i := 0; i < 250; i++ {
			if want := fmt.Sprintf("Lib%s Movie %d", key, i); got[idx].Title != want {
				t.Fatalf("item %d: got %q, want %q (results must stay in library order)", idx, got[idx].Title, want)
			}
			idx++
		}
	}
}
