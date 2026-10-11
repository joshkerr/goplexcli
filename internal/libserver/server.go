// Package libserver is a tiny HTTP server that speaks just enough of the
// Plex Media Server wire protocol for goplexcli's own clients (CLI, desktop
// GUI, iOS app) to play locally indexed items without any client changes.
//
// It implements the handful of endpoints those clients actually call:
// /identity, item metadata, part streaming, poster images (proxied from
// TMDB), /photo/:/transcode, /:/timeline and an empty /library/sections.
package libserver

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultListen is the address the daemon binds by default.
	DefaultListen = ":47822"
	// KeyPrefix prefixes every local item key (the Plex "key"/ratingKey path).
	KeyPrefix = "/local/metadata/"
	// FilePrefix prefixes every local part key (the streamable file).
	FilePrefix = "/local/file/"
	// ImagePrefix prefixes every local poster Thumb path.
	ImagePrefix = "/local/tmdb/"

	defaultMachineID = "goplexcli-library"
	defaultThumbSize = "w500"
)

// ItemKey returns the cache Key for an item id.
func ItemKey(id string) string { return KeyPrefix + id }

// IDFromKey extracts the item id from a local item key. ok is false when
// key is not a local key or carries no id.
func IDFromKey(key string) (string, bool) {
	if !IsLocalKey(key) {
		return "", false
	}
	id := strings.TrimPrefix(key, KeyPrefix)
	if i := strings.IndexAny(id, "?#"); i >= 0 {
		id = id[:i]
	}
	id = strings.TrimSuffix(id, "/")
	if !validID(id) {
		return "", false
	}
	return id, true
}

// ThumbPath builds a Thumb path from a TMDB image path ("/abc.jpg") at the
// default poster size (w500).
func ThumbPath(tmdbPath string) string {
	return ImagePrefix + defaultThumbSize + "/" + strings.TrimPrefix(tmdbPath, "/")
}

// IsLocalKey reports whether key starts with KeyPrefix.
func IsLocalKey(key string) bool { return strings.HasPrefix(key, KeyPrefix) }

// FileInfo describes the on-disk file behind an item.
type FileInfo struct {
	Path     string // absolute local path
	Size     int64
	Duration int // ms, may be 0
	Title    string
	Type     string // "movie" | "episode"
	// Rclone is the file's rclone path ("remote:dir/file.mkv"); when the
	// local Path can't be opened, the file is streamed through rclone instead.
	Rclone string
}

// Resolver is implemented by the daemon that owns the index.
type Resolver interface {
	// Lookup returns the file for an item id; ok=false → 404.
	Lookup(id string) (FileInfo, bool)
	// Timeline records playback progress for an item (state is playing|paused|stopped).
	Timeline(id, state string, timeMs, durationMs int)
}

// Options configures a Server.
type Options struct {
	Token         string       // if non-empty, every request except /identity must carry it as X-Plex-Token (query or header); else 401 JSON {"error":"unauthorized"}
	ImageCacheDir string       // where proxied TMDB images are stored (required for image routes)
	ImageCacheMax int64        // bytes; prune oldest files beyond this (default 2 GiB)
	ImageBaseURL  string       // default "https://image.tmdb.org/t/p"; tests override
	HTTPClient    *http.Client // for image fetches; default 30s timeout
	MachineID     string       // reported by /identity; default "goplexcli-library"
	Version       string       // reported by /identity
	RcloneBin     string       // rclone executable for streaming unmounted files; default "rclone"
}

// Server serves the Plex-compatible endpoints for a Resolver.
type Server struct {
	rclone *rcloneStreamer
	res    Resolver
	opts   Options

	images *imageCache
}

// New builds a Server. Zero-value Options fields take the documented defaults.
func New(res Resolver, opts Options) *Server {
	if opts.ImageCacheMax <= 0 {
		opts.ImageCacheMax = defaultImageCacheMax
	}
	if opts.ImageBaseURL == "" {
		opts.ImageBaseURL = defaultImageBaseURL
	}
	opts.ImageBaseURL = strings.TrimRight(opts.ImageBaseURL, "/")
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if opts.MachineID == "" {
		opts.MachineID = defaultMachineID
	}
	s := &Server{res: res, opts: opts}
	s.images = newImageCache(opts)
	s.rclone = newRcloneStreamer(opts.RcloneBin)
	return s
}

// Handler returns the http.Handler implementing all routes.
func (s *Server) Handler() http.Handler { return s }

