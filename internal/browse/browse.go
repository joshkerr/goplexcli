// Package browse answers library queries (category grids, search, item and
// show details) over the cached media items. It backs the library server's
// JSON API, which lets thin clients such as the Android app browse a large
// library page by page instead of downloading the whole cache.
//
// The semantics mirror the desktop GUI (gui/media.go): shows are grouped by
// ParentTitle, "Continue Watching" holds items with a resume position under
// 95%, recently added is ordered by AddedAt.
package browse

import (
	"sort"
	"strings"
	"sync"

	"github.com/joshkerr/goplexcli/internal/libserver"
	"github.com/joshkerr/goplexcli/internal/plex"
	"github.com/sahilm/fuzzy"
)

// Card is a grid tile.
type Card struct {
	Key          string `json:"key"`
	Type         string `json:"type"` // movie | show | episode
	Title        string `json:"title"`
	Subtitle     string `json:"subtitle,omitempty"` // "S1 · E2 · Show" for episodes
	Year         int    `json:"year,omitempty"`
	Thumb        string `json:"thumb,omitempty"` // library thumb path; render via /photo/:/transcode
	ProgressPct  int    `json:"progressPct,omitempty"`
	ViewCount    int    `json:"viewCount,omitempty"`
	EpisodeCount int    `json:"episodeCount,omitempty"`
	NewCount     int    `json:"newCount,omitempty"`
	Favorite     bool   `json:"favorite,omitempty"`
}

// Media is a full movie or episode record.
type Media struct {
	Key             string  `json:"key"`
	Type            string  `json:"type"`
	Title           string  `json:"title"`
	Year            int     `json:"year,omitempty"`
	Summary         string  `json:"summary,omitempty"`
	Rating          float64 `json:"rating,omitempty"`
	Duration        int     `json:"duration,omitempty"` // ms
	ContentRating   string  `json:"contentRating,omitempty"`
	Studio          string  `json:"studio,omitempty"`
	Director        string  `json:"director,omitempty"`
	Genre           string  `json:"genre,omitempty"`
	Cast            string  `json:"cast,omitempty"`
	OriginallyAired string  `json:"originallyAired,omitempty"`
	ParentTitle     string  `json:"parentTitle,omitempty"` // show (episodes)
	GrandTitle      string  `json:"grandTitle,omitempty"`  // season name (episodes)
	Index           int64   `json:"index,omitempty"`       // episode number
	ParentIndex     int64   `json:"parentIndex,omitempty"` // season number
	ViewOffset      int     `json:"viewOffset,omitempty"`  // ms
	ViewCount       int     `json:"viewCount,omitempty"`
	ProgressPct     int     `json:"progressPct,omitempty"`
	Thumb           string  `json:"thumb,omitempty"`
	ShowThumb       string  `json:"showThumb,omitempty"`
	Favorite        bool    `json:"favorite,omitempty"`
	// Playable is true for items this server streams; StreamPath is the
	// path to request for the file bytes (Range supported).
	Playable   bool   `json:"playable"`
	StreamPath string `json:"streamPath,omitempty"`
	// RclonePath is what a `goplexcli serve` daemon needs to download it.
	RclonePath string `json:"rclonePath,omitempty"`
	Server     string `json:"server,omitempty"`
}

// Season summarizes one season of a show.
type Season struct {
	Season       int    `json:"season"`
	Title        string `json:"title"`
	EpisodeCount int    `json:"episodeCount"`
	WatchedCount int    `json:"watchedCount"`
}

// Show is a show's detail record.
type Show struct {
	Key           string   `json:"key"` // "show:<title>"
	Title         string   `json:"title"`
	Year          int      `json:"year,omitempty"`
	Summary       string   `json:"summary,omitempty"`
	Genre         string   `json:"genre,omitempty"`
	ContentRating string   `json:"contentRating,omitempty"`
	Studio        string   `json:"studio,omitempty"`
	Cast          string   `json:"cast,omitempty"`
	Thumb         string   `json:"thumb,omitempty"`
	EpisodeCount  int      `json:"episodeCount"`
	Favorite      bool     `json:"favorite,omitempty"`
	Seasons       []Season `json:"seasons"`
	// NextUp is the episode to play next: the most recently watched
	// in-progress episode, else the first unwatched one after the last
	// watched, else the first episode.
	NextUp *Media `json:"nextUp,omitempty"`
}

