package main

import (
	"testing"

	"github.com/joshkerr/goplexcli/internal/cache"
	"github.com/joshkerr/goplexcli/internal/config"
	"github.com/joshkerr/goplexcli/internal/plex"
)

func TestProgressPct(t *testing.T) {
	cases := []struct {
		name   string
		item   plex.MediaItem
		want   int
		inProg bool
	}{
		{"unwatched", plex.MediaItem{Duration: 1000}, 0, false},
		{"halfway", plex.MediaItem{Duration: 1000, ViewOffset: 500}, 50, true},
		{"nearly done", plex.MediaItem{Duration: 1000, ViewOffset: 960}, 96, false},
		{"no duration", plex.MediaItem{ViewOffset: 500}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := progressPct(&tc.item); got != tc.want {
				t.Errorf("progressPct = %d, want %d", got, tc.want)
			}
			if got := isInProgress(&tc.item); got != tc.inProg {
				t.Errorf("isInProgress = %v, want %v", got, tc.inProg)
			}
		})
	}
}

func TestGroupShows(t *testing.T) {
	a := NewApp()
	c := &cache.Cache{Media: []plex.MediaItem{
		{Key: "m1", Type: "movie", Title: "A Movie"},
		{Key: "e1", Type: "episode", Title: "Pilot", ParentTitle: "Show Z", ParentIndex: 1, Index: 1, AddedAt: 100},
		{Key: "e2", Type: "episode", Title: "Ep2", ParentTitle: "Show Z", ParentIndex: 1, Index: 2, AddedAt: 500},
		{Key: "e3", Type: "episode", Title: "Ep1", ParentTitle: "Show A", ParentIndex: 2, Index: 1, AddedAt: 300},
	}}

	shows := a.groupShowCards(c)
	if len(shows) != 2 {
		t.Fatalf("expected 2 shows, got %d", len(shows))
	}
	// Sorted by most recently added episode: Show Z (500) before Show A (300).
	if shows[0].Title != "Show Z" || shows[1].Title != "Show A" {
		t.Errorf("shows not sorted by latest episode: %q, %q", shows[0].Title, shows[1].Title)
	}
	if shows[0].EpisodeCount != 2 {
		t.Errorf("Show Z episode count = %d, want 2", shows[0].EpisodeCount)
	}
	if shows[0].Type != "show" || shows[0].Key != "show:Show Z" {
		t.Errorf("unexpected show row: type=%q key=%q", shows[0].Type, shows[0].Key)
	}
}

func TestRecentlyAdded(t *testing.T) {
	a := NewApp()
	c := &cache.Cache{Media: []plex.MediaItem{
		{Key: "old", Type: "movie", Title: "Old", AddedAt: 100},
		{Key: "new", Type: "movie", Title: "New", AddedAt: 300},
		{Key: "mid", Type: "movie", Title: "Mid", AddedAt: 200},
		{Key: "ep", Type: "episode", Title: "Ep", AddedAt: 999},
	}}

	got := recentlyAddedCards(a, c, "movie", 2)
	if len(got) != 2 {
		t.Fatalf("expected 2 results (limited), got %d", len(got))
	}
	if got[0].Key != "new" || got[1].Key != "mid" {
		t.Errorf("recentlyAdded order = %q, %q; want new, mid", got[0].Key, got[1].Key)
	}
}

