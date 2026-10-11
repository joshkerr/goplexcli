// Package tmdb is a small, polite client for The Movie Database API v3.
//
// It rate limits requests with a token bucket, retries on 429 (honoring
// Retry-After) and on 5xx/transport errors with bounded backoff, and caches
// every successful GET through an optional Store so that reindexing a large
// library does not re-hit the API.
package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ImageBase is the root of TMDB's image CDN.
const ImageBase = "https://image.tmdb.org/t/p"

// DefaultBaseURL is the public TMDB v3 API endpoint.
const DefaultBaseURL = "https://api.themoviedb.org/3"

// ErrNotFound is returned by Movie, TV and Season when TMDB answers 404.
var ErrNotFound = errors.New("tmdb: not found")

const (
	maxAttempts       = 4
	defaultRetryAfter = 2 * time.Second
	maxCastNames      = 20
)

var backoffSchedule = []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second}

// Client talks to the TMDB v3 API.
type Client struct {
	apiKey  string
	bearer  bool
	store   Store
	http    *http.Client
	limiter *tokenBucket

	mu      sync.RWMutex
	baseURL string
}

// New returns a client. store may be nil (no caching). A v3 key is sent as
// api_key=, a v4 token (starts with "eyJ") as Authorization: Bearer.
func New(apiKey string, store Store) *Client {
	return &Client{
		apiKey:  apiKey,
		bearer:  strings.HasPrefix(apiKey, "eyJ"),
		store:   store,
		http:    &http.Client{Timeout: 30 * time.Second},
		limiter: newTokenBucket(35),
		baseURL: DefaultBaseURL,
	}
}

// SetBaseURL overrides the API endpoint (e.g. to point at an httptest server).
func (c *Client) SetBaseURL(u string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baseURL = strings.TrimRight(u, "/")
}

// SetRateLimit changes the request rate. Values <= 0 disable rate limiting.
func (c *Client) SetRateLimit(perSecond float64) {
	c.limiter.setRate(perSecond)
}

// Movie is the subset of a TMDB movie record used by the indexer.
type Movie struct {
	ID            int
	IMDBID        string
	Title         string
	OriginalTitle string
	Overview      string
	Tagline       string
	ReleaseDate   string // "2006-01-02"
	Year          int    // parsed from ReleaseDate, 0 if unknown
	Runtime       int    // minutes
	VoteAverage   float64
	Genres        []string
	Studios       []string // production_companies names
	Directors     []string // crew with Job == "Director"
	Cast          []string // cast names in billing order, max 20
	Certification string   // US certification from release_dates, "" if none
	PosterPath    string   // e.g. "/abc.jpg"
	BackdropPath  string
}

// Show is the subset of a TMDB TV record used by the indexer.
type Show struct {
	ID             int
	IMDBID         string
	Name           string
	OriginalName   string
	Overview       string
	FirstAirDate   string
	Year           int
	EpisodeRunTime int // first of episode_run_time, minutes; 0 if none
	VoteAverage    float64
	Genres         []string
	Studios        []string // networks names first, then production_companies
	Creators       []string // created_by names
	Cast           []string // max 20
	ContentRating  string   // US rating from content_ratings, "" if none
	PosterPath     string
	BackdropPath   string
	Seasons        []SeasonSummary
}

// SeasonSummary describes one season of a Show.
type SeasonSummary struct {
	SeasonNumber int
	EpisodeCount int
}

// Episode is one episode from a season payload.
type Episode struct {
	SeasonNumber  int
	EpisodeNumber int
	Name          string
	Overview      string
	AirDate       string
	StillPath     string
	Runtime       int // minutes; 0 if unknown
	VoteAverage   float64
	Directors     []string // crew Job == "Director" if present in the season payload
}

// ---- wire types ----------------------------------------------------------

type named struct {
	Name string `json:"name"`
}

type creditsPayload struct {
	Cast []struct {
		Name  string `json:"name"`
		Order int    `json:"order"`
	} `json:"cast"`
	Crew []struct {
		Name string `json:"name"`
		Job  string `json:"job"`
	} `json:"crew"`
}