// Status counts the library.
type Status struct {
	Count    int `json:"count"`
	Movies   int `json:"movies"`
	Shows    int `json:"shows"`
	Episodes int `json:"episodes"`
}

// Query selects a page of a category.
type Query struct {
	Category string // movies | shows | recent-movies | recent-shows | continue | favorite-movies | favorite-shows
	Sort     string // title | year | added | rating | duration ("" = category default)
	Desc     bool
	Genre    string
	Offset   int
	Limit    int
}

// Page is a slice of a category.
type Page struct {
	Total  int    `json:"total"`
	Offset int    `json:"offset"`
	Items  []Card `json:"items"`
}

// Categories lists the valid Query.Category values.
var Categories = []string{"movies", "shows", "recent-movies", "recent-shows", "continue", "favorite-movies", "favorite-shows"}

const (
	recentLimit = 60
	genreLimit  = 16
	maxLimit    = 500
)

type showInfo struct {
	title    string
	episodes []*plex.MediaItem // in cache order
	latest   int64             // newest AddedAt
	rep      *plex.MediaItem   // representative episode for show metadata
}

// Index is a read-only view over a slice of items, built once per cache
// generation. It holds pointers into the slice, so in-place progress updates
// (ViewOffset, ViewCount) are reflected without a rebuild.
type Index struct {
	items      []plex.MediaItem
	local      func(*plex.MediaItem) bool
	byKey      map[string]*plex.MediaItem
	movies     []*plex.MediaItem
	shows      []*showInfo // newest episode first
	showByName map[string]*showInfo
	episodes   []*plex.MediaItem // newest first

	genresOnce sync.Once
	genres     []string
	titlesOnce sync.Once
	titles     []string // movie titles then show titles, for fuzzy search
}

// NewIndex builds an index. local reports whether an item is streamed by
// this server (nil means every item with a local key).
func NewIndex(items []plex.MediaItem, local func(*plex.MediaItem) bool) *Index {
	if local == nil {
		local = func(it *plex.MediaItem) bool { return libserver.IsLocalKey(it.Key) }
	}
	ix := &Index{items: items, local: local, byKey: make(map[string]*plex.MediaItem, len(items)), showByName: map[string]*showInfo{}}
	for i := range items {
		it := &items[i]
		ix.byKey[it.Key] = it
		switch it.Type {
		case "movie":
			ix.movies = append(ix.movies, it)
		case "episode":
			if it.ParentTitle == "" {
				continue
			}
			ix.episodes = append(ix.episodes, it)
			s, ok := ix.showByName[it.ParentTitle]
			if !ok {
				s = &showInfo{title: it.ParentTitle, rep: it}
				ix.showByName[it.ParentTitle] = s
				ix.shows = append(ix.shows, s)
			}
			s.episodes = append(s.episodes, it)
			if it.AddedAt > s.latest {
				s.latest = it.AddedAt
			}
			// Prefer an episode that carries the show poster and summary.
			if s.rep.GrandparentThumb == "" && it.GrandparentThumb != "" {
				s.rep = it
			}
		}
	}
	sort.SliceStable(ix.shows, func(i, j int) bool {
		if ix.shows[i].latest != ix.shows[j].latest {
			return ix.shows[i].latest > ix.shows[j].latest
		}
		return strings.ToLower(ix.shows[i].title) < strings.ToLower(ix.shows[j].title)
	})
	sort.SliceStable(ix.episodes, func(i, j int) bool { return ix.episodes[i].AddedAt > ix.episodes[j].AddedAt })
	return ix
}

// Items returns the slice the index was built over (to detect generations).
func (ix *Index) Items() []plex.MediaItem { return ix.items }

// Status counts the library.
func (ix *Index) Status() Status {
	return Status{Count: len(ix.items), Movies: len(ix.movies), Shows: len(ix.shows), Episodes: len(ix.episodes)}
}

