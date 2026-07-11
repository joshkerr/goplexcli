// Package plex provides a client for interacting with Plex Media Server.
// It supports authentication, library browsing, and stream URL generation.
// The client handles API versioning gracefully and logs warnings for unexpected responses.
package plex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/LukeHagar/plexgo"
	"github.com/LukeHagar/plexgo/models/operations"
	"golang.org/x/sync/errgroup"
)

// sectionHTTPClient is shared by the indexing path (section listing and page
// fetches). The per-request timeout ensures a hung or unreachable Plex server
// fails an index run with an error instead of blocking it forever; pages are
// small (see sectionPageSize), so healthy responses finish well within it.
var sectionHTTPClient = &http.Client{Timeout: 60 * time.Second}

// errPlexServerError indicates the Plex server returned a 5xx response for a
// page request. Large libraries can make the server fail on big container
// windows, so callers detect this and retry with a smaller page size.
var errPlexServerError = errors.New("plex server error")

// apiLogger is used for logging API warnings (defaults to stderr, silent in production)
var apiLogger = log.New(os.Stderr, "[plex] ", log.LstdFlags)

// SetAPILogger allows customizing the logger for API warnings
func SetAPILogger(l *log.Logger) {
	if l != nil {
		apiLogger = l
	}
}

// SilenceAPIWarnings disables API warning logging
func SilenceAPIWarnings() {
	apiLogger = log.New(io.Discard, "", 0)
}

type Client struct {
	sdk          *plexgo.PlexAPI
	serverURL    string
	serverName   string
	token        string
	pathMappings []PathMapping
}

// PathMapping describes how to translate a Plex on-disk file path into an
// rclone remote path. A file path beginning with Prefix has that prefix
// replaced by Remote. For example {Prefix: "/home/joshkerr/plexcloudservers2/",
// Remote: "plexcloudservers2:"} turns
// "/home/joshkerr/plexcloudservers2/Media/TV/x.mkv" into
// "plexcloudservers2:Media/TV/x.mkv".
type PathMapping struct {
	Prefix string
	Remote string
}

// SetPathMappings configures the rclone path-translation rules used when
// building media items. Mappings are tried longest-prefix-first.
func (c *Client) SetPathMappings(mappings []PathMapping) {
	c.pathMappings = mappings
}

type MediaItem struct {
	Key              string
	Title            string
	Year             int
	Type             string // movie, show, season, episode
	Summary          string
	Rating           float64
	Duration         int
	FilePath         string
	RclonePath       string
	ParentTitle      string // For episodes: show name
	GrandTitle       string // For episodes: season name
	Index            int64  // Episode or season number
	ParentIndex      int64  // Season number for episodes
	Thumb            string // Poster/thumbnail URL path (episode still for episodes)
	GrandparentThumb string // For episodes: the show poster path (grandparentThumb)
	ServerName       string // Name of the Plex server this item belongs to
	ServerURL        string // URL of the Plex server this item belongs to
	ViewOffset       int    // Playback position in milliseconds (0 if not started)
	ViewCount        int    // Number of times fully watched
	LastViewedAt     int64  // Unix timestamp of last playback (0 if never viewed)
	ContentRating    string // e.g., "PG-13", "TV-MA"
	Studio           string // Production studio
	Director         string // Director name(s)
	Genre            string // Genre(s), comma-separated
	Cast             string // Cast members, comma-separated
	AddedAt          int64  // Unix timestamp when added to library
	OriginallyAired  string // Original air date for episodes
}

// New creates a new Plex client
func New(serverURL, token string) (*Client, error) {
	return NewWithName(serverURL, token, "")
}

// NewWithName creates a new Plex client with a server name
func NewWithName(serverURL, token, serverName string) (*Client, error) {
	sdk := plexgo.New(
		plexgo.WithServerURL(serverURL),
		plexgo.WithSecurity(token),
		plexgo.WithClientIdentifier("goplexcli"),
		plexgo.WithProduct("GoplexCLI"),
		plexgo.WithVersion("1.0"),
	)

	// If no server name provided, use URL as fallback
	if serverName == "" {
		serverName = serverURL
	}

	return &Client{
		sdk:        sdk,
		serverURL:  serverURL,
		serverName: serverName,
		token:      token,
	}, nil
}

// Test validates the connection to the Plex server
func (c *Client) Test() error {
	return c.TestContext(context.Background())
}

// TestContext validates the connection to the Plex server, honoring the
// caller's context for cancellation and deadlines.
func (c *Client) TestContext(ctx context.Context) error {
	_, err := c.sdk.General.GetIdentity(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to plex server: %w", err)
	}
	return nil
}

// Library represents a Plex library section
type Library struct {
	Key   string
	Title string
	Type  string
}

// Custom response structures to handle Plex's inconsistent JSON
type sectionsResponse struct {
	MediaContainer struct {
		Directory []struct {
			Key   string `json:"key"`
			Title string `json:"title"`
			Type  string `json:"type"`
		} `json:"Directory"`
	} `json:"MediaContainer"`
}

