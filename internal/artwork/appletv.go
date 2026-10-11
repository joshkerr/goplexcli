package artwork

// Apple TV catalog client. Apple's TV-app search service
// (uts-api.itunes.apple.com) needs no API key and is the native source of the
// 16:9 "coverArt16X9" horizontal posters the Apple TV app shows, alongside
// the usual 2:3 "coverArt" poster.
//
// This file also holds the small retry and cache helpers shared with the
// Netflix client (netflix.go); they carry the apple* prefix only to keep the
// package's unexported namespace tidy.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// AppleDefaultStorefront is the US storefront id. Availability (and localized
// artwork) varies per storefront.
const AppleDefaultStorefront = "143441"

const (
	appleBaseURL   = "https://uts-api.itunes.apple.com/uts/v2"
	appleUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)"

	appleHorizontalSize    = "1920x1080"
	appleHorizontalMaxSize = "3840x2160"
	appleVerticalSize      = "1000x1500"
	appleVerticalMaxSize   = "2000x3000"

	appleMaxBodyBytes = 8 << 20
)

// appleImage is one artwork asset with a {w}x{h}.{f} URL template.
type appleImage struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	URL    string `json:"url"`
}

// AppleResult is one movie or show from the Apple TV catalog.
type AppleResult struct {
	ID    string
	Kind  string // "movie" | "tv"
	Title string
	Year  int // from releaseDate (epoch ms); 0 if absent

	images map[string]appleImage
}

// appleRenderTemplate substitutes a {w}x{h}.{f} artwork URL template.
func appleRenderTemplate(tmpl, size string) string {
	wh := strings.SplitN(size, "x", 2)
	if len(wh) != 2 {
		return ""
	}
	s := strings.Replace(tmpl, "{w}", wh[0], 1)
	s = strings.Replace(s, "{h}", wh[1], 1)
	return strings.Replace(s, "{f}", "jpg", 1)
}

// ArtworkURL renders the image for an orientation: "horizontal" is the 16:9
// cover at 1920x1080 (3840x2160 when max is set); anything else is the 2:3
// poster at 1000x1500 (2000x3000 when max is set). It returns "" when the
// catalog has no such asset for this title.
func (r *AppleResult) ArtworkURL(orientation string, max bool) string {
	if r == nil {
		return ""
	}
	key, size := "coverArt", appleVerticalSize
	if orientation == "horizontal" {
		key, size = "coverArt16X9", appleHorizontalSize
		if max {
			size = appleHorizontalMaxSize
		}
	} else if max {
		size = appleVerticalMaxSize
	}
	img, ok := r.images[key]
	if !ok || img.URL == "" {
		return ""
	}
	return appleRenderTemplate(img.URL, size)
}

// AppleClient queries the Apple TV catalog.
type AppleClient struct {
	Storefront string
	Locale     string
	// BaseURL is the catalog root (".../uts/v2"); tests point it at httptest.
	BaseURL    string
	HTTPClient *http.Client

	cache *appleCache[[]AppleResult]
}

// NewAppleClient creates a catalog client. storefront may be "" for the US
// default; the locale is always en-US.
func NewAppleClient(storefront string) *AppleClient {
	if storefront == "" {
		storefront = AppleDefaultStorefront
	}
	return &AppleClient{
		Storefront: storefront,
		Locale:     "en-US",
		BaseURL:    appleBaseURL,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		cache:      newAppleCache[[]AppleResult](),
	}
}

// appleItem mirrors one catalog item as the uts-api returns it.
type appleItem struct {
	ID          string                `json:"id"`
	Type        string                `json:"type"` // "Movie" or "Show"
	Title       string                `json:"title"`
	ReleaseDate int64                 `json:"releaseDate"` // epoch millis
	Images      map[string]appleImage `json:"images"`
}

// appleSearchResponse mirrors the fields of the uts-api response we consume.
type appleSearchResponse struct {
	Data struct {
		Canvas struct {
			Shelves []struct {
				Items []appleItem `json:"items"`
			} `json:"shelves"`
		} `json:"canvas"`
	} `json:"data"`
}

