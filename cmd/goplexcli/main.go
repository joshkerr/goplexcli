package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/joshkerr/goplexcli/internal/cache"
	"github.com/joshkerr/goplexcli/internal/config"
	"github.com/joshkerr/goplexcli/internal/download"
	"github.com/joshkerr/goplexcli/internal/player"
	"github.com/joshkerr/goplexcli/internal/plex"
	"github.com/joshkerr/goplexcli/internal/stream"
	"github.com/joshkerr/goplexcli/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// version is set at build time via ldflags: -X main.version=$(VERSION)
// If not set during build, falls back to VERSION file
var version = "dev"

func init() {
	// If version wasn't set at build time, try to read from VERSION file
	if version == "dev" {
		// Try current directory first (development)
		if data, err := os.ReadFile("VERSION"); err == nil {
			if v := strings.TrimSpace(string(data)); v != "" {
				version = v
				return
			}
		}
		// Try relative to executable (installed binary)
		if exe, err := os.Executable(); err == nil {
			versionPath := filepath.Join(filepath.Dir(exe), "VERSION")
			if data, err := os.ReadFile(versionPath); err == nil {
				if v := strings.TrimSpace(string(data)); v != "" {
					version = v
				}
			}
		}
	}
}

var (
	// Styles
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			MarginBottom(1)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("86"))

	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214"))
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "goplexcli",
		Short: "A CLI tool for browsing and streaming from your Plex server",
		Long:  "A powerful command-line interface for interacting with your Plex media server.\nBrowse, stream, and download your media with ease.",
	}

	// Login command
	loginCmd := &cobra.Command{
		Use:   "login",
		Short: "Login to your Plex account",
		RunE:  runLogin,
	}

	// Browse command
	browseCmd := &cobra.Command{
		Use:   "browse",
		Short: "Browse and play media from your Plex server",
		RunE:  runBrowse,
	}

	// Cache command
	cacheCmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage media cache",
	}

	cacheUpdateCmd := &cobra.Command{
		Use:   "update",
		Short: "Update cache with new media",
		RunE:  runCacheUpdate,
	}

	cacheReindexCmd := &cobra.Command{
		Use:   "reindex",
		Short: "Rebuild cache from scratch",
		RunE:  runCacheReindex,
	}

	cacheInfoCmd := &cobra.Command{
		Use:   "info",
		Short: "Show cache information",
		RunE:  runCacheInfo,
	}

	cacheSearchCmd := &cobra.Command{
		Use:   "search [title]",
		Short: "Search for a specific title in both cache and Plex",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runCacheSearch,
	}

	cacheCmd.AddCommand(cacheUpdateCmd, cacheReindexCmd, cacheInfoCmd, cacheSearchCmd)

	// Config command
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Show configuration",
		RunE:  runConfig,
	}

	// Stream command
	streamCmd := &cobra.Command{
		Use:   "stream",
		Short: "Discover and play streams from other devices",
		RunE:  runStream,
	}

	// Server command
	serverCmd := &cobra.Command{
		Use:   "server",
		Short: "Manage Plex servers",
	}

	serverListCmd := &cobra.Command{
		Use:   "list",
		Short: "List all configured servers",
		RunE:  runServerList,
	}

	serverEnableCmd := &cobra.Command{
		Use:   "enable [server-name]",
		Short: "Enable a server for indexing",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runServerEnable,
	}

	serverDisableCmd := &cobra.Command{
		Use:   "disable [server-name]",
		Short: "Disable a server from indexing",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runServerDisable,
	}

	serverCmd.AddCommand(serverListCmd, serverEnableCmd, serverDisableCmd)

	// Version command
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("goplexcli v%s\n", version)
		},
	}

	rootCmd.AddCommand(loginCmd, browseCmd, cacheCmd, configCmd, streamCmd, serverCmd, versionCmd)

	// Show logo before executing any command
	ui.Logo(version)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(errorStyle.Render("Error: " + err.Error()))
		os.Exit(1)
	}
}

