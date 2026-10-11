package library

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joshkerr/goplexcli/internal/config"
	"github.com/joshkerr/goplexcli/internal/libserver"
	"github.com/joshkerr/goplexcli/internal/plex"
	"github.com/joshkerr/goplexcli/internal/tmdb"
)

// fakeTMDB serves just enough of the TMDB API for the fixtures below.
func fakeTMDB(t *testing.T, hits *int32) *httptest.Server {
	t.Helper()
	write := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/find/tt16270280":
			write(w, map[string]any{"movie_results": []map[string]any{{"id": 1001}}, "tv_results": []any{}})
		case r.URL.Path == "/find/tt31806037":
			write(w, map[string]any{"movie_results": []any{}, "tv_results": []map[string]any{{"id": 2001}}})
		case r.URL.Path == "/search/movie":
			if strings.Contains(strings.ToLower(q.Get("query")), "burbs") {
				write(w, map[string]any{"results": []map[string]any{{"id": 1002, "title": "The 'Burbs", "release_date": "1989-02-17"}}})
				return
			}
			write(w, map[string]any{"results": []any{}})
		case r.URL.Path == "/search/tv":
			if strings.Contains(strings.ToLower(q.Get("query")), "witches") {
				write(w, map[string]any{"results": []map[string]any{{"id": 2002, "name": "A Discovery of Witches", "first_air_date": "2018-09-14"}}})
				return
			}
			write(w, map[string]any{"results": []any{}})
		case r.URL.Path == "/movie/1001":
			write(w, map[string]any{
				"id": 1001, "title": "A '90s Christmas", "original_title": "A '90s Christmas", "overview": "Back to 1999.",
				"release_date": "2024-11-01", "runtime": 84, "vote_average": 6.4, "poster_path": "/p1001.jpg", "backdrop_path": "/b1001.jpg",
				"genres":               []map[string]any{{"name": "Romance"}, {"name": "Comedy"}},
				"production_companies": []map[string]any{{"name": "Vortex Productions"}},
				"credits": map[string]any{
					"cast": []map[string]any{{"name": "Eva Bourne"}, {"name": "Chandler Massey"}},
					"crew": []map[string]any{{"name": "Marni Banack", "job": "Director"}},
				},
				"release_dates": map[string]any{"results": []map[string]any{{"iso_3166_1": "US", "release_dates": []map[string]any{{"certification": "TV-G"}}}}},
			})
		case r.URL.Path == "/movie/1002":
			write(w, map[string]any{"id": 1002, "title": "The 'Burbs", "overview": "Suburbia.", "release_date": "1989-02-17", "runtime": 101, "vote_average": 6.9, "poster_path": "/p1002.jpg"})
		case r.URL.Path == "/tv/2001":
			write(w, map[string]any{
				"id": 2001, "name": "Adolescence", "overview": "A family.", "first_air_date": "2025-03-13", "episode_run_time": []int{60},
				"vote_average": 8.1, "poster_path": "/p2001.jpg", "genres": []map[string]any{{"name": "Drama"}},
				"networks": []map[string]any{{"name": "Netflix"}}, "created_by": []map[string]any{{"name": "Jack Thorne"}},
				"credits":         map[string]any{"cast": []map[string]any{{"name": "Stephen Graham"}}},
				"content_ratings": map[string]any{"results": []map[string]any{{"iso_3166_1": "US", "rating": "TV-MA"}}},
			})
		case r.URL.Path == "/tv/2001/season/1":
			write(w, map[string]any{"episodes": []map[string]any{
				{"episode_number": 1, "season_number": 1, "name": "Episode 1", "overview": "Arrest.", "air_date": "2025-03-13", "still_path": "/s1.jpg", "runtime": 65, "vote_average": 8.3},
				{"episode_number": 2, "season_number": 1, "name": "Episode 2", "overview": "School.", "air_date": "2025-03-13", "still_path": "/s2.jpg"},
			}})
		case r.URL.Path == "/tv/2002":
			write(w, map[string]any{"id": 2002, "name": "A Discovery of Witches", "first_air_date": "2018-09-14", "poster_path": "/p2002.jpg"})
		case strings.HasPrefix(r.URL.Path, "/tv/2002/season/"):
			write(w, map[string]any{"episodes": []any{}})
		default:
			w.WriteHeader(http.StatusNotFound)
			write(w, map[string]any{"status_code": 34})
		}
	}))
}