// ServeHTTP routes requests. Routing is done by hand because several Plex
// paths contain ":" segments that are awkward to express as mux patterns.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := path.Clean(r.URL.Path)

	if p == "/identity" {
		if !allowMethods(w, r, http.MethodGet, http.MethodHead) {
			return
		}
		s.handleIdentity(w, r)
		return
	}

	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	switch {
	case strings.HasPrefix(p, KeyPrefix):
		if !allowMethods(w, r, http.MethodGet, http.MethodHead) {
			return
		}
		s.handleMetadata(w, r, strings.TrimPrefix(p, KeyPrefix))
	case strings.HasPrefix(p, FilePrefix):
		if !allowMethods(w, r, http.MethodGet, http.MethodHead) {
			return
		}
		s.handleFile(w, r, strings.TrimPrefix(p, FilePrefix))
	case strings.HasPrefix(p, ImagePrefix):
		if !allowMethods(w, r, http.MethodGet, http.MethodHead) {
			return
		}
		size, file, ok := splitImagePath(p)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		s.images.serve(w, r, size, file)
	case p == "/photo/:/transcode":
		if !allowMethods(w, r, http.MethodGet, http.MethodHead) {
			return
		}
		s.handleTranscode(w, r)
	case p == "/:/timeline":
		if !allowMethods(w, r, http.MethodGet, http.MethodPost, http.MethodPut) {
			return
		}
		s.handleTimeline(w, r)
	case p == SearchPath:
		if !allowMethods(w, r, http.MethodGet) {
			return
		}
		s.handleSearch(w, r)
	case p == MatchPath:
		if !allowMethods(w, r, http.MethodPost) {
			return
		}
		s.handleMatch(w, r)
	case p == "/library/sections":
		if !allowMethods(w, r, http.MethodGet, http.MethodHead) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"MediaContainer": map[string]any{"size": 0, "Directory": []any{}},
		})
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

// authorized reports whether the request carries the configured token (or
// no token is configured).
func (s *Server) authorized(r *http.Request) bool {
	if s.opts.Token == "" {
		return true
	}
	got := r.Header.Get("X-Plex-Token")
	if got == "" {
		got = r.URL.Query().Get("X-Plex-Token")
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.opts.Token)) == 1
}

func (s *Server) handleIdentity(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"MediaContainer": map[string]any{
			"size":              0,
			"claimed":           true,
			"machineIdentifier": s.opts.MachineID,
			"version":           s.opts.Version,
		},
	})
}

// Plex-shaped metadata response, limited to the fields our clients read.
type metadataResponse struct {
	MediaContainer struct {
		Size     int            `json:"size"`
		Metadata []metadataItem `json:"Metadata"`
	} `json:"MediaContainer"`
}

type metadataItem struct {
	RatingKey string          `json:"ratingKey"`
	Key       string          `json:"key"`
	Type      string          `json:"type"`
	Title     string          `json:"title"`
	Duration  int             `json:"duration"`
	Media     []metadataMedia `json:"Media"`
}

type metadataMedia struct {
	ID       int            `json:"id"`
	Duration int            `json:"duration"`
	Part     []metadataPart `json:"Part"`
}

type metadataPart struct {
	ID        int    `json:"id"`
	Key       string `json:"key"`
	File      string `json:"file"`
	Size      int64  `json:"size"`
	Duration  int    `json:"duration"`
	Container string `json:"container"`
}

