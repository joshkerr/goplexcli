package plex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/jrudio/go-plex-client"
)

type Client struct {
	plex *plex.Plex
}

type MediaItem struct {
	Key          string
	Title        string
	Year         int
	Type         string // movie, show, season, episode
	Summary      string
	Rating       float64
	Duration     int
	FilePath     string
	RclonePath   string
	ParentTitle  string // For episodes: show name
	GrandTitle   string // For episodes: season name
	Index        int64  // Episode or season number
	ParentIndex  int64  // Season number for episodes
}

// New creates a new Plex client
func New(serverURL, token string) (*Client, error) {
	plexClient, err := plex.New(serverURL, token)
	if err != nil {
		return nil, fmt.Errorf("failed to create plex client: %w", err)
	}
	
	return &Client{plex: plexClient}, nil
}

// Test validates the connection to the Plex server
func (c *Client) Test() error {
	_, err := c.plex.GetServers()
	if err != nil {
		return fmt.Errorf("failed to connect to plex server: %w", err)
	}
	return nil
}

// GetLibraries returns all library sections
func (c *Client) GetLibraries() ([]plex.Directory, error) {
	sections, err := c.plex.GetLibraries()
	if err != nil {
		return nil, fmt.Errorf("failed to get libraries: %w", err)
	}
	return sections.MediaContainer.Directory, nil
}

// GetAllMedia returns all media items from all libraries
func (c *Client) GetAllMedia(ctx context.Context) ([]MediaItem, error) {
	sections, err := c.GetLibraries()
	if err != nil {
		return nil, err
	}
	
	var allMedia []MediaItem
	
	for _, section := range sections {
		if section.Type == "movie" || section.Type == "show" {
			media, err := c.GetMediaFromSection(ctx, section.Key, section.Type)
			if err != nil {
				return nil, fmt.Errorf("failed to get media from section %s: %w", section.Title, err)
			}
			allMedia = append(allMedia, media...)
		}
	}
	
	return allMedia, nil
}

// GetMediaFromSection returns media items from a specific library section
func (c *Client) GetMediaFromSection(ctx context.Context, sectionKey, sectionType string) ([]MediaItem, error) {
	var items []MediaItem
	
	if sectionType == "movie" {
		metadata, err := c.plex.GetLibraryContent(sectionKey, "")
		if err != nil {
			return nil, err
		}
		
		for _, video := range metadata.MediaContainer.Metadata {
			item := MediaItem{
				Key:      video.Key,
				Title:    video.Title,
				Year:     video.Year,
				Type:     "movie",
				Summary:  video.Summary,
				Rating:   video.Rating,
				Duration: video.Duration,
			}
			
			// Get file path
			if len(video.Media) > 0 && len(video.Media[0].Part) > 0 {
				item.FilePath = video.Media[0].Part[0].File
				item.RclonePath = convertToRclonePath(item.FilePath)
			}
			
			items = append(items, item)
		}
	} else if sectionType == "show" {
		// Get all shows
		metadata, err := c.plex.GetLibraryContent(sectionKey, "")
		if err != nil {
			return nil, err
		}
		
		for _, show := range metadata.MediaContainer.Metadata {
			// Get all seasons for this show
			seasons, err := c.plex.GetMetadata(show.Key)
			if err != nil {
				continue
			}
			
			for _, season := range seasons.MediaContainer.Metadata {
				// Get all episodes for this season
				episodes, err := c.plex.GetMetadata(season.Key)
				if err != nil {
					continue
				}
				
				for _, episode := range episodes.MediaContainer.Metadata {
					item := MediaItem{
						Key:         episode.Key,
						Title:       episode.Title,
						Year:        episode.Year,
						Type:        "episode",
						Summary:     episode.Summary,
						Rating:      episode.Rating,
						Duration:    episode.Duration,
						ParentTitle: show.Title,
						GrandTitle:  season.Title,
						Index:       episode.Index,
						ParentIndex: season.Index,
					}
					
					// Get file path
					if len(episode.Media) > 0 && len(episode.Media[0].Part) > 0 {
						item.FilePath = episode.Media[0].Part[0].File
						item.RclonePath = convertToRclonePath(item.FilePath)
					}
					
					items = append(items, item)
				}
			}
		}
	}
	
	return items, nil
}