func (p creditsPayload) castNames() []string {
	var out []string
	for _, c := range p.Cast {
		if c.Name == "" {
			continue
		}
		out = append(out, c.Name)
		if len(out) >= maxCastNames {
			break
		}
	}
	return out
}

func (p creditsPayload) directors() []string {
	var out []string
	for _, c := range p.Crew {
		if c.Job == "Director" && c.Name != "" {
			out = append(out, c.Name)
		}
	}
	return out
}

type externalIDs struct {
	IMDBID string `json:"imdb_id"`
}

type moviePayload struct {
	ID                  int            `json:"id"`
	IMDBID              string         `json:"imdb_id"`
	Title               string         `json:"title"`
	OriginalTitle       string         `json:"original_title"`
	Overview            string         `json:"overview"`
	Tagline             string         `json:"tagline"`
	ReleaseDate         string         `json:"release_date"`
	Runtime             int            `json:"runtime"`
	VoteAverage         float64        `json:"vote_average"`
	Genres              []named        `json:"genres"`
	ProductionCompanies []named        `json:"production_companies"`
	PosterPath          string         `json:"poster_path"`
	BackdropPath        string         `json:"backdrop_path"`
	Credits             creditsPayload `json:"credits"`
	ReleaseDates        struct {
		Results []struct {
			ISO          string `json:"iso_3166_1"`
			ReleaseDates []struct {
				Certification string `json:"certification"`
			} `json:"release_dates"`
		} `json:"results"`
	} `json:"release_dates"`
	ExternalIDs externalIDs `json:"external_ids"`
}

type tvPayload struct {
	ID                  int     `json:"id"`
	Name                string  `json:"name"`
	OriginalName        string  `json:"original_name"`
	Overview            string  `json:"overview"`
	FirstAirDate        string  `json:"first_air_date"`
	EpisodeRunTime      []int   `json:"episode_run_time"`
	VoteAverage         float64 `json:"vote_average"`
	Genres              []named `json:"genres"`
	Networks            []named `json:"networks"`
	ProductionCompanies []named `json:"production_companies"`
	CreatedBy           []named `json:"created_by"`
	PosterPath          string  `json:"poster_path"`
	BackdropPath        string  `json:"backdrop_path"`
	Seasons             []struct {
		SeasonNumber int `json:"season_number"`
		EpisodeCount int `json:"episode_count"`
	} `json:"seasons"`
	Credits        creditsPayload `json:"credits"`
	ContentRatings struct {
		Results []struct {
			ISO    string `json:"iso_3166_1"`
			Rating string `json:"rating"`
		} `json:"results"`
	} `json:"content_ratings"`
	ExternalIDs externalIDs `json:"external_ids"`
}

type seasonPayload struct {
	SeasonNumber int `json:"season_number"`
	Episodes     []struct {
		SeasonNumber  int     `json:"season_number"`
		EpisodeNumber int     `json:"episode_number"`
		Name          string  `json:"name"`
		Overview      string  `json:"overview"`
		AirDate       string  `json:"air_date"`
		StillPath     string  `json:"still_path"`
		Runtime       *int    `json:"runtime"`
		VoteAverage   float64 `json:"vote_average"`
		Crew          []struct {
			Name string `json:"name"`
			Job  string `json:"job"`
		} `json:"crew"`
	} `json:"episodes"`
}

type findPayload struct {
	MovieResults []struct {
		ID int `json:"id"`
	} `json:"movie_results"`
	TVResults []struct {
		ID int `json:"id"`
	} `json:"tv_results"`
}

type searchPayload struct {
	Results []struct {
		ID           int    `json:"id"`
		ReleaseDate  string `json:"release_date"`
		FirstAirDate string `json:"first_air_date"`
	} `json:"results"`
}

// ---- public API ------------------------------------------------------------

