package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/joshkerr/goplexcli/internal/cache"
	"github.com/joshkerr/goplexcli/internal/plex"
	"github.com/sahilm/fuzzy"
)

// MediaDTO is the frontend-facing shape of a media item (or a synthetic "show"
// row). For real items Key is the Plex metadata key; for grouped shows it is
// "show:<title>".
type MediaDTO struct {
	Key           string  `json:"key"`
	Type          string  `json:"type"` // movie | show | episode
	Title         string  `json:"title"`
	DisplayTitle  string  `json:"displayTitle"`
	Year          int     `json:"year"`
	Summary       string  `json:"summary"`
	Rating        float64 `json:"rating"`
	Duration      int     `json:"duration"`
	ContentRating string  `json:"contentRating"`
	Studio        string  `json:"studio"`
	Director      string  `json:"director"`
	Genre         string  `json:"genre"`
	Cast          string  `json:"cast"`
	ParentTitle   string  `json:"parentTitle"` // show name (episodes)
	GrandTitle    string  `json:"grandTitle"`  // season name (episodes)
	Index         int64   `json:"index"`       // episode number
	ParentIndex   int64   `json:"parentIndex"` // season number
	ViewOffset    int     `json:"viewOffset"`
	ViewCount     int     `json:"viewCount"`
	ProgressPct   int     `json:"progressPct"`
	ThumbURL      string  `json:"thumbURL"`
	ServerName    string  `json:"serverName"`
	EpisodeCount  int     `json:"episodeCount"` // for show rows
}

// MediaCardDTO is the lightweight shape sent to the poster grid. It omits the
// heavy text fields (summary, cast, genre, director) so a 20k-item library
// serializes to a manageable payload; full details are fetched per-item via
// GetItem when a card is opened.
type MediaCardDTO struct {
	Key          string `json:"key"`
	Type         string `json:"type"`
	Title        string `json:"title"`
	Year         int    `json:"year"`
	DisplayTitle string `json:"displayTitle"`
	ThumbURL     string `json:"thumbURL"`
	ProgressPct  int    `json:"progressPct"`
	ViewCount    int    `json:"viewCount"`
	EpisodeCount int    `json:"episodeCount"`
}

// SeasonDTO describes one season of a show for the drill-down view.
type SeasonDTO struct {
	Season       int `json:"season"`
	EpisodeCount int `json:"episodeCount"`
}

// media returns the parsed media cache, memoized in memory. The first call (or
// the first after an invalidation) reads and decodes media.json; subsequent
// calls reuse the parsed copy. Returns nil if the cache can't be loaded.
func (a *App) media() *cache.Cache {
	a.mediaMu.RLock()
	c := a.mediaCache
	a.mediaMu.RUnlock()
	if c != nil {
		return c
	}

	loaded, err := cache.Load()
	if err != nil {
		return nil
	}
	a.mediaMu.Lock()
	a.mediaCache = loaded
	a.mediaMu.Unlock()
	return loaded
}

// setMedia replaces the in-memory media cache (used after a reindex so the new
// library is served without re-reading disk).
func (a *App) setMedia(c *cache.Cache) {
	a.mediaMu.Lock()
	a.mediaCache = c
	a.mediaMu.Unlock()
}

// invalidateMedia drops the in-memory copy so the next access reloads from disk.
func (a *App) invalidateMedia() {
	a.mediaMu.Lock()
	a.mediaCache = nil
	a.mediaMu.Unlock()
}

// thumbURL registers a token-free, same-origin URL backed by the persistent
// poster cache. Plex produces a rendition sized for its display context.
func (a *App) thumbURL(item *plex.MediaItem, width, height int) string {
	if item.Thumb == "" {
		return ""
	}
	base := item.ServerURL
	if base == "" {
		return ""
	}
	return a.posters.register(posterSource{
		ServerURL: strings.TrimRight(base, "/"),
		ThumbPath: item.Thumb,
		Token:     a.config().TokenForURL(base),
		Width:     width,
		Height:    height,
	})
}

