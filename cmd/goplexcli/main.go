package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/joshkerr/goplexcli/internal/cache"
	"github.com/joshkerr/goplexcli/internal/config"
	"github.com/joshkerr/goplexcli/internal/download"
	"github.com/joshkerr/goplexcli/internal/player"
	"github.com/joshkerr/goplexcli/internal/plex"
	"github.com/joshkerr/goplexcli/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

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
		Long: titleStyle.Render("GoplexCLI") + "\n\n" +
			"A powerful command-line interface for interacting with your Plex media server.\n" +
			"Browse, stream, and download your media with ease.",
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

	cacheCmd.AddCommand(cacheUpdateCmd, cacheReindexCmd, cacheInfoCmd)

	// Config command
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Show configuration",
		RunE:  runConfig,
	}

	rootCmd.AddCommand(loginCmd, browseCmd, cacheCmd, configCmd)

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
	
	// Update only Plex-related fields
	cfg.PlexURL = selectedURL
	cfg.PlexToken = token
	cfg.PlexUsername = username

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println(successStyle.Render("✓ Configuration saved"))
	fmt.Println(infoStyle.Render("\nServer URL: " + selectedURL))
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

	// Ask user to select media type using fzf if available
	var mediaType string
	if ui.IsAvailable(cfg.FzfPath) {
		var err error
		mediaType, err = ui.SelectMediaType(cfg.FzfPath)
		if err != nil {
			return fmt.Errorf("media type selection failed: %w", err)
		}
	} else {
		// Fallback to manual selection
		var err error
		mediaType, err = selectMediaTypeManual()
		if err != nil {
			return err
		}
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
		return nil
	}

	fmt.Println(infoStyle.Render(fmt.Sprintf("\nBrowsing %d items...\n", len(filteredMedia))))

	// Use fzf with preview to select media
	selectedIndex, err := ui.SelectMediaWithPreview(filteredMedia, "Select media:", cfg.FzfPath, cfg.PlexURL, cfg.PlexToken)
	if err != nil {
		if err.Error() == "cancelled by user" {
			return nil
		}
		return fmt.Errorf("media selection failed: %w", err)
	}
	
	if selectedIndex < 0 || selectedIndex >= len(filteredMedia) {
		return fmt.Errorf("invalid selection")
	}
	
	selectedMedia := &filteredMedia[selectedIndex]

	// Ask what to do
	action, err := ui.PromptAction(cfg.FzfPath)
	if err != nil {
		if err.Error() == "cancelled by user" {
			return nil
		}
		return err
	}

	switch action {
	case "watch":
		return handleWatch(cfg, selectedMedia)
	case "download":
		return handleDownload(cfg, selectedMedia)
	case "cancel":
		return nil
	default:
		return nil
	}
}

func handleWatch(cfg *config.Config, media *plex.MediaItem) error {
	fmt.Println(infoStyle.Render("\nPreparing to play: " + media.FormatMediaTitle()))

	// Check if MPV is available
	if !player.IsAvailable(cfg.MPVPath) {
		return fmt.Errorf("mpv is not installed. Please install mpv to watch media")
	}

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

	fmt.Println(successStyle.Render("✓ Starting playback..."))

	// Play with MPV
	if err := player.Play(streamURL, cfg.MPVPath); err != nil {
		return fmt.Errorf("playback failed: %w", err)
	}

	fmt.Println(successStyle.Render("✓ Playback finished"))
	return nil
}

func handleDownload(cfg *config.Config, media *plex.MediaItem) error {
	fmt.Println(infoStyle.Render("\nPreparing to download: " + media.FormatMediaTitle()))

	// Check if rclone is available
	if !download.IsAvailable(cfg.RclonePath) {
		return fmt.Errorf("rclone is not installed. Please install rclone to download media")
	}

	if media.RclonePath == "" {
		return fmt.Errorf("no rclone path available for this media")
	}

	fmt.Println(infoStyle.Render("Remote path: " + media.RclonePath))
	fmt.Println(successStyle.Render("✓ Starting download..."))

	// Get current directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Download with rclone
	ctx := context.Background()
	if err := download.Download(ctx, media.RclonePath, cwd, cfg.RclonePath); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	fmt.Println(successStyle.Render("✓ Download complete"))
	return nil
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
	fmt.Println(infoStyle.Render("Connecting to Plex server..."))

	// Create Plex client
	client, err := plex.New(cfg.PlexURL, cfg.PlexToken)
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
	ctx := context.Background()
	totalItems := 0
	
	media, err := client.GetAllMedia(ctx, func(libraryName string, itemCount, totalLibs, currentLib int) {
		totalItems += itemCount
		fmt.Printf("\r%s [%d/%d] %s: %d items (Total: %d)    ",
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
	
	// Count by type
	movieCount := 0
	episodeCount := 0
	for _, item := range media {
		switch item.Type {
		case "movie":
			movieCount++
		case "episode":
			episodeCount++
		}
	}
	
	fmt.Println(infoStyle.Render(fmt.Sprintf("\nTotal items: %d", len(media))))
	fmt.Println(infoStyle.Render(fmt.Sprintf("  Movies: %d", movieCount)))
	fmt.Println(infoStyle.Render(fmt.Sprintf("  Episodes: %d", episodeCount)))

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