// FindByIMDB uses GET /find/{imdb}?external_source=imdb_id. Returns the first
// movie_results id and the first tv_results id (either may be 0). err only on
// transport/HTTP failure.
func (c *Client) FindByIMDB(ctx context.Context, imdbID string) (movieID, tvID int, err error) {
	var p findPayload
	q := url.Values{"external_source": {"imdb_id"}}
	if err := c.getJSON(ctx, "/find/"+url.PathEscape(imdbID), q, &p); err != nil {
		if errors.Is(err, ErrNotFound) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	if len(p.MovieResults) > 0 {
		movieID = p.MovieResults[0].ID
	}
	if len(p.TVResults) > 0 {
		tvID = p.TVResults[0].ID
	}
	return movieID, tvID, nil
}

// SearchMovie uses GET /search/movie?query=&year= (when year>0), then retries
// without year if nothing matched. Prefers a result whose release_date year
// equals year; otherwise the first result. Returns 0 when nothing is found
// (no error).
func (c *Client) SearchMovie(ctx context.Context, title string, year int) (int, error) {
	return c.search(ctx, "/search/movie", "year", title, year, false)
}

// SearchTV is SearchMovie for TV shows, using /search/tv and
// first_air_date_year=.
func (c *Client) SearchTV(ctx context.Context, name string, year int) (int, error) {
	return c.search(ctx, "/search/tv", "first_air_date_year", name, year, true)
}

func (c *Client) search(ctx context.Context, path, yearParam, query string, year int, tv bool) (int, error) {
	if strings.TrimSpace(query) == "" {
		return 0, nil
	}
	run := func(withYear bool) (int, error) {
		q := url.Values{"query": {query}, "include_adult": {"false"}}
		if withYear {
			q.Set(yearParam, strconv.Itoa(year))
		}
		var p searchPayload
		if err := c.getJSON(ctx, path, q, &p); err != nil {
			if errors.Is(err, ErrNotFound) {
				return 0, nil
			}
			return 0, err
		}
		if len(p.Results) == 0 {
			return 0, nil
		}
		if year > 0 {
			for _, r := range p.Results {
				date := r.ReleaseDate
				if tv {
					date = r.FirstAirDate
				}
				if yearOf(date) == year {
					return r.ID, nil
				}
			}
		}
		return p.Results[0].ID, nil
	}
	if year > 0 {
		id, err := run(true)
		if err != nil || id != 0 {
			return id, err
		}
	}
	return run(false)
}

// Movie fetches GET /movie/{id}?append_to_response=credits,release_dates,external_ids.
func (c *Client) Movie(ctx context.Context, id int) (*Movie, error) {
	var p moviePayload
	q := url.Values{"append_to_response": {"credits,release_dates,external_ids"}}
	if err := c.getJSON(ctx, "/movie/"+strconv.Itoa(id), q, &p); err != nil {
		return nil, err
	}
	m := &Movie{
		ID:            p.ID,
		IMDBID:        firstNonEmpty(p.ExternalIDs.IMDBID, p.IMDBID),
		Title:         p.Title,
		OriginalTitle: p.OriginalTitle,
		Overview:      p.Overview,
		Tagline:       p.Tagline,
		ReleaseDate:   p.ReleaseDate,
		Year:          yearOf(p.ReleaseDate),
		Runtime:       p.Runtime,
		VoteAverage:   p.VoteAverage,
		Genres:        names(p.Genres),
		Studios:       names(p.ProductionCompanies),
		Directors:     p.Credits.directors(),
		Cast:          p.Credits.castNames(),
		PosterPath:    p.PosterPath,
		BackdropPath:  p.BackdropPath,
	}
	for _, r := range p.ReleaseDates.Results {
		if r.ISO != "US" {
			continue
		}
		for _, rd := range r.ReleaseDates {
			if rd.Certification != "" {
				m.Certification = rd.Certification
				break
			}
		}
		if m.Certification != "" {
			break
		}
	}
	return m, nil
}

// TV fetches GET /tv/{id}?append_to_response=credits,content_ratings,external_ids.
func (c *Client) TV(ctx context.Context, id int) (*Show, error) {
	var p tvPayload
	q := url.Values{"append_to_response": {"credits,content_ratings,external_ids"}}
	if err := c.getJSON(ctx, "/tv/"+strconv.Itoa(id), q, &p); err != nil {
		return nil, err
	}
	s := &Show{
		ID:           p.ID,
		IMDBID:       p.ExternalIDs.IMDBID,
		Name:         p.Name,
		OriginalName: p.OriginalName,
		Overview:     p.Overview,
		FirstAirDate: p.FirstAirDate,
		Year:         yearOf(p.FirstAirDate),
		VoteAverage:  p.VoteAverage,
		Genres:       names(p.Genres),
		Studios:      append(names(p.Networks), names(p.ProductionCompanies)...),
		Creators:     names(p.CreatedBy),
		Cast:         p.Credits.castNames(),
		PosterPath:   p.PosterPath,
		BackdropPath: p.BackdropPath,
	}
	if len(p.EpisodeRunTime) > 0 {
		s.EpisodeRunTime = p.EpisodeRunTime[0]
	}
	for _, r := range p.ContentRatings.Results {
		if r.ISO == "US" && r.Rating != "" {
			s.ContentRating = r.Rating
			break
		}
	}
	for _, se := range p.Seasons {
		s.Seasons = append(s.Seasons, SeasonSummary{SeasonNumber: se.SeasonNumber, EpisodeCount: se.EpisodeCount})
	}
	return s, nil
}

// Season fetches GET /tv/{id}/season/{n} and returns its episodes.
func (c *Client) Season(ctx context.Context, tvID, season int) ([]Episode, error) {
	var p seasonPayload
	path := "/tv/" + strconv.Itoa(tvID) + "/season/" + strconv.Itoa(season)
	if err := c.getJSON(ctx, path, nil, &p); err != nil {
		return nil, err
	}
	eps := make([]Episode, 0, len(p.Episodes))
	for _, e := range p.Episodes {
		ep := Episode{
			SeasonNumber:  e.SeasonNumber,
			EpisodeNumber: e.EpisodeNumber,
			Name:          e.Name,
			Overview:      e.Overview,
			AirDate:       e.AirDate,
			StillPath:     e.StillPath,
			VoteAverage:   e.VoteAverage,
		}
		if ep.SeasonNumber == 0 {
			ep.SeasonNumber = season
		}
		if e.Runtime != nil {
			ep.Runtime = *e.Runtime
		}
		for _, cr := range e.Crew {
			if cr.Job == "Director" && cr.Name != "" {
				ep.Directors = append(ep.Directors, cr.Name)
			}
		}
		eps = append(eps, ep)
	}
	return eps, nil
}

// ImageURL joins ImageBase, size (e.g. "w342", "w500", "original") and a TMDB
// path ("/abc.jpg"). An empty path yields "".
func ImageURL(path, size string) string {
	if path == "" {
		return ""
	}
	if size == "" {
		size = "original"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return ImageBase + "/" + size + path
}

var posterSizes = []struct {
	name  string
	width int
}{
	{"w92", 92},
	{"w154", 154},
	{"w185", 185},
	{"w342", 342},
	{"w500", 500},
	{"w780", 780},
}

// BestSize returns the smallest standard TMDB poster size that is >= width
// (w92 w154 w185 w342 w500 w780 original); width<=0 yields "w500".
func BestSize(width int) string {
	if width <= 0 {
		return "w500"
	}
	for _, s := range posterSizes {
		if s.width >= width {
			return s.name
		}
	}
	return "original"
}

// ---- transport -------------------------------------------------------------

// httpError is returned for non-2xx responses that are not 404.
type httpError struct {
	Status int
	Body   string
}

func (e *httpError) Error() string {
	msg := strings.TrimSpace(e.Body)
	if len(msg) > 200 {
		msg = msg[:200] + "..."
	}
	if msg == "" {
		return fmt.Sprintf("tmdb: HTTP %d", e.Status)
	}
	return fmt.Sprintf("tmdb: HTTP %d: %s", e.Status, msg)
}

func (c *Client) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	data, err := c.get(ctx, path, q)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("tmdb: decode %s: %w", path, err)
	}
	return nil
}