// GetStreamURL returns the direct stream URL for a media item
func (c *Client) GetStreamURL(mediaKey string) (string, error) {
	// Get the server URL
	serverURL := c.plex.URL
	
	// Build the stream URL
	streamURL := fmt.Sprintf("%s%s?download=1&X-Plex-Token=%s", serverURL, mediaKey, c.plex.Token)
	
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

// PlexAuthResponse represents the authentication response from Plex.tv
type PlexAuthResponse struct {
	User struct {
		AuthToken string `json:"authToken"`
		ID        int    `json:"id"`
		UUID      string `json:"uuid"`
		Email     string `json:"email"`
		Username  string `json:"username"`
	} `json:"user"`
}

// PlexDevice represents a Plex server device
type PlexDevice struct {
	Name       string `json:"name"`
	Product    string `json:"product"`
	Provides   string `json:"provides"`
	Connection []struct {
		Protocol string `json:"protocol"`
		Address  string `json:"address"`
		Port     int    `json:"port"`
		URI      string `json:"uri"`
		Local    int    `json:"local"`
	} `json:"Connection"`
}

// Authenticate authenticates with Plex using username and password
func Authenticate(username, password string) (string, string, error) {
	// Make direct HTTP request to Plex.tv API
	client := &http.Client{}
	
	// Create the authentication request
	authURL := "https://plex.tv/api/v2/users/signin"
	
	authData := map[string]string{
		"login":    username,
		"password": password,
	}
	
	authJSON, err := json.Marshal(authData)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal auth data: %w", err)
	}
	
	req, err := http.NewRequest("POST", authURL, bytes.NewBuffer(authJSON))
	if err != nil {
		return "", "", fmt.Errorf("failed to create request: %w", err)
	}
	
	// Set required headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Product", "GoplexCLI")
	req.Header.Set("X-Plex-Version", "1.0")
	req.Header.Set("X-Plex-Client-Identifier", "goplexcli")
	
	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("authentication request failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("authentication failed with status %d: %s", resp.StatusCode, string(body))
	}
	
	// Parse the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed to read response: %w", err)
	}
	
	var authResp PlexAuthResponse
	if err := json.Unmarshal(body, &authResp); err != nil {
		return "", "", fmt.Errorf("failed to parse auth response: %w", err)
	}
	
	if authResp.User.AuthToken == "" {
		return "", "", fmt.Errorf("no auth token received")
	}
	
	token := authResp.User.AuthToken
	
	// Get available servers using the token
	devicesURL := "https://plex.tv/api/v2/resources?includeHttps=1&includeRelay=1"
	
	req, err = http.NewRequest("GET", devicesURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to create devices request: %w", err)
	}
	
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Token", token)
	req.Header.Set("X-Plex-Product", "GoplexCLI")
	req.Header.Set("X-Plex-Version", "1.0")
	req.Header.Set("X-Plex-Client-Identifier", "goplexcli")
	
	resp, err = client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("failed to get devices: %w", err)
	}
	defer resp.Body.Close()
	
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed to read devices response: %w", err)
	}
	
	var devices []PlexDevice
	if err := json.Unmarshal(body, &devices); err != nil {
		return "", "", fmt.Errorf("failed to parse devices: %w", err)
	}
	
	// Find a server device
	var serverURL string
	for _, device := range devices {
		if strings.Contains(device.Provides, "server") {
			if len(device.Connection) > 0 {
				// Prefer local connection
				for _, conn := range device.Connection {
					if conn.Local == 1 {
						serverURL = conn.URI
						break
					}
				}
				// Fallback to first connection
				if serverURL == "" {
					serverURL = device.Connection[0].URI
				}
				break
			}
		}
	}
	
	if serverURL == "" {
		return "", "", fmt.Errorf("no server found")
	}
	
	// Ensure URL is properly formatted
	if _, err := url.Parse(serverURL); err != nil {
		return "", "", fmt.Errorf("invalid server URL: %w", err)
	}
	
	return serverURL, token, nil
}