func runLogin(cmd *cobra.Command, args []string) error {
	fmt.Println(titleStyle.Render("Plex Login"))

	// Get username
	fmt.Print("Username: ")
	var username string
	if _, err := fmt.Scanln(&username); err != nil {
		return fmt.Errorf("failed to read username: %w", err)
	}

	// Get password (hidden input)
	fmt.Print("Password: ")
	passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return fmt.Errorf("failed to read password: %w", err)
	}
	password := string(passwordBytes)
	fmt.Println() // New line after password input

	fmt.Println(infoStyle.Render("\nAuthenticating..."))

	token, servers, err := plex.Authenticate(username, password)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	fmt.Println(successStyle.Render("✓ Authentication successful"))

	// Select server
	var selectedServer plex.Server
	var selectedURL string
	
	if len(servers) == 1 {
		selectedServer = servers[0]
		fmt.Println(infoStyle.Render(fmt.Sprintf("\nFound server: %s", selectedServer.Name)))
		
		// If server has multiple connections, let user choose
		if len(selectedServer.Connections) > 1 {
			selectedURL, err = selectConnection(selectedServer)
			if err != nil {
				return err
			}
		} else {
			selectedURL = selectedServer.URL
		}
	} else {
		// Multiple servers - let user choose
		fmt.Println(infoStyle.Render(fmt.Sprintf("\nFound %d servers", len(servers))))

		// Load config to check for fzf
		cfg, _ := config.Load()

		// Format servers for selection
		var serverNames []string
		for i, server := range servers {
			owned := ""
			if server.Owned {
				owned = " (owned)"
			}
			serverNames = append(serverNames, fmt.Sprintf("%d. %s%s", i+1, server.Name, owned))
		}

		// Check if fzf is available
		if ui.IsAvailable(cfg.FzfPath) {
			_, idx, err := ui.SelectWithFzf(serverNames, "Select server:", cfg.FzfPath)
			if err != nil {
				return fmt.Errorf("server selection failed: %w", err)
			}
			if idx >= 0 && idx < len(servers) {
				selectedServer = servers[idx]
			} else {
				return fmt.Errorf("invalid server selection")
			}
		} else {
			// Fallback to manual selection
			for _, name := range serverNames {
				fmt.Println("  " + name)
			}
			fmt.Print("\nSelect server number: ")
			var choice int
			if _, err := fmt.Scanln(&choice); err != nil {
				return fmt.Errorf("failed to read selection: %w", err)
			}
			if choice < 1 || choice > len(servers) {
				return fmt.Errorf("invalid selection")
			}
			selectedServer = servers[choice-1]
		}
		
		// Now select connection for the chosen server
		if len(selectedServer.Connections) > 1 {
			selectedURL, err = selectConnection(selectedServer)
			if err != nil {
				return err
			}
		} else {
			selectedURL = selectedServer.URL
		}
	}

	fmt.Println(successStyle.Render(fmt.Sprintf("✓ Selected server: %s", selectedServer.Name)))

	// Load existing config to preserve custom settings
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	
	// Check if we want to add this as an additional server or replace
	if len(cfg.Servers) > 0 {
		fmt.Print("\nAdd this as an additional server? (y/n): ")
		var addMore string
		if _, err := fmt.Scanln(&addMore); err != nil {
			addMore = "n" // Default to no on error
		}
		
		if strings.ToLower(addMore) == "y" || strings.ToLower(addMore) == "yes" {
			// Check if server already exists
			serverExists := false
			for i, s := range cfg.Servers {
				if s.URL == selectedURL {
					cfg.Servers[i].Enabled = true
					serverExists = true
					fmt.Println(infoStyle.Render("Server already exists, enabled it"))
					break
				}
			}
			
			if !serverExists {
				// Add new server
				cfg.Servers = append(cfg.Servers, config.PlexServer{
					Name:    selectedServer.Name,
					URL:     selectedURL,
					Enabled: true,
				})
				fmt.Println(successStyle.Render(fmt.Sprintf("✓ Added server '%s'", selectedServer.Name)))
			}
		} else {
			// Replace with new single-server config
			cfg.Servers = []config.PlexServer{
				{
					Name:    selectedServer.Name,
					URL:     selectedURL,
					Enabled: true,
				},
			}
			fmt.Println(infoStyle.Render("Replaced existing server configuration"))
		}
	} else {
		// First server
		cfg.Servers = []config.PlexServer{
			{
				Name:    selectedServer.Name,
				URL:     selectedURL,
				Enabled: true,
			},
		}
	}
	
	// Update legacy fields for backward compatibility
	cfg.PlexURL = selectedURL
	cfg.PlexToken = token
	cfg.PlexUsername = username

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println(successStyle.Render("✓ Configuration saved"))
	
	if len(cfg.Servers) > 1 {
		fmt.Println(infoStyle.Render(fmt.Sprintf("\nYou now have %d servers configured:", len(cfg.Servers))))
		for _, s := range cfg.Servers {
			enabledStr := ""
			if s.Enabled {
				enabledStr = " (enabled)"
			}
			fmt.Println(infoStyle.Render(fmt.Sprintf("  - %s%s", s.Name, enabledStr)))
		}
	} else {
		fmt.Println(infoStyle.Render("\nServer URL: " + selectedURL))
	}
	
	fmt.Println(infoStyle.Render("\nRun 'goplexcli cache reindex' to build your media cache"))

	return nil
}

func selectConnection(server plex.Server) (string, error) {
	fmt.Println(infoStyle.Render(fmt.Sprintf("\nServer '%s' has %d available connections:", server.Name, len(server.Connections))))
	
	// Load config to check for fzf
	cfg, _ := config.Load()
	
	// Format connections for selection
	var connectionDescs []string
	for i, conn := range server.Connections {
		connType := "Remote"
		if i == 0 && server.Local {
			connType = "Local"
		} else {
			// Check if this connection looks local (private IP)
			if strings.HasPrefix(conn, "http://192.168.") || 
			   strings.HasPrefix(conn, "http://10.") || 
			   strings.HasPrefix(conn, "http://172.") {
				connType = "Local"
			}
		}
		connectionDescs = append(connectionDescs, fmt.Sprintf("%d. %s [%s]", i+1, conn, connType))
	}
	
	var selectedIdx int
	
	// Check if fzf is available
	if ui.IsAvailable(cfg.FzfPath) {
		_, idx, err := ui.SelectWithFzf(connectionDescs, "Select connection:", cfg.FzfPath)
		if err != nil {
			return "", fmt.Errorf("connection selection failed: %w", err)
		}
		selectedIdx = idx
	} else {
		// Fallback to manual selection
		for _, desc := range connectionDescs {
			fmt.Println("  " + desc)
		}
		fmt.Print("\nSelect connection number: ")
		var choice int
		if _, err := fmt.Scanln(&choice); err != nil {
			return "", fmt.Errorf("failed to read selection: %w", err)
		}
		if choice < 1 || choice > len(server.Connections) {
			return "", fmt.Errorf("invalid selection")
		}
		selectedIdx = choice - 1
	}
	
	if selectedIdx < 0 || selectedIdx >= len(server.Connections) {
		return "", fmt.Errorf("invalid connection selection")
	}
	
	return server.Connections[selectedIdx], nil
}

func selectMediaTypeManual() (string, error) {
	fmt.Println(infoStyle.Render("\nSelect media type:"))
	fmt.Println("  1. Movies")
	fmt.Println("  2. TV Shows")
	fmt.Println("  3. All")
	fmt.Print("\nChoice (1-3): ")
	
	var choice int
	if _, err := fmt.Scanln(&choice); err != nil {
		return "", fmt.Errorf("failed to read selection: %w", err)
	}
	
	switch choice {
	case 1:
		return "movies", nil
	case 2:
		return "tv shows", nil
	case 3:
		return "all", nil
	default:
		return "", fmt.Errorf("invalid selection")
	}
}

