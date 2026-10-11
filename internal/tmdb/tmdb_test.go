package tmdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const movieJSON = `{
  "id": 603, "imdb_id": "tt0133093", "title": "The Matrix", "original_title": "The Matrix",
  "overview": "A hacker learns the truth.", "tagline": "Welcome to the Real World.",
  "release_date": "1999-03-30", "runtime": 136, "vote_average": 8.2,
  "genres": [{"id": 28, "name": "Action"}, {"id": 878, "name": "Science Fiction"}],
  "production_companies": [{"id": 79, "name": "Village Roadshow Pictures"}, {"id": 174, "name": "Warner Bros. Pictures"}],
  "poster_path": "/matrix.jpg", "backdrop_path": "/matrix-bd.jpg",
  "credits": {
    "cast": [{"name": "Keanu Reeves", "order": 0}, {"name": "Laurence Fishburne", "order": 1}, {"name": "Carrie-Anne Moss", "order": 2}],
    "crew": [{"name": "Lana Wachowski", "job": "Director"}, {"name": "Joel Silver", "job": "Producer"}, {"name": "Lilly Wachowski", "job": "Director"}]
  },
  "release_dates": {"results": [
    {"iso_3166_1": "GB", "release_dates": [{"certification": "15"}]},
    {"iso_3166_1": "US", "release_dates": [{"certification": ""}, {"certification": "R"}]}
  ]},
  "external_ids": {"imdb_id": "tt0133093"}
}`

const tvJSON = `{
  "id": 1396, "name": "Breaking Bad", "original_name": "Breaking Bad",
  "overview": "A chemistry teacher cooks meth.", "first_air_date": "2008-01-20",
  "episode_run_time": [45, 47], "vote_average": 8.9,
  "genres": [{"name": "Drama"}, {"name": "Crime"}],
  "networks": [{"name": "AMC"}],
  "production_companies": [{"name": "Sony Pictures Television"}, {"name": "High Bridge Productions"}],
  "created_by": [{"name": "Vince Gilligan"}],
  "poster_path": "/bb.jpg", "backdrop_path": "/bb-bd.jpg",
  "seasons": [{"season_number": 0, "episode_count": 10}, {"season_number": 1, "episode_count": 7}, {"season_number": 2, "episode_count": 13}],
  "credits": {"cast": [{"name": "Bryan Cranston"}, {"name": "Aaron Paul"}], "crew": []},
  "content_ratings": {"results": [{"iso_3166_1": "DE", "rating": "16"}, {"iso_3166_1": "US", "rating": "TV-MA"}]},
  "external_ids": {"imdb_id": "tt0903747"}
}`

const seasonJSON = `{
  "season_number": 1,
  "episodes": [
    {"season_number": 1, "episode_number": 1, "name": "Pilot", "overview": "Walt turns 50.", "air_date": "2008-01-20",
     "still_path": "/s1e1.jpg", "runtime": 58, "vote_average": 8.3,
     "crew": [{"name": "Vince Gilligan", "job": "Director"}, {"name": "Someone", "job": "Writer"}]},
    {"season_number": 1, "episode_number": 2, "name": "Cat's in the Bag...", "overview": "", "air_date": "2008-01-27",
     "still_path": "/s1e2.jpg", "vote_average": 8.0}
  ]
}`

const findJSON = `{"movie_results": [{"id": 603}], "tv_results": [{"id": 1396}]}`

func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Client, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	c := New("testkey", nil)
	c.SetBaseURL(srv.URL)
	c.SetRateLimit(0)
	return srv, c, &calls
}

func cannedHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") != "testkey" {
			http.Error(w, `{"status_message":"missing key"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/find/tt0133093":
			if r.URL.Query().Get("external_source") != "imdb_id" {
				http.Error(w, "bad external_source", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(findJSON))
		case r.URL.Path == "/find/tt0000000":
			_, _ = w.Write([]byte(`{"movie_results": [], "tv_results": []}`))
		case r.URL.Path == "/search/movie":
			q := r.URL.Query()
			switch {
			case q.Get("query") == "The Matrix" && q.Get("year") == "1999":
				_, _ = w.Write([]byte(`{"results": [{"id": 1, "release_date": "2003-05-15"}, {"id": 603, "release_date": "1999-03-30"}]}`))
			case q.Get("query") == "The Matrix" && q.Get("year") == "":
				_, _ = w.Write([]byte(`{"results": [{"id": 1, "release_date": "2003-05-15"}]}`))
			case q.Get("query") == "Obscure" && q.Get("year") != "":
				_, _ = w.Write([]byte(`{"results": []}`))
			case q.Get("query") == "Obscure":
				_, _ = w.Write([]byte(`{"results": [{"id": 42, "release_date": "1980-01-01"}]}`))
			default:
				_, _ = w.Write([]byte(`{"results": []}`))
			}
		case r.URL.Path == "/search/tv":
			q := r.URL.Query()
			if q.Get("query") == "Breaking Bad" && q.Get("first_air_date_year") == "2008" {
				_, _ = w.Write([]byte(`{"results": [{"id": 9, "first_air_date": "2019-01-01"}, {"id": 1396, "first_air_date": "2008-01-20"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"results": []}`))
		case r.URL.Path == "/movie/603":
			if r.URL.Query().Get("append_to_response") != "credits,release_dates,external_ids" {
				http.Error(w, "bad append_to_response", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(movieJSON))
		case r.URL.Path == "/tv/1396":
			if r.URL.Query().Get("append_to_response") != "credits,content_ratings,external_ids" {
				http.Error(w, "bad append_to_response", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(tvJSON))
		case r.URL.Path == "/tv/1396/season/1":
			_, _ = w.Write([]byte(seasonJSON))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"status_code":34,"status_message":"The resource you requested could not be found."}`))
		}
	}
}

