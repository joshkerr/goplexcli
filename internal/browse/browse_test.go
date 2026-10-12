package browse

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"path/filepath"
	"sync"
	"testing"

	"github.com/joshkerr/goplexcli/internal/favorites"
	"github.com/joshkerr/goplexcli/internal/libserver"
	"github.com/joshkerr/goplexcli/internal/plex"
)

func fixture() []plex.MediaItem {
	ep := func(id, show string, season, n int64, added int64) plex.MediaItem {
		return plex.MediaItem{
			Key: libserver.ItemKey(id), Type: "episode", Title: "Ep " + id, ParentTitle: show,
			ParentIndex: season, Index: n, AddedAt: added, Year: 2020, Duration: 1000,
			Genre: "Drama", GrandparentThumb: "/local/tmdb/w500/" + show + ".jpg", Thumb: "/local/tmdb/w500/still" + id + ".jpg",
			RclonePath: "remote:" + id, ServerName: "vault",
		}
	}
	return []plex.MediaItem{
		{Key: libserver.ItemKey("m1"), Type: "movie", Title: "Heat", Year: 1995, AddedAt: 10, Rating: 8.3, Duration: 10000, Genre: "Crime, Drama", Director: "Michael Mann", Cast: "Al Pacino, Robert De Niro", Thumb: "/local/tmdb/w500/heat.jpg", RclonePath: "remote:heat.mkv", ServerName: "vault"},
		{Key: libserver.ItemKey("m2"), Type: "movie", Title: "Alien", Year: 1979, AddedAt: 30, Rating: 8.5, Duration: 7000, Genre: "Horror", Thumb: "/local/tmdb/w500/alien.jpg", ServerName: "vault", ViewOffset: 3500, LastViewedAt: 50},
		{Key: libserver.ItemKey("m3"), Type: "movie", Title: "Aliens", Year: 1986, AddedAt: 20, Rating: 8.4, Duration: 9000, Genre: "Horror, Action", ServerName: "vault"},
		{Key: "/library/metadata/9", Type: "movie", Title: "Plexed", Year: 2001, AddedAt: 5, Thumb: "/library/metadata/9/thumb", ServerName: "plex"},
		ep("e1", "Severance", 1, 1, 100),
		ep("e2", "Severance", 1, 2, 101),
		ep("e3", "Severance", 2, 1, 300),
		ep("e0", "Severance", 0, 1, 50),
		ep("x1", "The Office", 1, 1, 200),
	}
}

func TestBrowseCategories(t *testing.T) {
	items := fixture()
	items[5].ViewCount = 1 // Severance S1E2 watched
	ix := NewIndex(items, nil)
	fav := map[string]bool{libserver.ItemKey("m1"): true, "show:The Office": true}

	if st := ix.Status(); st.Movies != 4 || st.Shows != 2 || st.Episodes != 5 || st.Count != 9 {
		t.Errorf("status = %+v", st)
	}

	p := ix.Browse(Query{Category: "movies"}, fav)
	if p.Total != 4 || p.Items[0].Title != "Alien" || p.Items[1].Title != "Aliens" || p.Items[2].Title != "Heat" {
		t.Errorf("movies by title: %+v", p)
	}
	if !p.Items[2].Favorite || p.Items[2].Thumb != "/local/tmdb/w500/heat.jpg" {
		t.Errorf("heat card: %+v", p.Items[2])
	}
	if p.Items[3].Title != "Plexed" || p.Items[3].Thumb != "" {
		t.Errorf("non-local items carry no thumb path: %+v", p.Items[3])
	}
	p = ix.Browse(Query{Category: "movies", Sort: "rating", Desc: true, Limit: 2, Offset: 1}, fav)
	if p.Total != 4 || len(p.Items) != 2 || p.Items[0].Title != "Aliens" || p.Items[1].Title != "Heat" {
		t.Errorf("rating desc page 2: %+v", p)
	}
	p = ix.Browse(Query{Category: "movies", Genre: "horror"}, fav)
	if p.Total != 2 {
		t.Errorf("genre filter: %+v", p)
	}
	if p := ix.Browse(Query{Category: "favorite-movies"}, fav); p.Total != 1 || p.Items[0].Title != "Heat" {
		t.Errorf("favorite movies: %+v", p)
	}

	p = ix.Browse(Query{Category: "shows"}, fav)
	if p.Total != 2 || p.Items[0].Title != "Severance" || p.Items[0].EpisodeCount != 4 || p.Items[0].Key != "show:Severance" || p.Items[0].Thumb != "/local/tmdb/w500/Severance.jpg" {
		t.Errorf("shows newest first: %+v", p)
	}
	p = ix.Browse(Query{Category: "shows", Sort: "title"}, fav)
	if p.Items[0].Title != "Severance" || p.Items[1].Title != "The Office" {
		t.Errorf("shows by title: %+v", p)
	}
	if p := ix.Browse(Query{Category: "favorite-shows"}, fav); p.Total != 1 || p.Items[0].Title != "The Office" || !p.Items[0].Favorite {
		t.Errorf("favorite shows: %+v", p)
	}
	p = ix.Browse(Query{Category: "recent-shows"}, fav)
	if p.Total != 2 || p.Items[0].Title != "Severance" || p.Items[0].NewCount != 4 {
		t.Errorf("recent shows: %+v", p)
	}
	p = ix.Browse(Query{Category: "recent-movies"}, fav)
	if p.Items[0].Title != "Alien" {
		t.Errorf("recent movies: %+v", p)
	}
	p = ix.Browse(Query{Category: "continue"}, fav)
	if p.Total != 1 || p.Items[0].Title != "Alien" || p.Items[0].ProgressPct != 50 {
		t.Errorf("continue: %+v", p)
	}
	if p := ix.Browse(Query{Category: "nope"}, fav); p.Total != 0 || p.Items == nil {
		t.Errorf("unknown category: %+v", p)
	}
}