func selectMediaManual(media []plex.MediaItem) (*plex.MediaItem, error) {
	fmt.Println(infoStyle.Render("\nAvailable media:"))
	for i, item := range media {
		if i >= 20 {
			fmt.Printf("  ... and %d more items\n", len(media)-20)
			break
		}
		fmt.Printf("  %d. %s\n", i+1, item.FormatMediaTitle())
	}
	fmt.Printf("\nEnter number (1-%d): ", len(media))
	
	var choice int
	if _, err := fmt.Scanln(&choice); err != nil {
		return nil, fmt.Errorf("failed to read selection: %w", err)
	}
	
	if choice < 1 || choice > len(media) {
		return nil, fmt.Errorf("invalid selection")
	}
	
	return &media[choice-1], nil
}


func runBrowse(cmd *cobra.Command, args []string) error {
	// Load config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w. Please run 'goplexcli login' first", err)
	}

	// Load cache
	mediaCache, err := cache.Load()
	if err != nil {
		return fmt.Errorf("failed to load cache: %w", err)
	}

	if len(mediaCache.Media) == 0 {
		fmt.Println(warningStyle.Render("Cache is empty. Run 'goplexcli cache reindex' first."))
		return nil
	}

	fmt.Println(infoStyle.Render(fmt.Sprintf("Loaded %d media items from cache", len(mediaCache.Media))))
	fmt.Println(infoStyle.Render(fmt.Sprintf("Last updated: %s", mediaCache.LastUpdated.Format(time.RFC822))))

	// Initialize queue
	var queue []*plex.MediaItem

browseLoop:
	for {
		// Ask user to select media type using fzf if available
		var mediaType string
		if ui.IsAvailable(cfg.FzfPath) {
			var err error
			mediaType, err = ui.SelectMediaTypeWithQueue(cfg.FzfPath, len(queue))
			if err != nil {
				if err.Error() == "cancelled by user" {
					return nil
				}
				return fmt.Errorf("media type selection failed: %w", err)
			}
		} else {
			// Fallback to manual selection
			var err error
			mediaType, err = selectMediaTypeManualWithQueue(len(queue))
			if err != nil {
				return err
			}
		}

		// Handle queue view
		if mediaType == "queue" {
			result, err := handleQueueView(cfg, &queue)
			if err != nil {
				return err
			}
			if result == "done" {
				return nil
			}
			continue browseLoop
		}

		// Filter media by type
		var filteredMedia []plex.MediaItem
		switch mediaType {
		case "movies":
			for _, item := range mediaCache.Media {
				if item.Type == "movie" {
					filteredMedia = append(filteredMedia, item)
				}
			}
		case "tv shows":
			for _, item := range mediaCache.Media {
				if item.Type == "episode" {
					filteredMedia = append(filteredMedia, item)
				}
			}
		case "all":
			filteredMedia = mediaCache.Media
		default:
			filteredMedia = mediaCache.Media
		}

		if len(filteredMedia) == 0 {
			fmt.Println(warningStyle.Render("No media found for selected type."))
			continue browseLoop
		}

		fmt.Println(infoStyle.Render(fmt.Sprintf("\nBrowsing %d items...\n", len(filteredMedia))))

		// Use fzf with preview to select media if fzf available, otherwise use manual selection
		var selectedMediaItems []*plex.MediaItem
		if ui.IsAvailable(cfg.FzfPath) {
			selectedIndices, err := ui.SelectMediaWithPreview(filteredMedia, "Select media (TAB for multi-select):", cfg.FzfPath, cfg.PlexURL, cfg.PlexToken)
			if err != nil {
				if err.Error() == "cancelled by user" {
					return nil
				}
				return fmt.Errorf("media selection failed: %w", err)
			}

			// Build list of selected media items
			for _, index := range selectedIndices {
				if index >= 0 && index < len(filteredMedia) {
					selectedMediaItems = append(selectedMediaItems, &filteredMedia[index])
				}
			}
		} else {
			// Fallback to manual selection (no fzf required)
			var err error
			selectedMedia, err := selectMediaManual(filteredMedia)
			if err != nil {
				return err
			}
			selectedMediaItems = []*plex.MediaItem{selectedMedia}
		}

		if len(selectedMediaItems) == 0 {
			return fmt.Errorf("no media selected")
		}

		// Ask what to do
		var action string
		if ui.IsAvailable(cfg.FzfPath) {
			action, err = ui.PromptActionWithQueue(cfg.FzfPath, len(queue))
			if err != nil {
				if err.Error() == "cancelled by user" {
					return nil
				}
				return err
			}
		} else {
			action, err = promptActionManualWithQueue(len(queue))
			if err != nil {
				return err
			}
		}

		switch action {
		case "watch":
			return handleWatchMultiple(cfg, selectedMediaItems)
		case "download":
			return handleDownloadMultiple(cfg, selectedMediaItems)
		case "queue":
			addToQueue(&queue, selectedMediaItems)
			fmt.Println(successStyle.Render(fmt.Sprintf("Added %d item(s) to queue. Queue now has %d items.", len(selectedMediaItems), len(queue))))
			continue browseLoop
		case "stream":
			if len(selectedMediaItems) > 1 {
				fmt.Println(warningStyle.Render("Note: Stream only supports single selection, using first item"))
			}
			return handleStream(cfg, selectedMediaItems[0])
		case "cancel":
			return nil
		default:
			return nil
		}
	}
}

func handleWatchMultiple(cfg *config.Config, mediaItems []*plex.MediaItem) error {
	if len(mediaItems) == 0 {
		return fmt.Errorf("no media items provided")
	}

	// Check if MPV is available
	if !player.IsAvailable(cfg.MPVPath) {
		return fmt.Errorf("mpv is not installed. Please install mpv to watch media")
	}

	fmt.Println(infoStyle.Render(fmt.Sprintf("\nPreparing to play %d items...", len(mediaItems))))

	// Create Plex client
	client, err := plex.New(cfg.PlexURL, cfg.PlexToken)
	if err != nil {
		return fmt.Errorf("failed to create plex client: %w", err)
	}

	// Get stream URLs for all items
	var streamURLs []string
	for i, media := range mediaItems {
		fmt.Printf("\r\x1b[K%s [%d/%d] %s",
			infoStyle.Render("Getting stream URLs"),
			i+1,
			len(mediaItems),
			media.FormatMediaTitle(),
		)
		
		streamURL, err := client.GetStreamURL(media.Key)
		if err != nil {
			fmt.Println()
			return fmt.Errorf("failed to get stream URL for %s: %w", media.FormatMediaTitle(), err)
		}
		streamURLs = append(streamURLs, streamURL)
	}
	fmt.Println()

	fmt.Println(successStyle.Render(fmt.Sprintf("✓ Starting playback of %d items...", len(mediaItems))))
	fmt.Println(infoStyle.Render("Use 'n' in MPV to skip to next item"))

	// Play with MPV (creates a playlist)
	if err := player.PlayMultiple(streamURLs, cfg.MPVPath); err != nil {
		return fmt.Errorf("playback failed: %w", err)
	}

	fmt.Println(successStyle.Render("✓ Playback finished"))
	return nil
}

