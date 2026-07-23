package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func useTempConfigDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
}

func TestPosterCacheResizesHidesTokenAndReusesDisk(t *testing.T) {
	useTempConfigDir(t)
	var requests atomic.Int32
	plexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/photo/:/transcode" {
			t.Errorf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("width") != "320" || q.Get("height") != "480" {
			t.Errorf("dimensions = %sx%s", q.Get("width"), q.Get("height"))
		}
		if q.Get("url") != "/library/metadata/1/thumb/2" {
			t.Errorf("url = %q", q.Get("url"))
		}
		if q.Get("X-Plex-Token") != "secret-token" {
			t.Errorf("token missing from Plex request")
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("fake-jpeg"))
	}))
	defer plexServer.Close()

	cache := newPosterCache(plexServer.Client())
	path := cache.register(posterSource{
		ServerURL: plexServer.URL,
		ThumbPath: "/library/metadata/1/thumb/2",
		Token:     "secret-token",
		Width:     320,
		Height:    480,
	})
	if strings.Contains(path, "secret-token") || !strings.HasPrefix(path, "/posters/") {
		t.Fatalf("unsafe poster path %q", path)
	}

	for range 2 {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		res := httptest.NewRecorder()
		cache.serve(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %q", res.Code, res.Body.String())
		}
		if got := res.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
			t.Errorf("Cache-Control = %q", got)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("Plex requests = %d, want 1", got)
	}
}

func TestPosterCacheDeduplicatesConcurrentMisses(t *testing.T) {
	useTempConfigDir(t)
	var requests atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{})
	plexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("image"))
	}))
	defer plexServer.Close()

	cache := newPosterCache(plexServer.Client())
	path := cache.register(posterSource{ServerURL: plexServer.URL, ThumbPath: "/thumb", Width: 320, Height: 480})
	done := make(chan int, 2)
	for range 2 {
		go func() {
			res := httptest.NewRecorder()
			cache.serve(res, httptest.NewRequest(http.MethodGet, path, nil))
			done <- res.Code
		}()
	}
	<-started
	close(release)
	for range 2 {
		if code := <-done; code != http.StatusOK {
			t.Fatalf("status = %d", code)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("Plex requests = %d, want 1", got)
	}
}

func TestPosterCacheWarmAllCrawlsAndSkipsCached(t *testing.T) {
	useTempConfigDir(t)
	var requests atomic.Int32
	plexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("image"))
	}))
	defer plexServer.Close()

	cache := newPosterCache(plexServer.Client())
	urls := make([]string, 0, 10)
	for i := range 10 {
		urls = append(urls, cache.register(posterSource{
			ServerURL: plexServer.URL,
			ThumbPath: fmt.Sprintf("/thumb/%d", i),
			Width:     320,
			Height:    480,
		}))
	}

	cache.warmAll(urls)
	if got := requests.Load(); got != 10 {
		t.Fatalf("Plex requests after first crawl = %d, want 10", got)
	}
	for _, u := range urls {
		path, err := cache.cachedPath(posterIDFromURL(u))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("poster %s not cached: %v", u, err)
		}
	}

	// A re-crawl over the same set is all stats, no re-fetches.
	cache.warmAll(urls)
	if got := requests.Load(); got != 10 {
		t.Fatalf("Plex requests after re-crawl = %d, want 10", got)
	}
}

func TestPosterCacheRejectsNonImages(t *testing.T) {
	useTempConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("not an image"))
	}))
	defer server.Close()

	cache := newPosterCache(server.Client())
	path := cache.register(posterSource{ServerURL: server.URL, ThumbPath: "/thumb", Width: 320, Height: 480})
	res := httptest.NewRecorder()
	cache.serve(res, httptest.NewRequest(http.MethodGet, path, nil))
	if res.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusBadGateway)
	}

	dir, err := cache.cachedPath(strings.TrimPrefix(path, "/posters/"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Clean(dir)); !os.IsNotExist(err) {
		t.Fatalf("invalid response was cached: %v", err)
	}
}

func TestPosterCacheLoopbackServer(t *testing.T) {
	useTempConfigDir(t)
	plexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("image"))
	}))
	defer plexServer.Close()

	cache := newPosterCache(plexServer.Client())
	if err := cache.start(); err != nil {
		t.Fatal(err)
	}
	defer cache.close(context.Background())
	posterURL := cache.register(posterSource{ServerURL: plexServer.URL, ThumbPath: "/thumb", Width: 320, Height: 480})
	if !strings.HasPrefix(posterURL, "http://127.0.0.1:") {
		t.Fatalf("poster URL is not loopback: %q", posterURL)
	}
	resp, err := http.Get(posterURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}