func TestSortMovieItems(t *testing.T) {
	c := &cache.Cache{Media: []plex.MediaItem{
		{Key: "b", Type: "movie", Title: "Beta", Year: 2001, AddedAt: 100, Rating: 7, Genre: "Action, Comedy"},
		{Key: "a", Type: "movie", Title: "Alpha", Year: 1999, AddedAt: 300, Rating: 9, Genre: "Drama"},
		{Key: "c", Type: "movie", Title: "Gamma", Year: 2010, AddedAt: 200, Rating: 5, Genre: "Comedy"},
		{Key: "ep", Type: "episode", Title: "Nope", Genre: "Comedy"},
	}}

	keys := func(items []*plex.MediaItem) []string {
		out := make([]string, len(items))
		for i, it := range items {
			out[i] = it.Key
		}
		return out
	}
	eq := func(t *testing.T, got, want []string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("len = %d (%v), want %d (%v)", len(got), got, len(want), want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("order = %v, want %v", got, want)
			}
		}
	}

	// Default (empty opts): all movies A-Z by title, episodes excluded.
	eq(t, keys(sortMovieItems(c, BrowseOptions{})), []string{"a", "b", "c"})
	// Sort by date added, descending.
	eq(t, keys(sortMovieItems(c, BrowseOptions{SortField: "added", Desc: true})), []string{"a", "c", "b"})
	// Sort by year ascending.
	eq(t, keys(sortMovieItems(c, BrowseOptions{SortField: "year"})), []string{"a", "b", "c"})
	// Genre filter matches a token within a comma-separated field.
	eq(t, keys(sortMovieItems(c, BrowseOptions{Genre: "Comedy"})), []string{"b", "c"})
}

func TestParseFieldQuery(t *testing.T) {
	cases := []struct {
		query     string
		wantField string
		wantValue string
		wantOK    bool
	}{
		{`director:"Christopher Nolan"`, "director", "Christopher Nolan", true},
		{`cast:"Tom Hanks"`, "cast", "Tom Hanks", true},
		{`genre:Comedy`, "genre", "Comedy", true},
		{`DIRECTOR:"Nolan"`, "director", "Nolan", true}, // field is case-insensitive
		{`cast:"  Spaced  "`, "cast", "Spaced", true},    // value trimmed
		{`The Matrix`, "", "", false},                    // plain title
		{`Aliens: Special Edition`, "", "", false},       // colon but unknown prefix
		{`studio:"A24"`, "", "", false},                  // unsupported field
		{`director:`, "", "", false},                     // empty value
		{`director:""`, "", "", false},                   // empty quoted value
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			field, value, ok := parseFieldQuery(tc.query)
			if ok != tc.wantOK || field != tc.wantField || value != tc.wantValue {
				t.Errorf("parseFieldQuery(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.query, field, value, ok, tc.wantField, tc.wantValue, tc.wantOK)
			}
		})
	}
}

func TestSearchByField(t *testing.T) {
	a := NewApp()
	a.setMedia(&cache.Cache{Media: []plex.MediaItem{
		{Key: "inception", Type: "movie", Title: "Inception", Director: "Christopher Nolan", Cast: "Leonardo DiCaprio, Tom Hardy", Genre: "Sci-Fi, Action"},
		{Key: "dunkirk", Type: "movie", Title: "Dunkirk", Director: "Christopher Nolan", Cast: "Tom Hardy, Fionn Whitehead", Genre: "War, Action"},
		{Key: "forrest", Type: "movie", Title: "Forrest Gump", Director: "Robert Zemeckis", Cast: "Tom Hanks, Robin Wright", Genre: "Drama"},
		{Key: "ep", Type: "episode", Title: "An Episode", Director: "Christopher Nolan", Genre: "Action"}, // excluded: not a movie
	}})

	keys := func(cards []MediaCardDTO) []string {
		out := make([]string, len(cards))
		for i, c := range cards {
			out[i] = c.Key
		}
		return out
	}

	// Director match: both Nolan movies, sorted A-Z by title; episode excluded.
	if got := keys(a.Search(`director:"Christopher Nolan"`)); len(got) != 2 || got[0] != "dunkirk" || got[1] != "inception" {
		t.Errorf("director search = %v, want [dunkirk inception]", got)
	}
	// Cast match spans multiple movies.
	if got := keys(a.Search(`cast:"Tom Hardy"`)); len(got) != 2 || got[0] != "dunkirk" || got[1] != "inception" {
		t.Errorf("cast search = %v, want [dunkirk inception]", got)
	}
	// Cast match is a whole-token match, not a substring (Tom Hardy != Tom Hanks).
	if got := keys(a.Search(`cast:"Tom Hanks"`)); len(got) != 1 || got[0] != "forrest" {
		t.Errorf("cast search = %v, want [forrest]", got)
	}
	// Genre match.
	if got := keys(a.Search(`genre:Action`)); len(got) != 2 || got[0] != "dunkirk" || got[1] != "inception" {
		t.Errorf("genre search = %v, want [dunkirk inception]", got)
	}
	// No match returns an empty (non-nil) slice.
	if got := a.Search(`director:"Nobody"`); got == nil || len(got) != 0 {
		t.Errorf("no-match search = %v, want empty non-nil slice", got)
	}
}