func handleDownloadMultiple(cfg *config.Config, mediaItems []*plex.MediaItem) error {
	if len(mediaItems) == 0 {
		return fmt.Errorf("no media items provided")
	}

	// Check if rclone is available
	if !download.IsAvailable(cfg.RclonePath) {
		return fmt.Errorf("rclone is not installed. Please install rclone to download media")
	}

	fmt.Println(infoStyle.Render(fmt.Sprintf("\nPreparing to download %d items...", len(mediaItems))))

	// Collect rclone paths and validate
	var rclonePaths []string
	for _, media := range mediaItems {
		if media.RclonePath == "" {
			fmt.Println(warningStyle.Render(fmt.Sprintf("⚠ Skipping %s (no rclone path)", media.FormatMediaTitle())))
			continue
		}
		rclonePaths = append(rclonePaths, media.RclonePath)
		fmt.Println(infoStyle.Render(fmt.Sprintf("  - %s", media.FormatMediaTitle())))
	}

	if len(rclonePaths) == 0 {
		return fmt.Errorf("no valid rclone paths available")
	}

	fmt.Println(successStyle.Render(fmt.Sprintf("\n✓ Starting download of %d items...", len(rclonePaths))))

	// Get current directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Download with rclone
	ctx := context.Background()
	if err := download.DownloadMultiple(ctx, rclonePaths, cwd, cfg.RclonePath); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	fmt.Println(successStyle.Render("✓ All downloads complete"))
	return nil
}

func handleStream(cfg *config.Config, media *plex.MediaItem) error {
	fmt.Println(infoStyle.Render("\nPublishing stream: " + media.FormatMediaTitle()))

	// Create Plex client
	client, err := plex.New(cfg.PlexURL, cfg.PlexToken)
	if err != nil {
		return fmt.Errorf("failed to create plex client: %w", err)
	}

	// Get stream URL
	streamURL, err := client.GetStreamURL(media.Key)
	if err != nil {
		return fmt.Errorf("failed to get stream URL: %w", err)
	}

	// Create and start stream server
	server, err := stream.NewServer(stream.DefaultPort)
	if err != nil {
		return fmt.Errorf("failed to create stream server: %w", err)
	}

	// Publish the stream
	streamID := server.PublishStream(media, streamURL, cfg.PlexURL, cfg.PlexToken)
	
	localIP := stream.GetLocalIP()
	webURL := fmt.Sprintf("http://%s:%d", localIP, stream.DefaultPort)
	
	// URL encode for deep links
	encodedURL := url.QueryEscape(streamURL)
	
	fmt.Println(successStyle.Render("✓ Stream published"))
	fmt.Println(infoStyle.Render(fmt.Sprintf("Stream ID: %s", streamID)))
	fmt.Println(infoStyle.Render(fmt.Sprintf("Title: %s", media.FormatMediaTitle())))
	fmt.Println(warningStyle.Render(fmt.Sprintf("\n🌐 Stream server running on port %d", stream.DefaultPort)))
	
	fmt.Println(successStyle.Render("\n📱 Click to open in your player:"))
	fmt.Println()
	
	playerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)
	linkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Underline(true)
	
	fmt.Printf("  %s  %s\n", playerStyle.Render("Infuse:"), linkStyle.Render(fmt.Sprintf("infuse://x-callback-url/play?url=%s", encodedURL)))
	fmt.Printf("  %s  %s\n", playerStyle.Render("OutPlayer:"), linkStyle.Render(fmt.Sprintf("outplayer://x-callback-url/play?url=%s", encodedURL)))
	fmt.Printf("  %s  %s\n", playerStyle.Render("SenPlayer:"), linkStyle.Render(fmt.Sprintf("senplayer://x-callback-url/play?url=%s", encodedURL)))
	fmt.Printf("  %s  %s\n", playerStyle.Render("VLC:"), linkStyle.Render(fmt.Sprintf("vlc://%s", encodedURL)))
	fmt.Printf("  %s  %s\n", playerStyle.Render("VidHub:"), linkStyle.Render(fmt.Sprintf("open-vidhub://x-callback-url/open?url=%s", encodedURL)))
	
	fmt.Println()
	fmt.Println(successStyle.Render("🌐 Web UI: ") + linkStyle.Render(webURL))
	fmt.Println()
	fmt.Println(infoStyle.Render("Press Ctrl+C or 'q' to stop the server\n"))

	// Setup signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println(warningStyle.Render("\n\nShutting down stream server..."))
		cancel()
	}()

	// Setup keyboard input for 'q' to quit
	go func() {
		oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			return
		}
		defer func() {
			_ = term.Restore(int(os.Stdin.Fd()), oldState)
		}()

		b := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(b)
			if err != nil || n == 0 {
				return
			}
			if b[0] == 'q' || b[0] == 'Q' {
				fmt.Println(warningStyle.Render("\n\nShutting down stream server..."))
				cancel()
				return
			}
		}
	}()

	// Start server (blocks until context cancelled)
	if err := server.Start(ctx); err != nil {
		return fmt.Errorf("stream server failed: %w", err)
	}

	fmt.Println(successStyle.Render("✓ Stream server stopped"))
	return nil
}

// addToQueue appends items to queue, avoiding duplicates by Key
func addToQueue(queue *[]*plex.MediaItem, items []*plex.MediaItem) {
	existing := make(map[string]bool)
	for _, item := range *queue {
		existing[item.Key] = true
	}

	for _, item := range items {
		if !existing[item.Key] {
			*queue = append(*queue, item)
			existing[item.Key] = true
		}
	}
}