func TestMovie(t *testing.T) {
	_, c, _ := newTestServer(t, cannedHandler(t))
	m, err := c.Movie(context.Background(), 603)
	if err != nil {
		t.Fatal(err)
	}
	want := &Movie{
		ID: 603, IMDBID: "tt0133093", Title: "The Matrix", OriginalTitle: "The Matrix",
		Overview: "A hacker learns the truth.", Tagline: "Welcome to the Real World.",
		ReleaseDate: "1999-03-30", Year: 1999, Runtime: 136, VoteAverage: 8.2,
		Genres:        []string{"Action", "Science Fiction"},
		Studios:       []string{"Village Roadshow Pictures", "Warner Bros. Pictures"},
		Directors:     []string{"Lana Wachowski", "Lilly Wachowski"},
		Cast:          []string{"Keanu Reeves", "Laurence Fishburne", "Carrie-Anne Moss"},
		Certification: "R", PosterPath: "/matrix.jpg", BackdropPath: "/matrix-bd.jpg",
	}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("Movie mismatch:\n got %+v\nwant %+v", m, want)
	}
}

func TestMovieCastCappedAt20(t *testing.T) {
	var cast []string
	for i := 0; i < 30; i++ {
		cast = append(cast, `{"name":"Actor `+strconv.Itoa(i)+`"}`)
	}
	body := `{"id": 1, "title": "Big Cast", "credits": {"cast": [` + strings.Join(cast, ",") + `]}}`
	_, c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	})
	m, err := c.Movie(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Cast) != 20 {
		t.Errorf("Cast len = %d, want 20", len(m.Cast))
	}
	if m.Year != 0 || m.Certification != "" || m.IMDBID != "" {
		t.Errorf("unexpected defaults: %+v", m)
	}
}

func TestTV(t *testing.T) {
	_, c, _ := newTestServer(t, cannedHandler(t))
	s, err := c.TV(context.Background(), 1396)
	if err != nil {
		t.Fatal(err)
	}
	want := &Show{
		ID: 1396, IMDBID: "tt0903747", Name: "Breaking Bad", OriginalName: "Breaking Bad",
		Overview: "A chemistry teacher cooks meth.", FirstAirDate: "2008-01-20", Year: 2008,
		EpisodeRunTime: 45, VoteAverage: 8.9,
		Genres:        []string{"Drama", "Crime"},
		Studios:       []string{"AMC", "Sony Pictures Television", "High Bridge Productions"},
		Creators:      []string{"Vince Gilligan"},
		Cast:          []string{"Bryan Cranston", "Aaron Paul"},
		ContentRating: "TV-MA", PosterPath: "/bb.jpg", BackdropPath: "/bb-bd.jpg",
		Seasons: []SeasonSummary{{0, 10}, {1, 7}, {2, 13}},
	}
	if !reflect.DeepEqual(s, want) {
		t.Errorf("TV mismatch:\n got %+v\nwant %+v", s, want)
	}
}