// showThumbURL builds a poster URL for a show card derived from one of its
// episodes. It prefers the show poster (the episode's grandparentThumb) over the
// episode still (Thumb), which is only a screenshot from that episode. Older
// caches indexed before grandparentThumb was captured fall back to the still.
func (a *App) showThumbURL(episode *plex.MediaItem, width, height int) string {
	if episode.GrandparentThumb == "" {
		return a.thumbURL(episode, width, height)
	}
	base := episode.ServerURL
	if base == "" {
		return ""
	}
	return a.posters.register(posterSource{
		ServerURL: strings.TrimRight(base, "/"),
		ThumbPath: episode.GrandparentThumb,
		Token:     a.config().TokenForURL(base),
		Width:     width,
		Height:    height,
	})
}

// toDTO converts a cached MediaItem into its frontend shape.
func (a *App) toDTO(item *plex.MediaItem) MediaDTO {
	return MediaDTO{
		Key:           item.Key,
		Type:          item.Type,
		Title:         item.Title,
		DisplayTitle:  item.FormatMediaTitle(),
		Year:          item.Year,
		Summary:       item.Summary,
		Rating:        item.Rating,
		Duration:      item.Duration,
		ContentRating: item.ContentRating,
		Studio:        item.Studio,
		Director:      item.Director,
		Genre:         item.Genre,
		Cast:          item.Cast,
		ParentTitle:   item.ParentTitle,
		GrandTitle:    item.GrandTitle,
		Index:         item.Index,
		ParentIndex:   item.ParentIndex,
		ViewOffset:    item.ViewOffset,
		ViewCount:     item.ViewCount,
		ProgressPct:   progressPct(item),
		ThumbURL:      a.thumbURL(item, 500, 750),
		ServerName:    item.ServerName,
	}
}

// toCard converts a cached MediaItem into the lightweight grid shape. Episode
// cards (e.g. "New Episodes") use the show poster rather than the episode still,
// which is a 16:9 screenshot that is often missing and doesn't suit the tall
// poster grid.
func (a *App) toCard(item *plex.MediaItem) MediaCardDTO {
	thumb := a.thumbURL(item, 320, 480)
	if item.Type == "episode" {
		thumb = a.showThumbURL(item, 320, 480)
	}
	return MediaCardDTO{
		Key:          item.Key,
		Type:         item.Type,
		Title:        item.Title,
		Year:         item.Year,
		DisplayTitle: item.FormatMediaTitle(),
		ThumbURL:     thumb,
		ProgressPct:  progressPct(item),
		ViewCount:    item.ViewCount,
	}
}

// progressPct returns the watched percentage (0-100) for an item, matching the
// logic in plex.FormatMediaTitle.
func progressPct(item *plex.MediaItem) int {
	if item.Duration <= 0 || item.ViewOffset <= 0 {
		return 0
	}
	return int(float64(item.ViewOffset) * 100 / float64(item.Duration))
}

// isInProgress reports whether an item belongs in "Continue Watching": it has a
// resume position and is less than 95% complete.
func isInProgress(item *plex.MediaItem) bool {
	pct := progressPct(item)
	return item.ViewOffset > 0 && pct > 0 && pct < 95
}

// BrowseOptions carries the genre filter and sort order the frontend applies to
// the Movies grid. It's ignored for every other category. An empty value
// (Genre "", SortField "") yields the historical default (all movies, A-Z).
type BrowseOptions struct {
	Genre     string `json:"genre"`     // "" = all genres
	SortField string `json:"sortField"` // title | year | added | rating | duration
	Desc      bool   `json:"desc"`
}