// cacheKey is the request path plus its sorted query, never including the
// API key, so rotating keys does not invalidate the cache.
func cacheKey(path string, q url.Values) string {
	if len(q) == 0 {
		return path
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(path)
	b.WriteByte('?')
	first := true
	for _, k := range keys {
		vals := append([]string(nil), q[k]...)
		sort.Strings(vals)
		for _, v := range vals {
			if !first {
				b.WriteByte('&')
			}
			first = false
			b.WriteString(url.QueryEscape(k))
			b.WriteByte('=')
			b.WriteString(url.QueryEscape(v))
		}
	}
	return b.String()
}

// get performs a cached, rate-limited, retrying GET and returns the body of a
// 200 response.
func (c *Client) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	key := cacheKey(path, q)
	if c.store != nil {
		if data, ok := c.store.Get(key); ok {
			return data, nil
		}
	}

	full := url.Values{}
	for k, v := range q {
		full[k] = v
	}
	if c.apiKey != "" && !c.bearer {
		full.Set("api_key", c.apiKey)
	}
	c.mu.RLock()
	base := c.baseURL
	c.mu.RUnlock()
	reqURL := base + path
	if len(full) > 0 {
		reqURL += "?" + full.Encode()
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			var wait time.Duration
			var he *httpError
			if errors.As(lastErr, &he) && he.Status == http.StatusTooManyRequests {
				wait = retryAfterOf(lastErr)
			} else {
				wait = backoffSchedule[min(attempt-1, len(backoffSchedule)-1)]
			}
			if err := sleepCtx(ctx, wait); err != nil {
				return nil, err
			}
		}
		if err := c.limiter.wait(ctx); err != nil {
			return nil, err
		}

		data, err := c.doOnce(ctx, reqURL)
		if err == nil {
			if c.store != nil {
				c.store.Put(key, data)
			}
			return data, nil
		}
		if !retryable(err) {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		lastErr = err
	}
	return nil, lastErr
}

