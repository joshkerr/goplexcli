package libserver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeResolver is an in-memory Resolver that records timeline calls.
type fakeResolver struct {
	items map[string]FileInfo

	mu        sync.Mutex
	timelines []timelineCall
}

type timelineCall struct {
	id, state  string
	timeMs     int
	durationMs int
}

func (f *fakeResolver) Lookup(id string) (FileInfo, bool) {
	fi, ok := f.items[id]
	return fi, ok
}

func (f *fakeResolver) Timeline(id, state string, timeMs, durationMs int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.timelines = append(f.timelines, timelineCall{id, state, timeMs, durationMs})
}

// fakeTMDB is an upstream image server that counts hits per path.
type fakeTMDB struct {
	*httptest.Server
	hits atomic.Int64
	body []byte
}

func newFakeTMDB(t *testing.T) *fakeTMDB {
	t.Helper()
	f := &fakeTMDB{body: bytes.Repeat([]byte("JPEGDATA"), 64)}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		if strings.HasSuffix(r.URL.Path, "/missing.jpg") {
			http.Error(w, "nope", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		// Echo the size segment so tests can check which size was chosen.
		w.Header().Set("X-Size", strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")[0])
		_, _ = w.Write(f.body)
	}))
	t.Cleanup(f.Server.Close)
	return f
}

type fixture struct {
	srv      *httptest.Server
	res      *fakeResolver
	tmdb     *fakeTMDB
	cacheDir string
	server   *Server
	video    []byte
	id       string
}

func newFixture(t *testing.T, opts Options) *fixture {
	t.Helper()
	dir := t.TempDir()
	video := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
	videoPath := filepath.Join(dir, "Café Movie (2024).mkv")
	if err := os.WriteFile(videoPath, video, 0o644); err != nil {
		t.Fatal(err)
	}
	const id = "0a1b2c3d"
	res := &fakeResolver{items: map[string]FileInfo{
		id: {Path: videoPath, Size: int64(len(video)), Duration: 5400000, Title: "Café Movie", Type: "movie"},
	}}
	tmdb := newFakeTMDB(t)
	cacheDir := filepath.Join(dir, "imgcache")
	opts.ImageCacheDir = cacheDir
	opts.ImageBaseURL = tmdb.URL
	s := New(res, opts)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return &fixture{srv: srv, res: res, tmdb: tmdb, cacheDir: cacheDir, server: s, video: video, id: id}
}

func get(t *testing.T, url string, hdr ...string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, body
}

func TestKeyHelpers(t *testing.T) {
	if got := ItemKey("abc"); got != "/local/metadata/abc" {
		t.Fatalf("ItemKey = %q", got)
	}
	if id, ok := IDFromKey("/local/metadata/abc"); !ok || id != "abc" {
		t.Fatalf("IDFromKey = %q, %v", id, ok)
	}
	if _, ok := IDFromKey("/library/metadata/123"); ok {
		t.Fatal("IDFromKey accepted a Plex key")
	}
	if _, ok := IDFromKey("/local/metadata/"); ok {
		t.Fatal("IDFromKey accepted an empty id")
	}
	if !IsLocalKey("/local/metadata/x") || IsLocalKey("/library/metadata/1") {
		t.Fatal("IsLocalKey wrong")
	}
	if got := ThumbPath("/abc123.jpg"); got != "/local/tmdb/w500/abc123.jpg" {
		t.Fatalf("ThumbPath = %q", got)
	}
}

