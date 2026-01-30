// Package plex provides a client for interacting with Plex Media Server.
// It supports authentication, library browsing, and stream URL generation.
// The client handles API versioning gracefully and logs warnings for unexpected responses.
package plex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/LukeHagar/plexgo"
	"github.com/LukeHagar/plexgo/models/operations"
)

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
	sdk        *plexgo.PlexAPI
	serverURL  string
	serverName string
	token      string
}

type MediaItem struct {
	Key         string
	Title       string
	Year        int
	Type        string // movie, show, season, episode
	Summary     string
	Rating      float64
	Duration    int
	FilePath    string
	RclonePath  string
	ParentTitle string // For episodes: show name
	GrandTitle  string // For episodes: season name
	Index       int64  // Episode or season number
	ParentIndex int64  // Season number for episodes
	Thumb       string // Poster/thumbnail URL path
	ServerName  string // Name of the Plex server this item belongs to
	ServerURL   string // URL of the Plex server this item belongs to
	ViewOffset  int    // Playback position in milliseconds (0 if not started)
	ViewCount   int    // Number of times fully watched
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
	ctx := context.Background()
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

	client := &http.Client{}
	resp, err := client.Do(req)
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

// ProgressCallback is called during media fetching to report progress
type ProgressCallback func(libraryName string, itemCount int, totalLibraries int, currentLibrary int)

// ServerProgressCallback is called during multi-server media fetching
type ServerProgressCallback func(serverName, libraryName string, itemCount int, totalLibraries int, currentLibrary int, serverNum int, totalServers int)

// GetAllMedia returns all media items from all libraries
func (c *Client) GetAllMedia(ctx context.Context, progressCallback ProgressCallback) ([]MediaItem, error) {
	libraries, err := c.GetLibraries(ctx)
	if err != nil {
		return nil, err
	}

	var allMedia []MediaItem
	totalLibs := 0

	// Count libraries we'll actually process
	for _, lib := range libraries {
		if lib.Type == "movie" || lib.Type == "show" {
			totalLibs++
		}
	}

	currentLib := 0
	for _, lib := range libraries {
		if lib.Type == "movie" || lib.Type == "show" {
			currentLib++
			media, err := c.GetMediaFromSection(ctx, lib.Key, lib.Type)
			if err != nil {
				return nil, fmt.Errorf("failed to get media from section %s: %w", lib.Title, err)
			}
			allMedia = append(allMedia, media...)

			// Report progress
			if progressCallback != nil {
				progressCallback(lib.Title, len(media), totalLibs, currentLib)
			}
		}
	}

	return allMedia, nil
}

// GetAllMediaFromServers returns all media items from multiple Plex servers
func GetAllMediaFromServers(ctx context.Context, serverConfigs []struct{ Name, URL, Token string }, progressCallback ServerProgressCallback) ([]MediaItem, error) {
	var allMedia []MediaItem
	totalServers := len(serverConfigs)

	for serverNum, serverConfig := range serverConfigs {
		client, err := NewWithName(serverConfig.URL, serverConfig.Token, serverConfig.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to create client for server %s: %w", serverConfig.Name, err)
		}

		// Test connection
		if err := client.Test(); err != nil {
			return nil, fmt.Errorf("failed to connect to server %s: %w", serverConfig.Name, err)
		}

		libraries, err := client.GetLibraries(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get libraries from server %s: %w", serverConfig.Name, err)
		}

		totalLibs := 0
		for _, lib := range libraries {
			if lib.Type == "movie" || lib.Type == "show" {
				totalLibs++
			}
		}

		currentLib := 0
		for _, lib := range libraries {
			if lib.Type == "movie" || lib.Type == "show" {
				currentLib++
				media, err := client.GetMediaFromSection(ctx, lib.Key, lib.Type)
				if err != nil {
					return nil, fmt.Errorf("failed to get media from section %s on server %s: %w", lib.Title, serverConfig.Name, err)
				}
				allMedia = append(allMedia, media...)

				if progressCallback != nil {
					progressCallback(serverConfig.Name, lib.Title, len(media), totalLibs, currentLib, serverNum+1, totalServers)
				}
			}
		}
	}

	return allMedia, nil
}

// GetMediaFromSection returns media items from a specific library section
func (c *Client) GetMediaFromSection(ctx context.Context, sectionKey, sectionType string) ([]MediaItem, error) {
	var items []MediaItem

	// Build the URL based on section type
	var url string
	if sectionType == "show" {
		// For TV shows, specifically request type=4 (episodes)
		url = fmt.Sprintf("%s/library/sections/%s/all?type=4&X-Plex-Token=%s", c.serverURL, sectionKey, c.token)
	} else {
		// For movies, use the default all endpoint
		url = fmt.Sprintf("%s/library/sections/%s/all?X-Plex-Token=%s", c.serverURL, sectionKey, c.token)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Client-Identifier", "goplexcli")
	req.Header.Set("X-Plex-Product", "GoplexCLI")
	req.Header.Set("X-Plex-Version", "1.0")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get library items: %w", err)
	}
	defer resp.Body.Close()

	// Check HTTP status code
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("authentication failed: invalid or expired token (status %d)", resp.StatusCode)
		}
		if resp.StatusCode == http.StatusNotFound {
			apiLogger.Printf("warning: section %s not found - it may have been removed", sectionKey)
			return nil, fmt.Errorf("library section %s not found (status %d)", sectionKey, resp.StatusCode)
		}
		return nil, fmt.Errorf("unexpected status code %d from Plex server", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse the response
	var mediaResp struct {
		MediaContainer struct {
			Metadata []struct {
				Key              string   `json:"key"`
				Title            string   `json:"title"`
				Year             *int     `json:"year"`
				Summary          *string  `json:"summary"`
				Rating           *float32 `json:"rating"`
				Duration         *int     `json:"duration"`
				Thumb            *string  `json:"thumb"`
				GrandparentTitle *string  `json:"grandparentTitle"`
				ParentTitle      *string  `json:"parentTitle"`
				Index            *int     `json:"index"`
				ParentIndex      *int     `json:"parentIndex"`
				ViewOffset       *int     `json:"viewOffset"`
				ViewCount        *int     `json:"viewCount"`
				Media            []struct {
					Part []struct {
						File *string `json:"file"`
					} `json:"Part"`
				} `json:"Media"`
			} `json:"Metadata"`
		} `json:"MediaContainer"`
	}

	if err := json.Unmarshal(body, &mediaResp); err != nil {
		apiLogger.Printf("warning: failed to parse media response for section %s, API format may have changed: %v", sectionKey, err)
		return nil, fmt.Errorf("failed to parse media response: %w", err)
	}

	if sectionType == "movie" {
		// Process movies
		for _, metadata := range mediaResp.MediaContainer.Metadata {
			// Validate required fields
			if metadata.Key == "" {
				apiLogger.Printf("warning: movie item missing key field, skipping")
				continue
			}
			if metadata.Title == "" {
				apiLogger.Printf("warning: movie item %s missing title field", metadata.Key)
			}

			item := MediaItem{
				Key:        metadata.Key,
				Title:      metadata.Title,
				Year:       valueOrZeroInt(metadata.Year),
				Type:       "movie",
				Summary:    valueOrEmpty(metadata.Summary),
				Rating:     float64(valueOrZeroFloat32(metadata.Rating)),
				Duration:   valueOrZeroInt(metadata.Duration),
				Thumb:      valueOrEmpty(metadata.Thumb),
				ServerName: c.serverName,
				ServerURL:  c.serverURL,
				ViewOffset: valueOrZeroInt(metadata.ViewOffset),
				ViewCount:  valueOrZeroInt(metadata.ViewCount),
			}

			// Get file path
			if len(metadata.Media) > 0 && len(metadata.Media[0].Part) > 0 {
				item.FilePath = valueOrEmpty(metadata.Media[0].Part[0].File)
				item.RclonePath = convertToRclonePath(item.FilePath)
			} else {
				apiLogger.Printf("warning: movie %q has no media parts", metadata.Title)
			}

			items = append(items, item)
		}
	} else if sectionType == "show" {
		// For TV shows, we explicitly requested type=4 (episodes)
		for _, metadata := range mediaResp.MediaContainer.Metadata {
			// Validate required fields
			if metadata.Key == "" {
				apiLogger.Printf("warning: episode item missing key field, skipping")
				continue
			}
			if metadata.Title == "" {
				apiLogger.Printf("warning: episode item %s missing title field", metadata.Key)
			}

			item := MediaItem{
				Key:         metadata.Key,
				Title:       metadata.Title,
				Year:        valueOrZeroInt(metadata.Year),
				Type:        "episode",
				Summary:     valueOrEmpty(metadata.Summary),
				Rating:      float64(valueOrZeroFloat32(metadata.Rating)),
				Duration:    valueOrZeroInt(metadata.Duration),
				Thumb:       valueOrEmpty(metadata.Thumb),
				ParentTitle: valueOrEmpty(metadata.GrandparentTitle),
				GrandTitle:  valueOrEmpty(metadata.ParentTitle),
				Index:       int64(valueOrZeroInt(metadata.Index)),
				ParentIndex: int64(valueOrZeroInt(metadata.ParentIndex)),
				ServerName:  c.serverName,
				ServerURL:   c.serverURL,
				ViewOffset:  valueOrZeroInt(metadata.ViewOffset),
				ViewCount:   valueOrZeroInt(metadata.ViewCount),
			}

			// Get file path
			if len(metadata.Media) > 0 && len(metadata.Media[0].Part) > 0 {
				item.FilePath = valueOrEmpty(metadata.Media[0].Part[0].File)
				item.RclonePath = convertToRclonePath(item.FilePath)
			} else {
				apiLogger.Printf("warning: episode %q has no media parts", metadata.Title)
			}

			items = append(items, item)
		}
	}

	return items, nil
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

// convertToRclonePath converts a Plex file path to an rclone remote path
// Input: /home/joshkerr/plexcloudservers2/Media/TV/...
// Output: plexcloudservers2:/Media/TV/...
func convertToRclonePath(filePath string) string {
	if filePath == "" {
		return ""
	}

	// Remove /home/joshkerr/ prefix
	path := strings.TrimPrefix(filePath, "/home/joshkerr/")

	// Find the first directory component (plexcloudservers or plexcloudservers2)
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		return ""
	}

	remoteName := parts[0]
	remotePath := parts[1]

	// Format as rclone remote path
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

	// Add server name if present and multiple servers might be in use
	if m.ServerName != "" && m.ServerName != "Default Server" {
		title = fmt.Sprintf("[%s] %s", m.ServerName, title)
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
				Name:  device.Name,
				Owned: device.Owned,
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
