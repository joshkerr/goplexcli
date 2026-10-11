package libserver

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultImageBaseURL  = "https://image.tmdb.org/t/p"
	defaultImageCacheMax = 2 << 30  // 2 GiB
	maxImageBytes        = 32 << 20 // refuse absurd upstream bodies
	imageCacheControl    = "public, max-age=604800"
	touchInterval        = time.Hour // minimum gap between mtime bumps on hits
)

// tmdbSizes is the TMDB poster size table, ascending. "original" has no
// fixed pixel width and is always last.
var tmdbSizes = []struct {
	name string
	px   int
}{
	{"w92", 92},
	{"w154", 154},
	{"w185", 185},
	{"w342", 342},
	{"w500", 500},
	{"w780", 780},
	{"w1280", 1280},
	{"original", 0},
}

var imageFileRe = regexp.MustCompile(`^[A-Za-z0-9_-]+\.(jpg|jpeg|png|webp)$`)

func validImageSize(size string) bool {
	for _, s := range tmdbSizes {
		if s.name == size {
			return true
		}
	}
	return false
}

// sizeFor picks the smallest TMDB size whose width covers a poster box of
// width x height (posters are 2:3, so height/1.5 is the implied width).
// The minimum is w92; anything wider than the table gets "original".
func sizeFor(width, height int) string {
	target := width
	if h := int(math.Ceil(float64(height) / 1.5)); h > target {
		target = h
	}
	for _, s := range tmdbSizes {
		if s.px == 0 {
			continue
		}
		if s.px >= target {
			return s.name
		}
	}
	return "original"
}

func imageContentType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

// imageCache proxies TMDB images to disk, deduplicating concurrent fetches
// of the same file and pruning least-recently-used files past a byte cap.
type imageCache struct {
	dir     string
	max     int64
	baseURL string
	client  *http.Client

	mu       sync.Mutex
	inflight map[string]*fetchCall
	pruning  atomic.Bool
}

type fetchCall struct {
	done        chan struct{}
	contentType string
	err         error
}

func newImageCache(opts Options) *imageCache {
	return &imageCache{
		dir:      opts.ImageCacheDir,
		max:      opts.ImageCacheMax,
		baseURL:  opts.ImageBaseURL,
		client:   opts.HTTPClient,
		inflight: make(map[string]*fetchCall),
	}
}

// errUpstream marks a failure talking to TMDB; it maps to 502.
type errUpstream struct{ msg string }

func (e errUpstream) Error() string { return e.msg }

// serve validates size/file, ensures the image is cached, and streams it.
func (c *imageCache) serve(w http.ResponseWriter, r *http.Request, size, file string) {
	if c.dir == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "image cache not configured"})
		return
	}
	if !validImageSize(size) || !imageFileRe.MatchString(file) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	contentType, err := c.ensure(size, file)
	if err != nil {
		var up errUpstream
		if errors.As(err, &up) {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": up.msg})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	f, err := os.Open(c.path(size, file))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", imageCacheControl)
	// Zero modtime: the file's mtime doubles as our LRU clock, so it must
	// not leak into Last-Modified where a touch would look like a change.
	http.ServeContent(w, r, file, time.Time{}, f)
}

func (c *imageCache) path(size, file string) string {
	return filepath.Join(c.dir, size, file)
}

// ensure makes sure size/file is on disk and returns its content type.
// Concurrent callers for the same file share one upstream fetch.
func (c *imageCache) ensure(size, file string) (string, error) {
	p := c.path(size, file)
	if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
		c.touch(p, st)
		return imageContentType(file), nil
	}

	key := size + "/" + file
	c.mu.Lock()
	if call, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		<-call.done
		return call.contentType, call.err
	}
	call := &fetchCall{done: make(chan struct{})}
	c.inflight[key] = call
	c.mu.Unlock()

	call.contentType, call.err = c.fetch(size, file)
	close(call.done)

	c.mu.Lock()
	delete(c.inflight, key)
	c.mu.Unlock()

	if call.err == nil {
		c.maybePrune()
	}
	return call.contentType, call.err
}

// touch bumps the mtime so prune treats the file as recently used. It is
// rate-limited so hot files don't cause a write per request.
func (c *imageCache) touch(p string, st fs.FileInfo) {
	now := time.Now()
	if now.Sub(st.ModTime()) < touchInterval {
		return
	}
	_ = os.Chtimes(p, now, now)
}

// fetch downloads the image from upstream into the cache atomically.
func (c *imageCache) fetch(size, file string) (string, error) {
	u := c.baseURL + "/" + size + "/" + file
	resp, err := c.client.Get(u)
	if err != nil {
		return "", errUpstream{"upstream fetch failed: " + err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", errUpstream{fmt.Sprintf("upstream returned %d", resp.StatusCode)}
	}

	dir := filepath.Join(c.dir, size)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, "."+file+".*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	n, copyErr := io.Copy(tmp, io.LimitReader(resp.Body, maxImageBytes+1))
	closeErr := tmp.Close()
	switch {
	case copyErr != nil:
		cleanup()
		return "", errUpstream{"upstream read failed: " + copyErr.Error()}
	case closeErr != nil:
		cleanup()
		return "", closeErr
	case n > maxImageBytes:
		cleanup()
		return "", errUpstream{"upstream image too large"}
	case n == 0:
		cleanup()
		return "", errUpstream{"upstream returned empty body"}
	}
	if err := os.Rename(tmpPath, c.path(size, file)); err != nil {
		cleanup()
		return "", err
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		contentType = imageContentType(file)
	}
	return contentType, nil
}

// maybePrune starts a background prune unless one is already running.
func (c *imageCache) maybePrune() {
	if !c.pruning.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer c.pruning.Store(false)
		_ = c.prune()
	}()
}

type cachedFile struct {
	path  string
	size  int64
	mtime time.Time
}

// prune deletes the least-recently-used cached images until the cache is
// within its byte cap. It returns the number of files removed.
func (c *imageCache) prune() int {
	if c.dir == "" || c.max <= 0 {
		return 0
	}
	var files []cachedFile
	var total int64
	_ = filepath.WalkDir(c.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil // in-flight temp file
		}
		st, err := d.Info()
		if err != nil {
			return nil
		}
		files = append(files, cachedFile{p, st.Size(), st.ModTime()})
		total += st.Size()
		return nil
	})
	if total <= c.max {
		return 0
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mtime.Before(files[j].mtime) })
	removed := 0
	for _, f := range files {
		if total <= c.max {
			break
		}
		if err := os.Remove(f.path); err == nil {
			total -= f.size
			removed++
		}
	}
	return removed
}
