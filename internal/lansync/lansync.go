// Package lansync shares a goplexcli media cache between machines on the same
// LAN. Each participating process advertises itself via mDNS and serves its
// cache over HTTP; a peer can discover the others, find whichever has the
// freshest cache, and pull it — far faster than a full reindex from Plex for a
// large library.
//
// The HTTP endpoint is intentionally unauthenticated (LAN-only, user opt-in):
// it exposes library metadata (titles, file paths, server URLs) but never Plex
// tokens, which live in config rather than the cache.
//
// It is GUI-agnostic: the Wails GUI, the `goplexcli sync serve` daemon, and the
// `goplexcli sync pull` command all use it. Progress is reported through a
// caller-supplied callback rather than any UI framework.
package lansync

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
	"github.com/joshkerr/goplexcli/internal/cache"
	"github.com/joshkerr/goplexcli/internal/favorites"
)

const (
	// ServiceType is the mDNS service goplexcli instances advertise for cache sync.
	ServiceType = "_goplexcli-sync._tcp"
	// Domain is the mDNS domain used for discovery.
	Domain = "local."

	// DefaultPort is the well-known port the sync server binds by default, so a
	// peer can be addressed directly (e.g. `sync pull --peer host`) without
	// knowing a random port. If it's already in use, an ephemeral port is used
	// instead and mDNS is relied on for discovery.
	DefaultPort = 47820

	metaTimeout = 4 * time.Second
	pullTimeout = 10 * time.Minute
	discoverFor = 5 * time.Second
)

// httpClient is shared by the client-side probes and pulls. Timeouts are bound
// per-request via context, so no client-level Timeout (which would cap a large
// legitimate download).
var httpClient = &http.Client{}

// Meta is a peer's cache freshness summary, returned by /cache/meta.
type Meta struct {
	Instance    string    `json:"instance"`
	Count       int       `json:"count"`
	LastUpdated time.Time `json:"lastUpdated"`
}

// MetaFunc supplies the local cache freshness (Count + LastUpdated). The server
// stamps Instance itself, so implementations can leave it zero.
type MetaFunc func() Meta

// CacheMetaFunc adapts internal/cache's LoadMeta into a MetaFunc — the freshness
// source for headless servers (the CLI daemon) that don't hold an in-memory
// cache. A missing sidecar reports as empty (older than anything).
func CacheMetaFunc() MetaFunc {
	return func() Meta {
		m, _ := cache.LoadMeta()
		return Meta{Count: m.Count, LastUpdated: m.LastUpdated}
	}
}

// Server advertises this machine's cache on the LAN and serves it to peers.
type Server struct {
	metaFn MetaFunc

	mu       sync.Mutex
	server   *http.Server
	zc       *zeroconf.Server
	instance string
	port     int
	advErr   error // non-nil if mDNS advertising failed (serving still works)

	fav        *favorites.Store // nil = favorites endpoints disabled
	favChanged func()           // optional, called after a peer's POST changes the set
}

// NewServer creates a Server that reports freshness via metaFn.
func NewServer(metaFn MetaFunc) *Server {
	return &Server{metaFn: metaFn}
}

// ServeFavorites enables the /favorites endpoints backed by store. onChange
// (optional) is invoked whenever a peer's push changes the local set, so the
// host can refresh its UI. Call before Start.
func (s *Server) ServeFavorites(store *favorites.Store, onChange func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fav = store
	s.favChanged = onChange
}

// Instance returns this server's unique mDNS instance name (empty until Start).
func (s *Server) Instance() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.instance
}

// Port returns the bound TCP port (0 until Start succeeds).
func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// AdvertiseError returns the mDNS registration error, if any. A non-nil value
// means the server is reachable but won't be auto-discovered, so peers would
// need another way to learn its address.
func (s *Server) AdvertiseError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.advErr
}

// Start binds the server on DefaultPort (falling back to an ephemeral port if
// it's taken) and advertises it via mDNS.
func (s *Server) Start() error {
	return s.StartOn(DefaultPort)
}

