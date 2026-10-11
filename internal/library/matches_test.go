package library

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshkerr/goplexcli/internal/config"
	"github.com/joshkerr/goplexcli/internal/plex"
	"github.com/joshkerr/goplexcli/internal/tmdb"
)

// rematchTMDB serves two movies (1002 wrong, 1003 right) and two shows
// (2001 wrong, 2003 right) plus searches that list both.
func rematchTMDB(t *testing.T) *httptest.Server {
	t.Helper()
	write := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/movie":
			write(w, map[string]any{"results": []map[string]any{
				{"id": 1002, "title": "The Thing (wrong)", "release_date": "1982-06-25", "overview": "wrong", "poster_path": "/w.jpg"},
				{"id": 1003, "title": "The Thing", "release_date": "2011-10-14", "overview": "right", "poster_path": "/r.jpg", "vote_average": 6.1},
			}})
		case "/search/tv":
			write(w, map[string]any{"results": []map[string]any{
				{"id": 2001, "name": "Accused (UK)", "first_air_date": "2010-11-15"},
				{"id": 2003, "name": "Accused", "first_air_date": "2023-01-22", "overview": "US"},
			}})
		case "/movie/1002":
			write(w, map[string]any{"id": 1002, "title": "The Thing (wrong)", "release_date": "1982-06-25", "poster_path": "/w.jpg", "overview": "wrong"})
		case "/movie/1003":
			write(w, map[string]any{"id": 1003, "title": "The Thing", "release_date": "2011-10-14", "poster_path": "/r.jpg", "overview": "right"})
		case "/tv/2001":
			write(w, map[string]any{"id": 2001, "name": "Accused (UK)", "first_air_date": "2010-11-15", "poster_path": "/uk.jpg"})
		case "/tv/2003":
			write(w, map[string]any{"id": 2003, "name": "Accused", "first_air_date": "2023-01-22", "poster_path": "/us.jpg"})
		case "/tv/2001/season/1", "/tv/2003/season/1":
			id := "UK"
			if strings.HasPrefix(r.URL.Path, "/tv/2003") {
				id = "US"
			}
			write(w, map[string]any{"episodes": []map[string]any{
				{"episode_number": 1, "season_number": 1, "name": "Pilot " + id},
				{"episode_number": 2, "season_number": 1, "name": "Two " + id},
			}})
		default:
			w.WriteHeader(404)
			write(w, map[string]any{"status_code": 34})
		}
	}))
}