// GetLibraries returns all library sections using direct HTTP to avoid unmarshaling issues
func (c *Client) GetLibraries(ctx context.Context) ([]Library, error) {
	// Use direct HTTP request to avoid library's unmarshaling issues with hidden field
	url := fmt.Sprintf("%s/library/sections?X-Plex-Token=%s", c.serverURL, c.token)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Client-Identifier", "goplexcli")
	req.Header.Set("X-Plex-Product", "GoplexCLI")
	req.Header.Set("X-Plex-Version", "1.0")

	resp, err := sectionHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get sections: %w", err)
	}
	defer resp.Body.Close()

	// Check HTTP status code
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("authentication failed: invalid or expired token (status %d)", resp.StatusCode)
		}
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("library sections endpoint not found - Plex API may have changed (status %d)", resp.StatusCode)
		}
		return nil, fmt.Errorf("unexpected status code %d from Plex server", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var sectionsResp sectionsResponse
	if err := json.Unmarshal(body, &sectionsResp); err != nil {
		apiLogger.Printf("warning: failed to parse sections response, API format may have changed: %v", err)
		return nil, fmt.Errorf("failed to parse sections: %w", err)
	}

	// Log warning if response structure seems unexpected
	if len(sectionsResp.MediaContainer.Directory) == 0 {
		apiLogger.Printf("warning: no library sections returned - server may be empty or API format changed")
	}

	var libraries []Library
	for _, dir := range sectionsResp.MediaContainer.Directory {
		// Validate required fields
		if dir.Key == "" {
			apiLogger.Printf("warning: library section missing key field, skipping")
			continue
		}
		libraries = append(libraries, Library{
			Key:   dir.Key,
			Title: dir.Title,
			Type:  dir.Type,
		})
	}

	return libraries, nil
}

// ProgressCallback is called during media fetching to report progress. It may
// be called multiple times per library as pages are fetched: itemCount is the
// number of items retrieved so far in the current library, and totalItems is
// the library's total (0 if unknown).
type ProgressCallback func(libraryName string, itemCount int, totalItems int, totalLibraries int, currentLibrary int)

// ServerProgressCallback is called during multi-server media fetching. As with
// ProgressCallback, it may fire repeatedly per library with the running
// itemCount and the library's totalItems.
type ServerProgressCallback func(serverName, libraryName string, itemCount int, totalItems int, totalLibraries int, currentLibrary int, serverNum int, totalServers int)

// GetAllMedia returns all media items from all libraries.
func (c *Client) GetAllMedia(ctx context.Context, progressCallback ProgressCallback) ([]MediaItem, error) {
	return c.getMedia(ctx, nil, progressCallback)
}

// GetMediaSince returns only items added since a per-library-type threshold,
// for incremental cache updates. sinceFor receives the library type
// ("movie" or "show") and returns the newest addedAt already known for that
// type (return 0 to fetch the whole library).
func (c *Client) GetMediaSince(ctx context.Context, sinceFor func(libType string) int64, progressCallback ProgressCallback) ([]MediaItem, error) {
	return c.getMedia(ctx, sinceFor, progressCallback)
}

// getMedia is the shared implementation for GetAllMedia and GetMediaSince.
func (c *Client) getMedia(ctx context.Context, sinceFor func(libType string) int64, progressCallback ProgressCallback) ([]MediaItem, error) {
	libraries, err := c.GetLibraries(ctx)
	if err != nil {
		return nil, err
	}

	var tasks []sectionFetchTask
	for _, lib := range libraries {
		if lib.Type != "movie" && lib.Type != "show" {
			continue
		}
		var since int64
		if sinceFor != nil {
			since = sinceFor(lib.Type)
		}
		tasks = append(tasks, sectionFetchTask{
			client: c,
			lib:    lib,
			libNum: len(tasks) + 1,
			since:  since,
		})
	}
	for i := range tasks {
		tasks[i].totalLibs = len(tasks)
	}

	return fetchSections(ctx, tasks, func(task sectionFetchTask, fetched, total int) {
		if progressCallback != nil {
			progressCallback(task.lib.Title, fetched, total, task.totalLibs, task.libNum)
		}
	})
}

// GetAllMediaFromServers returns all media items from multiple Plex servers.
// mappings configures rclone path translation (see PathMapping); pass nil to
// use the legacy fallback.
func GetAllMediaFromServers(ctx context.Context, serverConfigs []struct{ Name, URL, Token string }, mappings []PathMapping, progressCallback ServerProgressCallback) ([]MediaItem, error) {
	return getMediaFromServers(ctx, serverConfigs, mappings, nil, progressCallback)
}

// GetNewMediaFromServers returns only items added since a per-server,
// per-library-type threshold across multiple Plex servers, for incremental
// cache updates. sinceFor receives the server name and library type
// ("movie"/"show") and returns the newest addedAt already known (0 to fetch
// the whole library).
func GetNewMediaFromServers(ctx context.Context, serverConfigs []struct{ Name, URL, Token string }, mappings []PathMapping, sinceFor func(serverName, libType string) int64, progressCallback ServerProgressCallback) ([]MediaItem, error) {
	return getMediaFromServers(ctx, serverConfigs, mappings, sinceFor, progressCallback)
}

// getMediaFromServers is the shared implementation for GetAllMediaFromServers
// and GetNewMediaFromServers.
func getMediaFromServers(ctx context.Context, serverConfigs []struct{ Name, URL, Token string }, mappings []PathMapping, sinceFor func(serverName, libType string) int64, progressCallback ServerProgressCallback) ([]MediaItem, error) {
	totalServers := len(serverConfigs)

	var tasks []sectionFetchTask
	for serverNum, serverConfig := range serverConfigs {
		client, err := NewWithName(serverConfig.URL, serverConfig.Token, serverConfig.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to create client for server %s: %w", serverConfig.Name, err)
		}
		client.SetPathMappings(mappings)

		// Bound the connection test so one hung server fails fast instead of
		// stalling the whole index run.
		testCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err = client.TestContext(testCtx)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("failed to connect to server %s: %w", serverConfig.Name, err)
		}

		libraries, err := client.GetLibraries(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get libraries from server %s: %w", serverConfig.Name, err)
		}

		serverTaskStart := len(tasks)
		libNum := 0
		for _, lib := range libraries {
			if lib.Type != "movie" && lib.Type != "show" {
				continue
			}
			libNum++
			var since int64
			if sinceFor != nil {
				since = sinceFor(serverConfig.Name, lib.Type)
			}
			tasks = append(tasks, sectionFetchTask{
				client:       client,
				lib:          lib,
				libNum:       libNum,
				serverName:   serverConfig.Name,
				serverNum:    serverNum + 1,
				totalServers: totalServers,
				since:        since,
			})
		}
		for i := serverTaskStart; i < len(tasks); i++ {
			tasks[i].totalLibs = libNum
		}
	}

	return fetchSections(ctx, tasks, func(task sectionFetchTask, fetched, total int) {
		if progressCallback != nil {
			progressCallback(task.serverName, task.lib.Title, fetched, total, task.totalLibs, task.libNum, task.serverNum, task.totalServers)
		}
	})
}