// ListCategory returns the poster-grid rows for a sidebar category as
// lightweight cards, read from the in-memory cache. opts (genre filter + sort)
// is honored only for the "movies" category; other categories keep their fixed
// ordering.
//
//	movies                  — all movies, filtered/sorted per opts (default A-Z)
//	tv-shows                — distinct shows (grouped from episodes), A-Z
//	recently-added-movies   — newest movies by AddedAt
//	recently-added-tv       — newest episodes by AddedAt
//	continue-watching       — in-progress items, most recently viewed first
func (a *App) ListCategory(category string, opts BrowseOptions) []MediaCardDTO {
	c := a.media()
	if c == nil {
		return []MediaCardDTO{}
	}

	switch category {
	case "movies":
		items := sortMovieItems(c, opts)
		out := make([]MediaCardDTO, 0, len(items))
		for _, it := range items {
			out = append(out, a.toCard(it))
		}
		return a.warmedCards(out)

	case "tv-shows":
		return a.warmedCards(a.groupShowCards(c))

	case "recently-added-movies":
		return a.warmedCards(recentlyAddedCards(a, c, "movie", 60))

	case "recently-added-tv":
		return a.warmedCards(recentlyAddedCards(a, c, "episode", 60))

	case "continue-watching":
		var items []*plex.MediaItem
		for i := range c.Media {
			if isInProgress(&c.Media[i]) {
				items = append(items, &c.Media[i])
			}
		}
		sort.Slice(items, func(i, j int) bool { return items[i].LastViewedAt > items[j].LastViewedAt })
		out := make([]MediaCardDTO, 0, len(items))
		for _, it := range items {
			out = append(out, a.toCard(it))
		}
		return a.warmedCards(out)
	}

	return []MediaCardDTO{}
}

// GetItem returns the full details for a single card key, fetched on demand
// when a card is opened. The key may be a real Plex metadata key or a synthetic
// "show:<title>" key produced by show grouping.
func (a *App) GetItem(key string) (MediaDTO, error) {
	c := a.media()
	if c == nil {
		return MediaDTO{}, fmt.Errorf("media cache is empty")
	}

	if title, ok := strings.CutPrefix(key, "show:"); ok {
		return a.showDTO(c, title)
	}

	for i := range c.Media {
		if c.Media[i].Key == key {
			return a.toDTO(&c.Media[i]), nil
		}
	}
	return MediaDTO{}, fmt.Errorf("item not found")
}

// showDTO builds a full show detail row by aggregating its episodes (summary,
// genre and poster come from the first episode; the count from all of them).
func (a *App) showDTO(c *cache.Cache, title string) (MediaDTO, error) {
	dto := MediaDTO{Key: "show:" + title, Type: "show", Title: title, DisplayTitle: title}
	found := false
	for i := range c.Media {
		item := &c.Media[i]
		if item.Type != "episode" || item.ParentTitle != title {
			continue
		}
		if !found {
			dto.Year = item.Year
			dto.Summary = item.Summary
			dto.Genre = item.Genre
			dto.ThumbURL = a.showThumbURL(item, 500, 750)
			dto.ServerName = item.ServerName
			found = true
		}
		dto.EpisodeCount++
	}
	if !found {
		return MediaDTO{}, fmt.Errorf("show not found")
	}
	return dto, nil
}

// movieGenreLimit caps how many genres the movie genre filter offers. The
// library's genre tags have a long tail (niche tags like "News" or "Short"); we
// surface only the most common ones, which map to Plex's primary genres.
const movieGenreLimit = 12

// sortMovieItems returns the movie items matching opts.Genre, ordered by
// opts.SortField / opts.Desc. An empty opts yields all movies sorted A-Z by
// title, matching the historical default. Title is the tiebreaker for every
// field so equal values keep a stable, readable order.
func sortMovieItems(c *cache.Cache, opts BrowseOptions) []*plex.MediaItem {
	var items []*plex.MediaItem
	for i := range c.Media {
		if c.Media[i].Type != "movie" {
			continue
		}
		if opts.Genre != "" && !hasGenre(c.Media[i].Genre, opts.Genre) {
			continue
		}
		items = append(items, &c.Media[i])
	}

	byTitle := func(i, j int) bool {
		return strings.ToLower(items[i].Title) < strings.ToLower(items[j].Title)
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		switch opts.SortField {
		case "year":
			if a.Year != b.Year {
				return less(a.Year < b.Year, a.Year > b.Year, opts.Desc)
			}
		case "added":
			if a.AddedAt != b.AddedAt {
				return less(a.AddedAt < b.AddedAt, a.AddedAt > b.AddedAt, opts.Desc)
			}
		case "rating":
			if a.Rating != b.Rating {
				return less(a.Rating < b.Rating, a.Rating > b.Rating, opts.Desc)
			}
		case "duration":
			if a.Duration != b.Duration {
				return less(a.Duration < b.Duration, a.Duration > b.Duration, opts.Desc)
			}
		default: // "title" and unknown fields
			la, lb := strings.ToLower(a.Title), strings.ToLower(b.Title)
			if la != lb {
				return less(la < lb, la > lb, opts.Desc)
			}
		}
		return byTitle(i, j)
	})
	return items
}

