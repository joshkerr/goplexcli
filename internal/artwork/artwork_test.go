package artwork

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshkerr/goplexcli/internal/plex"
	"github.com/joshkerr/goplexcli/internal/tmdb"
)

// fakeTMDBArt serves a movie with a backdrop and a show with only a poster.
func fakeTMDBArt(t *testing.T) *tmdb.Client {
	t.Helper()
	write := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/movie":
			write(w, map[string]any{"results": []map[string]any{{"id": 7, "title": "Backdropped", "release_date": "2001-01-01"}}})
		case "/movie/7":
			write(w, map[string]any{"id": 7, "title": "Backdropped", "release_date": "2001-01-01", "poster_path": "/p.jpg", "backdrop_path": "/b.jpg", "imdb_id": "tt0000007"})
		case "/search/tv":
			write(w, map[string]any{"results": []map[string]any{{"id": 9, "name": "Postered", "first_air_date": "2010-01-01"}}})
		case "/tv/9":
			write(w, map[string]any{"id": 9, "name": "Postered", "first_air_date": "2010-01-01", "poster_path": "/tvp.jpg"})
		default:
			w.WriteHeader(404)
			write(w, map[string]any{"status_code": 34})
		}
	}))
	t.Cleanup(srv.Close)
	c := tmdb.New("k", nil)
	c.SetBaseURL(srv.URL)
	c.SetRateLimit(0)
	return c
}

func TestFindFallsBackToTMDB(t *testing.T) {
	opts := Options{TMDB: fakeTMDBArt(t)} // no Apple, no Netflix
	ctx := context.Background()

	c, err := Find(ctx, opts, Item{Title: "Backdropped", Year: 2001, Kind: "movie"})
	if err != nil || c.Source != "tmdb" || c.URL != tmdb.ImageURL("/b.jpg", "w1280") {
		t.Errorf("horizontal movie: %+v %v", c, err)
	}
	opts.Orientation = Vertical
	c, err = Find(ctx, opts, Item{Title: "Backdropped", Year: 2001, Kind: "movie"})
	if err != nil || c.URL != tmdb.ImageURL("/p.jpg", "w780") {
		t.Errorf("vertical movie: %+v %v", c, err)
	}
	// A show with only a poster still yields art in horizontal mode (poster
	// is the second choice once Netflix has nothing).
	opts.Orientation = Horizontal
	c, err = Find(ctx, opts, Item{Title: "Postered", Year: 2010, Kind: "tv"})
	if err != nil || c.Source != "tmdb" || c.URL != tmdb.ImageURL("/tvp.jpg", "w780") {
		t.Errorf("poster-only show: %+v %v", c, err)
	}
	if _, err := Find(ctx, opts, Item{Title: "", Kind: "movie"}); err != ErrNoArtwork {
		t.Errorf("empty title: %v", err)
	}
	if _, err := Find(ctx, Options{}, Item{Title: "x", Kind: "movie"}); err != ErrNoArtwork {
		t.Errorf("no sources: %v", err)
	}
}

func TestItemForAndSidecar(t *testing.T) {
	if it := ItemFor(&plex.MediaItem{Type: "episode", ParentTitle: "Severance", Year: 2022, Title: "Good News"}); it != (Item{Title: "Severance", Year: 2022, Kind: "tv"}) {
		t.Errorf("episode item: %+v", it)
	}
	if it := ItemFor(&plex.MediaItem{Type: "movie", Title: "Heat", Year: 1995}); it != (Item{Title: "Heat", Year: 1995, Kind: "movie"}) {
		t.Errorf("movie item: %+v", it)
	}

	dir := t.TempDir()
	art := filepath.Join(dir, "art.jpg")
	if err := os.WriteFile(art, []byte("jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	show := filepath.Join(dir, "Severance")
	if err := os.MkdirAll(filepath.Join(show, "Season 01"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := showFolder(filepath.Join(show, "Season 01", "ep.mkv"), "Severance"); got != show {
		t.Errorf("showFolder via season folder = %q", got)
	}
	if got := showFolder(filepath.Join(show, "ep.mkv"), "Severance"); got != show {
		t.Errorf("showFolder flat = %q", got)
	}
	if got := showFolder(filepath.Join(dir, "Downloads", "ep.mkv"), "Severance"); got != "" {
		t.Errorf("unrelated folder = %q", got)
	}
	wrote, err := WriteSidecar(show, art)
	if err != nil || !wrote {
		t.Fatalf("WriteSidecar: %v %v", wrote, err)
	}
	if b, _ := os.ReadFile(filepath.Join(show, "poster.jpg")); string(b) != "jpeg" {
		t.Error("poster.jpg content")
	}
	wrote, _ = WriteSidecar(show, art)
	if wrote {
		t.Error("second WriteSidecar should not overwrite")
	}
}

func TestFetchCaches(t *testing.T) {
	// An image server standing in for Apple's CDN, reached through a
	// candidate injected via a fake Apple client.
	img := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("\xff\xd8\xff\xe0 fake jpeg"))
	}))
	defer img.Close()
	old := embedHTTPClient
	embedHTTPClient = img.Client()
	defer func() { embedHTTPClient = old }()

	apple := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"canvas":{"shelves":[{"items":[{"id":"m1","type":"Movie","title":"Heat","releaseDate":818035200000,"images":{"coverArt16X9":{"width":3840,"height":2160,"url":"` + img.URL + `/wide/{w}x{h}.{f}"}}}]}]}}}`))
	}))
	defer apple.Close()
	ac := NewAppleClient("")
	ac.BaseURL = apple.URL

	opts := Options{Apple: ac, CacheDir: t.TempDir()}
	path1, cand, err := Fetch(context.Background(), opts, Item{Title: "Heat", Year: 1995, Kind: "movie"})
	if err != nil || cand.Source != "apple" {
		t.Fatalf("Fetch: %v %+v", err, cand)
	}
	apple.Close() // a second fetch must not need the catalog
	path2, cand2, err := Fetch(context.Background(), opts, Item{Title: "heat", Year: 1995, Kind: "movie"})
	if err != nil || path2 != path1 || cand2.Source != "apple" {
		t.Errorf("cached Fetch: %v %q vs %q %+v", err, path2, path1, cand2)
	}
}