func TestShowSeasonsEpisodesNextUp(t *testing.T) {
	items := fixture()
	items[4].ViewCount = 1 // S1E1 watched
	items[5].ViewCount = 1 // S1E2 watched
	ix := NewIndex(items, nil)
	s, ok := ix.Show("Severance", nil)
	if !ok {
		t.Fatal("show missing")
	}
	if len(s.Seasons) != 3 || s.Seasons[0].Season != 1 || s.Seasons[0].WatchedCount != 2 || s.Seasons[2].Season != 0 || s.Seasons[2].Title != "Specials" {
		t.Errorf("seasons: %+v", s.Seasons)
	}
	if s.NextUp == nil || s.NextUp.Key != libserver.ItemKey("e3") {
		t.Errorf("next up after S1 watched should be S2E1: %+v", s.NextUp)
	}
	items[6].ViewOffset = 400 // S2E1 in progress
	items[6].LastViewedAt = 9
	if s, _ := ix.Show("Severance", nil); s.NextUp == nil || s.NextUp.ViewOffset != 400 {
		t.Errorf("next up should resume the in-progress episode: %+v", s.NextUp)
	}
	eps := ix.Episodes("Severance", 1, nil)
	if len(eps) != 2 || eps[0].Index != 1 || eps[1].Index != 2 {
		t.Errorf("episodes: %+v", eps)
	}
	e := eps[0]
	if !e.Playable || e.StreamPath != libserver.FilePrefix+"e1" || e.RclonePath != "remote:e1" || e.ShowThumb != "/local/tmdb/w500/Severance.jpg" {
		t.Errorf("episode media: %+v", e)
	}
	if m, ok := ix.Item("/library/metadata/9", nil); !ok || m.Playable || m.StreamPath != "" {
		t.Errorf("plex item should not be playable here: %+v", m)
	}
	if _, ok := ix.Show("Nope", nil); ok {
		t.Error("missing show found")
	}
}

func TestSearch(t *testing.T) {
	ix := NewIndex(fixture(), nil)
	res := ix.Search("alien", 10, nil)
	if len(res) < 2 || res[0].Title != "Alien" || res[1].Title != "Aliens" {
		t.Errorf("exact then prefix: %+v", res)
	}
	if res := ix.Search("sevrance", 10, nil); len(res) == 0 || res[0].Title != "Severance" || res[0].Type != "show" {
		t.Errorf("fuzzy show: %+v", res)
	}
	if res := ix.Search(`director:"Michael Mann"`, 10, nil); len(res) != 1 || res[0].Title != "Heat" {
		t.Errorf("director query: %+v", res)
	}
	if res := ix.Search("cast:robert de niro", 10, nil); len(res) != 1 {
		t.Errorf("cast query: %+v", res)
	}
	if res := ix.Search("  ", 10, nil); len(res) != 0 || res == nil {
		t.Errorf("blank query: %+v", res)
	}
	if g := ix.Genres(); len(g) == 0 || g[0] != "Drama" && g[0] != "Horror" {
		t.Errorf("genres: %v", g)
	}
}

// fakeSource guards items with a mutex like the daemon does.
type fakeSource struct {
	mu    sync.Mutex
	items []plex.MediaItem
}

func (f *fakeSource) view(fn func([]plex.MediaItem)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f.items)
}

func (f *fakeSource) setWatched(key string, watched bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.items {
		if f.items[i].Key == key {
			if watched {
				f.items[i].ViewCount = 1
			} else {
				f.items[i].ViewCount = 0
			}
			return nil
		}
	}
	return ErrNotFound
}