// less picks the ascending or descending comparison result based on desc.
func less(asc, desc, wantDesc bool) bool {
	if wantDesc {
		return desc
	}
	return asc
}

// hasGenre reports whether a comma-separated genre field ("Romance, Comedy")
// contains want, compared case-insensitively as a whole token.
func hasGenre(genreField, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	for _, g := range strings.Split(genreField, ",") {
		if strings.ToLower(strings.TrimSpace(g)) == want {
			return true
		}
	}
	return false
}

// MovieGenres returns the most common movie genres (up to movieGenreLimit),
// most frequent first, for the genre filter dropdown. Ties are broken
// alphabetically so the list is stable across calls.
func (a *App) MovieGenres() []string {
	c := a.media()
	if c == nil {
		return []string{}
	}
	counts := map[string]int{}
	for i := range c.Media {
		if c.Media[i].Type != "movie" {
			continue
		}
		for _, g := range strings.Split(c.Media[i].Genre, ",") {
			g = strings.TrimSpace(g)
			if g != "" {
				counts[g]++
			}
		}
	}
	genres := make([]string, 0, len(counts))
	for g := range counts {
		genres = append(genres, g)
	}
	sort.Slice(genres, func(i, j int) bool {
		if counts[genres[i]] != counts[genres[j]] {
			return counts[genres[i]] > counts[genres[j]]
		}
		return genres[i] < genres[j]
	})
	if len(genres) > movieGenreLimit {
		genres = genres[:movieGenreLimit]
	}
	return genres
}

// recentlyAddedCards returns the newest items of a given type, newest first.
func recentlyAddedCards(a *App, c *cache.Cache, mediaType string, limit int) []MediaCardDTO {
	var items []*plex.MediaItem
	for i := range c.Media {
		if c.Media[i].Type == mediaType {
			items = append(items, &c.Media[i])
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].AddedAt > items[j].AddedAt })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	out := make([]MediaCardDTO, 0, len(items))
	for _, it := range items {
		out = append(out, a.toCard(it))
	}
	return out
}

// groupShowCards collapses cached episodes into one card per show, keyed by show
// title (ParentTitle). The first episode encountered supplies a poster. Shows are
// ordered by their most recently added episode, newest first.
func (a *App) groupShowCards(c *cache.Cache) []MediaCardDTO {
	order := []string{}
	byShow := map[string]*MediaCardDTO{}
	latestAdded := map[string]int64{}
	for i := range c.Media {
		item := &c.Media[i]
		if item.Type != "episode" || item.ParentTitle == "" {
			continue
		}
		show, ok := byShow[item.ParentTitle]
		if !ok {
			card := MediaCardDTO{
				Key:          "show:" + item.ParentTitle,
				Type:         "show",
				Title:        item.ParentTitle,
				DisplayTitle: item.ParentTitle,
				Year:         item.Year,
				ThumbURL:     a.showThumbURL(item, 320, 480),
			}
			byShow[item.ParentTitle] = &card
			order = append(order, item.ParentTitle)
			show = &card
		}
		show.EpisodeCount++
		if item.AddedAt > latestAdded[item.ParentTitle] {
			latestAdded[item.ParentTitle] = item.AddedAt
		}
	}
	out := make([]MediaCardDTO, 0, len(order))
	for _, name := range order {
		out = append(out, *byShow[name])
	}
	sort.Slice(out, func(i, j int) bool {
		ai, aj := latestAdded[out[i].Title], latestAdded[out[j].Title]
		if ai != aj {
			return ai > aj
		}
		return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
	})
	return out
}