func touch(t *testing.T, root string, rel string, mod time.Time) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func fixtureLibraries(t *testing.T) (movies, shows config.LibraryFolder) {
	t.Helper()
	root := t.TempDir()
	mod := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	m := filepath.Join(root, "Movies")
	touch(t, m, "Movies-Christmas/A '90s Christmas (2024) [imdb-tt16270280]/A.'90s.Christmas.2024.1080p.WEB-DL.mkv", mod)
	touch(t, m, "Movies-Classic/The 'Burbs (1989)/The.'Burbs.1989.1080p.BluRay.mkv", mod)
	touch(t, m, "Movies-Classic/The 'Burbs (1989)/The.'Burbs.1989-trailer.mkv", mod)
	touch(t, m, "Movies-Classic/The 'Burbs (1989)/poster.jpg", mod)
	touch(t, m, "Unknown Thing (2001)/Unknown.Thing.2001.720p.mkv", mod)
	touch(t, m, "Sample/sample.mkv", mod)
	s := filepath.Join(root, "TV")
	touch(t, s, "TV/Adolescence (2025) [imdb-tt31806037]/Season 01/Adolescence.S01E01.NF.1080p.mkv", mod)
	touch(t, s, "TV/Adolescence (2025) [imdb-tt31806037]/Season 01/Adolescence.S01E02.NF.1080p.mkv", mod)
	touch(t, s, "TV/A Discovery of Witches/Season 03/A.Discovery.of.Witches.S03E01.mkv", mod)
	touch(t, s, "Loose.Show.S02E05.mkv", mod)
	return config.LibraryFolder{Name: "Movies", Path: m, Type: "movie", Remote: "gdrive:Media/Movies", Enabled: true},
		config.LibraryFolder{Name: "TV", Path: s, Type: "show", Enabled: true}
}

func byTitle(items []plex.MediaItem, title string) *plex.MediaItem {
	for i := range items {
		if items[i].Title == title {
			return &items[i]
		}
	}
	return nil
}