// sectionFetchTask describes one library section to index: which client to
// fetch it with, how to attribute progress, and the incremental threshold.
type sectionFetchTask struct {
	client       *Client
	lib          Library
	libNum       int
	totalLibs    int
	serverName   string
	serverNum    int
	totalServers int
	since        int64
}

// sectionFetchConcurrency bounds how many library sections are fetched in
// parallel during indexing. Parallel sections overlap network latency across
// libraries (and across servers in multi-server mode) while staying gentle
// enough not to overload a modest Plex server.
const sectionFetchConcurrency = 4

// fetchSections runs all section fetch tasks through a bounded worker pool
// and returns their items concatenated in task order, so cache ordering stays
// deterministic regardless of which section finishes first. onProgress calls
// are serialized, so callers may safely write terminal progress from them. A
// failed task cancels the remaining ones and its error is returned.
func fetchSections(ctx context.Context, tasks []sectionFetchTask, onProgress func(task sectionFetchTask, fetched, total int)) ([]MediaItem, error) {
	results := make([][]MediaItem, len(tasks))
	var progressMu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(sectionFetchConcurrency)
	for i, task := range tasks {
		g.Go(func() error {
			onPage := func(fetched, total int) {
				if onProgress == nil {
					return
				}
				progressMu.Lock()
				defer progressMu.Unlock()
				onProgress(task, fetched, total)
			}
			media, err := task.client.getMediaFromSection(gctx, task.lib.Key, task.lib.Type, task.since, onPage)
			if err != nil {
				if task.serverName != "" {
					return fmt.Errorf("failed to get media from section %s on server %s: %w", task.lib.Title, task.serverName, err)
				}
				return fmt.Errorf("failed to get media from section %s: %w", task.lib.Title, err)
			}
			results[i] = media
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	var allMedia []MediaItem
	for _, media := range results {
		allMedia = append(allMedia, media...)
	}
	return allMedia, nil
}

// sectionPageSize is how many items to request per page when enumerating a
// library section. Large libraries (tens of thousands of items) cause the Plex
// server to return HTTP 500 if the entire section is requested in one
// unpaginated call, so we always page through using the
// X-Plex-Container-Start / X-Plex-Container-Size protocol. The size is kept
// conservative because servers also return 500 once a single response grows
// past a few hundred items.
const sectionPageSize = 200

// minSectionPageSize is the floor for adaptive page-size backoff. When the
// server returns an HTTP 500 for a page (common for very large libraries at
// deep container offsets), we halve the page window and retry the same offset,
// but never shrink below this so a persistently failing page still surfaces an
// error instead of looping forever.
const minSectionPageSize = 10

// pageRetryDelay is a short, fixed courtesy pause between page retries so we
// don't hammer the server with back-to-back requests. Retries are only useful
// while they shrink the page window (see pageMetadata): once the window is at
// the floor and the server still 500s, the failure is deterministic and no
// amount of waiting helps, so we don't escalate the delay or keep retrying.
// A variable rather than a constant so tests can shrink it.
var pageRetryDelay = 500 * time.Millisecond

// pageNetRetries is how many consecutive transport-level failures (connection
// reset, request timeout, ...) are retried at the same offset before giving
// up. Unlike a 500 — which signals the response was too big and shrinking the
// window is the fix — a transport error is usually transient, so the same
// request is simply tried again after a short pause. The counter resets after
// every successful page so a long index run tolerates occasional blips.
const pageNetRetries = 2

// sectionMetadata mirrors a single item in a library section's Metadata array.
type sectionMetadata struct {
	Key                   string       `json:"key"`
	RatingKey             string       `json:"ratingKey"`
	Title                 string       `json:"title"`
	Year                  *int         `json:"year"`
	Summary               *string      `json:"summary"`
	Rating                *float32     `json:"rating"`
	Duration              *int         `json:"duration"`
	Thumb                 *string      `json:"thumb"`
	GrandparentThumb      *string      `json:"grandparentThumb"`
	GrandparentTitle      *string      `json:"grandparentTitle"`
	ParentTitle           *string      `json:"parentTitle"`
	Index                 *int         `json:"index"`
	ParentIndex           *int         `json:"parentIndex"`
	ViewOffset            *int         `json:"viewOffset"`
	ViewCount             *int         `json:"viewCount"`
	LastViewedAt          *int64       `json:"lastViewedAt"`
	ContentRating         *string      `json:"contentRating"`
	Studio                *string      `json:"studio"`
	AddedAt               *int64       `json:"addedAt"`
	OriginallyAvailableAt *string      `json:"originallyAvailableAt"`
	Director              []taggedItem `json:"Director"`
	Genre                 []taggedItem `json:"Genre"`
	Role                  []taggedItem `json:"Role"`
	Media                 []struct {
		Part []struct {
			File *string `json:"file"`
		} `json:"Part"`
	} `json:"Media"`
}

// GetMediaFromSection returns media items from a specific library section.
// It pages through the section rather than requesting everything at once,
// because large libraries make the Plex server return HTTP 500 for a single
// unpaginated /all request.
func (c *Client) GetMediaFromSection(ctx context.Context, sectionKey, sectionType string) ([]MediaItem, error) {
	return c.getMediaFromSection(ctx, sectionKey, sectionType, 0, nil)
}

// getMediaFromSection is the paginating implementation behind
// GetMediaFromSection. If onPage is non-nil it is called after each page is
// fetched with the number of items retrieved so far and the section's total,
// allowing callers to report incremental progress during long fetches.
//
// If since > 0 the section is fetched newest-first (sort=addedAt:desc) and only
// items with addedAt >= since are returned, stopping as soon as an older item
// is seen. This powers incremental cache updates. Boundary items (addedAt ==
// since) are included and rely on the caller deduplicating by key.
func (c *Client) getMediaFromSection(ctx context.Context, sectionKey, sectionType string, since int64, onPage func(fetched, total int)) ([]MediaItem, error) {
	var items []MediaItem

	// Build the base URL based on section type. Pagination params are added
	// per request below.
	var baseURL string
	if sectionType == "show" {
		// For TV shows, specifically request type=4 (episodes)
		baseURL = fmt.Sprintf("%s/library/sections/%s/all?type=4&X-Plex-Token=%s", c.serverURL, sectionKey, c.token)
	} else {
		// For movies, use the default all endpoint
		baseURL = fmt.Sprintf("%s/library/sections/%s/all?X-Plex-Token=%s", c.serverURL, sectionKey, c.token)
	}

	// For incremental fetches, ask the server for newest items first so we can
	// stop early once we reach items we already have.
	if since > 0 {
		baseURL += "&sort=addedAt:desc"
	}

	allMetadata, err := c.pageMetadata(ctx, baseURL, "section "+sectionKey, since, onPage)
	if err != nil {
		// For TV libraries the flat type=4 query enumerates every episode in the
		// library in one sorted list. Some servers cannot compute that for very
		// large libraries and return HTTP 500 even at the smallest page window.
		// Fall back to walking the library show-by-show, which issues far
		// smaller per-show queries.
		if sectionType == "show" && errors.Is(err, errPlexServerError) {
			apiLogger.Printf("flat episode enumeration failed for section %s (%v); falling back to per-show traversal", sectionKey, err)
			allMetadata, err = c.fetchEpisodesPerShow(ctx, sectionKey, since, onPage)
		}
		if err != nil {
			return nil, err
		}
	}

	if sectionType == "movie" {
		// Process movies
		for _, metadata := range allMetadata {
			// Validate required fields
			if metadata.Key == "" {
				apiLogger.Printf("warning: movie item missing key field, skipping")
				continue
			}
			if metadata.Title == "" {
				apiLogger.Printf("warning: movie item %s missing title field", metadata.Key)
			}

			item := MediaItem{
				Key:             metadata.Key,
				Title:           metadata.Title,
				Year:            valueOrZeroInt(metadata.Year),
				Type:            "movie",
				Summary:         valueOrEmpty(metadata.Summary),
				Rating:          float64(valueOrZeroFloat32(metadata.Rating)),
				Duration:        valueOrZeroInt(metadata.Duration),
				Thumb:           valueOrEmpty(metadata.Thumb),
				ServerName:      c.serverName,
				ServerURL:       c.serverURL,
				ViewOffset:      valueOrZeroInt(metadata.ViewOffset),
				ViewCount:       valueOrZeroInt(metadata.ViewCount),
				LastViewedAt:    valueOrZeroInt64(metadata.LastViewedAt),
				ContentRating:   valueOrEmpty(metadata.ContentRating),
				Studio:          valueOrEmpty(metadata.Studio),
				Director:        strings.Join(extractTags(metadata.Director, 0), ", "),
				Genre:           strings.Join(extractTags(metadata.Genre, 0), ", "),
				Cast:            strings.Join(extractTags(metadata.Role, 5), ", "),
				AddedAt:         valueOrZeroInt64(metadata.AddedAt),
				OriginallyAired: valueOrEmpty(metadata.OriginallyAvailableAt),
			}

			// Get file path
			if len(metadata.Media) > 0 && len(metadata.Media[0].Part) > 0 {
				item.FilePath = valueOrEmpty(metadata.Media[0].Part[0].File)
				item.RclonePath = c.convertToRclonePath(item.FilePath)
			} else {
				apiLogger.Printf("warning: movie %q has no media parts", metadata.Title)
			}

			items = append(items, item)
		}
	} else if sectionType == "show" {
		// For TV shows, we explicitly requested type=4 (episodes)
		for _, metadata := range allMetadata {
			// Validate required fields
			if metadata.Key == "" {
				apiLogger.Printf("warning: episode item missing key field, skipping")
				continue
			}
			if metadata.Title == "" {
				apiLogger.Printf("warning: episode item %s missing title field", metadata.Key)
			}

			item := MediaItem{
				Key:              metadata.Key,
				Title:            metadata.Title,
				Year:             valueOrZeroInt(metadata.Year),
				Type:             "episode",
				Summary:          valueOrEmpty(metadata.Summary),
				Rating:           float64(valueOrZeroFloat32(metadata.Rating)),
				Duration:         valueOrZeroInt(metadata.Duration),
				Thumb:            valueOrEmpty(metadata.Thumb),
				GrandparentThumb: valueOrEmpty(metadata.GrandparentThumb),
				ParentTitle:      valueOrEmpty(metadata.GrandparentTitle),
				GrandTitle:       valueOrEmpty(metadata.ParentTitle),
				Index:            int64(valueOrZeroInt(metadata.Index)),
				ParentIndex:      int64(valueOrZeroInt(metadata.ParentIndex)),
				ServerName:       c.serverName,
				ServerURL:        c.serverURL,
				ViewOffset:       valueOrZeroInt(metadata.ViewOffset),
				ViewCount:        valueOrZeroInt(metadata.ViewCount),
				LastViewedAt:     valueOrZeroInt64(metadata.LastViewedAt),
				ContentRating:    valueOrEmpty(metadata.ContentRating),
				Studio:           valueOrEmpty(metadata.Studio),
				Director:         strings.Join(extractTags(metadata.Director, 0), ", "),
				Genre:            strings.Join(extractTags(metadata.Genre, 0), ", "),
				Cast:             strings.Join(extractTags(metadata.Role, 5), ", "),
				AddedAt:          valueOrZeroInt64(metadata.AddedAt),
				OriginallyAired:  valueOrEmpty(metadata.OriginallyAvailableAt),
			}

			// Get file path
			if len(metadata.Media) > 0 && len(metadata.Media[0].Part) > 0 {
				item.FilePath = valueOrEmpty(metadata.Media[0].Part[0].File)
				item.RclonePath = c.convertToRclonePath(item.FilePath)
			} else {
				apiLogger.Printf("warning: episode %q has no media parts", metadata.Title)
			}

			items = append(items, item)
		}
	}

	return items, nil
}

// pageMetadata pages through a Plex MediaContainer endpoint using container
// pagination with adaptive backoff, returning all item metadata. baseURL must
// already contain its query string (token, type, sort); the container
// Start/Size parameters are appended per request. logKey labels the resource in
// log and retry messages.
//
// On an HTTP 500 the same offset is retried with a smaller page window (large
// windows at deep offsets make the server 500). Retrying only helps while it
// shrinks the window, so once the window is already at the floor a further 500
// is treated as a deterministic failure and returned immediately rather than
// waited on — waiting doesn't fix a request the server structurally can't
// satisfy. A short fixed pause separates retries so we don't hammer the server.
//
// If since > 0 the endpoint is assumed to be ordered newest-first: paging stops
// as soon as an item older than since is seen, and only items with
// addedAt >= since are returned. report, if non-nil, is called after each page
// with the running item count and the container's total (0 when unknown, e.g.
// in incremental mode).
func (c *Client) pageMetadata(ctx context.Context, baseURL, logKey string, since int64, report func(fetched, total int)) ([]sectionMetadata, error) {
	var collected []sectionMetadata
	fetched := 0
	size := sectionPageSize
	netRetries := 0
	for start := 0; ; {
		page, total, err := c.fetchSectionPage(ctx, baseURL, logKey, start, size)
		if err != nil {
			// Retry with a smaller window, but only while shrinking is still
			// possible; a 500 at the floor is deterministic, so give up fast.
			if errors.Is(err, errPlexServerError) && size > minSectionPageSize {
				newSize := size / 2
				if newSize < minSectionPageSize {
					newSize = minSectionPageSize
				}
				apiLogger.Printf("plex returned a server error for %s at start=%d size=%d; retrying with size=%d", logKey, start, size, newSize)
				size = newSize
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(pageRetryDelay):
				}
				continue
			}
			// Transport-level failures (connection reset, request timeout) are
			// usually transient: retry the same offset a couple of times before
			// surfacing the error, so a blip doesn't abort a long index run.
			var urlErr *url.Error
			if errors.As(err, &urlErr) && ctx.Err() == nil && netRetries < pageNetRetries {
				netRetries++
				apiLogger.Printf("transient network error for %s at start=%d (retry %d/%d): %v", logKey, start, netRetries, pageNetRetries, err)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(pageRetryDelay):
				}
				continue
			}
			return nil, err
		}
		netRetries = 0
		fetched += len(page)

		// In incremental mode the page is newest-first; keep items until we
		// hit one older than the threshold, then stop.
		reachedKnown := false
		if since > 0 {
			for i := range page {
				if valueOrZeroInt64(page[i].AddedAt) < since {
					reachedKnown = true
					break
				}
				collected = append(collected, page[i])
			}
		} else {
			collected = append(collected, page...)
		}

		// Report incremental progress so long fetches don't look frozen. The
		// total is meaningless in incremental mode (we fetch a small slice), so
		// report it as unknown.
		if report != nil {
			progressTotal := total
			if since > 0 {
				progressTotal = 0
			}
			report(len(collected), progressTotal)
		}

		// Stop when we've reached known items, exhausted the container, or the
		// server reported fewer than a full page.
		if reachedKnown || len(page) < size || (total > 0 && fetched >= total) {
			break
		}

		// Advance by the number of items actually returned so the next request
		// picks up where this page ended, regardless of any backoff resize.
		start += len(page)
	}
	return collected, nil
}

// fetchEpisodesPerShow enumerates a TV library by walking it show-by-show: it
// lists the shows in the section, then fetches each show's episodes via the
// per-show /allLeaves endpoint. This is the fallback for libraries so large
// that the single, library-wide type=4 query 500s even at the smallest page
// window. Each per-show query is small, so the server can satisfy it.
//
// A show with so many episodes that even its /allLeaves query 500s (e.g. a
// long-running daily series) is retried one level deeper, season-by-season.
//
// When since > 0 only episodes added on or after since are returned. allLeaves
// ordering is not guaranteed, so every episode is checked rather than stopping
// early. A show whose episodes can't be fetched even per-season is logged and
// skipped rather than failing the whole library.
func (c *Client) fetchEpisodesPerShow(ctx context.Context, sectionKey string, since int64, onPage func(fetched, total int)) ([]sectionMetadata, error) {
	// List the shows in this section. The default /all (no type) returns the
	// show directories, a far smaller set than every episode.
	showsURL := fmt.Sprintf("%s/library/sections/%s/all?X-Plex-Token=%s", c.serverURL, sectionKey, c.token)
	shows, err := c.pageMetadata(ctx, showsURL, "section "+sectionKey+" shows", 0, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list shows: %w", err)
	}
	apiLogger.Printf("per-show traversal of section %s: walking %d shows", sectionKey, len(shows))

	var episodes []sectionMetadata
	for _, show := range shows {
		if show.RatingKey == "" {
			apiLogger.Printf("warning: show %q has no ratingKey, skipping", show.Title)
			continue
		}

		leavesURL := fmt.Sprintf("%s/library/metadata/%s/allLeaves?X-Plex-Token=%s", c.serverURL, show.RatingKey, c.token)

		// Report progress cumulatively across shows so long traversals don't
		// look frozen. base is the count before this show; pageMetadata reports
		// the running count within the show synchronously, so this is safe.
		base := len(episodes)
		report := func(fetched, total int) {
			if onPage != nil {
				onPage(base+fetched, 0)
			}
		}

		showEpisodes, err := c.pageMetadata(ctx, leavesURL, "show "+show.RatingKey, 0, report)
		if err != nil {
			// Respect cancellation immediately.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			// Some shows have so many episodes that even /allLeaves 500s. Drop
			// one level deeper and walk the show season-by-season.
			if errors.Is(err, errPlexServerError) {
				apiLogger.Printf("allLeaves failed for show %q (ratingKey %s); falling back to per-season traversal", show.Title, show.RatingKey)
				showEpisodes, err = c.fetchEpisodesPerSeason(ctx, show.RatingKey, base, onPage)
			}
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return nil, err
				}
				apiLogger.Printf("warning: failed to get episodes for show %q (ratingKey %s): %v; skipping", show.Title, show.RatingKey, err)
				continue
			}
		}

		if since > 0 {
			for i := range showEpisodes {
				if valueOrZeroInt64(showEpisodes[i].AddedAt) >= since {
					episodes = append(episodes, showEpisodes[i])
				}
			}
		} else {
			episodes = append(episodes, showEpisodes...)
		}
	}
	return episodes, nil
}

