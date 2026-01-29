// Package interfaces defines contracts for external tool integrations.
// These interfaces enable dependency injection and testing by allowing
// mock implementations to be substituted for real external tools.
package interfaces

import (
	"context"

	"github.com/joshkerr/goplexcli/internal/plex"
)

// Player defines the interface for media playback.
// Implementations handle playing media files through external players like mpv.
type Player interface {
	// Play plays a single media URL
	Play(ctx context.Context, url string) error

	// PlayMultiple plays multiple URLs as a playlist
	PlayMultiple(ctx context.Context, urls []string) error

	// IsAvailable checks if the player is available on the system
	IsAvailable() bool
}

// Downloader defines the interface for downloading files.
// Implementations handle downloading media files through tools like rclone.
type Downloader interface {
	// Download downloads a single file from a remote path to a local destination
	Download(ctx context.Context, remotePath, destDir string) error

	// DownloadMultiple downloads multiple files from remote paths
	DownloadMultiple(ctx context.Context, remotePaths []string, destDir string) error

	// IsAvailable checks if the download tool is available on the system
	IsAvailable() bool
}

// MediaSelector defines the interface for interactive media selection.
// Implementations provide UI for selecting media items, typically using fzf.
type MediaSelector interface {
	// SelectMedia presents media items for selection and returns selected indices
	SelectMedia(items []plex.MediaItem, prompt string) ([]int, error)

	// SelectOption presents options and returns the selected option string
	SelectOption(options []string, prompt string) (string, int, error)

	// IsAvailable checks if the selector UI is available
	IsAvailable() bool
}

// PlexClient defines the interface for Plex server interactions.
// This enables mocking Plex API calls in tests.
type PlexClient interface {
	// Test validates the connection to the Plex server
	Test() error

	// GetLibraries returns all library sections
	GetLibraries(ctx context.Context) ([]plex.Library, error)

	// GetAllMedia returns all media items from all libraries
	GetAllMedia(ctx context.Context, progress plex.ProgressCallback) ([]plex.MediaItem, error)

	// GetMediaFromSection returns media items from a specific library section
	GetMediaFromSection(ctx context.Context, sectionKey, sectionType string) ([]plex.MediaItem, error)

	// GetStreamURL returns the direct stream URL for a media item
	GetStreamURL(mediaKey string) (string, error)
}

// StreamServer defines the interface for stream publishing and discovery.
type StreamServer interface {
	// Start starts the stream server (blocks until context is cancelled)
	Start(ctx context.Context) error

	// PublishStream publishes a stream and returns a stream ID
	PublishStream(media *plex.MediaItem, streamURL, plexURL, plexToken string) string
}

// StreamDiscoverer defines the interface for discovering stream servers.
type StreamDiscoverer interface {
	// Discover finds stream servers on the local network
	Discover(ctx context.Context, timeout interface{}) ([]*DiscoveredServer, error)

	// FetchStreams fetches available streams from a discovered server
	FetchStreams(server *DiscoveredServer) ([]*StreamItem, error)
}

// DiscoveredServer represents a discovered stream server (matches stream package)
type DiscoveredServer struct {
	Name      string
	Addresses []string
	Port      int
}

// StreamItem represents a published stream (matches stream package)
type StreamItem struct {
	ID        string
	Title     string
	Year      int
	Duration  int
	StreamURL string
}