// Search returns catalog results for a query. kind "movie" keeps movies, "tv"
// keeps shows, and "" keeps both.
func (c *AppleClient) Search(ctx context.Context, query, kind string) ([]AppleResult, error) {
	cacheKey := c.Storefront + ":" + kind + ":" + query
	if results, ok := c.cache.get(cacheKey); ok {
		return append([]AppleResult(nil), results...), nil
	}

	params := url.Values{}
	params.Set("sf", c.Storefront)
	params.Set("locale", c.Locale)
	params.Set("caller", "wta")
	params.Set("utsk", "0")
	params.Set("v", "34")
	params.Set("pfm", "desktop")
	params.Set("q", query)
	searchURL := strings.TrimSuffix(c.BaseURL, "/") + "/search/incremental?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", appleUserAgent)

	body, err := appleFetch(ctx, c.HTTPClient, req, appleMaxBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("apple tv search failed: %w", err)
	}

	var parsed appleSearchResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse apple tv response: %w", err)
	}

	results := []AppleResult{}
	for _, shelf := range parsed.Data.Canvas.Shelves {
		for _, item := range shelf.Items {
			var itemKind string
			switch item.Type {
			case "Movie":
				itemKind = "movie"
			case "Show":
				itemKind = "tv"
			default:
				continue
			}
			if kind != "" && kind != itemKind {
				continue
			}
			year := 0
			if item.ReleaseDate != 0 {
				year = time.UnixMilli(item.ReleaseDate).UTC().Year()
			}
			results = append(results, AppleResult{
				ID:     item.ID,
				Kind:   itemKind,
				Title:  item.Title,
				Year:   year,
				images: item.Images,
			})
		}
	}

	c.cache.set(cacheKey, results)
	return append([]AppleResult(nil), results...), nil
}

// Find searches and picks the best match for a title and year: an exact year
// with a case-insensitive title match first, then an exact year alone, then a
// case-insensitive title match alone. It never falls back to the top result
// blindly (a wrong poster is worse than none); when year is 0 a title match
// alone is accepted. It returns (nil, nil) when nothing fits.
func (c *AppleClient) Find(ctx context.Context, title string, year int, kind string) (*AppleResult, error) {
	results, err := c.Search(ctx, title, kind)
	if err != nil {
		return nil, err
	}
	if year != 0 {
		for i := range results {
			if results[i].Year == year && strings.EqualFold(results[i].Title, title) {
				return &results[i], nil
			}
		}
		for i := range results {
			if results[i].Year == year {
				return &results[i], nil
			}
		}
	}
	for i := range results {
		if strings.EqualFold(results[i].Title, title) {
			return &results[i], nil
		}
	}
	return nil, nil
}

// --- shared helpers -------------------------------------------------------

// appleRetryAttempts is how many times a request is tried before giving up.
const appleRetryAttempts = 3

// appleRetryDelays are the waits before the second, third, ... attempt. Tests
// shorten them.
var appleRetryDelays = []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second}

// appleHTTPError is a non-200 response.
type appleHTTPError struct {
	Status int
}

func (e *appleHTTPError) Error() string {
	return fmt.Sprintf("HTTP %d", e.Status)
}

// retryable reports whether the status is transient: 5xx and 429 (and 408)
// are retried, every other 4xx is permanent.
func (e *appleHTTPError) retryable() bool {
	return e.Status >= 500 || e.Status == http.StatusTooManyRequests || e.Status == http.StatusRequestTimeout
}

// appleFetch performs a GET request (which must carry no body) with up to
// appleRetryAttempts tries, backing off between them, and returns the body of
// the first 200 response capped at maxBytes. Transport errors, 5xx and 429
// are retried; other 4xx responses and context cancellation are not.
func appleFetch(ctx context.Context, client *http.Client, req *http.Request, maxBytes int64) ([]byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	var lastErr error
	for attempt := 0; attempt < appleRetryAttempts; attempt++ {
		if attempt > 0 {
			delay := appleRetryDelays[min(attempt-1, len(appleRetryDelays)-1)]
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		body, retryable, err := appleAttempt(ctx, client, req, maxBytes)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retryable || ctx.Err() != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

// appleAttempt is one try of appleFetch; retryable says whether the failure
// is worth another attempt.
func appleAttempt(ctx context.Context, client *http.Client, req *http.Request, maxBytes int64) (body []byte, retryable bool, err error) {
	resp, err := client.Do(req.Clone(ctx))
	if err != nil {
		return nil, ctx.Err() == nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		herr := &appleHTTPError{Status: resp.StatusCode}
		return nil, herr.retryable(), herr
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, ctx.Err() == nil, err
	}
	return body, false, nil
}

// appleHTTPStatus returns the status code behind err, or 0 when err is not a
// non-200 response.
func appleHTTPStatus(err error) int {
	var herr *appleHTTPError
	if errors.As(err, &herr) {
		return herr.Status
	}
	return 0
}

// appleCache is a mutex-protected map keyed by query string. A nil cache is
// safe to use and never stores anything.
type appleCache[V any] struct {
	mu sync.Mutex
	m  map[string]V
}

func newAppleCache[V any]() *appleCache[V] {
	return &appleCache[V]{m: map[string]V{}}
}

func (c *appleCache[V]) get(key string) (V, bool) {
	var zero V
	if c == nil {
		return zero, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[key]
	return v, ok
}

func (c *appleCache[V]) set(key string, v V) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]V{}
	}
	c.m[key] = v
}