// fetchEpisodesPerSeason walks a single show season-by-season, the deepest
// fallback for a show whose /allLeaves query is too large for the server to
// satisfy. It lists the show's seasons, then fetches each season's episodes via
// the per-season /children endpoint (a handful of items each). base is the
// running episode count before this show, used only to keep progress reporting
// cumulative. A season that can't be fetched is logged and skipped.
func (c *Client) fetchEpisodesPerSeason(ctx context.Context, showRatingKey string, base int, onPage func(fetched, total int)) ([]sectionMetadata, error) {
	seasonsURL := fmt.Sprintf("%s/library/metadata/%s/children?X-Plex-Token=%s", c.serverURL, showRatingKey, c.token)
	seasons, err := c.pageMetadata(ctx, seasonsURL, "show "+showRatingKey+" seasons", 0, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list seasons: %w", err)
	}

	var episodes []sectionMetadata
	for _, season := range seasons {
		if season.RatingKey == "" {
			continue
		}

		episodesURL := fmt.Sprintf("%s/library/metadata/%s/children?X-Plex-Token=%s", c.serverURL, season.RatingKey, c.token)

		// Report cumulatively: base (episodes before this show) plus what this
		// show has accumulated across earlier seasons plus the current page.
		seasonBase := len(episodes)
		report := func(fetched, total int) {
			if onPage != nil {
				onPage(base+seasonBase+fetched, 0)
			}
		}

		seasonEpisodes, err := c.pageMetadata(ctx, episodesURL, "season "+season.RatingKey, 0, report)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			apiLogger.Printf("warning: failed to get episodes for season %q (ratingKey %s) of show %s: %v; skipping", season.Title, season.RatingKey, showRatingKey, err)
			continue
		}
		episodes = append(episodes, seasonEpisodes...)
	}
	return episodes, nil
}