func TestMovieGenres(t *testing.T) {
	a := NewApp()
	a.setMedia(&cache.Cache{Media: []plex.MediaItem{
		{Type: "movie", Genre: "Drama, Comedy"},
		{Type: "movie", Genre: "Drama"},
		{Type: "movie", Genre: "Comedy"},
		{Type: "movie", Genre: "Action"},
		{Type: "episode", Genre: "Documentary"}, // ignored: not a movie
	}})
	got := a.MovieGenres()
	// Drama (2) and Comedy (2) outrank Action (1); ties broken alphabetically.
	if len(got) != 3 || got[0] != "Comedy" || got[1] != "Drama" || got[2] != "Action" {
		t.Errorf("MovieGenres = %v, want [Comedy Drama Action]", got)
	}
}

func TestGetItem(t *testing.T) {
	a := NewApp()
	a.setMedia(&cache.Cache{Media: []plex.MediaItem{
		{Key: "m1", Type: "movie", Title: "A Movie", Summary: "summary"},
		{Key: "e1", Type: "episode", Title: "Pilot", ParentTitle: "Show Z", Summary: "ep summary", ParentIndex: 1, Index: 1},
		{Key: "e2", Type: "episode", Title: "Ep2", ParentTitle: "Show Z", ParentIndex: 1, Index: 2},
	}})

	movie, err := a.GetItem("m1")
	if err != nil || movie.Summary != "summary" {
		t.Fatalf("GetItem(movie) = %+v, err=%v", movie, err)
	}

	show, err := a.GetItem("show:Show Z")
	if err != nil {
		t.Fatalf("GetItem(show) error: %v", err)
	}
	if show.Type != "show" || show.EpisodeCount != 2 || show.Summary != "ep summary" {
		t.Errorf("unexpected show DTO: %+v", show)
	}

	if _, err := a.GetItem("missing"); err == nil {
		t.Error("expected error for missing key")
	}
}