// StartOn binds a LAN-reachable HTTP server on the given port (0 = ephemeral)
// and advertises it via mDNS. If a non-zero port is already in use it falls back
// to an ephemeral port rather than failing. It returns an error only when
// serving cannot start at all; a failure to advertise is recorded in
// AdvertiseError and does not stop serving.
func (s *Server) StartOn(port int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		return nil
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil && port != 0 {
		// Well-known port busy (e.g. the GUI is already serving on it) — take an
		// ephemeral port and let mDNS handle discovery.
		listener, err = net.Listen("tcp", ":0")
	}
	if err != nil {
		return fmt.Errorf("lan sync listen: %w", err)
	}
	s.port = listener.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	mux.HandleFunc("/cache/meta", s.serveMeta)
	mux.HandleFunc("/cache", s.serveCache)
	mux.HandleFunc("/favorites", s.serveFavorites)
	s.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = s.server.Serve(listener) }()

	host, _ := os.Hostname()
	if host == "" {
		host = "goplexcli"
	}
	// Append the PID so two machines that share a hostname still get distinct
	// instance names (and neither mistakes the other for itself in Discover).
	s.instance = fmt.Sprintf("%s-%d", host, os.Getpid())

	zc, err := zeroconf.Register(s.instance, ServiceType, Domain, s.port, nil, nil)
	if err != nil {
		s.advErr = err // non-fatal: keep serving
		return nil
	}
	s.zc = zc
	return nil
}