// fetchSectionPage requests a single page of a library section and returns the
// parsed metadata along with the section's reported total size. The container
// pagination parameters are appended to baseURL.
func (c *Client) fetchSectionPage(ctx context.Context, baseURL, sectionKey string, start, size int) ([]sectionMetadata, int, error) {
	url := fmt.Sprintf("%s&X-Plex-Container-Start=%d&X-Plex-Container-Size=%d", baseURL, start, size)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Client-Identifier", "goplexcli")
	req.Header.Set("X-Plex-Product", "GoplexCLI")
	req.Header.Set("X-Plex-Version", "1.0")

	resp, err := sectionHTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get library items: %w", err)
	}
	defer resp.Body.Close()

	// Check HTTP status code
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, 0, fmt.Errorf("authentication failed: invalid or expired token (status %d)", resp.StatusCode)
		}
		if resp.StatusCode == http.StatusNotFound {
			apiLogger.Printf("warning: section %s not found - it may have been removed", sectionKey)
			return nil, 0, fmt.Errorf("library section %s not found (status %d)", sectionKey, resp.StatusCode)
		}
		if resp.StatusCode >= 500 {
			// Wrap with errPlexServerError so the pager can retry this page
			// with a smaller container window.
			return nil, 0, fmt.Errorf("unexpected status code %d from Plex server: %w", resp.StatusCode, errPlexServerError)
		}
		return nil, 0, fmt.Errorf("unexpected status code %d from Plex server", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read response: %w", err)
	}

	var mediaResp struct {
		MediaContainer struct {
			TotalSize int               `json:"totalSize"`
			Size      int               `json:"size"`
			Metadata  []sectionMetadata `json:"Metadata"`
		} `json:"MediaContainer"`
	}

	if err := json.Unmarshal(body, &mediaResp); err != nil {
		apiLogger.Printf("warning: failed to parse media response for section %s, API format may have changed: %v", sectionKey, err)
		return nil, 0, fmt.Errorf("failed to parse media response: %w", err)
	}

	return mediaResp.MediaContainer.Metadata, mediaResp.MediaContainer.TotalSize, nil
}