// handleQueueView displays queue and handles queue actions
// Returns "done" (after download), "back" (continue browsing), or error
func handleQueueView(cfg *config.Config, queue *[]*plex.MediaItem) (string, error) {
	if len(*queue) == 0 {
		fmt.Println(warningStyle.Render("Queue is empty"))
		return "back", nil
	}

	fmt.Println(titleStyle.Render("Download Queue"))
	fmt.Println(infoStyle.Render(fmt.Sprintf("%d item(s) in queue:\n", len(*queue))))

	for i, item := range *queue {
		fmt.Printf("  %d. %s\n", i+1, item.FormatMediaTitle())
	}
	fmt.Println()

	// Prompt for queue action
	var action string
	var err error

	if ui.IsAvailable(cfg.FzfPath) {
		action, err = ui.PromptQueueAction(cfg.FzfPath, len(*queue))
		if err != nil {
			if err.Error() == "cancelled by user" {
				return "back", nil
			}
			return "", err
		}
	} else {
		action, err = promptQueueActionManual(len(*queue))
		if err != nil {
			return "", err
		}
	}

	switch action {
	case "download":
		err := handleDownloadMultiple(cfg, *queue)
		if err != nil {
			return "", err
		}
		*queue = nil // Clear queue after download
		return "done", nil

	case "clear":
		*queue = nil
		fmt.Println(successStyle.Render("Queue cleared"))
		return "back", nil

	case "remove":
		if ui.IsAvailable(cfg.FzfPath) {
			indices, err := ui.SelectQueueItemsForRemoval(*queue, cfg.FzfPath)
			if err != nil {
				if err.Error() == "cancelled by user" {
					return "back", nil
				}
				return "", err
			}
			removeFromQueue(queue, indices)
			fmt.Println(successStyle.Render(fmt.Sprintf("Removed %d item(s) from queue", len(indices))))
		} else {
			err := removeFromQueueManual(queue)
			if err != nil {
				return "", err
			}
		}
		return "back", nil

	case "back":
		return "back", nil

	default:
		return "back", nil
	}
}

// removeFromQueue removes items at specified indices from queue
func removeFromQueue(queue *[]*plex.MediaItem, indices []int) {
	if len(indices) == 0 {
		return
	}

	// Sort indices in descending order to remove from end first
	sort.Sort(sort.Reverse(sort.IntSlice(indices)))

	for _, idx := range indices {
		if idx >= 0 && idx < len(*queue) {
			*queue = append((*queue)[:idx], (*queue)[idx+1:]...)
		}
	}
}

// promptQueueActionManual - fallback for no-fzf queue action selection
func promptQueueActionManual(queueCount int) (string, error) {
	fmt.Println(infoStyle.Render("\nQueue actions:"))
	fmt.Printf("  1. Download All (%d items)\n", queueCount)
	fmt.Println("  2. Clear Queue")
	fmt.Println("  3. Remove Items")
	fmt.Println("  4. Back to Browse")
	fmt.Print("\nChoice (1-4): ")

	var choice int
	if _, err := fmt.Scanln(&choice); err != nil {
		return "", fmt.Errorf("failed to read selection: %w", err)
	}

	switch choice {
	case 1:
		return "download", nil
	case 2:
		return "clear", nil
	case 3:
		return "remove", nil
	case 4:
		return "back", nil
	default:
		return "back", nil
	}
}

// removeFromQueueManual - fallback for no-fzf queue item removal
func removeFromQueueManual(queue *[]*plex.MediaItem) error {
	fmt.Println(infoStyle.Render("\nSelect items to remove:"))
	for i, item := range *queue {
		fmt.Printf("  %d. %s\n", i+1, item.FormatMediaTitle())
	}
	fmt.Print("\nEnter item numbers to remove (comma-separated, e.g., 1,3,5): ")

	var input string
	if _, err := fmt.Scanln(&input); err != nil {
		return fmt.Errorf("failed to read selection: %w", err)
	}

	// Parse comma-separated indices
	parts := strings.Split(input, ",")
	var indices []int
	for _, part := range parts {
		part = strings.TrimSpace(part)
		var num int
		if _, err := fmt.Sscanf(part, "%d", &num); err == nil {
			if num >= 1 && num <= len(*queue) {
				indices = append(indices, num-1) // Convert to 0-based index
			}
		}
	}

	if len(indices) > 0 {
		removeFromQueue(queue, indices)
		fmt.Println(successStyle.Render(fmt.Sprintf("Removed %d item(s) from queue", len(indices))))
	}

	return nil
}

// selectMediaTypeManualWithQueue - fallback for no-fzf with queue option
func selectMediaTypeManualWithQueue(queueCount int) (string, error) {
	fmt.Println(infoStyle.Render("\nSelect media type:"))

	optionNum := 1
	if queueCount > 0 {
		fmt.Printf("  %d. View Queue (%d items)\n", optionNum, queueCount)
		optionNum++
	}
	fmt.Printf("  %d. Movies\n", optionNum)
	fmt.Printf("  %d. TV Shows\n", optionNum+1)
	fmt.Printf("  %d. All\n", optionNum+2)

	maxChoice := optionNum + 2
	fmt.Printf("\nChoice (1-%d): ", maxChoice)

	var choice int
	if _, err := fmt.Scanln(&choice); err != nil {
		return "", fmt.Errorf("failed to read selection: %w", err)
	}

	if queueCount > 0 {
		switch choice {
		case 1:
			return "queue", nil
		case 2:
			return "movies", nil
		case 3:
			return "tv shows", nil
		case 4:
			return "all", nil
		default:
			return "", fmt.Errorf("invalid selection")
		}
	} else {
		switch choice {
		case 1:
			return "movies", nil
		case 2:
			return "tv shows", nil
		case 3:
			return "all", nil
		default:
			return "", fmt.Errorf("invalid selection")
		}
	}
}