func TestSeason(t *testing.T) {
	_, c, _ := newTestServer(t, cannedHandler(t))
	eps, err := c.Season(context.Background(), 1396, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []Episode{
		{SeasonNumber: 1, EpisodeNumber: 1, Name: "Pilot", Overview: "Walt turns 50.", AirDate: "2008-01-20",
			StillPath: "/s1e1.jpg", Runtime: 58, VoteAverage: 8.3, Directors: []string{"Vince Gilligan"}},
		{SeasonNumber: 1, EpisodeNumber: 2, Name: "Cat's in the Bag...", AirDate: "2008-01-27",
			StillPath: "/s1e2.jpg", Runtime: 0, VoteAverage: 8.0},
	}
	if !reflect.DeepEqual(eps, want) {
		t.Errorf("Season mismatch:\n got %+v\nwant %+v", eps, want)
	}
}

func TestFindByIMDB(t *testing.T) {
	_, c, _ := newTestServer(t, cannedHandler(t))
	movieID, tvID, err := c.FindByIMDB(context.Background(), "tt0133093")
	if err != nil {
		t.Fatal(err)
	}
	if movieID != 603 || tvID != 1396 {
		t.Errorf("FindByIMDB = (%d, %d), want (603, 1396)", movieID, tvID)
	}
	movieID, tvID, err = c.FindByIMDB(context.Background(), "tt0000000")
	if err != nil || movieID != 0 || tvID != 0 {
		t.Errorf("FindByIMDB empty = (%d, %d, %v), want (0, 0, nil)", movieID, tvID, err)
	}
}

func TestSearchMovie(t *testing.T) {
	_, c, calls := newTestServer(t, cannedHandler(t))
	ctx := context.Background()

	// Prefers the result whose release year matches, not the first.
	id, err := c.SearchMovie(ctx, "The Matrix", 1999)
	if err != nil || id != 603 {
		t.Errorf("SearchMovie(year=1999) = (%d, %v), want (603, nil)", id, err)
	}
	// No year: first result.
	id, err = c.SearchMovie(ctx, "The Matrix", 0)
	if err != nil || id != 1 {
		t.Errorf("SearchMovie(year=0) = (%d, %v), want (1, nil)", id, err)
	}
	// Year query yields nothing, falls back to a yearless query.
	atomic.StoreInt32(calls, 0)
	id, err = c.SearchMovie(ctx, "Obscure", 1985)
	if err != nil || id != 42 {
		t.Errorf("SearchMovie(Obscure, 1985) = (%d, %v), want (42, nil)", id, err)
	}
	if n := atomic.LoadInt32(calls); n != 2 {
		t.Errorf("fallback should make 2 requests, made %d", n)
	}
	// Nothing at all.
	id, err = c.SearchMovie(ctx, "Nope", 2000)
	if err != nil || id != 0 {
		t.Errorf("SearchMovie(Nope) = (%d, %v), want (0, nil)", id, err)
	}
}

func TestSearchTV(t *testing.T) {
	_, c, _ := newTestServer(t, cannedHandler(t))
	id, err := c.SearchTV(context.Background(), "Breaking Bad", 2008)
	if err != nil || id != 1396 {
		t.Errorf("SearchTV = (%d, %v), want (1396, nil)", id, err)
	}
	id, err = c.SearchTV(context.Background(), "Nothing", 0)
	if err != nil || id != 0 {
		t.Errorf("SearchTV(Nothing) = (%d, %v), want (0, nil)", id, err)
	}
}

func TestNotFound(t *testing.T) {
	_, c, calls := newTestServer(t, cannedHandler(t))
	ctx := context.Background()
	if _, err := c.Movie(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("Movie(404) err = %v, want ErrNotFound", err)
	}
	if _, err := c.TV(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("TV(404) err = %v, want ErrNotFound", err)
	}
	if _, err := c.Season(ctx, 1396, 99); !errors.Is(err, ErrNotFound) {
		t.Errorf("Season(404) err = %v, want ErrNotFound", err)
	}
	if n := atomic.LoadInt32(calls); n != 3 {
		t.Errorf("404 must not be retried; made %d requests, want 3", n)
	}
}

func TestDiskStoreAvoidsSecondRequest(t *testing.T) {
	store, err := NewDiskStore(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_, c, calls := newTestServer(t, cannedHandler(t))
	c.store = store
	ctx := context.Background()

	m1, err := c.Movie(ctx, 603)
	if err != nil {
		t.Fatal(err)
	}
	m2, err := c.Movie(ctx, 603)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m1, m2) {
		t.Errorf("cached result differs")
	}
	if n := atomic.LoadInt32(calls); n != 1 {
		t.Errorf("made %d HTTP requests, want 1 (second should hit the cache)", n)
	}

	// A different client with a different API key shares the cache (key is not part of the cache key).
	c2 := New("otherkey", store)
	c2.SetBaseURL("http://127.0.0.1:1") // unreachable: must not be contacted
	c2.SetRateLimit(0)
	if _, err := c2.Movie(ctx, 603); err != nil {
		t.Errorf("second client should be served from cache, got %v", err)
	}

	// 404s are not cached.
	atomic.StoreInt32(calls, 0)
	for i := 0; i < 2; i++ {
		if _, err := c.Movie(ctx, 999999); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
	}
	if n := atomic.LoadInt32(calls); n != 2 {
		t.Errorf("404 should not be cached; made %d requests, want 2", n)
	}
}

func TestDiskStoreTTL(t *testing.T) {
	dir := t.TempDir()
	store, err := NewDiskStore(dir, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	store.Put("/movie/1", []byte(`{"id":1}`))
	if got, ok := store.Get("/movie/1"); !ok || string(got) != `{"id":1}` {
		t.Fatalf("Get = (%q, %v), want hit", got, ok)
	}
	time.Sleep(80 * time.Millisecond)
	if _, ok := store.Get("/movie/1"); ok {
		t.Error("expired entry should be a miss")
	}
	if _, ok := store.Get("/never"); ok {
		t.Error("unknown key should be a miss")
	}
}

func TestRetryOn429HonorsRetryAfter(t *testing.T) {
	var n int32
	_, c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"status_message":"slow down"}`))
			return
		}
		_, _ = w.Write([]byte(movieJSON))
	})
	start := time.Now()
	m, err := c.Movie(context.Background(), 603)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "The Matrix" {
		t.Errorf("Title = %q", m.Title)
	}
	if atomic.LoadInt32(&n) != 2 {
		t.Errorf("requests = %d, want 2", n)
	}
	if elapsed < 900*time.Millisecond {
		t.Errorf("retry waited only %v, expected ~1s from Retry-After", elapsed)
	}
}

func TestRetryOn5xxGivesUp(t *testing.T) {
	var n int32
	_, c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		http.Error(w, "boom", http.StatusBadGateway)
	})
	_, err := c.Movie(context.Background(), 603)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("err = %v, want HTTP 502", err)
	}
	if atomic.LoadInt32(&n) != maxAttempts {
		t.Errorf("requests = %d, want %d", n, maxAttempts)
	}
}

func TestRetryRespectsContextCancel(t *testing.T) {
	_, c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Movie(ctx, 603)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("did not abort retry sleep on ctx cancel")
	}
}

func TestBearerToken(t *testing.T) {
	var gotAuth, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotKey = r.URL.Query().Get("api_key")
		_, _ = w.Write([]byte(findJSON))
	}))
	defer srv.Close()
	c := New("eyJhbGciOiJIUzI1NiJ9.token", nil)
	c.SetBaseURL(srv.URL)
	if _, _, err := c.FindByIMDB(context.Background(), "tt1"); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer eyJhbGciOiJIUzI1NiJ9.token" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotKey != "" {
		t.Errorf("api_key should not be sent with a v4 token, got %q", gotKey)
	}
}

func TestCacheKeyExcludesAPIKeyAndSortsQuery(t *testing.T) {
	a := cacheKey("/search/movie", map[string][]string{"year": {"1999"}, "query": {"The Matrix"}})
	b := cacheKey("/search/movie", map[string][]string{"query": {"The Matrix"}, "year": {"1999"}})
	if a != b {
		t.Errorf("cache key order-dependent: %q vs %q", a, b)
	}
	if strings.Contains(a, "api_key") {
		t.Errorf("cache key contains api_key: %q", a)
	}
	if a != "/search/movie?query=The+Matrix&year=1999" {
		t.Errorf("cacheKey = %q", a)
	}
	if k := cacheKey("/tv/1/season/2", nil); k != "/tv/1/season/2" {
		t.Errorf("cacheKey(no query) = %q", k)
	}
}

func TestRateLimiter(t *testing.T) {
	b := newTokenBucket(100) // 100/s, burst 100
	ctx := context.Background()
	start := time.Now()
	for i := 0; i < 120; i++ {
		if err := b.wait(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// 100 immediate, then 20 more at 10ms each ≈ 200ms.
	if el := time.Since(start); el < 150*time.Millisecond {
		t.Errorf("120 requests at 100/s took only %v", el)
	}
	b.setRate(0)
	start = time.Now()
	for i := 0; i < 1000; i++ {
		_ = b.wait(ctx)
	}
	if el := time.Since(start); el > 100*time.Millisecond {
		t.Errorf("rate 0 should not block, took %v", el)
	}
}

func TestBestSize(t *testing.T) {
	cases := map[int]string{
		-1: "w500", 0: "w500", 1: "w92", 92: "w92", 93: "w154", 154: "w154", 185: "w185",
		200: "w342", 342: "w342", 343: "w500", 500: "w500", 501: "w780", 780: "w780",
		781: "original", 4000: "original",
	}
	for w, want := range cases {
		if got := BestSize(w); got != want {
			t.Errorf("BestSize(%d) = %q, want %q", w, got, want)
		}
	}
}

func TestImageURL(t *testing.T) {
	if got := ImageURL("/abc.jpg", "w342"); got != "https://image.tmdb.org/t/p/w342/abc.jpg" {
		t.Errorf("ImageURL = %q", got)
	}
	if got := ImageURL("abc.jpg", "original"); got != "https://image.tmdb.org/t/p/original/abc.jpg" {
		t.Errorf("ImageURL(no slash) = %q", got)
	}
	if got := ImageURL("", "w500"); got != "" {
		t.Errorf("ImageURL(empty) = %q, want empty", got)
	}
}
