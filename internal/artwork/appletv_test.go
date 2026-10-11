package artwork

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

// appleFastRetries shortens the backoff for the duration of a test.
func appleFastRetries(t *testing.T) {
	t.Helper()
	saved := appleRetryDelays
	appleRetryDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { appleRetryDelays = saved })
}

func appleMillis(year int) int64 {
	return time.Date(year, time.June, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
}

// appleTestPayload is a shelves response with a movie, a show, a sequel and
// an episode (which the client must drop).
func appleTestPayload() map[string]any {
	item := func(id, typ, title string, year int, images map[string]any) map[string]any {
		m := map[string]any{"id": id, "type": typ, "title": title, "images": images}
		if year != 0 {
			m["releaseDate"] = appleMillis(year)
		}
		return m
	}
	both := map[string]any{
		"coverArt16X9": map[string]any{"width": 3840, "height": 2160, "url": "https://is1.example/wide/{w}x{h}.{f}"},
		"coverArt":     map[string]any{"width": 2000, "height": 3000, "url": "https://is1.example/tall/{w}x{h}.{f}"},
	}
	wideOnly := map[string]any{
		"coverArt16X9": map[string]any{"width": 3840, "height": 2160, "url": "https://is1.example/show/{w}x{h}.{f}"},
	}
	return map[string]any{
		"data": map[string]any{
			"canvas": map[string]any{
				"shelves": []any{
					map[string]any{"items": []any{
						item("umc.cmc.movie1", "Movie", "The Example", 2019, both),
						item("umc.cmc.show1", "Show", "The Example", 2021, wideOnly),
						item("umc.cmc.ep1", "Episode", "The Example: Pilot", 2021, nil),
					}},
					map[string]any{"items": []any{
						item("umc.cmc.movie2", "Movie", "Example: The Sequel", 2023, both),
						item("umc.cmc.movie3", "Movie", "Undated Example", 0, both),
					}},
				},
			},
		},
	}
}

// appleTestServer serves the canned payload and records every query it saw.
func appleTestServer(t *testing.T) (*AppleClient, *[]url.Values) {
	t.Helper()
	var mu sync.Mutex
	var seen []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/incremental" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		seen = append(seen, r.URL.Query())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(appleTestPayload())
	}))
	t.Cleanup(srv.Close)
	c := NewAppleClient("")
	c.BaseURL = srv.URL
	return c, &seen
}

func TestAppleSearchFiltersByKind(t *testing.T) {
	c, _ := appleTestServer(t)
	ctx := context.Background()

	all, err := c.Search(ctx, "The Example", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Fatalf("kind %q: got %d results, want 4 (episode dropped): %+v", "", len(all), all)
	}
	for _, r := range all {
		if r.Kind != "movie" && r.Kind != "tv" {
			t.Errorf("result %s has kind %q", r.ID, r.Kind)
		}
	}

	movies, err := c.Search(ctx, "The Example", "movie")
	if err != nil {
		t.Fatal(err)
	}
	if len(movies) != 3 {
		t.Fatalf("kind movie: got %d results, want 3", len(movies))
	}
	for _, r := range movies {
		if r.Kind != "movie" {
			t.Errorf("movie search returned kind %q for %s", r.Kind, r.ID)
		}
	}

	shows, err := c.Search(ctx, "The Example", "tv")
	if err != nil {
		t.Fatal(err)
	}
	if len(shows) != 1 || shows[0].Kind != "tv" || shows[0].ID != "umc.cmc.show1" {
		t.Fatalf("kind tv: got %+v", shows)
	}
}

func TestAppleSearchYear(t *testing.T) {
	c, _ := appleTestServer(t)
	results, err := c.Search(context.Background(), "The Example", "")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		"umc.cmc.movie1": 2019,
		"umc.cmc.show1":  2021,
		"umc.cmc.movie2": 2023,
		"umc.cmc.movie3": 0,
	}
	for _, r := range results {
		if r.Year != want[r.ID] {
			t.Errorf("%s: Year = %d, want %d", r.ID, r.Year, want[r.ID])
		}
	}
}

func TestAppleSearchQueryParams(t *testing.T) {
	c, seen := appleTestServer(t)
	c.Storefront = "143444" // UK
	if _, err := c.Search(context.Background(), "The Example", ""); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 {
		t.Fatalf("server saw %d requests, want 1", len(*seen))
	}
	q := (*seen)[0]
	for k, want := range map[string]string{
		"sf":     "143444",
		"caller": "wta",
		"q":      "The Example",
		"locale": "en-US",
		"utsk":   "0",
		"v":      "34",
		"pfm":    "desktop",
	} {
		if got := q.Get(k); got != want {
			t.Errorf("query %s = %q, want %q", k, got, want)
		}
	}
}

func TestAppleDefaultStorefront(t *testing.T) {
	c, seen := appleTestServer(t)
	if c.Storefront != AppleDefaultStorefront || c.Locale != "en-US" {
		t.Fatalf("NewAppleClient(\"\") = storefront %q locale %q", c.Storefront, c.Locale)
	}
	if _, err := c.Search(context.Background(), "x", ""); err != nil {
		t.Fatal(err)
	}
	if got := (*seen)[0].Get("sf"); got != "143441" {
		t.Errorf("sf = %q, want 143441", got)
	}
}