// promptActionManualWithQueue - fallback for no-fzf action selection with queue
func promptActionManualWithQueue(queueCount int) (string, error) {
	queueLabel := "Add to Queue"
	if queueCount > 0 {
		queueLabel = fmt.Sprintf("Add to Queue (%d items)", queueCount)
	}

	fmt.Println(infoStyle.Render("\nSelect action:"))
	fmt.Println("  1. Watch")
	fmt.Println("  2. Download")
	fmt.Printf("  3. %s\n", queueLabel)
	fmt.Println("  4. Stream")
	fmt.Println("  5. Cancel")
	fmt.Print("\nChoice (1-5): ")

	var choice int
	if _, err := fmt.Scanln(&choice); err != nil {
		return "", fmt.Errorf("failed to read selection: %w", err)
	}

	switch choice {
	case 1:
		return "watch", nil
	case 2:
		return "download", nil
	case 3:
		return "queue", nil
	case 4:
		return "stream", nil
	case 5:
		return "cancel", nil
	default:
		return "cancel", nil
	}
}

func runCacheUpdate(cmd *cobra.Command, args []string) error {
	return updateCache(false)
}

func runCacheReindex(cmd *cobra.Command, args []string) error {
	return updateCache(true)
}

func updateCache(fullReindex bool) error {
	// Load config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w. Please run 'goplexcli login' first", err)
	}

	action := "Updating"
	if fullReindex {
		action = "Reindexing"
	}

	fmt.Println(titleStyle.Render(action + " Media Cache"))

	// Check if we have multiple servers
	enabledServers := cfg.GetEnabledServers()
	
	var media []plex.MediaItem
	ctx := context.Background()

	if len(enabledServers) > 1 {
		// Multi-server mode
		fmt.Println(infoStyle.Render(fmt.Sprintf("Found %d enabled servers", len(enabledServers))))
		
		// Build server configs
		var serverConfigs []struct{ Name, URL, Token string }
		for _, server := range enabledServers {
			serverConfigs = append(serverConfigs, struct{ Name, URL, Token string }{
				Name:  server.Name,
				URL:   server.URL,
				Token: cfg.PlexToken,
			})
		}

		totalItems := 0
		media, err = plex.GetAllMediaFromServers(ctx, serverConfigs, func(serverName, libraryName string, itemCount, totalLibs, currentLib, serverNum, totalServers int) {
			totalItems += itemCount
			fmt.Printf("\r\x1b[K%s [Server %d/%d: %s] [%d/%d] %s: %d items (Total: %d)",
				infoStyle.Render("Processing"),
				serverNum,
				totalServers,
				serverName,
				currentLib,
				totalLibs,
				libraryName,
				itemCount,
				totalItems,
			)
		})
		if err != nil {
			return fmt.Errorf("failed to get media: %w", err)
		}
	} else {
		// Single-server mode (legacy or single enabled server)
		var serverURL string
		if len(enabledServers) == 1 {
			serverURL = enabledServers[0].URL
		} else {
			serverURL = cfg.PlexURL
		}

		fmt.Println(infoStyle.Render("Connecting to Plex server..."))

		// Create Plex client
		client, err := plex.New(serverURL, cfg.PlexToken)
		if err != nil {
			return fmt.Errorf("failed to create plex client: %w", err)
		}

		// Test connection
		if err := client.Test(); err != nil {
			return fmt.Errorf("failed to connect to plex server: %w", err)
		}

		fmt.Println(successStyle.Render("✓ Connected to Plex server"))
		fmt.Println(infoStyle.Render("Fetching media library..."))

		// Get all media with progress
		totalItems := 0
		
		media, err = client.GetAllMedia(ctx, func(libraryName string, itemCount, totalLibs, currentLib int) {
			totalItems += itemCount
			fmt.Printf("\r\x1b[K%s [%d/%d] %s: %d items (Total: %d)",
				infoStyle.Render("Processing libraries"),
				currentLib,
				totalLibs,
				libraryName,
				itemCount,
				totalItems,
			)
		})
		if err != nil {
			return fmt.Errorf("failed to get media: %w", err)
		}
	}
	
	fmt.Println() // New line after progress

	fmt.Println(successStyle.Render(fmt.Sprintf("✓ Retrieved %d media items", len(media))))

	// Save to cache
	mediaCache := &cache.Cache{
		Media: media,
	}

	if err := mediaCache.Save(); err != nil {
		return fmt.Errorf("failed to save cache: %w", err)
	}

	fmt.Println(successStyle.Render("✓ Cache saved successfully"))
	
	// Count by type and by server
	movieCount := 0
	episodeCount := 0
	serverCounts := make(map[string]int)
	
	for _, item := range media {
		switch item.Type {
		case "movie":
			movieCount++
		case "episode":
			episodeCount++
		}
		if item.ServerName != "" {
			serverCounts[item.ServerName]++
		}
	}
	
	fmt.Println(infoStyle.Render(fmt.Sprintf("\nTotal items: %d", len(media))))
	fmt.Println(infoStyle.Render(fmt.Sprintf("  Movies: %d", movieCount)))
	fmt.Println(infoStyle.Render(fmt.Sprintf("  Episodes: %d", episodeCount)))
	
	if len(serverCounts) > 1 {
		fmt.Println(infoStyle.Render("\nBy server:"))
		for serverName, count := range serverCounts {
			fmt.Println(infoStyle.Render(fmt.Sprintf("  %s: %d items", serverName, count)))
		}
	}

	return nil
}

func runCacheInfo(cmd *cobra.Command, args []string) error {
	mediaCache, err := cache.Load()
	if err != nil {
		return fmt.Errorf("failed to load cache: %w", err)
	}

	fmt.Println(titleStyle.Render("Cache Information"))

	if len(mediaCache.Media) == 0 {
		fmt.Println(warningStyle.Render("Cache is empty"))
		return nil
	}

	fmt.Println(infoStyle.Render(fmt.Sprintf("Total items: %d", len(mediaCache.Media))))
	fmt.Println(infoStyle.Render(fmt.Sprintf("Last updated: %s", mediaCache.LastUpdated.Format(time.RFC822))))

	// Count by type
	movieCount := 0
	episodeCount := 0
	for _, item := range mediaCache.Media {
		switch item.Type {
		case "movie":
			movieCount++
		case "episode":
			episodeCount++
		}
	}

	fmt.Println(infoStyle.Render(fmt.Sprintf("Movies: %d", movieCount)))
	fmt.Println(infoStyle.Render(fmt.Sprintf("Episodes: %d", episodeCount)))

	return nil
}