// GetSeasons returns the seasons available for a show, ascending.
func (a *App) GetSeasons(showTitle string) []SeasonDTO {
	c := a.media()
	if c == nil {
		return []SeasonDTO{}
	}
	counts := map[int]int{}
	for i := range c.Media {
		item := &c.Media[i]
		if item.Type == "episode" && item.ParentTitle == showTitle {
			counts[int(item.ParentIndex)]++
		}
	}
	out := make([]SeasonDTO, 0, len(counts))
	for season, n := range counts {
		out = append(out, SeasonDTO{Season: season, EpisodeCount: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Season < out[j].Season })
	return out
}

// GetEpisodes returns the episodes of a show's season, in episode order. A
// season holds at most a few dozen items, so it returns full DTOs (the episode
// list in the detail modal needs titles, durations and progress).
func (a *App) GetEpisodes(showTitle string, season int) []MediaDTO {
	c := a.media()
	if c == nil {
		return []MediaDTO{}
	}
	var items []*plex.MediaItem
	for i := range c.Media {
		item := &c.Media[i]
		if item.Type == "episode" && item.ParentTitle == showTitle && int(item.ParentIndex) == season {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Index < items[j].Index })
	out := make([]MediaDTO, 0, len(items))
	for _, it := range items {
		out = append(out, a.toDTO(it))
	}
	if out == nil {
		return []MediaDTO{}
	}
	return out
}

// searchLimit caps the number of results returned for a query. With a
// virtualized grid the frontend can render more, but capping keeps results
// relevant and the payload small.
const searchLimit = 200

// Search fuzzy-matches the query against movie titles and show names, returning
// lightweight cards (capped at searchLimit). An empty query returns nothing.
func (a *App) Search(query string) []MediaCardDTO {
	query = strings.TrimSpace(query)
	if query == "" {
		return []MediaCardDTO{}
	}
	c := a.media()
	if c == nil {
		return []MediaCardDTO{}
	}

	// Candidate set: all movies plus one card per show.
	candidates := make([]MediaCardDTO, 0, len(c.Media))
	for i := range c.Media {
		if c.Media[i].Type == "movie" {
			candidates = append(candidates, a.toCard(&c.Media[i]))
		}
	}
	candidates = append(candidates, a.groupShowCards(c)...)

	titles := make([]string, len(candidates))
	for i := range candidates {
		titles[i] = candidates[i].Title
	}

	matches := fuzzy.Find(query, titles)
	out := make([]MediaCardDTO, 0, min(len(matches), searchLimit))
	for _, m := range matches {
		out = append(out, candidates[m.Index])
		if len(out) >= searchLimit {
			break
		}
	}
	a.warmCards(out)
	return nonNilCards(out)
}

// nonNilCards guarantees a non-nil slice so the frontend always receives a JSON
// array, never null.
func nonNilCards(in []MediaCardDTO) []MediaCardDTO {
	if in == nil {
		return []MediaCardDTO{}
	}
	return in
}

// warmPosterCount is how many of a result set's posters are pre-fetched into
// the disk cache when the set is computed — enough to cover the first few
// scrolls of a virtualized grid without transcoding the whole library.
const warmPosterCount = 60

// warmedCards warms the first posters of a result set and returns it as a
// guaranteed non-nil slice — a convenience for the many category branches.
func (a *App) warmedCards(cards []MediaCardDTO) []MediaCardDTO {
	a.warmCards(cards)
	return nonNilCards(cards)
}

// WarmPosters pre-fetches the given poster URLs into the disk cache off the UI
// thread. The frontend calls it as the visible grid window settles (notably
// after a far jump-scroll) so the backend transcodes that window in parallel on
// its own connection pool — independent of the browser's ~6-connections-per-
// origin cap — turning the browser's subsequent image requests into fast file
// serves. Already-cached posters are skipped cheaply (a stat). Returns
// immediately; warming continues in the background.
func (a *App) WarmPosters(urls []string) {
	if a.posters == nil || len(urls) == 0 {
		return
	}
	go a.posters.warm(urls)
}

// warmCards kicks off a background warm of the first posters in a result set so
// they're cached before the browser requests them. It's a no-op once the
// posters are already on disk (the warmer stats before fetching).
func (a *App) warmCards(cards []MediaCardDTO) {
	if a.posters == nil {
		return
	}
	urls := make([]string, 0, warmPosterCount)
	for i := range cards {
		if len(urls) >= warmPosterCount {
			break
		}
		if cards[i].ThumbURL != "" {
			urls = append(urls, cards[i].ThumbURL)
		}
	}
	if len(urls) > 0 {
		go a.posters.warm(urls)
	}
}
