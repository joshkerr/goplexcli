package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joshkerr/goplexcli/internal/config"
	"golang.org/x/sync/singleflight"
)

const (
	posterCacheMaxBytes = int64(256 << 20)
	posterMaxImageBytes = int64(12 << 20)
	// warmConcurrency bounds how many posters are pre-fetched from Plex in
	// parallel. Warming runs on the Go client's own connection pool, independent
	// of the browser's ~6-connections-per-origin cap, so a comfortably higher
	// value lets a jump-scrolled window fill in parallel instead of trickling in
	// six at a time — while staying gentle enough not to flood Plex's transcoder.
	warmConcurrency = 12
)

type posterSource struct {
	ServerURL string
	ThumbPath string
	Token     string
	Width     int
	Height    int
}

type posterCache struct {
	client  *http.Client
	mu      sync.RWMutex
	sources map[string]posterSource
	group   singleflight.Group
	warmSem chan struct{}
	server  *http.Server
	baseURL string
}

func newPosterCache(client *http.Client) *posterCache {
	if client == nil || client == http.DefaultClient {
		// A dedicated transport with a larger idle-connection pool so the warm
		// worker pool reuses connections to Plex instead of re-handshaking on
		// every parallel transcode request.
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.MaxIdleConns = 100
		transport.MaxIdleConnsPerHost = warmConcurrency + 4
		client = &http.Client{Timeout: 20 * time.Second, Transport: transport}
	}
	return &posterCache{
		client:  client,
		sources: make(map[string]posterSource),
		warmSem: make(chan struct{}, warmConcurrency),
	}
}

func (p *posterCache) register(source posterSource) string {
	identity := fmt.Sprintf("%s\x00%s\x00%d\x00%d", source.ServerURL, source.ThumbPath, source.Width, source.Height)
	sum := sha256.Sum256([]byte(identity))
	id := hex.EncodeToString(sum[:])
	p.mu.Lock()
	p.sources[id] = source
	baseURL := p.baseURL
	p.mu.Unlock()
	if baseURL == "" {
		return "/posters/" + id
	}
	return baseURL + "/posters/" + id
}

// start launches a loopback-only image server. Keeping posters outside the
// Wails asset middleware is important: Wails v2's external development asset
// handler is incompatible with custom middleware when used with Vite 5.
func (p *posterCache) start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.server != nil {
		return nil
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/posters/", p.serve)
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	p.server = server
	p.baseURL = "http://" + listener.Addr().String()
	go func() {
		_ = server.Serve(listener)
	}()
	return nil
}

func (p *posterCache) close(ctx context.Context) {
	p.mu.Lock()
	server := p.server
	p.server = nil
	p.baseURL = ""
	p.mu.Unlock()
	if server != nil {
		if err := server.Shutdown(ctx); err != nil {
			_ = server.Close()
		}
	}
}

func (p *posterCache) serve(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/posters/")
	if len(id) != 64 || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	path, err := p.ensureCached(id)
	if err != nil {
		if err == errUnknownPoster {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "poster unavailable", http.StatusBadGateway)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, path)
}

var errUnknownPoster = fmt.Errorf("unknown poster id")

// ensureCached guarantees the poster for id is present on disk, fetching it
// from Plex at most once concurrently (singleflight dedupes overlapping serve
// and warm requests for the same poster). It returns the cached file path.
func (p *posterCache) ensureCached(id string) (string, error) {
	p.mu.RLock()
	source, ok := p.sources[id]
	p.mu.RUnlock()
	if !ok {
		return "", errUnknownPoster
	}
	path, err := p.cachedPath(id)
	if err != nil {
		return "", err
	}
	if info, statErr := os.Stat(path); statErr == nil {
		if info.Size() > 0 {
			return path, nil
		}
	} else if !os.IsNotExist(statErr) {
		return "", statErr
	}
	_, err, _ = p.group.Do(id, func() (any, error) {
		if info, statErr := os.Stat(path); statErr == nil && info.Size() > 0 {
			return nil, nil
		}
		return nil, p.fetch(path, source)
	})
	if err != nil {
		return "", err
	}
	return path, nil
}

// warm pre-fetches the posters behind the given loopback URLs into the disk
// cache using a bounded worker pool. Call it (in a goroutine) when a screen of
// results is computed so images are already cached — a fast file serve — by the
// time the browser, capped at ~6 connections per origin, requests them.
func (p *posterCache) warm(urls []string) {
	var wg sync.WaitGroup
	for _, u := range urls {
		id := posterIDFromURL(u)
		if id == "" {
			continue
		}
		p.warmSem <- struct{}{}
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			defer func() { <-p.warmSem }()
			_, _ = p.ensureCached(id)
		}(id)
	}
	wg.Wait()
}

// posterIDFromURL extracts the 64-char cache id from a registered poster URL
// (either "/posters/<id>" or "http://host/posters/<id>").
func posterIDFromURL(u string) string {
	i := strings.LastIndex(u, "/posters/")
	if i < 0 {
		return ""
	}
	id := u[i+len("/posters/"):]
	if len(id) != 64 || strings.Contains(id, "/") {
		return ""
	}
	return id
}

func (p *posterCache) cachedPath(id string) (string, error) {
	dir, err := config.GetCacheDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "posters")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(dir, id+".img"), nil
}

func (p *posterCache) fetch(path string, source posterSource) error {
	endpoint, err := url.Parse(source.ServerURL + "/photo/:/transcode")
	if err != nil {
		return err
	}
	q := endpoint.Query()
	q.Set("width", strconv.Itoa(source.Width))
	q.Set("height", strconv.Itoa(source.Height))
	q.Set("minSize", "1")
	q.Set("upscale", "1")
	q.Set("url", source.ThumbPath)
	q.Set("X-Plex-Token", source.Token)
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Plex poster request returned %s", resp.Status)
	}
	if contentType := resp.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "image/") {
		return fmt.Errorf("Plex poster response is not an image")
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".poster-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	written, copyErr := io.Copy(tmp, io.LimitReader(resp.Body, posterMaxImageBytes+1))
	closeErr := tmp.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > posterMaxImageBytes {
		return fmt.Errorf("Plex poster exceeds size limit")
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	go p.prune()
	return nil
}

func (p *posterCache) prune() {
	dir, err := config.GetCacheDir()
	if err != nil {
		return
	}
	dir = filepath.Join(dir, "posters")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type cachedFile struct {
		path string
		size int64
		mod  time.Time
	}
	files := make([]cachedFile, 0, len(entries))
	var total int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		total += info.Size()
		files = append(files, cachedFile{filepath.Join(dir, entry.Name()), info.Size(), info.ModTime()})
	}
	if total <= posterCacheMaxBytes {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for _, file := range files {
		if total <= posterCacheMaxBytes {
			break
		}
		if os.Remove(file.path) == nil {
			total -= file.size
		}
	}
}