func TestIdentity(t *testing.T) {
	fx := newFixture(t, Options{Token: "secret", Version: "1.2.3"})
	// No token required.
	resp, body := get(t, fx.srv.URL+"/identity", "Accept", "application/json")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type %q", ct)
	}
	// Same shape plexgo's GetIdentityResponseBody unmarshals into.
	var out struct {
		MediaContainer *struct {
			Claimed           *bool   `json:"claimed,omitempty"`
			MachineIdentifier *string `json:"machineIdentifier,omitempty"`
			Size              *int64  `json:"size,omitempty"`
			Version           *string `json:"version,omitempty"`
		} `json:"MediaContainer,omitempty"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, body)
	}
	mc := out.MediaContainer
	if mc == nil || mc.MachineIdentifier == nil || *mc.MachineIdentifier != "goplexcli-library" {
		t.Fatalf("bad machineIdentifier: %s", body)
	}
	if mc.Version == nil || *mc.Version != "1.2.3" || mc.Claimed == nil || !*mc.Claimed || mc.Size == nil {
		t.Fatalf("bad identity: %s", body)
	}
}

func TestMetadataMatchesGetStreamURL(t *testing.T) {
	fx := newFixture(t, Options{})
	resp, body := get(t, fx.srv.URL+ItemKey(fx.id)+"?X-Plex-Token=", "Accept", "application/json")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	// The exact anonymous struct GetStreamURL unmarshals into.
	var metadataResp struct {
		MediaContainer struct {
			Metadata []struct {
				Media []struct {
					Part []struct {
						Key *string `json:"key"`
					} `json:"Part"`
				} `json:"Media"`
			} `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	if err := json.Unmarshal(body, &metadataResp); err != nil {
		t.Fatal(err)
	}
	md := metadataResp.MediaContainer.Metadata
	if len(md) != 1 || len(md[0].Media) != 1 || len(md[0].Media[0].Part) != 1 {
		t.Fatalf("unexpected shape: %s", body)
	}
	if k := md[0].Media[0].Part[0].Key; k == nil || *k != FilePrefix+fx.id {
		t.Fatalf("part key = %v", k)
	}

	// Full field check.
	var full metadataResponse
	if err := json.Unmarshal(body, &full); err != nil {
		t.Fatal(err)
	}
	item := full.MediaContainer.Metadata[0]
	if full.MediaContainer.Size != 1 || item.RatingKey != fx.id || item.Key != ItemKey(fx.id) ||
		item.Type != "movie" || item.Title != "Café Movie" || item.Duration != 5400000 {
		t.Fatalf("bad item: %+v", item)
	}
	part := item.Media[0].Part[0]
	if part.Container != "mkv" || part.Size != int64(len(fx.video)) || part.Duration != 5400000 || part.File == "" {
		t.Fatalf("bad part: %+v", part)
	}

	// Unknown id → 404 JSON.
	resp, body = get(t, fx.srv.URL+ItemKey("deadbeef"))
	if resp.StatusCode != 404 || !strings.Contains(string(body), `"error"`) {
		t.Fatalf("unknown id: %d %s", resp.StatusCode, body)
	}
}