func (s *Server) handleMetadata(w http.ResponseWriter, _ *http.Request, id string) {
	if !validID(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	fi, ok := s.res.Lookup(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	var resp metadataResponse
	resp.MediaContainer.Size = 1
	resp.MediaContainer.Metadata = []metadataItem{{
		RatingKey: id,
		Key:       ItemKey(id),
		Type:      fi.Type,
		Title:     fi.Title,
		Duration:  fi.Duration,
		Media: []metadataMedia{{
			ID:       1,
			Duration: fi.Duration,
			Part: []metadataPart{{
				ID:        1,
				Key:       FilePrefix + id,
				File:      fi.Path,
				Size:      fi.Size,
				Duration:  fi.Duration,
				Container: strings.TrimPrefix(strings.ToLower(filepath.Ext(fi.Path)), "."),
			}},
		}},
	}}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleFile(w http.ResponseWriter, r *http.Request, id string) {
	if !validID(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	fi, ok := s.res.Lookup(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	f, err := os.Open(fi.Path)
	if err != nil {
		// Not on disk (no mount, or the mount is down): stream it through
		// rclone when the item knows its remote path.
		if fi.Rclone != "" && s.rclone.available() {
			s.rclone.serve(w, r, fi.Rclone, fi.Size)
			return
		}
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "file missing"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file missing"})
		return
	}
	base := filepath.Base(fi.Path)
	w.Header().Set("Content-Type", videoContentType(base))
	w.Header().Set("Content-Disposition", contentDisposition("inline", base))
	w.Header().Set("Accept-Ranges", "bytes")
	// download=1 is accepted and ignored: we always serve the raw file.
	http.ServeContent(w, r, base, st.ModTime(), f)
}

func (s *Server) handleTranscode(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	thumb := q.Get("url")
	if !strings.HasPrefix(thumb, ImagePrefix) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	_, file, ok := splitImagePath(path.Clean(thumb))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	width, _ := strconv.Atoi(q.Get("width"))
	height, _ := strconv.Atoi(q.Get("height"))
	s.images.serve(w, r, sizeFor(width, height), file)
}

func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id := q.Get("ratingKey")
	if id == "" {
		// Fall back to the key form some clients send.
		if k, ok := IDFromKey(q.Get("key")); ok {
			id = k
		}
	}
	if !validID(id) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing ratingKey"})
		return
	}
	state := q.Get("state")
	switch state {
	case "playing", "paused", "stopped":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid state"})
		return
	}
	timeMs, _ := strconv.Atoi(q.Get("time"))
	durationMs, _ := strconv.Atoi(q.Get("duration"))
	if timeMs < 0 {
		timeMs = 0
	}
	if durationMs < 0 {
		durationMs = 0
	}
	s.res.Timeline(id, state, timeMs, durationMs)
	w.WriteHeader(http.StatusOK)
}

// validID accepts the opaque ids used in keys: non-empty, no path
// separators, no "..". Ids are expected to be lowercase hex, but anything
// path-safe is tolerated so the Resolver stays the single source of truth.
func validID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	if strings.ContainsAny(id, "/\\") || strings.Contains(id, "..") {
		return false
	}
	return true
}

// splitImagePath splits "/local/tmdb/<size>/<file>" into its parts.
func splitImagePath(p string) (size, file string, ok bool) {
	rest := strings.TrimPrefix(p, ImagePrefix)
	size, file, found := strings.Cut(rest, "/")
	if !found || size == "" || file == "" || strings.Contains(file, "/") {
		return "", "", false
	}
	return size, file, true
}

func allowMethods(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	for _, m := range methods {
		if r.Method == m {
			return true
		}
	}
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// videoContentType maps a filename to the MIME type clients expect.
func videoContentType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mkv":
		return "video/x-matroska"
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".avi":
		return "video/x-msvideo"
	case ".mov":
		return "video/quicktime"
	case ".webm":
		return "video/webm"
	case ".ts":
		return "video/mp2t"
	default:
		return "application/octet-stream"
	}
}

// contentDisposition builds a Content-Disposition header value, adding an
// RFC 5987 filename* parameter when the name is not plain ASCII.
func contentDisposition(disposition, name string) string {
	ascii := true
	for _, r := range name {
		if r > 0x7e || r < 0x20 {
			ascii = false
			break
		}
	}
	quoted := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	if ascii {
		return fmt.Sprintf(`%s; filename="%s"`, disposition, quoted.Replace(name))
	}
	fallback := strings.Map(func(r rune) rune {
		if r > 0x7e || r < 0x20 {
			return '_'
		}
		return r
	}, name)
	return fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`,
		disposition, quoted.Replace(fallback), rfc5987Encode(name))
}

// rfc5987Encode percent-encodes everything outside attr-char.
func rfc5987Encode(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			b.WriteByte(c)
		case strings.IndexByte("!#$&+-.^_`|~", c) >= 0:
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0xf])
		}
	}
	return b.String()
}

// LanURLs returns http://<ipv4>:<port> for every non-loopback IPv4 address
// on this host, so the daemon can print how to reach it.
func LanURLs(listen string) []string {
	_, port, err := net.SplitHostPort(listen)
	if err != nil || port == "" {
		return nil
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var urls []string
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() {
			continue
		}
		if ip4 := ipNet.IP.To4(); ip4 != nil {
			urls = append(urls, fmt.Sprintf("http://%s:%s", ip4, port))
		}
	}
	return urls
}