func TestBuildServerConfigs(t *testing.T) {
	// Multi-server.
	cfg := &config.Config{
		PlexToken: "tok",
		Servers: []config.PlexServer{
			{Name: "S1", URL: "http://a", Enabled: true},
			{Name: "S2", URL: "http://b", Enabled: false},
		},
	}
	got, err := buildServerConfigs(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "S1" || got[0].Token != "tok" {
		t.Errorf("expected only enabled S1 with token, got %+v", got)
	}

	// Legacy single-server fallback.
	legacy := &config.Config{PlexToken: "t2", PlexURL: "http://legacy"}
	got, err = buildServerConfigs(legacy)
	if err != nil || len(got) != 1 || got[0].URL != "http://legacy" {
		t.Errorf("legacy fallback failed: got=%+v err=%v", got, err)
	}

	// No servers.
	if _, err := buildServerConfigs(&config.Config{}); err == nil {
		t.Error("expected error when no servers configured")
	}
}

func TestRecentShowCards(t *testing.T) {
	a := NewApp()
	c := &cache.Cache{Media: []plex.MediaItem{
		{Key: "m1", Type: "movie", Title: "A Movie", AddedAt: 900},
		{Key: "e1", Type: "episode", Title: "Old Pilot", ParentTitle: "Show A", AddedAt: 100},
		{Key: "e2", Type: "episode", Title: "Newest", ParentTitle: "Show B", AddedAt: 500},
		{Key: "e3", Type: "episode", Title: "Recent A1", ParentTitle: "Show A", AddedAt: 400},
		{Key: "e4", Type: "episode", Title: "Recent A2", ParentTitle: "Show A", AddedAt: 300},
	}}

	// Pool limited to the 3 newest episodes: e2 (Show B), e3, e4 (Show A).
	// e1 falls outside the pool, so Show A's NewCount is 2, not 3.
	cards := recentShowCards(a, c, 3)
	if len(cards) != 2 {
		t.Fatalf("expected 2 show cards, got %d", len(cards))
	}
	if cards[0].Title != "Show B" || cards[1].Title != "Show A" {
		t.Errorf("order: got %q, %q; want Show B, Show A (newest episode first)", cards[0].Title, cards[1].Title)
	}
	if cards[0].NewCount != 1 || cards[1].NewCount != 2 {
		t.Errorf("NewCount: got %d, %d; want 1, 2", cards[0].NewCount, cards[1].NewCount)
	}
	if cards[0].Type != "show" || cards[0].Key != "show:Show B" {
		t.Errorf("card shape: type=%q key=%q; want show / show:Show B", cards[0].Type, cards[0].Key)
	}
}

func TestSearchPeople(t *testing.T) {
	a := NewApp()
	a.setMedia(&cache.Cache{Media: []plex.MediaItem{
		{Key: "m1", Type: "movie", Title: "Inception", Director: "Christopher Nolan", Cast: "Leonardo DiCaprio, Elliot Page"},
		{Key: "m2", Type: "movie", Title: "Oppenheimer", Director: "Christopher Nolan", Cast: "Cillian Murphy"},
		{Key: "m3", Type: "movie", Title: "Cape Fear", Director: "Martin Scorsese", Cast: "Nick Nolte, Nolan North"},
		{Key: "e1", Type: "episode", Title: "Pilot", ParentTitle: "Some Show", Director: "Nolan Impostor"},
	}})

	t.Run("matches directors and actors, ranked by count", func(t *testing.T) {
		people := a.SearchPeople("nol")
		if len(people) != 3 {
			t.Fatalf("got %d people, want 3: %+v", len(people), people)
		}
		// "Nolan North" starts with the query so it ranks first; among the
		// substring matches, count decides: Nolan (2 movies) before Nolte (1).
		want := []string{"Nolan North", "Christopher Nolan", "Nick Nolte"}
		for i, name := range want {
			if people[i].Name != name {
				t.Errorf("person %d: got %q, want %q", i, people[i].Name, name)
			}
		}
		if people[1].Role != "director" || people[1].Count != 2 {
			t.Errorf("Nolan: %+v; want director with count 2", people[1])
		}
	})

	t.Run("prefix match outranks higher count", func(t *testing.T) {
		people := a.SearchPeople("nolan")
		if len(people) != 2 {
			t.Fatalf("got %d people, want 2: %+v", len(people), people)
		}
		// "Nolan North" starts with the query, so it beats Christopher Nolan
		// despite Nolan's higher movie count.
		if people[0].Name != "Nolan North" || people[1].Name != "Christopher Nolan" {
			t.Errorf("order: got %q, %q; want Nolan North first", people[0].Name, people[1].Name)
		}
	})

	t.Run("episodes are ignored", func(t *testing.T) {
		for _, p := range a.SearchPeople("impostor") {
			t.Errorf("episode-only person leaked into results: %+v", p)
		}
	})

	t.Run("short queries return nothing", func(t *testing.T) {
		if got := a.SearchPeople("n"); len(got) != 0 {
			t.Errorf("1-char query: got %+v, want none", got)
		}
	})
}