// GetStreamURL returns the direct stream URL for a media item
// This gets the actual file URL that can be streamed by MPV
func (c *Client) GetStreamURL(mediaKey string) (string, error) {
	// First, get the metadata for this item to find the media part key
	url := fmt.Sprintf("%s%s?X-Plex-Token=%s", c.serverURL, mediaKey, c.token)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Client-Identifier", "goplexcli")
	req.Header.Set("X-Plex-Product", "GoplexCLI")
	req.Header.Set("X-Plex-Version", "1.0")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get metadata: %w", err)
	}
	defer resp.Body.Close()

	// Check HTTP status code
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return "", fmt.Errorf("authentication failed: invalid or expired token (status %d)", resp.StatusCode)
		}
		if resp.StatusCode == http.StatusNotFound {
			return "", fmt.Errorf("media item not found: %s (status %d)", mediaKey, resp.StatusCode)
		}
		return "", fmt.Errorf("unexpected status code %d from Plex server", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Parse to get the media part
	var metadataResp struct {
		MediaContainer struct {
			Metadata []struct {
				Media []struct {
					Part []struct {
						Key *string `json:"key"`
					} `json:"Part"`
				} `json:"Media"`
			} `json:"Metadata"`
		} `json:"MediaContainer"`
	}

	if err := json.Unmarshal(body, &metadataResp); err != nil {
		apiLogger.Printf("warning: failed to parse stream metadata for %s, API format may have changed: %v", mediaKey, err)
		return "", fmt.Errorf("failed to parse metadata: %w", err)
	}

	// Get the part key
	if len(metadataResp.MediaContainer.Metadata) > 0 &&
		len(metadataResp.MediaContainer.Metadata[0].Media) > 0 &&
		len(metadataResp.MediaContainer.Metadata[0].Media[0].Part) > 0 {

		partKey := metadataResp.MediaContainer.Metadata[0].Media[0].Part[0].Key
		if partKey != nil && *partKey != "" {
			// Use download=1 to get direct file (no transcoding)
			// This is faster and works better with most players
			streamURL := fmt.Sprintf("%s%s?download=1&X-Plex-Token=%s",
				c.serverURL, *partKey, c.token)
			return streamURL, nil
		}
	}

	// Fallback to simple download URL if part key not found
	apiLogger.Printf("warning: could not find media part key for %s, using fallback URL", mediaKey)
	streamURL := fmt.Sprintf("%s%s?download=1&X-Plex-Token=%s",
		c.serverURL, mediaKey, c.token)
	return streamURL, nil
}