func TestHandler(t *testing.T) {
	src := &fakeSource{items: fixture()}
	store := favorites.NewStoreAt(filepath.Join(t.TempDir(), "fav.json"))
	h := NewHandler(Source{View: src.view, Favorites: store, SetWatched: src.setWatched, Name: "vault", Version: "test"})
	srv := httptest.NewServer(h)
	defer srv.Close()

	get := func(path string, out any) int {
		t.Helper()
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if out != nil {
			_ = json.NewDecoder(resp.Body).Decode(out)
		}
		return resp.StatusCode
	}
	post := func(path string, body any, out any) int {
		t.Helper()
		b, _ := json.Marshal(body)
		resp, err := http.Post(srv.URL+path, "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if out != nil {
			_ = json.NewDecoder(resp.Body).Decode(out)
		}
		return resp.StatusCode
	}

	var status struct {
		Name     string   `json:"name"`
		API      int      `json:"api"`
		Status   Status   `json:"status"`
		Features []string `json:"features"`
	}
	if code := get("/api/v1/status", &status); code != 200 || status.Name != "vault" || status.API != 1 || status.Status.Movies != 4 || len(status.Features) < 6 {
		t.Errorf("status %d %+v", code, status)
	}
	var page Page
	if code := get("/api/v1/browse?category=movies&sort=year&desc=1&limit=2", &page); code != 200 || page.Total != 4 || len(page.Items) != 2 || page.Items[0].Title != "Plexed" {
		t.Errorf("browse %d %+v", code, page)
	}
	if code := get("/api/v1/browse?category=bogus", nil); code != 400 {
		t.Errorf("bad category %d", code)
	}
	var show Show
	if code := get("/api/v1/item?key="+urlQuery("show:Severance"), &show); code != 200 || show.Title != "Severance" || len(show.Seasons) != 3 {
		t.Errorf("show via item %d %+v", code, show)
	}
	var m Media
	if code := get("/api/v1/item?key="+urlQuery(libserver.ItemKey("m1")), &m); code != 200 || m.Title != "Heat" || !m.Playable {
		t.Errorf("item %d %+v", code, m)
	}
	if code := get("/api/v1/item?key=nope", nil); code != 404 {
		t.Errorf("missing item %d", code)
	}
	var eps struct{ Items []Media }
	if code := get("/api/v1/episodes?show=Severance&season=1", &eps); code != 200 || len(eps.Items) != 2 {
		t.Errorf("episodes %d %+v", code, eps)
	}
	var search struct{ Items []Card }
	if code := get("/api/v1/search?q=heat", &search); code != 200 || len(search.Items) == 0 || search.Items[0].Title != "Heat" {
		t.Errorf("search %d %+v", code, search)
	}

	// Favorites round trip through the shared store.
	if code := post("/api/v1/favorite", map[string]any{"key": "show:Severance", "favorite": true}, nil); code != 200 {
		t.Errorf("favorite %d", code)
	}
	if code := get("/api/v1/browse?category=favorite-shows", &page); code != 200 || page.Total != 1 || !page.Items[0].Favorite {
		t.Errorf("favorite shows after toggle %+v", page)
	}
	post("/api/v1/favorite", map[string]any{"key": "show:Severance", "favorite": true}, nil) // idempotent
	if snap, _ := store.Snapshot(); !snap["show:Severance"] {
		t.Error("favorite not idempotent")
	}
	post("/api/v1/favorite", map[string]any{"key": "show:Severance", "favorite": false}, nil)
	if snap, _ := store.Snapshot(); snap["show:Severance"] {
		t.Error("unfavorite failed")
	}

	// Watched toggle is reflected live (same index, in-place update).
	if code := post("/api/v1/watched", map[string]any{"key": libserver.ItemKey("e1"), "watched": true}, nil); code != 200 {
		t.Errorf("watched %d", code)
	}
	get("/api/v1/item?key="+urlQuery(libserver.ItemKey("e1")), &m)
	if m.ViewCount != 1 {
		t.Errorf("watched not reflected: %+v", m)
	}
	if code := post("/api/v1/watched", map[string]any{"key": "nope", "watched": true}, nil); code != 404 {
		t.Errorf("watched unknown %d", code)
	}

	// Replacing the cache rebuilds the index.
	src.mu.Lock()
	src.items = src.items[:3]
	src.mu.Unlock()
	if get("/api/v1/status", &status); status.Status.Count != 3 {
		t.Errorf("index not rebuilt: %+v", status.Status)
	}

	// CORS preflight.
	req, _ := http.NewRequest(http.MethodOptions, srv.URL+"/api/v1/browse", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 204 || resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("preflight %d %v", resp.StatusCode, resp.Header)
	}
}

func urlQuery(s string) string { return neturl.QueryEscape(s) }