// Close stops advertising and shuts the server down.
func (s *Server) Close(ctx context.Context) {
	s.mu.Lock()
	server, zc := s.server, s.zc
	s.server, s.zc = nil, nil
	s.mu.Unlock()

	if zc != nil {
		// zeroconf's Shutdown takes no context and can block indefinitely in
		// its network teardown (observed hanging on macOS), so run it aside
		// and cap the wait even for callers with a background ctx — the
		// goroutine is abandoned, but callers close only when the process is
		// exiting anyway.
		zcDone := make(chan struct{})
		go func() { zc.Shutdown(); close(zcDone) }()
		select {
		case <-zcDone:
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
	if server != nil {
		if err := server.Shutdown(ctx); err != nil {
			_ = server.Close()
		}
	}
}

func (s *Server) serveMeta(w http.ResponseWriter, r *http.Request) {
	var m Meta
	if s.metaFn != nil {
		m = s.metaFn()
	}
	m.Instance = s.Instance()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(m)
}

// serveCache streams the on-disk media.json gzipped. Serving the raw file (vs.
// re-marshaling) preserves the exact LastUpdated stamp so freshness comparisons
// stay meaningful as a cache hops between machines.
func (s *Server) serveCache(w http.ResponseWriter, r *http.Request) {
	path, err := cache.GetCachePath()
	if err != nil {
		http.Error(w, "cache unavailable", http.StatusInternalServerError)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "no cache", http.StatusNotFound)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/gzip")
	gz := gzip.NewWriter(w)
	defer gz.Close()
	_, _ = io.Copy(gz, f)
}

// serveFavorites shares the favorites set with peers. GET returns the local
// set as v2 JSON; POST merges the peer's set into the local one (last-writer-
// wins per key, so pushes from any number of peers converge). 404 when the
// host didn't enable favorites (an older version, or no store configured) —
// clients treat that as "nothing to sync".
func (s *Server) serveFavorites(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	st, onChange := s.fav, s.favChanged
	s.mu.Unlock()
	if st == nil {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := st.Export()
		if err != nil {
			http.Error(w, "favorites unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	case http.MethodPost:
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<22))
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		changed, err := st.MergeData(body)
		if err != nil {
			http.Error(w, "bad favorites payload", http.StatusBadRequest)
			return
		}
		if changed && onChange != nil {
			onChange()
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// Peer is a discovered instance we can query and pull from.
type Peer struct {
	Instance string
	Addr     string // "host:port" using the first IPv4 address
}

func (p Peer) baseURL() string { return "http://" + p.Addr }

// Host returns a friendly machine name for the peer (the hostname without the
// PID suffix baked into the instance name).
func (p Peer) Host() string { return hostFromInstance(p.Instance) }

func hostFromInstance(instance string) string {
	if i := strings.LastIndex(instance, "-"); i > 0 {
		return instance[:i]
	}
	return instance
}

// Discover browses the LAN for peers, excluding excludeInstance (pass the local
// server's instance so a process that also serves doesn't pull from itself; pass
// "" for a pull-only process). It blocks up to discoverFor.
func Discover(ctx context.Context, excludeInstance string) ([]Peer, error) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil, fmt.Errorf("resolver: %w", err)
	}

	entries := make(chan *zeroconf.ServiceEntry, 16)
	var peers []Peer
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for entry := range entries {
			if entry.Instance == excludeInstance {
				continue
			}
			if len(entry.AddrIPv4) == 0 {
				continue
			}
			mu.Lock()
			peers = append(peers, Peer{
				Instance: entry.Instance,
				Addr:     fmt.Sprintf("%s:%d", entry.AddrIPv4[0].String(), entry.Port),
			})
			mu.Unlock()
		}
	}()

	dctx, cancel := context.WithTimeout(ctx, discoverFor)
	defer cancel()
	if err := resolver.Browse(dctx, ServiceType, Domain, entries); err != nil {
		close(entries)
		wg.Wait()
		return nil, fmt.Errorf("browse: %w", err)
	}
	<-dctx.Done()
	wg.Wait()
	return peers, nil
}

// FetchMeta fetches a peer's freshness summary.
func FetchMeta(ctx context.Context, p Peer) (Meta, error) {
	rctx, cancel := context.WithTimeout(ctx, metaTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(rctx, http.MethodGet, p.baseURL()+"/cache/meta", nil)
	resp, err := httpClient.Do(req)
	if err != nil {
		return Meta{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Meta{}, fmt.Errorf("meta %s", resp.Status)
	}
	var m Meta
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&m); err != nil {
		return Meta{}, err
	}
	if m.Instance == "" {
		m.Instance = p.Instance
	}
	return m, nil
}

// Pull downloads a peer's gzipped cache, decompresses it, atomically replaces
// the local media.json, refreshes the freshness sidecar to match, and returns
// the loaded cache.
func Pull(ctx context.Context, p Peer) (*cache.Cache, error) {
	path, err := cache.GetCachePath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}

	rctx, cancel := context.WithTimeout(ctx, pullTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(rctx, http.MethodGet, p.baseURL()+"/cache", nil)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cache %s", resp.Status)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}
	defer gz.Close()

	tmp, err := os.CreateTemp(filepath.Dir(path), ".media-sync-*.json.tmp")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, gz); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return nil, err
	}

	loaded, err := cache.Load()
	if err != nil {
		return nil, fmt.Errorf("reload synced cache: %w", err)
	}
	// Keep the sidecar consistent with the file we just wrote (preserving the
	// source's LastUpdated, not resetting it), so this machine reports accurate
	// freshness if it in turn serves the cache.
	_ = cache.SaveMeta(cache.CacheMeta{Count: len(loaded.Media), LastUpdated: loaded.LastUpdated})
	return loaded, nil
}

// FetchFavorites downloads a peer's favorites JSON. A 404 (peer predates
// favorites sync, or has it disabled) returns nil data and no error.
func FetchFavorites(ctx context.Context, p Peer) ([]byte, error) {
	rctx, cancel := context.WithTimeout(ctx, metaTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(rctx, http.MethodGet, p.baseURL()+"/favorites", nil)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("favorites %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<22))
}

// PushFavorites uploads a favorites set for the peer to merge into its own.
// A 404 (peer predates favorites sync) is not an error.
func PushFavorites(ctx context.Context, p Peer, data []byte) error {
	rctx, cancel := context.WithTimeout(ctx, metaTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(rctx, http.MethodPost, p.baseURL()+"/favorites", strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("favorites %s", resp.Status)
	}
	return nil
}

// SyncFavoritesWith merges favorites with the given peers: each reachable
// peer's set is folded into the local store, then the merged result is pushed
// back so the peers converge too. Merging is commutative and idempotent, so
// every peer is visited (not just the freshest, as the cache pull does).
// Returns whether the local set changed. Unreachable peers are skipped.
func SyncFavoritesWith(ctx context.Context, store *favorites.Store, peers []Peer, progress func(string)) bool {
	if store == nil || len(peers) == 0 {
		return false
	}
	if progress != nil {
		progress("Syncing favorites…")
	}
	changed := false
	for _, p := range peers {
		data, err := FetchFavorites(ctx, p)
		if err != nil || data == nil {
			continue
		}
		if ch, err := store.MergeData(data); err == nil && ch {
			changed = true
		}
	}
	if data, err := store.Export(); err == nil {
		for _, p := range peers {
			_ = PushFavorites(ctx, p, data)
		}
	}
	return changed
}

// Result reports the outcome of SyncFromLAN.
type Result struct {
	Updated          bool         // a newer cache was pulled
	UpToDate         bool         // peers found, but none newer than local
	Source           string       // friendly hostname the cache came from (when Updated)
	Cache            *cache.Cache // the pulled cache (non-nil when Updated)
	FavoritesChanged bool         // the local favorites set gained changes from a peer
}

// SyncFromLAN discovers peers (excluding excludeInstance), finds the one whose
// cache is newest and strictly newer than local, and pulls it. If fav is
// non-nil, favorites are also merged with every reachable peer — even when the
// cache is already up to date. progress, if non-nil, is called with
// human-readable status lines. Errors are returned with user-facing messages;
// Result.FavoritesChanged is meaningful even alongside a cache error, since
// favorites merge before the cache transfer.
func SyncFromLAN(ctx context.Context, excludeInstance string, local Meta, fav *favorites.Store, progress func(string)) (Result, error) {
	report := func(msg string) {
		if progress != nil {
			progress(msg)
		}
	}

	report("Looking for other computers…")
	peers, err := Discover(ctx, excludeInstance)
	if err != nil {
		return Result{}, err
	}
	if len(peers) == 0 {
		return Result{}, fmt.Errorf("no other running goplexcli found on the network")
	}

	res := Result{FavoritesChanged: SyncFavoritesWith(ctx, fav, peers, progress)}

	report(fmt.Sprintf("Comparing %d computer(s)…", len(peers)))
	var best *Peer
	var bestMeta Meta
	for i := range peers {
		m, err := FetchMeta(ctx, peers[i])
		if err != nil {
			continue // skip unreachable peers
		}
		if best == nil || m.LastUpdated.After(bestMeta.LastUpdated) {
			best = &peers[i]
			bestMeta = m
		}
	}
	if best == nil {
		return res, fmt.Errorf("found computers, but none could share their cache")
	}
	if !bestMeta.LastUpdated.After(local.LastUpdated) {
		res.UpToDate = true
		return res, nil
	}

	source := best.Host()
	report(fmt.Sprintf("Downloading cache from %s…", source))
	c, err := Pull(ctx, *best)
	if err != nil {
		return res, err
	}
	res.Updated = true
	res.Source = source
	res.Cache = c
	return res, nil
}

// SyncFromPeer pulls from an explicitly addressed peer, bypassing mDNS discovery
// entirely — the reliable path when multicast is blocked but the host is
// directly reachable (e.g. `--peer ghost-2.local`). It pulls only if the peer's
// cache is newer than local; favorites (when fav is non-nil) are merged either
// way.
func SyncFromPeer(ctx context.Context, addr string, local Meta, fav *favorites.Store, progress func(string)) (Result, error) {
	report := func(msg string) {
		if progress != nil {
			progress(msg)
		}
	}

	peer := Peer{Addr: addr}
	report(fmt.Sprintf("Contacting %s…", addr))
	m, err := FetchMeta(ctx, peer)
	if err != nil {
		return Result{}, fmt.Errorf("could not reach %s: %w", addr, err)
	}
	peer.Instance = m.Instance

	res := Result{FavoritesChanged: SyncFavoritesWith(ctx, fav, []Peer{peer}, progress)}

	if !m.LastUpdated.After(local.LastUpdated) {
		res.UpToDate = true
		return res, nil
	}

	source := peer.Host()
	if source == "" {
		source = addr
	}
	report(fmt.Sprintf("Downloading cache from %s…", source))
	c, err := Pull(ctx, peer)
	if err != nil {
		return res, err
	}
	res.Updated = true
	res.Source = source
	res.Cache = c
	return res, nil
}

// NormalizePeerAddr appends DefaultPort to a bare host (e.g. "ghost-2.local")
// so users can name a machine without remembering the port.
func NormalizePeerAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return addr
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return net.JoinHostPort(addr, strconv.Itoa(DefaultPort))
	}
	return addr
}
