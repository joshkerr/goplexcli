package plex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/LukeHagar/plexgo"
	"github.com/LukeHagar/plexgo/models/operations"
)

type Client struct {
	sdk       *plexgo.PlexAPI
	serverURL string
	token     string
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
}

// New creates a new Plex client
func New(serverURL, token string) (*Client, error) {
	sdk := plexgo.New(
		plexgo.WithServerURL(serverURL),
		plexgo.WithSecurity(token),
		plexgo.WithClientIdentifier("goplexcli"),
		plexgo.WithProduct("GoplexCLI"),
		plexgo.WithVersion("1.0"),
	)

	return &Client{
		sdk:       sdk,
		serverURL: serverURL,
		token:     token,
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
func (c *Client) GetLibraries() ([]Library, error) {
	// Use direct HTTP request to avoid library's unmarshaling issues with hidden field
	url := fmt.Sprintf("%s/library/sections?X-Plex-Token=%s", c.serverURL, c.token)
	
	req, err := http.NewRequest("GET", url, nil)
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
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	
	var sectionsResp sectionsResponse
	if err := json.Unmarshal(body, &sectionsResp); err != nil {
		return nil, fmt.Errorf("failed to parse sections: %w", err)
	}
	
	var libraries []Library
	for _, dir := range sectionsResp.MediaContainer.Directory {
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

// GetAllMedia returns all media items from all libraries
func (c *Client) GetAllMedia(ctx context.Context, progressCallback ProgressCallback) ([]MediaItem, error) {
	libraries, err := c.GetLibraries()
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

// GetMediaFromSection returns media items from a specific library section
func (c *Client) GetMediaFromSection(ctx context.Context, sectionKey, sectionType string) ([]MediaItem, error) {
	var items []MediaItem

	// Use direct HTTP request to get all items from a section
	url := fmt.Sprintf("%s/library/sections/%s/all?X-Plex-Token=%s", c.serverURL, sectionKey, c.token)
	
	req, err := http.NewRequest("GET", url, nil)
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
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	
	// Parse the response
	var mediaResp struct {
		MediaContainer struct {
			Metadata []struct {
				Key              string  `json:"key"`
				Title            string  `json:"title"`
				Year             *int    `json:"year"`
				Summary          *string `json:"summary"`
				Rating           *float32 `json:"rating"`
				Duration         *int    `json:"duration"`
				GrandparentTitle *string `json:"grandparentTitle"`
				ParentTitle      *string `json:"parentTitle"`
				Index            *int    `json:"index"`
				ParentIndex      *int    `json:"parentIndex"`
				Media            []struct {
					Part []struct {
						File *string `json:"file"`
					} `json:"Part"`
				} `json:"Media"`
			} `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	
	if err := json.Unmarshal(body, &mediaResp); err != nil {
		return nil, fmt.Errorf("failed to parse media response: %w", err)
	}

	if sectionType == "movie" {
		// Process movies
		for _, metadata := range mediaResp.MediaContainer.Metadata {
			item := MediaItem{
				Key:      metadata.Key,
				Title:    metadata.Title,
				Year:     valueOrZeroInt(metadata.Year),
				Type:     "movie",
				Summary:  valueOrEmpty(metadata.Summary),
				Rating:   float64(valueOrZeroFloat32(metadata.Rating)),
				Duration: valueOrZeroInt(metadata.Duration),
			}

			// Get file path
			if len(metadata.Media) > 0 && len(metadata.Media[0].Part) > 0 {
				item.FilePath = valueOrEmpty(metadata.Media[0].Part[0].File)
				item.RclonePath = convertToRclonePath(item.FilePath)
			}

			items = append(items, item)
		}
	} else if sectionType == "show" {
		// For TV shows, we need to get all episodes
		// The /all endpoint with a show section returns all episodes
		for _, metadata := range mediaResp.MediaContainer.Metadata {
			item := MediaItem{
				Key:         metadata.Key,
				Title:       metadata.Title,
				Year:        valueOrZeroInt(metadata.Year),
				Type:        "episode",
				Summary:     valueOrEmpty(metadata.Summary),
				Rating:      float64(valueOrZeroFloat32(metadata.Rating)),
				Duration:    valueOrZeroInt(metadata.Duration),
				ParentTitle: valueOrEmpty(metadata.GrandparentTitle),
				GrandTitle:  valueOrEmpty(metadata.ParentTitle),
				Index:       int64(valueOrZeroInt(metadata.Index)),
				ParentIndex: int64(valueOrZeroInt(metadata.ParentIndex)),
			}

			// Get file path
			if len(metadata.Media) > 0 && len(metadata.Media[0].Part) > 0 {
				item.FilePath = valueOrEmpty(metadata.Media[0].Part[0].File)
				item.RclonePath = convertToRclonePath(item.FilePath)
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
		return "", fmt.Errorf("failed to parse metadata: %w", err)
	}
	
	// Get the part key
	if len(metadataResp.MediaContainer.Metadata) > 0 &&
		len(metadataResp.MediaContainer.Metadata[0].Media) > 0 &&
		len(metadataResp.MediaContainer.Metadata[0].Media[0].Part) > 0 {
		
		partKey := metadataResp.MediaContainer.Metadata[0].Media[0].Part[0].Key
		if partKey != nil && *partKey != "" {
			// Build the direct stream URL using the part key
			streamURL := fmt.Sprintf("%s%s?X-Plex-Token=%s", c.serverURL, *partKey, c.token)
			return streamURL, nil
		}
	}
	
	// Fallback to simple download URL if part key not found
	streamURL := fmt.Sprintf("%s%s?download=1&X-Plex-Token=%s", c.serverURL, mediaKey, c.token)
	return streamURL, nil
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
	switch m.Type {
	case "movie":
		if m.Year > 0 {
			return fmt.Sprintf("%s (%d)", m.Title, m.Year)
		}
		return m.Title
	case "episode":
		return fmt.Sprintf("%s - S%02dE%02d - %s", m.ParentTitle, m.ParentIndex, m.Index, m.Title)
	default:
		return m.Title
	}
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

func valueOrZeroInt64(v *int64) int64 {
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

func parseFloat(s string) float64 {
	result, _ := strconv.ParseFloat(s, 64)
	return result
}