// Plex client headers - consistent across all API calls
const (
	plexClientIdentifier = "goplexcli"
	plexProduct          = "GoplexCLI"
	plexVersion          = "1.0"
)

// timelineClient is used for timeline updates with a reasonable timeout
// to prevent blocking if the Plex server is slow or unresponsive.
var timelineClient = &http.Client{
	Timeout: 5 * time.Second,
}

// UpdateTimeline reports playback progress to the Plex server.
// This updates the resume position and shows "Now Playing" on the Plex dashboard.
// state should be "playing", "paused", or "stopped".
// timeMs is the current position in milliseconds.
// durationMs is the total duration in milliseconds.
func (c *Client) UpdateTimeline(ratingKey string, state string, timeMs int, durationMs int) error {
	// Validate inputs
	if ratingKey == "" {
		return fmt.Errorf("ratingKey cannot be empty")
	}
	if state != "playing" && state != "paused" && state != "stopped" {
		return fmt.Errorf("invalid state %q: must be playing, paused, or stopped", state)
	}
	if timeMs < 0 {
		timeMs = 0
	}
	if durationMs < 0 {
		durationMs = 0
	}

	url := fmt.Sprintf("%s/:/timeline?ratingKey=%s&key=/library/metadata/%s&state=%s&time=%d&duration=%d&X-Plex-Token=%s",
		c.serverURL, ratingKey, ratingKey, state, timeMs, durationMs, c.token)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create timeline request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Client-Identifier", plexClientIdentifier)
	req.Header.Set("X-Plex-Product", plexProduct)
	req.Header.Set("X-Plex-Version", plexVersion)

	// Use timelineClient with timeout to prevent blocking on slow servers
	resp, err := timelineClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to update timeline: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("timeline update failed with status %d", resp.StatusCode)
	}

	return nil
}

// convertToRclonePath converts a Plex on-disk file path to an rclone remote
// path. If the client has configured PathMappings, the first matching mapping
// (longest prefix wins) is applied. When no mapping matches — including the
// case of no configured mappings at all — it falls back to the legacy
// heuristic that strips a "/home/joshkerr/" prefix and treats the first path
// component as the remote name, preserving behavior for existing installs.
func (c *Client) convertToRclonePath(filePath string) string {
	if filePath == "" {
		return ""
	}

	// Try configured mappings, longest prefix first so more specific rules win.
	if best, ok := longestMatchingMapping(c.pathMappings, filePath); ok {
		return best.Remote + filePath[len(best.Prefix):]
	}

	return legacyRclonePath(filePath)
}

