package library

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/joshkerr/goplexcli/internal/libserver"
	"github.com/joshkerr/goplexcli/internal/plex"
)

// ServiceOptions configures the library server that streams locally indexed
// files and proxies their artwork.
type ServiceOptions struct {
	// Listen is the bind address; empty means libserver.DefaultListen.
	Listen string
	// Token, when set, is required on every request (see config.LibraryToken).
	Token string
	// ImageCacheDir stores proxied TMDB artwork.
	ImageCacheDir string
	// Version is reported by /identity.
	Version string
	// RcloneBin streams files that aren't mounted locally (default "rclone").
	RcloneBin string
	// Timeline receives playback progress reports for local items (by cache
	// key). It is called on the request goroutine, so it should be quick.
	Timeline func(key, state string, timeMs, durationMs int)
	// Search and Match back the manual match-fix endpoints; leave nil when
	// this server can't rebuild items (the endpoints then answer 503).
	Search func(ctx context.Context, kind, query string, year int) ([]libserver.Candidate, error)
	Match  func(ctx context.Context, key string, tmdbID int) ([]plex.MediaItem, error)
}

// Service is the running library server. It resolves item ids to files from
// whatever items were last handed to SetItems, so the owner (daemon or GUI)
// refreshes it after every index.
type Service struct {
	opts   ServiceOptions
	mu     sync.RWMutex
	files  map[string]libserver.FileInfo
	srv    *libserver.Server
	server *http.Server
	port   int
}

// NewService prepares a library server; call Start to listen.
func NewService(opts ServiceOptions) *Service {
	s := &Service{opts: opts, files: map[string]libserver.FileInfo{}}
	s.srv = libserver.New(s, libserver.Options{
		Token:         opts.Token,
		ImageCacheDir: opts.ImageCacheDir,
		Version:       opts.Version,
		RcloneBin:     opts.RcloneBin,
	})
	return s
}

// SetItems rebuilds the id → file table from the local items in a cache
// (those with a local key and the given ServerName).
func (s *Service) SetItems(items []plex.MediaItem, serverName string) {
	files := make(map[string]libserver.FileInfo)
	for i := range items {
		it := &items[i]
		if it.ServerName != serverName {
			continue
		}
		id, ok := libserver.IDFromKey(it.Key)
		if !ok {
			continue
		}
		files[id] = libserver.FileInfo{
			Path:     it.FilePath,
			Duration: it.Duration,
			Title:    it.Title,
			Type:     it.Type,
			Rclone:   it.RclonePath,
		}
	}
	s.mu.Lock()
	s.files = files
	s.mu.Unlock()
}

// Count returns how many items the server can currently resolve.
func (s *Service) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.files)
}

// Lookup implements libserver.Resolver.
func (s *Service) Lookup(id string) (libserver.FileInfo, bool) {
	s.mu.RLock()
	f, ok := s.files[id]
	s.mu.RUnlock()
	if !ok {
		return libserver.FileInfo{}, false
	}
	if f.Size == 0 {
		if st, err := statFile(f.Path); err == nil {
			f.Size = st
		}
	}
	return f, true
}

// Timeline implements libserver.Resolver.
func (s *Service) Timeline(id, state string, timeMs, durationMs int) {
	if s.opts.Timeline != nil {
		s.opts.Timeline(libserver.ItemKey(id), state, timeMs, durationMs)
	}
}

// SearchTitles implements libserver.Matcher.
func (s *Service) SearchTitles(ctx context.Context, kind, query string, year int) ([]libserver.Candidate, error) {
	if s.opts.Search == nil {
		return nil, errors.New("match fixing is not available on this server")
	}
	return s.opts.Search(ctx, kind, query, year)
}

// ApplyMatch implements libserver.Matcher.
func (s *Service) ApplyMatch(ctx context.Context, key string, tmdbID int) ([]plex.MediaItem, error) {
	if s.opts.Match == nil {
		return nil, errors.New("match fixing is not available on this server")
	}
	return s.opts.Match(ctx, key, tmdbID)
}

// Start binds the listener and serves in the background.
func (s *Service) Start() error {
	listen := s.opts.Listen
	if listen == "" {
		listen = libserver.DefaultListen
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("library server listen %s: %w", listen, err)
	}
	s.port = ln.Addr().(*net.TCPAddr).Port
	s.server = &http.Server{Handler: s.srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = s.server.Serve(ln) }()
	return nil
}

// Port returns the bound port (0 before Start).
func (s *Service) Port() int { return s.port }

// Close stops the server.
func (s *Service) Close(ctx context.Context) {
	if s.server == nil {
		return
	}
	if err := s.server.Shutdown(ctx); err != nil {
		_ = s.server.Close()
	}
}

// Handler exposes the library routes for embedding in another mux.
func (s *Service) Handler() http.Handler { return s.srv.Handler() }

func statFile(path string) (int64, error) {
	st, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

// ServerURL picks the base URL to stamp on indexed items: the configured
// LibraryURL, else the first LAN IPv4 address and the listen port.
func ServerURL(configured, listen string) string {
	if u := strings.TrimRight(strings.TrimSpace(configured), "/"); u != "" {
		if !strings.Contains(u, "://") {
			u = "http://" + u
		}
		return u
	}
	if listen == "" {
		listen = libserver.DefaultListen
	}
	if urls := libserver.LanURLs(listen); len(urls) > 0 {
		return urls[0]
	}
	_, port, _ := net.SplitHostPort(listen)
	return "http://127.0.0.1:" + port
}

// ImageCacheDir is where proxied artwork lives under the goplexcli cache dir.
func ImageCacheDir(cacheDir string) string {
	return filepath.Join(cacheDir, "library", "images")
}

// TMDBCacheDir is where TMDB responses are cached under the goplexcli cache dir.
func TMDBCacheDir(cacheDir string) string {
	return filepath.Join(cacheDir, "library", "tmdb")
}

// ApplyTimeline folds a playback report into an item the way Plex does:
// the position is remembered while playing or paused, and a stop past 90%
// of the duration counts as a complete view (position reset, count bumped).
// It returns whether the item changed.
func ApplyTimeline(item *plex.MediaItem, state string, timeMs, durationMs int, now time.Time) bool {
	if durationMs <= 0 {
		durationMs = item.Duration
	}
	finished := durationMs > 0 && float64(timeMs) >= 0.9*float64(durationMs)
	switch state {
	case "stopped":
		if finished {
			item.ViewOffset = 0
			item.ViewCount++
		} else {
			item.ViewOffset = timeMs
		}
	case "playing", "paused":
		if finished {
			item.ViewOffset = 0
		} else {
			item.ViewOffset = timeMs
		}
	default:
		return false
	}
	item.LastViewedAt = now.Unix()
	return true
}