// rateLimitedError wraps a 429 with its Retry-After.
type rateLimitedError struct {
	httpError
	RetryAfter time.Duration
}

func (e *rateLimitedError) Unwrap() error { return &e.httpError }

func retryAfterOf(err error) time.Duration {
	var rl *rateLimitedError
	if errors.As(err, &rl) && rl.RetryAfter > 0 {
		return rl.RetryAfter
	}
	return defaultRetryAfter
}

func retryable(err error) bool {
	var he *httpError
	if errors.As(err, &he) {
		return he.Status == http.StatusTooManyRequests || he.Status >= 500
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// Anything else is a transport error.
	return true
}

func (c *Client) doOnce(ctx context.Context, reqURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if c.bearer {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusOK:
		return body, nil
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, &rateLimitedError{
			httpError:  httpError{Status: resp.StatusCode, Body: string(body)},
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	default:
		return nil, &httpError{Status: resp.StatusCode, Body: string(body)}
	}
}

func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.ParseFloat(v, 64); err == nil && secs >= 0 {
		return time.Duration(secs * float64(time.Second))
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ---- rate limiter ----------------------------------------------------------

// tokenBucket is a simple time-based token bucket safe for concurrent use.
type tokenBucket struct {
	mu       sync.Mutex
	rate     float64 // tokens per second; <= 0 disables limiting
	burst    float64
	tokens   float64
	lastFill time.Time
}

func newTokenBucket(perSecond float64) *tokenBucket {
	b := &tokenBucket{}
	b.setRate(perSecond)
	return b
}

func (b *tokenBucket) setRate(perSecond float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rate = perSecond
	b.burst = perSecond
	if b.burst < 1 {
		b.burst = 1
	}
	b.tokens = b.burst
	b.lastFill = time.Now()
}

// wait blocks until a token is available or ctx is done.
func (b *tokenBucket) wait(ctx context.Context) error {
	for {
		b.mu.Lock()
		if b.rate <= 0 {
			b.mu.Unlock()
			return nil
		}
		now := time.Now()
		b.tokens += now.Sub(b.lastFill).Seconds() * b.rate
		if b.tokens > b.burst {
			b.tokens = b.burst
		}
		b.lastFill = now
		if b.tokens >= 1 {
			b.tokens--
			b.mu.Unlock()
			return nil
		}
		wait := time.Duration((1 - b.tokens) / b.rate * float64(time.Second))
		b.mu.Unlock()
		if err := sleepCtx(ctx, wait); err != nil {
			return err
		}
	}
}

// ---- helpers ---------------------------------------------------------------

func names(in []named) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, n := range in {
		if n.Name != "" {
			out = append(out, n.Name)
		}
	}
	return out
}

func yearOf(date string) int {
	if len(date) < 4 {
		return 0
	}
	y, err := strconv.Atoi(date[:4])
	if err != nil || y <= 0 {
		return 0
	}
	return y
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