// ---- conversions ----

// ProgressPct is the resume position as a percentage of the duration.
func ProgressPct(it *plex.MediaItem) int {
	if it.Duration <= 0 || it.ViewOffset <= 0 {
		return 0
	}
	p := int(float64(it.ViewOffset) * 100 / float64(it.Duration))
	if p > 100 {
		p = 100
	}
	return p
}

func inProgress(it *plex.MediaItem) bool {
	p := ProgressPct(it)
	return it.ViewOffset > 0 && p > 0 && p < 95
}

func (ix *Index) thumbFor(it *plex.MediaItem) string {
	if !ix.local(it) {
		return ""
	}
	return it.Thumb
}

func showThumb(ix *Index, it *plex.MediaItem) string {
	if !ix.local(it) {
		return ""
	}
	if it.GrandparentThumb != "" {
		return it.GrandparentThumb
	}
	return it.Thumb
}

func episodeLabel(it *plex.MediaItem) string {
	parts := []string{}
	if it.ParentIndex > 0 || it.Index > 0 {
		parts = append(parts, "S"+itoa(int(it.ParentIndex))+" · E"+itoa(int(it.Index)))
	}
	if it.ParentTitle != "" {
		parts = append(parts, it.ParentTitle)
	}
	return strings.Join(parts, " · ")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func (ix *Index) card(it *plex.MediaItem, fav map[string]bool) Card {
	c := Card{
		Key: it.Key, Type: it.Type, Title: it.Title, Year: it.Year,
		Thumb: ix.thumbFor(it), ProgressPct: ProgressPct(it), ViewCount: it.ViewCount,
		Favorite: fav[it.Key],
	}
	if it.Type == "episode" {
		c.Subtitle = episodeLabel(it)
		if c.Thumb == "" {
			c.Thumb = showThumb(ix, it)
		}
	}
	return c
}

func (ix *Index) showCard(s *showInfo, fav map[string]bool) Card {
	return Card{
		Key: "show:" + s.title, Type: "show", Title: s.title, Year: s.rep.Year,
		Thumb: showThumb(ix, s.rep), EpisodeCount: len(s.episodes), Favorite: fav["show:"+s.title],
	}
}

func (ix *Index) media(it *plex.MediaItem, fav map[string]bool) Media {
	m := Media{
		Key: it.Key, Type: it.Type, Title: it.Title, Year: it.Year, Summary: it.Summary,
		Rating: it.Rating, Duration: it.Duration, ContentRating: it.ContentRating, Studio: it.Studio,
		Director: it.Director, Genre: it.Genre, Cast: it.Cast, OriginallyAired: it.OriginallyAired,
		ParentTitle: it.ParentTitle, GrandTitle: it.GrandTitle, Index: it.Index, ParentIndex: it.ParentIndex,
		ViewOffset: it.ViewOffset, ViewCount: it.ViewCount, ProgressPct: ProgressPct(it),
		Thumb: ix.thumbFor(it), Favorite: fav[it.Key], RclonePath: it.RclonePath, Server: it.ServerName,
	}
	if it.Type == "episode" {
		m.ShowThumb = showThumb(ix, it)
		m.Favorite = fav[it.Key] || fav["show:"+it.ParentTitle]
	}
	if ix.local(it) {
		if id, ok := libserver.IDFromKey(it.Key); ok {
			m.Playable = true
			m.StreamPath = libserver.FilePrefix + id
		}
	}
	return m
}

// ---- queries ----

// Browse returns a page of a category.
func (ix *Index) Browse(q Query, fav map[string]bool) Page {
	if q.Limit <= 0 || q.Limit > maxLimit {
		q.Limit = 100
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	var cards []Card
	total := 0
	page := func(n int) (int, int) {
		start := q.Offset
		if start > n {
			start = n
		}
		end := start + q.Limit
		if end > n {
			end = n
		}
		return start, end
	}

	switch q.Category {
	case "movies", "favorite-movies":
		var items []*plex.MediaItem
		for _, it := range ix.movies {
			if q.Category == "favorite-movies" && !fav[it.Key] {
				continue
			}
			if q.Genre != "" && !hasTag(it.Genre, q.Genre) {
				continue
			}
			items = append(items, it)
		}
		sortMovies(items, q.Sort, q.Desc)
		total = len(items)
		start, end := page(total)
		for _, it := range items[start:end] {
			cards = append(cards, ix.card(it, fav))
		}

	case "shows", "favorite-shows":
		var shows []*showInfo
		for _, s := range ix.shows {
			if q.Category == "favorite-shows" && !fav["show:"+s.title] {
				continue
			}
			if q.Genre != "" && !hasTag(s.rep.Genre, q.Genre) {
				continue
			}
			shows = append(shows, s)
		}
		sortShows(shows, q.Sort, q.Desc)
		total = len(shows)
		start, end := page(total)
		for _, s := range shows[start:end] {
			cards = append(cards, ix.showCard(s, fav))
		}

	case "recent-movies":
		items := append([]*plex.MediaItem(nil), ix.movies...)
		sort.SliceStable(items, func(i, j int) bool { return items[i].AddedAt > items[j].AddedAt })
		if len(items) > recentLimit {
			items = items[:recentLimit]
		}
		total = len(items)
		start, end := page(total)
		for _, it := range items[start:end] {
			cards = append(cards, ix.card(it, fav))
		}

	case "recent-shows":
		// Group the newest episodes into one card per show, newest first.
		limit := recentLimit * 3
		if limit > len(ix.episodes) {
			limit = len(ix.episodes)
		}
		var order []string
		counts := map[string]int{}
		for _, ep := range ix.episodes[:limit] {
			if counts[ep.ParentTitle] == 0 {
				order = append(order, ep.ParentTitle)
			}
			counts[ep.ParentTitle]++
		}
		if len(order) > recentLimit {
			order = order[:recentLimit]
		}
		total = len(order)
		start, end := page(total)
		for _, name := range order[start:end] {
			c := ix.showCard(ix.showByName[name], fav)
			c.NewCount = counts[name]
			cards = append(cards, c)
		}

	case "continue":
		var items []*plex.MediaItem
		for i := range ix.items {
			if inProgress(&ix.items[i]) {
				items = append(items, &ix.items[i])
			}
		}
		sort.SliceStable(items, func(i, j int) bool { return items[i].LastViewedAt > items[j].LastViewedAt })
		total = len(items)
		start, end := page(total)
		for _, it := range items[start:end] {
			cards = append(cards, ix.card(it, fav))
		}
	}
	if cards == nil {
		cards = []Card{}
	}
	return Page{Total: total, Offset: q.Offset, Items: cards}
}

// Item returns a movie or episode by key.
func (ix *Index) Item(key string, fav map[string]bool) (Media, bool) {
	it, ok := ix.byKey[key]
	if !ok {
		return Media{}, false
	}
	return ix.media(it, fav), true
}

// Show returns a show's details and seasons.
func (ix *Index) Show(title string, fav map[string]bool) (Show, bool) {
	s, ok := ix.showByName[title]
	if !ok {
		return Show{}, false
	}
	rep := s.rep
	out := Show{
		Key: "show:" + s.title, Title: s.title, Year: rep.Year, Genre: rep.Genre,
		ContentRating: rep.ContentRating, Studio: rep.Studio, Cast: rep.Cast,
		Thumb: showThumb(ix, rep), EpisodeCount: len(s.episodes), Favorite: fav["show:"+s.title],
	}
	seasons := map[int]*Season{}
	for _, ep := range s.episodes {
		n := int(ep.ParentIndex)
		se, ok := seasons[n]
		if !ok {
			title := ep.GrandTitle
			if title == "" {
				if n == 0 {
					title = "Specials"
				} else {
					title = "Season " + itoa(n)
				}
			}
			se = &Season{Season: n, Title: title}
			seasons[n] = se
		}
		se.EpisodeCount++
		if ep.ViewCount > 0 {
			se.WatchedCount++
		}
	}
	for _, se := range seasons {
		out.Seasons = append(out.Seasons, *se)
	}
	// Specials (season 0) sort last, like Plex.
	sort.Slice(out.Seasons, func(i, j int) bool {
		a, b := out.Seasons[i].Season, out.Seasons[j].Season
		if (a == 0) != (b == 0) {
			return b == 0
		}
		return a < b
	})
	if next := nextUp(s); next != nil {
		m := ix.media(next, fav)
		out.NextUp = &m
	}
	return out, true
}

// nextUp picks the episode to resume or start.
func nextUp(s *showInfo) *plex.MediaItem {
	eps := append([]*plex.MediaItem(nil), s.episodes...)
	sortEpisodes(eps)
	var resume *plex.MediaItem
	lastWatched := -1
	for i, ep := range eps {
		if inProgress(ep) && (resume == nil || ep.LastViewedAt > resume.LastViewedAt) {
			resume = ep
		}
		if ep.ViewCount > 0 {
			lastWatched = i
		}
	}
	if resume != nil {
		return resume
	}
	for i := lastWatched + 1; i < len(eps); i++ {
		if eps[i].ParentIndex != 0 || len(eps) == 1 {
			return eps[i]
		}
	}
	if len(eps) > 0 {
		return eps[0]
	}
	return nil
}

// sortEpisodes orders episodes by season (specials last) then episode.
func sortEpisodes(eps []*plex.MediaItem) {
	sort.SliceStable(eps, func(i, j int) bool {
		a, b := eps[i], eps[j]
		if a.ParentIndex != b.ParentIndex {
			if (a.ParentIndex == 0) != (b.ParentIndex == 0) {
				return b.ParentIndex == 0
			}
			return a.ParentIndex < b.ParentIndex
		}
		if a.Index != b.Index {
			return a.Index < b.Index
		}
		return a.Title < b.Title
	})
}

// Episodes returns one season of a show in episode order.
func (ix *Index) Episodes(title string, season int, fav map[string]bool) []Media {
	s, ok := ix.showByName[title]
	if !ok {
		return []Media{}
	}
	var eps []*plex.MediaItem
	for _, ep := range s.episodes {
		if int(ep.ParentIndex) == season {
			eps = append(eps, ep)
		}
	}
	sortEpisodes(eps)
	out := make([]Media, 0, len(eps))
	for _, ep := range eps {
		out = append(out, ix.media(ep, fav))
	}
	return out
}

// Genres returns the most common movie genres, most frequent first.
func (ix *Index) Genres() []string {
	ix.genresOnce.Do(func() {
		counts := map[string]int{}
		for _, it := range ix.movies {
			for _, g := range strings.Split(it.Genre, ",") {
				if g = strings.TrimSpace(g); g != "" {
					counts[g]++
				}
			}
		}
		for g := range counts {
			ix.genres = append(ix.genres, g)
		}
		sort.Slice(ix.genres, func(i, j int) bool {
			if counts[ix.genres[i]] != counts[ix.genres[j]] {
				return counts[ix.genres[i]] > counts[ix.genres[j]]
			}
			return ix.genres[i] < ix.genres[j]
		})
		if len(ix.genres) > genreLimit {
			ix.genres = ix.genres[:genreLimit]
		}
		if ix.genres == nil {
			ix.genres = []string{}
		}
	})
	return ix.genres
}

// Search finds movies and shows by title (fuzzy), or movies by a
// field-scoped query: director:"Name", cast:"Name", genre:"Comedy".
func (ix *Index) Search(query string, limit int, fav map[string]bool) []Card {
	query = strings.TrimSpace(query)
	if limit <= 0 || limit > maxLimit {
		limit = 60
	}
	if query == "" {
		return []Card{}
	}
	if field, value, ok := parseFieldQuery(query); ok {
		var out []Card
		for _, it := range ix.movies {
			var v string
			switch field {
			case "director":
				v = it.Director
			case "cast":
				v = it.Cast
			case "genre":
				v = it.Genre
			}
			if hasTag(v, value) {
				out = append(out, ix.card(it, fav))
				if len(out) >= limit {
					break
				}
			}
		}
		if out == nil {
			out = []Card{}
		}
		return out
	}

	ix.titlesOnce.Do(func() {
		ix.titles = make([]string, 0, len(ix.movies)+len(ix.shows))
		for _, it := range ix.movies {
			ix.titles = append(ix.titles, it.Title)
		}
		for _, s := range ix.shows {
			ix.titles = append(ix.titles, s.title)
		}
	})

	lq := strings.ToLower(query)
	type hit struct {
		idx   int
		score int
	}
	var hits []hit
	seen := map[int]bool{}
	// Exact and substring matches rank first so a precise title wins over
	// a fuzzy one.
	for i, t := range ix.titles {
		lt := strings.ToLower(t)
		switch {
		case lt == lq:
			hits = append(hits, hit{i, 1 << 30})
		case strings.HasPrefix(lt, lq):
			hits = append(hits, hit{i, 1<<29 - len(lt)})
		case strings.Contains(lt, lq):
			hits = append(hits, hit{i, 1<<28 - len(lt)})
		default:
			continue
		}
		seen[i] = true
	}
	if len(hits) < limit {
		for _, m := range fuzzy.Find(query, ix.titles) {
			if !seen[m.Index] {
				hits = append(hits, hit{m.Index, m.Score})
				seen[m.Index] = true
			}
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	out := make([]Card, 0, limit)
	for _, h := range hits {
		if len(out) >= limit {
			break
		}
		if h.idx < len(ix.movies) {
			out = append(out, ix.card(ix.movies[h.idx], fav))
		} else {
			out = append(out, ix.showCard(ix.shows[h.idx-len(ix.movies)], fav))
		}
	}
	return out
}

// ---- helpers ----

func parseFieldQuery(q string) (field, value string, ok bool) {
	i := strings.IndexByte(q, ':')
	if i <= 0 {
		return "", "", false
	}
	field = strings.ToLower(strings.TrimSpace(q[:i]))
	switch field {
	case "director", "cast", "genre":
	default:
		return "", "", false
	}
	value = strings.Trim(strings.TrimSpace(q[i+1:]), `"`)
	return field, value, value != ""
}

func hasTag(field, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	for _, g := range strings.Split(field, ",") {
		if strings.ToLower(strings.TrimSpace(g)) == want {
			return true
		}
	}
	return false
}

func less(asc, desc, wantDesc bool) bool {
	if wantDesc {
		return desc
	}
	return asc
}

func sortMovies(items []*plex.MediaItem, field string, desc bool) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		switch field {
		case "year":
			if a.Year != b.Year {
				return less(a.Year < b.Year, a.Year > b.Year, desc)
			}
		case "added":
			if a.AddedAt != b.AddedAt {
				return less(a.AddedAt < b.AddedAt, a.AddedAt > b.AddedAt, desc)
			}
		case "rating":
			if a.Rating != b.Rating {
				return less(a.Rating < b.Rating, a.Rating > b.Rating, desc)
			}
		case "duration":
			if a.Duration != b.Duration {
				return less(a.Duration < b.Duration, a.Duration > b.Duration, desc)
			}
		default:
			la, lb := strings.ToLower(a.Title), strings.ToLower(b.Title)
			if la != lb {
				return less(la < lb, la > lb, desc)
			}
		}
		return strings.ToLower(a.Title) < strings.ToLower(b.Title)
	})
}

// sortShows orders shows; "" and "added" keep newest-episode-first (desc)
// or reverse it (asc for "added").
func sortShows(shows []*showInfo, field string, desc bool) {
	switch field {
	case "", "added":
		if field == "added" && !desc {
			for i, j := 0, len(shows)-1; i < j; i, j = i+1, j-1 {
				shows[i], shows[j] = shows[j], shows[i]
			}
		}
	case "year":
		sort.SliceStable(shows, func(i, j int) bool {
			a, b := shows[i].rep.Year, shows[j].rep.Year
			if a != b {
				return less(a < b, a > b, desc)
			}
			return strings.ToLower(shows[i].title) < strings.ToLower(shows[j].title)
		})
	default:
		sort.SliceStable(shows, func(i, j int) bool {
			la, lb := strings.ToLower(shows[i].title), strings.ToLower(shows[j].title)
			return less(la < lb, la > lb, desc)
		})
	}
}