func TestAppleSearchCaches(t *testing.T) {
	c, seen := appleTestServer(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := c.Search(ctx, "The Example", "movie"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.Search(ctx, "The Example", "tv"); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 2 {
		t.Fatalf("server saw %d requests, want 2 (one per distinct query/kind)", len(*seen))
	}
}

func TestAppleFind(t *testing.T) {
	c, _ := appleTestServer(t)
	ctx := context.Background()
	cases := []struct {
		name   string
		title  string
		year   int
		kind   string
		wantID string // "" means nil
	}{
		{"exact year+title beats title-only earlier in the list", "The Example", 2021, "", "umc.cmc.show1"},
		{"title match is case-insensitive", "the example", 2019, "", "umc.cmc.movie1"},
		{"year-only beats title-only", "The Example", 2023, "", "umc.cmc.movie2"},
		{"title-only when no year matches", "The Example", 1999, "", "umc.cmc.movie1"},
		{"nothing fits", "Nothing Here", 1999, "", ""},
		{"year 0 accepts a title match", "THE EXAMPLE", 0, "", "umc.cmc.movie1"},
		{"year 0 without a title match is nil, not results[0]", "Example", 0, "", ""},
		{"kind narrows the candidates (no 2021 movie, so title-only)", "The Example", 2021, "movie", "umc.cmc.movie1"},
		{"kind tv with matching year", "The Example", 2021, "tv", "umc.cmc.show1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.Find(ctx, tc.title, tc.year, tc.kind)
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.wantID == "" && got != nil:
				t.Fatalf("Find = %+v, want nil", *got)
			case tc.wantID != "" && got == nil:
				t.Fatalf("Find = nil, want %s", tc.wantID)
			case tc.wantID != "" && got.ID != tc.wantID:
				t.Fatalf("Find = %s, want %s", got.ID, tc.wantID)
			}
		})
	}
}

func TestAppleArtworkURL(t *testing.T) {
	c, _ := appleTestServer(t)
	movie, err := c.Find(context.Background(), "The Example", 2019, "movie")
	if err != nil || movie == nil {
		t.Fatalf("Find movie: %v %v", movie, err)
	}
	show, err := c.Find(context.Background(), "The Example", 2021, "tv")
	if err != nil || show == nil {
		t.Fatalf("Find show: %v %v", show, err)
	}
	cases := []struct {
		r           *AppleResult
		orientation string
		max         bool
		want        string
	}{
		{movie, "horizontal", false, "https://is1.example/wide/1920x1080.jpg"},
		{movie, "horizontal", true, "https://is1.example/wide/3840x2160.jpg"},
		{movie, "vertical", false, "https://is1.example/tall/1000x1500.jpg"},
		{movie, "vertical", true, "https://is1.example/tall/2000x3000.jpg"},
		{show, "horizontal", false, "https://is1.example/show/1920x1080.jpg"},
		{show, "vertical", false, ""},
		{show, "vertical", true, ""},
		{nil, "horizontal", false, ""},
	}
	for _, tc := range cases {
		if got := tc.r.ArtworkURL(tc.orientation, tc.max); got != tc.want {
			t.Errorf("ArtworkURL(%q, %v) = %q, want %q", tc.orientation, tc.max, got, tc.want)
		}
	}
}

func TestAppleSearchRetriesOn503(t *testing.T) {
	appleFastRetries(t)
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(appleTestPayload())
	}))
	defer srv.Close()

	c := NewAppleClient("")
	c.BaseURL = srv.URL
	results, err := c.Search(context.Background(), "The Example", "")
	if err != nil {
		t.Fatalf("Search after 503: %v", err)
	}
	if len(results) != 4 {
		t.Fatalf("got %d results, want 4", len(results))
	}
	if calls != 2 {
		t.Fatalf("server saw %d calls, want 2 (503 then 200)", calls)
	}
}

func TestAppleSearchGivesUpAfterThreeAttempts(t *testing.T) {
	appleFastRetries(t)
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewAppleClient("")
	c.BaseURL = srv.URL
	_, err := c.Search(context.Background(), "x", "")
	if err == nil {
		t.Fatal("expected an error after exhausting retries")
	}
	if calls != appleRetryAttempts {
		t.Fatalf("server saw %d calls, want %d", calls, appleRetryAttempts)
	}
	if appleHTTPStatus(err) != http.StatusTooManyRequests {
		t.Fatalf("error %v does not carry the 429 status", err)
	}
}

func TestAppleSearchDoesNotRetry4xx(t *testing.T) {
	appleFastRetries(t)
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewAppleClient("")
	c.BaseURL = srv.URL
	_, err := c.Search(context.Background(), "x", "")
	if err == nil {
		t.Fatal("expected an error")
	}
	if calls != 1 {
		t.Fatalf("server saw %d calls, want 1 (4xx is permanent)", calls)
	}
}

func TestAppleSearchHonoursContext(t *testing.T) {
	appleFastRetries(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := NewAppleClient("")
	c.BaseURL = srv.URL
	if _, err := c.Search(ctx, "x", ""); err == nil {
		t.Fatal("expected an error from a cancelled context")
	}
}

func TestAppleRenderTemplate(t *testing.T) {
	if got := appleRenderTemplate("https://x/{w}x{h}.{f}", "480x270"); got != "https://x/480x270.jpg" {
		t.Errorf("got %q", got)
	}
	if got := appleRenderTemplate("https://x/{w}x{h}.{f}", "bad"); got != "" {
		t.Errorf("bad size rendered %q", got)
	}
}

func ExampleAppleResult_ArtworkURL() {
	r := &AppleResult{images: map[string]appleImage{
		"coverArt16X9": {URL: "https://is1.example/a/{w}x{h}.{f}"},
	}}
	fmt.Println(r.ArtworkURL("horizontal", false))
	fmt.Println(r.ArtworkURL("vertical", false) == "")
	// Output:
	// https://is1.example/a/1920x1080.jpg
	// true
}