func TestIndexFixtures(t *testing.T) {
	var hits int32
	srv := fakeTMDB(t, &hits)
	defer srv.Close()
	client := tmdb.New("k", nil)
	client.SetBaseURL(srv.URL)
	client.SetRateLimit(0)

	movies, shows := fixtureLibraries(t)
	opts := Options{
		TMDB:       client,
		ServerName: "vault",
		ServerURL:  "http://vault:47822",
		Logf:       t.Logf,
		Scan:       ScanOptions{NoRclone: true},
	}
	res, err := Index(context.Background(), []config.LibraryFolder{movies, shows}, nil, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	items := res.Items
	if len(items) != 7 {
		for _, it := range items {
			t.Logf("%s %q %s", it.Type, it.Title, it.FilePath)
		}
		t.Fatalf("got %d items, want 7 (3 movies + 4 episodes)", len(items))
	}
	if res.Unmatched != 2 { // Unknown Thing, Loose Show
		t.Errorf("unmatched = %d, want 2", res.Unmatched)
	}

	xmas := byTitle(items, "A '90s Christmas")
	if xmas == nil {
		t.Fatal("A '90s Christmas not indexed")
	}
	if xmas.Type != "movie" || xmas.Year != 2024 || xmas.Summary != "Back to 1999." || xmas.Duration != 84*60000 ||
		xmas.Genre != "Romance, Comedy" || xmas.Director != "Marni Banack" || xmas.Cast != "Eva Bourne, Chandler Massey" ||
		xmas.ContentRating != "TV-G" || xmas.Studio != "Vortex Productions" || xmas.OriginallyAired != "2024-11-01" || xmas.Rating != 6.4 {
		t.Errorf("movie metadata wrong: %+v", *xmas)
	}
	if xmas.Thumb != "/local/tmdb/w500/p1001.jpg" {
		t.Errorf("Thumb = %q", xmas.Thumb)
	}
	if !strings.HasPrefix(xmas.Key, libserver.KeyPrefix) || xmas.ServerName != "vault" || xmas.ServerURL != "http://vault:47822" {
		t.Errorf("identity wrong: %+v", *xmas)
	}
	if xmas.RclonePath != "gdrive:Media/Movies/Movies-Christmas/A '90s Christmas (2024) [imdb-tt16270280]/A.'90s.Christmas.2024.1080p.WEB-DL.mkv" {
		t.Errorf("RclonePath = %q", xmas.RclonePath)
	}
	if xmas.AddedAt != time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC).Unix() {
		t.Errorf("AddedAt = %d", xmas.AddedAt)
	}
	if b := byTitle(items, "The 'Burbs"); b == nil || b.Year != 1989 || b.Summary != "Suburbia." {
		t.Errorf("search-matched movie wrong: %+v", b)
	}
	if u := byTitle(items, "Unknown Thing"); u == nil || u.Year != 2001 || u.Thumb != "" {
		t.Errorf("unmatched movie should keep parsed name: %+v", u)
	}

	ep := byTitle(items, "Episode 2")
	if ep == nil {
		t.Fatal("episode 2 not indexed")
	}
	if ep.Type != "episode" || ep.ParentTitle != "Adolescence" || ep.ParentIndex != 1 || ep.Index != 2 || ep.GrandTitle != "Season 1" ||
		ep.Summary != "School." || ep.Year != 2025 || ep.ContentRating != "TV-MA" || ep.Genre != "Drama" || ep.Studio != "Netflix" ||
		ep.Duration != 60*60000 || ep.Thumb != "/local/tmdb/w500/s2.jpg" || ep.GrandparentThumb != "/local/tmdb/w500/p2001.jpg" {
		t.Errorf("episode metadata wrong: %+v", *ep)
	}
	var e1 *plex.MediaItem
	for i := range items {
		if items[i].ParentTitle == "Adolescence" && items[i].Index == 1 {
			e1 = &items[i]
		}
	}
	if e1 == nil || e1.Duration != 65*60000 || e1.Rating != 8.3 {
		t.Errorf("episode runtime/rating wrong: %+v", e1)
	}
	var witches, loose *plex.MediaItem
	for i := range items {
		switch items[i].ParentTitle {
		case "A Discovery of Witches":
			witches = &items[i]
		case "Loose Show":
			loose = &items[i]
		}
	}
	if witches == nil || witches.ParentIndex != 3 || witches.Index != 1 || witches.Year != 2018 || witches.GrandparentThumb == "" {
		t.Errorf("show without year folder wrong: %+v", witches)
	}
	if loose == nil || loose.ParentIndex != 2 || loose.Index != 5 || loose.Title != "Episode 5" {
		t.Errorf("loose episode wrong: %+v", loose)
	}

	// Second run: nothing changed, so everything is reused and TMDB is not hit.
	before := atomic.LoadInt32(&hits)
	for i := range items {
		if items[i].Title == "A '90s Christmas" {
			items[i].ViewOffset = 1234
		}
	}
	res2, err := Index(context.Background(), []config.LibraryFolder{movies, shows}, items, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Reused != 5 { // the two unmatched items get another lookup
		t.Errorf("reused = %d, want 5", res2.Reused)
	}
	if got := byTitle(res2.Items, "A '90s Christmas"); got == nil || got.ViewOffset != 1234 {
		t.Errorf("reused item lost progress: %+v", got)
	}
	// Only the two unmatched titles should have been looked up again.
	if after := atomic.LoadInt32(&hits); after-before > 4 {
		t.Errorf("second run made %d TMDB requests", after-before)
	}

	// Force: re-enriched but progress carried.
	force := opts
	force.Force = true
	res3, err := Index(context.Background(), []config.LibraryFolder{movies, shows}, items, force, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res3.Reused != 0 {
		t.Errorf("force reused %d", res3.Reused)
	}
	if got := byTitle(res3.Items, "A '90s Christmas"); got == nil || got.ViewOffset != 1234 || got.Summary == "" {
		t.Errorf("forced item wrong: %+v", got)
	}
}

func TestReplaceLocalAndSort(t *testing.T) {
	existing := []plex.MediaItem{
		{Key: "/library/metadata/1", ServerName: "plex", Title: "P"},
		{Key: libserver.ItemKey("a"), ServerName: "vault", Title: "Old"},
	}
	fresh := []plex.MediaItem{{Key: libserver.ItemKey("b"), ServerName: "vault", Title: "New"}}
	out := ReplaceLocal(existing, fresh, "vault")
	if len(out) != 2 || out[0].Title != "P" || out[1].Title != "New" {
		t.Errorf("ReplaceLocal = %+v", out)
	}
	items := []plex.MediaItem{
		{Type: "episode", ParentTitle: "B", ParentIndex: 1, Index: 2},
		{Type: "movie", Title: "Z"},
		{Type: "episode", ParentTitle: "B", ParentIndex: 1, Index: 1},
		{Type: "movie", Title: "A"},
	}
	SortItems(items)
	if items[0].Title != "B" && items[0].Type != "episode" {
		t.Errorf("sort: %+v", items)
	}
	if items[0].Index != 1 || items[1].Index != 2 || items[2].Title != "A" || items[3].Title != "Z" {
		t.Errorf("sort order: %+v", items)
	}
}

func TestApplyTimeline(t *testing.T) {
	now := time.Unix(1000, 0)
	it := plex.MediaItem{Duration: 100000}
	if !ApplyTimeline(&it, "playing", 40000, 100000, now) || it.ViewOffset != 40000 || it.LastViewedAt != 1000 {
		t.Errorf("playing: %+v", it)
	}
	if !ApplyTimeline(&it, "stopped", 95000, 0, now) || it.ViewOffset != 0 || it.ViewCount != 1 {
		t.Errorf("stopped near end: %+v", it)
	}
	if ApplyTimeline(&it, "bogus", 1, 1, now) {
		t.Error("bogus state applied")
	}
}

func TestRclonePath(t *testing.T) {
	f := ScannedFile{Path: "/mnt/g/Movies/x.mkv", Rel: "Movies/x.mkv"}
	if got := rclonePath(config.LibraryFolder{Remote: "g:"}, f, nil); got != "g:Movies/x.mkv" {
		t.Errorf("bare remote: %q", got)
	}
	if got := rclonePath(config.LibraryFolder{Remote: "g:Media/"}, f, nil); got != "g:Media/Movies/x.mkv" {
		t.Errorf("remote with path: %q", got)
	}
	maps := []config.PathMapping{{Prefix: "/mnt/", Remote: "m:"}, {Prefix: "/mnt/g/", Remote: "g:"}}
	if got := rclonePath(config.LibraryFolder{}, f, maps); got != "g:Movies/x.mkv" {
		t.Errorf("longest mapping: %q", got)
	}
	if got := rclonePath(config.LibraryFolder{}, f, nil); got != "" {
		t.Errorf("no mapping: %q", got)
	}
}

func TestParseLsjson(t *testing.T) {
	in := `[{"Path":"Movies/A (2001)/a.mkv","Name":"a.mkv","Size":10,"ModTime":"2026-01-02T03:04:05Z","IsDir":false},
{"Path":"Movies/A (2001)","Name":"A (2001)","Size":-1,"ModTime":"2026-01-02T03:04:05Z","IsDir":true},
{"Path":"Movies/A (2001)/poster.jpg","Name":"poster.jpg","Size":1,"ModTime":"2026-01-02T03:04:05Z","IsDir":false},
{"Path":"Samples/b.mkv","Name":"b.mkv","Size":10,"ModTime":"2026-01-02T03:04:05Z","IsDir":false},
{"Path":"Movies/A (2001)/a.mkv","Name":"a.mkv","Size":11,"ModTime":"2026-01-02T03:04:05Z","IsDir":false}]`
	files, err := parseLsjson(strings.NewReader(in), config.LibraryFolder{Path: "/mnt/g"}, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Rel != "Movies/A (2001)/a.mkv" || files[0].Path != filepath.Join("/mnt/g", "Movies", "A (2001)", "a.mkv") || files[0].Size != 10 {
		t.Errorf("parseLsjson = %+v", files)
	}
}