func TestRematchMovieAndShow(t *testing.T) {
	srv := rematchTMDB(t)
	defer srv.Close()
	cacheDir := t.TempDir()
	root := t.TempDir()
	mod := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	TMDBBaseURL = srv.URL
	defer func() { TMDBBaseURL = "" }()
	touch(t, filepath.Join(root, "Movies"), "The Thing (1982)/The.Thing.1982.1080p.mkv", mod)
	touch(t, filepath.Join(root, "TV"), "Accused (US)/Season 01/Accused.US.S01E01.mkv", mod)
	touch(t, filepath.Join(root, "TV"), "Accused (US)/Season 01/Accused.US.S01E02.mkv", mod)

	cfg := &config.Config{
		TMDBAPIKey:  "k",
		LibraryName: "vault",
		LibraryURL:  "http://vault:47822",
		Libraries: []config.LibraryFolder{
			{Name: "Movies", Path: filepath.Join(root, "Movies"), Type: "movie", Enabled: true},
			{Name: "TV", Path: filepath.Join(root, "TV"), Type: "show", Enabled: true},
		},
	}
	// Point the indexer's client at the fake server: Run builds its own
	// client, so index via Index with a prepared client instead.
	client := tmdb.New("k", nil)
	client.SetBaseURL(srv.URL)
	client.SetRateLimit(0)
	overrides, err := LoadOverrides(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{TMDB: client, ServerName: "vault", ServerURL: cfg.LibraryURL, Overrides: overrides, Scan: ScanOptions{NoRclone: true}}
	res, err := Index(context.Background(), cfg.Libraries, nil, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	items := res.Items
	movie := byTitle(items, "The Thing (wrong)")
	if movie == nil {
		t.Fatalf("expected the first search hit to be used initially; items: %+v", items)
	}
	movie.ViewOffset = 777
	var ep *plex.MediaItem
	for i := range items {
		if items[i].Type == "episode" && items[i].Index == 1 {
			ep = &items[i]
		}
	}
	if ep == nil || ep.ParentTitle != "Accused (UK)" {
		t.Fatalf("expected the show to match the first hit initially: %+v", ep)
	}

	rematch := func(key string, id int) []plex.MediaItem {
		t.Helper()
		out, err := Rematch(context.Background(), cfg, cacheDir, items, key, id, nil)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	fixed := rematch(movie.Key, 1003)
	if len(fixed) != 1 || fixed[0].Title != "The Thing" || fixed[0].Year != 2011 || fixed[0].Key != movie.Key {
		t.Errorf("movie rematch: %+v", fixed)
	}
	if fixed[0].ViewOffset != 777 {
		t.Errorf("movie rematch lost progress: %+v", fixed[0])
	}
	items = MergeByKey(items, fixed)

	fixedShow := rematch(ep.Key, 2003)
	if len(fixedShow) != 2 {
		t.Fatalf("show rematch rebuilt %d items", len(fixedShow))
	}
	for _, it := range fixedShow {
		if it.ParentTitle != "Accused" || !strings.HasSuffix(it.Title, " US") || it.GrandparentThumb != "/local/tmdb/w500/us.jpg" {
			t.Errorf("show rematch: %+v", it)
		}
	}
	items = MergeByKey(items, fixedShow)

	// A fresh scan with the override file keeps the fixes (files unchanged
	// → reused; forced → re-looked-up with the pinned ids).
	reloaded, err := LoadOverrides(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Len() != 2 {
		t.Errorf("overrides on disk = %d", reloaded.Len())
	}
	opts.Overrides = reloaded
	opts.Force = true
	res2, err := Index(context.Background(), cfg.Libraries, items, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m := byTitle(res2.Items, "The Thing"); m == nil || m.ViewOffset != 777 {
		t.Errorf("forced rescan dropped the movie override: %+v", m)
	}
	for _, it := range res2.Items {
		if it.Type == "episode" && it.ParentTitle != "Accused" {
			t.Errorf("forced rescan dropped the show override: %+v", it)
		}
	}

	// A pending (not yet applied) override forces a rebuild even without Force.
	pending, _ := LoadOverrides(cacheDir)
	pending.Set(movie.Key, Override{TMDB: 1002, Kind: "movie"})
	opts.Overrides = pending
	opts.Force = false
	res3, err := Index(context.Background(), cfg.Libraries, res2.Items, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m := byTitle(res3.Items, "The Thing (wrong)"); m == nil {
		t.Errorf("pending override was not applied on a normal scan")
	}
	if ov, _ := pending.Get(movie.Key); !ov.Applied {
		t.Errorf("override not marked applied after the scan")
	}

	// Back to automatic.
	auto := rematch(movie.Key, 0)
	if auto[0].Title != "The Thing (wrong)" {
		t.Errorf("auto rematch: %+v", auto[0])
	}

	// Candidate search.
	cands, err := SearchTitles(context.Background(), client, "movie", "The Thing", 0)
	if err != nil || len(cands) != 2 || cands[1].ID != 1003 || cands[1].Poster != "/local/tmdb/w500/r.jpg" || cands[1].Year != 2011 {
		t.Errorf("SearchTitles: %+v %v", cands, err)
	}
	tv, err := SearchTitles(context.Background(), client, "tv", "Accused", 2023)
	if err != nil || len(tv) != 2 || tv[0].Kind != "tv" {
		t.Errorf("SearchTitles tv: %+v %v", tv, err)
	}
}

func findKey(items []plex.MediaItem, key string) *plex.MediaItem {
	for i := range items {
		if items[i].Key == key {
			return &items[i]
		}
	}
	return nil
}