// longestMatchingMapping returns the mapping whose Prefix is the longest prefix
// of filePath, if any.
func longestMatchingMapping(mappings []PathMapping, filePath string) (PathMapping, bool) {
	var best PathMapping
	found := false
	for _, m := range mappings {
		if m.Prefix == "" {
			continue
		}
		if strings.HasPrefix(filePath, m.Prefix) && len(m.Prefix) > len(best.Prefix) {
			best = m
			found = true
		}
	}
	return best, found
}

// legacyRclonePath is the original hardcoded conversion, kept as a fallback for
// installs that have not configured path_mappings.
// Input:  /home/joshkerr/plexcloudservers2/Media/TV/...
// Output: plexcloudservers2:Media/TV/...
func legacyRclonePath(filePath string) string {
	path := strings.TrimPrefix(filePath, "/home/joshkerr/")

	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		return ""
	}

	remoteName := parts[0]
	remotePath := parts[1]

	return fmt.Sprintf("%s:%s", remoteName, remotePath)
}

// FormatMediaTitle returns a formatted title for display
func (m *MediaItem) FormatMediaTitle() string {
	var title string
	switch m.Type {
	case "movie":
		if m.Year > 0 {
			title = fmt.Sprintf("%s (%d)", m.Title, m.Year)
		} else {
			title = m.Title
		}
	case "episode":
		title = fmt.Sprintf("%s - S%02dE%02d - %s", m.ParentTitle, m.ParentIndex, m.Index, m.Title)
	default:
		title = m.Title
	}

	// Add progress indicator
	if m.Duration > 0 {
		if m.ViewCount > 0 {
			// Watched
			title = fmt.Sprintf("%s ✓", title)
		} else if m.ViewOffset > 0 {
			// Calculate percentage using float division for precision (consistent with HasResumableProgress)
			pct := int(float64(m.ViewOffset) * 100 / float64(m.Duration))
			if pct >= 95 {
				// >=95% complete, show as watched (consistent with HasResumableProgress)
				title = fmt.Sprintf("%s ✓", title)
			} else {
				// In progress
				title = fmt.Sprintf("%s ▶ %d%%", title, pct)
			}
		}
	}

	return title
}

// Server represents a Plex server
type Server struct {
	Name        string
	URL         string
	Local       bool
	Owned       bool
	Connections []string
	// AccessToken is the per-server token issued by plex.tv. For shared
	// (non-owner) users this is the only token the server accepts; the
	// account token used to talk to plex.tv gets a 401.
	AccessToken string
}

// Authenticate authenticates with Plex using username and password
// Returns auth token and list of available servers
func Authenticate(username, password string) (string, []Server, error) {
	// Create SDK client for authentication
	sdk := plexgo.New(
		plexgo.WithClientIdentifier("goplexcli"),
		plexgo.WithProduct("GoplexCLI"),
		plexgo.WithVersion("1.0"),
	)

	ctx := context.Background()

	// Sign in
	res, err := sdk.Authentication.PostUsersSignInData(ctx, operations.PostUsersSignInDataRequest{
		RequestBody: &operations.PostUsersSignInDataRequestBody{
			Login:    username,
			Password: password,
		},
	})
	if err != nil {
		return "", nil, fmt.Errorf("authentication failed: %w", err)
	}

	if res.UserPlexAccount == nil {
		return "", nil, fmt.Errorf("no auth token received")
	}

	token := res.UserPlexAccount.AuthToken

	// Get available servers/resources using the token
	// Create a new SDK instance with the auth token
	authSDK := plexgo.New(
		plexgo.WithSecurity(token),
		plexgo.WithClientIdentifier("goplexcli"),
		plexgo.WithProduct("GoplexCLI"),
		plexgo.WithVersion("1.0"),
	)

	resourcesRes, err := authSDK.Plex.GetServerResources(ctx, operations.GetServerResourcesRequest{})
	if err != nil {
		return "", nil, fmt.Errorf("failed to get servers: %w", err)
	}

	if len(resourcesRes.PlexDevices) == 0 {
		return "", nil, fmt.Errorf("no resources found")
	}

	// Build list of available servers
	var servers []Server
	for _, device := range resourcesRes.PlexDevices {
		if device.Provides != "" && strings.Contains(device.Provides, "server") {
			server := Server{
				Name:        device.Name,
				Owned:       device.Owned,
				AccessToken: device.AccessToken,
			}

			// Collect all connection URLs
			var connections []string
			for _, conn := range device.Connections {
				connections = append(connections, conn.URI)
				// Set the preferred URL (local first)
				if server.URL == "" {
					server.URL = conn.URI
					server.Local = conn.Local
				} else if conn.Local && !server.Local {
					// Prefer local connection
					server.URL = conn.URI
					server.Local = conn.Local
				}
			}
			server.Connections = connections

			if server.URL != "" {
				servers = append(servers, server)
			}
		}
	}

	if len(servers) == 0 {
		return "", nil, fmt.Errorf("no servers found")
	}

	return token, servers, nil
}

// taggedItem represents an item with a Tag field (used for Director, Genre, Role)
type taggedItem struct {
	Tag string `json:"tag"`
}

// extractTags extracts tag values from a slice of tagged items
func extractTags(items []taggedItem, limit int) []string {
	var tags []string
	for i, item := range items {
		if limit > 0 && i >= limit {
			break
		}
		tags = append(tags, item.Tag)
	}
	return tags
}

// Helper functions for handling pointer types
func valueOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func valueOrZeroInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func valueOrZeroFloat32(v *float32) float32 {
	if v == nil {
		return 0
	}
	return *v
}

func valueOrZeroInt64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}