func runConfig(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	fmt.Println(titleStyle.Render("Configuration"))

	if cfg.PlexURL == "" {
		fmt.Println(warningStyle.Render("Not logged in. Run 'goplexcli login' first."))
		return nil
	}

	fmt.Println(infoStyle.Render("Plex URL: " + cfg.PlexURL))
	if cfg.PlexUsername != "" {
		fmt.Println(infoStyle.Render("Username: " + cfg.PlexUsername))
	}
	fmt.Println(infoStyle.Render("Token: " + cfg.PlexToken[:10] + "..."))

	configPath, _ := config.GetConfigPath()
	fmt.Println(infoStyle.Render("\nConfig file: " + configPath))

	cachePath, _ := cache.GetCachePath()
	fmt.Println(infoStyle.Render("Cache file: " + cachePath))

	return nil
}

func runStream(cmd *cobra.Command, args []string) error {
	// Load config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	fmt.Println(titleStyle.Render("Stream Discovery"))
	fmt.Println(infoStyle.Render("Searching for goplexcli servers on local network...\n"))

	// Discover servers with 3 second timeout
	ctx := context.Background()
	servers, err := stream.Discover(ctx, 3*time.Second)
	if err != nil {
		return fmt.Errorf("discovery failed: %w", err)
	}

	if len(servers) == 0 {
		fmt.Println(warningStyle.Render("No stream servers found on the network"))
		fmt.Println(infoStyle.Render("\nTo publish a stream:"))
		fmt.Println(infoStyle.Render("  1. Run 'goplexcli browse' on another device"))
		fmt.Println(infoStyle.Render("  2. Select a media item"))
		fmt.Println(infoStyle.Render("  3. Choose 'Stream' option"))
		return nil
	}

	fmt.Println(successStyle.Render(fmt.Sprintf("✓ Found %d server(s)\n", len(servers))))

	// Let user select a server if multiple found
	var selectedServer *stream.DiscoveredServer
	if len(servers) == 1 {
		selectedServer = servers[0]
		fmt.Println(infoStyle.Render(fmt.Sprintf("Connecting to: %s", selectedServer.Name)))
	} else {
		// Format servers for selection
		var serverNames []string
		for _, srv := range servers {
			addr := "unknown"
			if len(srv.Addresses) > 0 {
				addr = srv.Addresses[0]
			}
			serverNames = append(serverNames, fmt.Sprintf("%s (%s)", srv.Name, addr))
		}

		if ui.IsAvailable(cfg.FzfPath) {
			_, idx, err := ui.SelectWithFzf(serverNames, "Select server:", cfg.FzfPath)
			if err != nil {
				if err.Error() == "cancelled by user" {
					return nil
				}
				return fmt.Errorf("server selection failed: %w", err)
			}
			selectedServer = servers[idx]
		} else {
			// Fallback to manual selection
			fmt.Println(infoStyle.Render("Available servers:"))
			for i, name := range serverNames {
				fmt.Printf("  %d. %s\n", i+1, name)
			}
			fmt.Print("\nSelect server number: ")
			var choice int
			if _, err := fmt.Scanln(&choice); err != nil {
				return fmt.Errorf("failed to read selection: %w", err)
			}
			if choice < 1 || choice > len(servers) {
				return fmt.Errorf("invalid selection")
			}
			selectedServer = servers[choice-1]
		}
	}

	// Fetch streams from selected server
	fmt.Println(infoStyle.Render("\nFetching available streams..."))
	streams, err := stream.FetchStreams(selectedServer)
	if err != nil {
		return fmt.Errorf("failed to fetch streams: %w", err)
	}

	if len(streams) == 0 {
		fmt.Println(warningStyle.Render("No streams available on this server"))
		return nil
	}

	fmt.Println(successStyle.Render(fmt.Sprintf("✓ Found %d stream(s)\n", len(streams))))

	// Let user select a stream
	var selectedStream *stream.StreamItem
	if len(streams) == 1 {
		selectedStream = streams[0]
	} else {
		// Format streams for selection
		var streamTitles []string
		for _, s := range streams {
			streamTitles = append(streamTitles, s.Title)
		}

		if ui.IsAvailable(cfg.FzfPath) {
			_, idx, err := ui.SelectWithFzf(streamTitles, "Select stream:", cfg.FzfPath)
			if err != nil {
				if err.Error() == "cancelled by user" {
					return nil
				}
				return fmt.Errorf("stream selection failed: %w", err)
			}
			selectedStream = streams[idx]
		} else {
			// Fallback to manual selection
			fmt.Println(infoStyle.Render("Available streams:"))
			for i, title := range streamTitles {
				fmt.Printf("  %d. %s\n", i+1, title)
			}
			fmt.Print("\nSelect stream number: ")
			var choice int
			if _, err := fmt.Scanln(&choice); err != nil {
				return fmt.Errorf("failed to read selection: %w", err)
			}
			if choice < 1 || choice > len(streams) {
				return fmt.Errorf("invalid selection")
			}
			selectedStream = streams[choice-1]
		}
	}

	// Show stream info
	fmt.Println(infoStyle.Render("\nStream: " + selectedStream.Title))
	if selectedStream.Year > 0 {
		fmt.Println(infoStyle.Render(fmt.Sprintf("Year: %d", selectedStream.Year)))
	}
	if selectedStream.Duration > 0 {
		fmt.Println(infoStyle.Render(fmt.Sprintf("Duration: %d min", selectedStream.Duration/60000)))
	}

	// Check if MPV is available
	if !player.IsAvailable(cfg.MPVPath) {
		fmt.Println(warningStyle.Render("\nMPV not found. You can still play the stream manually:"))
		fmt.Println(infoStyle.Render(selectedStream.StreamURL))
		return nil
	}

	fmt.Println(successStyle.Render("\n✓ Starting playback..."))

	// Play with MPV
	if err := player.Play(selectedStream.StreamURL, cfg.MPVPath); err != nil {
		return fmt.Errorf("playback failed: %w", err)
	}

	fmt.Println(successStyle.Render("✓ Playback finished"))
	return nil
}