func TestFileStreaming(t *testing.T) {
	fx := newFixture(t, Options{})
	url := fx.srv.URL + FilePrefix + fx.id + "?download=1&X-Plex-Token=x"

	resp, body := get(t, url)
	if resp.StatusCode != 200 || !bytes.Equal(body, fx.video) {
		t.Fatalf("full get: %d %q", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "video/x-matroska" {
		t.Fatalf("content-type %q", ct)
	}
	cd := resp.Header.Get("Content-Disposition")
	if !strings.HasPrefix(cd, `inline; filename="Caf_ Movie (2024).mkv"; filename*=UTF-8''Caf%C3%A9%20Movie%20%282024%29.mkv`) {
		t.Fatalf("content-disposition %q", cd)
	}
	if ar := resp.Header.Get("Accept-Ranges"); ar != "bytes" {
		t.Fatalf("accept-ranges %q", ar)
	}

	// Range request.
	resp, body = get(t, url, "Range", "bytes=10-19")
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("range status %d", resp.StatusCode)
	}
	if !bytes.Equal(body, fx.video[10:20]) {
		t.Fatalf("range body %q", body)
	}
	if cr := resp.Header.Get("Content-Range"); cr != "bytes 10-19/36" {
		t.Fatalf("content-range %q", cr)
	}

	// ASCII-only filename gets a plain disposition.
	if got := contentDisposition("inline", "plain.mp4"); got != `inline; filename="plain.mp4"` {
		t.Fatalf("plain disposition %q", got)
	}

	// Unknown id and traversal-looking ids → 404.
	for _, bad := range []string{"nope", "..", "..%2F..%2Fetc%2Fpasswd"} {
		resp, _ = get(t, fx.srv.URL+FilePrefix+bad)
		if resp.StatusCode != 404 {
			t.Fatalf("id %q: status %d", bad, resp.StatusCode)
		}
	}
}

func TestTokenEnforcement(t *testing.T) {
	fx := newFixture(t, Options{Token: "s3cret"})
	base := fx.srv.URL + ItemKey(fx.id)

	resp, body := get(t, base)
	if resp.StatusCode != 401 || string(body) != `{"error":"unauthorized"}` {
		t.Fatalf("no token: %d %s", resp.StatusCode, body)
	}
	resp, _ = get(t, base+"?X-Plex-Token=wrong")
	if resp.StatusCode != 401 {
		t.Fatalf("wrong token: %d", resp.StatusCode)
	}
	resp, _ = get(t, base+"?X-Plex-Token=s3cret")
	if resp.StatusCode != 200 {
		t.Fatalf("query token: %d", resp.StatusCode)
	}
	resp, _ = get(t, base, "X-Plex-Token", "s3cret")
	if resp.StatusCode != 200 {
		t.Fatalf("header token: %d", resp.StatusCode)
	}
	// Unknown route with token → 404 JSON.
	resp, body = get(t, fx.srv.URL+"/something/else?X-Plex-Token=s3cret")
	if resp.StatusCode != 404 || string(body) != `{"error":"not found"}` {
		t.Fatalf("unknown route: %d %s", resp.StatusCode, body)
	}
}

func TestImageProxyCaches(t *testing.T) {
	fx := newFixture(t, Options{})
	url := fx.srv.URL + "/local/tmdb/w500/abc123.jpg?X-Plex-Token=x"

	resp, body := get(t, url)
	if resp.StatusCode != 200 || !bytes.Equal(body, fx.tmdb.body) {
		t.Fatalf("first: %d %d bytes", resp.StatusCode, len(body))
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/jpeg" {
		t.Fatalf("content-type %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=604800" {
		t.Fatalf("cache-control %q", cc)
	}
	if _, err := os.Stat(filepath.Join(fx.cacheDir, "w500", "abc123.jpg")); err != nil {
		t.Fatalf("not cached: %v", err)
	}

	resp, body = get(t, url)
	if resp.StatusCode != 200 || !bytes.Equal(body, fx.tmdb.body) {
		t.Fatalf("second: %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/jpeg" {
		t.Fatalf("second content-type %q", ct)
	}
	if n := fx.tmdb.hits.Load(); n != 1 {
		t.Fatalf("upstream hits = %d, want 1", n)
	}

	// Bad size / bad file names are rejected without touching upstream.
	for _, bad := range []string{
		"/local/tmdb/w999/abc.jpg",
		"/local/tmdb/w500/abc.gif",
		"/local/tmdb/w500/..%2F..%2Fx.jpg",
		"/local/tmdb/w500/a%20b.jpg",
		"/local/tmdb/w500/",
	} {
		resp, _ = get(t, fx.srv.URL+bad)
		if resp.StatusCode != 404 {
			t.Fatalf("%s: status %d", bad, resp.StatusCode)
		}
	}
	if n := fx.tmdb.hits.Load(); n != 1 {
		t.Fatalf("upstream hits after bad requests = %d, want 1", n)
	}

	// Upstream 404 → 502, nothing cached.
	resp, _ = get(t, fx.srv.URL+"/local/tmdb/w500/missing.jpg")
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("upstream miss: status %d", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(fx.cacheDir, "w500", "missing.jpg")); err == nil {
		t.Fatal("missing image was cached")
	}
}

func TestImageProxySingleFlight(t *testing.T) {
	fx := newFixture(t, Options{})
	url := fx.srv.URL + "/local/tmdb/w342/same.jpg"
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(url)
			if err != nil {
				t.Error(err)
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Errorf("status %d", resp.StatusCode)
			}
		}()
	}
	wg.Wait()
	if n := fx.tmdb.hits.Load(); n != 1 {
		t.Fatalf("upstream hits = %d, want 1", n)
	}
}

func TestSizeFor(t *testing.T) {
	cases := []struct {
		w, h int
		want string
	}{
		{0, 0, "w92"},
		{50, 60, "w92"},
		{300, 450, "w342"},
		{300, 0, "w342"},
		{600, 900, "w780"},
		{600, 0, "w780"},
		{100, 1200, "w1280"}, // height dominates: 1200/1.5 = 800
		{2000, 3000, "original"},
	}
	for _, c := range cases {
		if got := sizeFor(c.w, c.h); got != c.want {
			t.Errorf("sizeFor(%d,%d) = %q, want %q", c.w, c.h, got, c.want)
		}
	}
}

func TestTranscodePicksSize(t *testing.T) {
	fx := newFixture(t, Options{})
	for _, c := range []struct {
		width, height string
		want          string
	}{
		{"300", "450", "w342"},
		{"600", "900", "w780"},
	} {
		url := fx.srv.URL + "/photo/:/transcode?width=" + c.width + "&height=" + c.height +
			"&minSize=1&upscale=1&url=" + ThumbPath("/poster.jpg") + "&X-Plex-Token=x"
		resp, body := get(t, url)
		if resp.StatusCode != 200 || !bytes.Equal(body, fx.tmdb.body) {
			t.Fatalf("width %s: %d", c.width, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
			t.Fatalf("width %s: content-type %q", c.width, ct)
		}
		if _, err := os.Stat(filepath.Join(fx.cacheDir, c.want, "poster.jpg")); err != nil {
			t.Fatalf("width %s: expected %s cached: %v", c.width, c.want, err)
		}
	}
	// Non-local thumb → 404.
	resp, _ := get(t, fx.srv.URL+"/photo/:/transcode?width=300&height=450&url=/library/metadata/1/thumb/2")
	if resp.StatusCode != 404 {
		t.Fatalf("plex thumb: status %d", resp.StatusCode)
	}
}

func TestTimeline(t *testing.T) {
	fx := newFixture(t, Options{})
	url := fx.srv.URL + "/:/timeline?ratingKey=" + fx.id + "&key=/library/metadata/" + fx.id +
		"&state=paused&time=123456&duration=5400000&X-Plex-Token=x"
	resp, body := get(t, url, "Accept", "application/json")
	if resp.StatusCode != 200 || len(body) != 0 {
		t.Fatalf("timeline: %d %q", resp.StatusCode, body)
	}
	req, _ := http.NewRequest(http.MethodPost, url, nil)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("POST timeline: %d", resp2.StatusCode)
	}

	fx.res.mu.Lock()
	calls := append([]timelineCall(nil), fx.res.timelines...)
	fx.res.mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("timeline calls = %d", len(calls))
	}
	want := timelineCall{fx.id, "paused", 123456, 5400000}
	if calls[0] != want || calls[1] != want {
		t.Fatalf("timeline calls = %+v", calls)
	}

	resp, _ = get(t, fx.srv.URL+"/:/timeline?ratingKey="+fx.id+"&state=bogus")
	if resp.StatusCode != 400 {
		t.Fatalf("bad state: %d", resp.StatusCode)
	}
}

func TestLibrarySections(t *testing.T) {
	fx := newFixture(t, Options{})
	resp, body := get(t, fx.srv.URL+"/library/sections?X-Plex-Token=x")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var out struct {
		MediaContainer struct {
			Size      int `json:"size"`
			Directory []struct {
				Key string `json:"key"`
			} `json:"Directory"`
		} `json:"MediaContainer"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out.MediaContainer.Size != 0 || len(out.MediaContainer.Directory) != 0 {
		t.Fatalf("sections: %s", body)
	}
}

func TestPruneRemovesOldest(t *testing.T) {
	dir := t.TempDir()
	c := newImageCache(Options{ImageCacheDir: dir, ImageCacheMax: 250})
	sizeDir := filepath.Join(dir, "w500")
	if err := os.MkdirAll(sizeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-10 * time.Hour)
	// Five 100-byte files, oldest first; cap 250 → three must go.
	for i, name := range []string{"a.jpg", "b.jpg", "c.jpg", "d.jpg", "e.jpg"} {
		p := filepath.Join(sizeDir, name)
		if err := os.WriteFile(p, bytes.Repeat([]byte("x"), 100), 0o644); err != nil {
			t.Fatal(err)
		}
		ts := base.Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(p, ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	// A temp file in progress must be left alone.
	tmp := filepath.Join(sizeDir, ".e.jpg.123")
	if err := os.WriteFile(tmp, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}

	if removed := c.prune(); removed != 3 {
		t.Fatalf("removed %d, want 3", removed)
	}
	for _, gone := range []string{"a.jpg", "b.jpg", "c.jpg"} {
		if _, err := os.Stat(filepath.Join(sizeDir, gone)); err == nil {
			t.Fatalf("%s should have been pruned", gone)
		}
	}
	for _, kept := range []string{"d.jpg", "e.jpg", ".e.jpg.123"} {
		if _, err := os.Stat(filepath.Join(sizeDir, kept)); err != nil {
			t.Fatalf("%s should have been kept: %v", kept, err)
		}
	}
	// Second prune is a no-op.
	if removed := c.prune(); removed != 0 {
		t.Fatalf("second prune removed %d", removed)
	}
}

func TestLanURLs(t *testing.T) {
	if got := LanURLs("bad"); got != nil {
		t.Fatalf("LanURLs(bad) = %v", got)
	}
	for _, u := range LanURLs(DefaultListen) {
		if !strings.HasPrefix(u, "http://") || !strings.HasSuffix(u, ":47822") {
			t.Fatalf("unexpected url %q", u)
		}
	}
}
