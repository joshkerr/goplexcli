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
	server  *http.Server
	baseURL string
}

func newPosterCache(client *http.Client) *posterCache {
	if client == nil || client == http.DefaultClient {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &posterCache{client: client, sources: make(map[string]posterSource)}
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
	p.mu.RLock()
	source, ok := p.sources[id]
	p.mu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	path, err := p.cachedPath(id)
	if err != nil {
		http.Error(w, "poster cache unavailable", http.StatusInternalServerError)
		return
	}
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		_, err, _ = p.group.Do(id, func() (any, error) {
			if _, statErr := os.Stat(path); statErr == nil {
				return nil, nil
			}
			return nil, p.fetch(path, source)
		})
	} else if statErr != nil {
		err = statErr
	}
	if err != nil {
		http.Error(w, "poster unavailable", http.StatusBadGateway)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, path)
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