func runCacheSearch(cmd *cobra.Command, args []string) error {
	searchTitle := strings.Join(args, " ")
	
	// Load config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w. Please run 'goplexcli login' first", err)
	}

	fmt.Println(titleStyle.Render("Searching for: " + searchTitle))

	// Search in cache first
	fmt.Println(infoStyle.Render("\n=== Checking Cache ==="))
	mediaCache, err := cache.Load()
	if err != nil {
		return fmt.Errorf("failed to load cache: %w", err)
	}

	foundInCache := false
	for _, item := range mediaCache.Media {
		if strings.Contains(strings.ToLower(item.Title), strings.ToLower(searchTitle)) {
			foundInCache = true
			fmt.Println(successStyle.Render("✓ Found in cache:"))
			fmt.Printf("  Title: %s\n", item.FormatMediaTitle())
			fmt.Printf("  Type: %s\n", item.Type)
			fmt.Printf("  Key: %s\n", item.Key)
			fmt.Printf("  FilePath: %s\n", item.FilePath)
			fmt.Printf("  RclonePath: %s\n", item.RclonePath)
			fmt.Println()
		}
	}

	if !foundInCache {
		fmt.Println(warningStyle.Render("✗ Not found in cache"))
	}

	// Search in Plex directly
	fmt.Println(infoStyle.Render("=== Checking Plex Server ==="))
	
	client, err := plex.New(cfg.PlexURL, cfg.PlexToken)
	if err != nil {
		return fmt.Errorf("failed to create plex client: %w", err)
	}

	if err := client.Test(); err != nil {
		return fmt.Errorf("failed to connect to plex server: %w", err)
	}

	ctx := context.Background()
	libraries, err := client.GetLibraries(ctx)
	if err != nil {
		return fmt.Errorf("failed to get libraries: %w", err)
	}

	foundInPlex := false
	for _, lib := range libraries {
		if lib.Type != "movie" && lib.Type != "show" {
			continue
		}

		media, err := client.GetMediaFromSection(ctx, lib.Key, lib.Type)
		if err != nil {
			return fmt.Errorf("failed to get media from section %s: %w", lib.Title, err)
		}

		for _, item := range media {
			if strings.Contains(strings.ToLower(item.Title), strings.ToLower(searchTitle)) {
				foundInPlex = true
				fmt.Println(successStyle.Render(fmt.Sprintf("✓ Found in Plex library '%s':", lib.Title)))
				fmt.Printf("  Title: %s\n", item.FormatMediaTitle())
				fmt.Printf("  Type: %s\n", item.Type)
				fmt.Printf("  Year: %d\n", item.Year)
				fmt.Printf("  Key: %s\n", item.Key)
				fmt.Printf("  FilePath: %s\n", item.FilePath)
				fmt.Printf("  RclonePath: %s\n", item.RclonePath)
				
				if item.FilePath == "" {
					fmt.Println(warningStyle.Render("  ⚠ WARNING: No file path found!"))
				}
				fmt.Println()
			}
		}
	}

	if !foundInPlex {
		fmt.Println(warningStyle.Render("✗ Not found in Plex"))
	}

	// Summary
	fmt.Println(infoStyle.Render("=== Summary ==="))
	if foundInCache && foundInPlex {
		fmt.Println(successStyle.Render("✓ Item exists in both cache and Plex"))
	} else if !foundInCache && foundInPlex {
		fmt.Println(warningStyle.Render("⚠ Item exists in Plex but NOT in cache"))
		fmt.Println(infoStyle.Render("  Run 'goplexcli cache reindex' to update the cache"))
	} else if foundInCache && !foundInPlex {
		fmt.Println(warningStyle.Render("⚠ Item exists in cache but NOT in Plex (stale cache)"))
		fmt.Println(infoStyle.Render("  Run 'goplexcli cache reindex' to update the cache"))
	} else {
		fmt.Println(warningStyle.Render("✗ Item not found in either cache or Plex"))
	}

	return nil
}

func runServerList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	fmt.Println(titleStyle.Render("Configured Plex Servers"))

	if len(cfg.Servers) == 0 {
		fmt.Println(warningStyle.Render("No servers configured. Run 'goplexcli login' first."))
		return nil
	}

	for i, server := range cfg.Servers {
		status := warningStyle.Render("disabled")
		if server.Enabled {
			status = successStyle.Render("enabled")
		}
		fmt.Printf("%d. %s - %s [%s]\n", i+1, server.Name, server.URL, status)
	}

	enabledCount := len(cfg.GetEnabledServers())
	fmt.Println(infoStyle.Render(fmt.Sprintf("\n%d of %d servers enabled", enabledCount, len(cfg.Servers))))

	return nil
}

func runServerEnable(cmd *cobra.Command, args []string) error {
	serverName := strings.Join(args, " ")
	
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	found := false
	for i, server := range cfg.Servers {
		if strings.EqualFold(server.Name, serverName) {
			cfg.Servers[i].Enabled = true
			found = true
			fmt.Println(successStyle.Render(fmt.Sprintf("✓ Enabled server '%s'", server.Name)))
			break
		}
	}

	if !found {
		return fmt.Errorf("server '%s' not found", serverName)
	}

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println(infoStyle.Render("Run 'goplexcli cache reindex' to update the cache"))

	return nil
}

func runServerDisable(cmd *cobra.Command, args []string) error {
	serverName := strings.Join(args, " ")
	
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	found := false
	for i, server := range cfg.Servers {
		if strings.EqualFold(server.Name, serverName) {
			cfg.Servers[i].Enabled = false
			found = true
			fmt.Println(successStyle.Render(fmt.Sprintf("✓ Disabled server '%s'", server.Name)))
			break
		}
	}

	if !found {
		return fmt.Errorf("server '%s' not found", serverName)
	}

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println(warningStyle.Render("Note: Cached items from this server will remain until next reindex"))

	return nil
}
