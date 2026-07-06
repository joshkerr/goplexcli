package main

// Must be first: its init fixes os.Args on Termux/Android before pflag's
// package-level CommandLine initializer reads os.Args[0].
// See github.com/termux/termux-packages#29385.
import _ "github.com/joshkerr/goplexcli/internal/termuxfix"

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/joshkerr/goplexcli/internal/cache"
	"github.com/joshkerr/goplexcli/internal/config"
	"github.com/joshkerr/goplexcli/internal/download"
	apperrors "github.com/joshkerr/goplexcli/internal/errors"
	"github.com/joshkerr/goplexcli/internal/logging"
	"github.com/joshkerr/goplexcli/internal/outplayer"
	"github.com/joshkerr/goplexcli/internal/player"
	"github.com/joshkerr/goplexcli/internal/plex"
	"github.com/joshkerr/goplexcli/internal/preview"
	"github.com/joshkerr/goplexcli/internal/progress"
	"github.com/joshkerr/goplexcli/internal/queue"
	"github.com/joshkerr/goplexcli/internal/stream"
	"github.com/joshkerr/goplexcli/internal/ui"
	"github.com/joshkerr/goplexcli/internal/update"
	"github.com/joshkerr/goplexcli/internal/webdav"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// version is set at build time via ldflags: -X main.version=$(VERSION)
// For development without ldflags, falls back to "dev"
var version = "dev"

// dryRun when true shows what would be downloaded without actually downloading
var dryRun bool

// downloadDest overrides the configured download directory for this run.
var downloadDest string

// updateCheckOnly, when true, makes `update` report availability without installing.
var updateCheckOnly bool

// searchDescriptions when true also matches against item summaries
var searchDescriptions bool

// sort command flags
var (
	sortDesc        bool
	sortAsc         bool
	sortLimit       int
	sortType        string
	sortInteractive bool
)

var (
	// Refined color palette for cohesive UI
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#C084FC")). // Purple accent
			MarginBottom(1)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4ADE80")) // Green (matches logo)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F87171")). // Softer red
			Bold(true)

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#60A5FA")) // Softer blue

	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FBBF24")) // Amber
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "goplexcli [search term]",
		Short: "A CLI tool for browsing and streaming from your Plex server",
		Long: `A powerful command-line interface for interacting with your Plex media server.
Browse, stream, and download your media with ease.

Pass a search term to find matching media:
  goplexcli "The Lincoln Lawyer"

Download a batch of items: queue them up while browsing, then run
'goplexcli browse' again — when the queue is non-empty the top of the
media-type picker offers "View Queue (N items)" → "Download All".`,
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return runSearch(cmd, args)
			}
			return runBrowse(cmd, args)
		},
	}
	rootCmd.Flags().BoolVarP(&searchDescriptions, "descriptions", "d", false, "Also search item descriptions/summaries (default: title only)")
	rootCmd.Flags().StringVar(&downloadDest, "dest", "", "Directory to download into (overrides download_dir in config; default: current directory)")

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
		Long: `Browse and play media from your Plex server.

Pick "Movies", "TV Shows", or "All", then drill in to choose what to
watch, download, or queue. Adding items to the queue ("Add to Queue"
in the action menu) lets you batch downloads.

Downloading queued items:
  When the queue is non-empty, 'browse' shows "View Queue (N items)"
  at the top of the media-type picker. Select it, then choose
  "Download All (N items)" to download every queued item back to back.
  The same menu can also transfer the whole queue to WebDAV or an
  Outplayer target, remove individual items, or clear the queue.`,
		RunE: runBrowse,
	}
	browseCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be downloaded without actually downloading")
	browseCmd.Flags().StringVar(&downloadDest, "dest", "", "Directory to download into (overrides download_dir in config; default: current directory)")

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
		Use:               "enable [server-name]",
		Short:             "Enable a server for indexing",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeServerNames,
		RunE:              runServerEnable,
	}

	serverDisableCmd := &cobra.Command{
		Use:               "disable [server-name]",
		Short:             "Disable a server from indexing",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeServerNames,
		RunE:              runServerDisable,
	}

	serverRemoveCmd := &cobra.Command{
		Use:               "remove [server-name]",
		Short:             "Remove a server from the configuration",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeServerNames,
		RunE:              runServerRemove,
	}

	serverCmd.AddCommand(serverListCmd, serverEnableCmd, serverDisableCmd, serverRemoveCmd)

	// WebDAV command: discover gowebdav transfer targets on the LAN and manage
	// the shared credentials used to reach them.
	webdavCmd := &cobra.Command{
		Use:   "webdav",
		Short: "Discover gowebdav servers and manage transfer credentials",
	}

	webdavDiscoverCmd := &cobra.Command{
		Use:   "discover",
		Short: "Scan the LAN for running gowebdav servers",
		RunE:  runWebDAVDiscover,
	}

	webdavSetCredsCmd := &cobra.Command{
		Use:   "set-creds",
		Short: "Set the shared username/password used for all gowebdav servers",
		RunE:  runWebDAVSetCreds,
	}

	webdavCmd.AddCommand(webdavDiscoverCmd, webdavSetCredsCmd)

	// Outplayer command: manage Outplayer "Wi-Fi transfer" targets, which are
	// user-defined HTTP upload destinations (an iOS app feature) rather than
	// LAN-discovered servers.
	outplayerCmd := &cobra.Command{
		Use:   "outplayer",
		Short: "Manage Outplayer Wi-Fi transfer targets",
	}

	outplayerListCmd := &cobra.Command{
		Use:   "list",
		Short: "List configured Outplayer targets",
		RunE:  runOutplayerList,
	}

	outplayerAddCmd := &cobra.Command{
		Use:   "add",
		Short: "Add an Outplayer target",
		RunE:  runOutplayerAdd,
	}

	outplayerRemoveCmd := &cobra.Command{
		Use:               "remove [name]",
		Short:             "Remove an Outplayer target",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeOutplayerNames,
		RunE:              runOutplayerRemove,
	}

	outplayerEnableCmd := &cobra.Command{
		Use:               "enable [name]",
		Short:             "Enable an Outplayer target",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeOutplayerNames,
		RunE:              runOutplayerEnable,
	}

	outplayerDisableCmd := &cobra.Command{
		Use:               "disable [name]",
		Short:             "Disable an Outplayer target (hides it from the transfer menu)",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeOutplayerNames,
		RunE:              runOutplayerDisable,
	}

	outplayerCmd.AddCommand(outplayerListCmd, outplayerAddCmd, outplayerRemoveCmd, outplayerEnableCmd, outplayerDisableCmd)

	// Sort command
	sortCmd := &cobra.Command{
		Use:   "sort [field]",
		Short: "Sort and display media from cache",
		Long: `Sort and display media from your cache by various fields.

Available sort fields:
  name      Sort alphabetically by title
  added     Sort by date added to library
  year      Sort by release year
  rating    Sort by Plex rating
  duration  Sort by media length

Examples:
  goplexcli sort added --desc --limit 20    # Last 20 added items
  goplexcli sort name --asc                 # A-Z by title
  goplexcli sort rating --desc --limit 10   # Top 10 rated
  goplexcli sort year --desc                # Newest releases first`,
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"name", "added", "year", "rating", "duration"},
		RunE:      runSort,
	}
	sortCmd.Flags().BoolVar(&sortDesc, "desc", false, "Sort descending (default for numeric fields)")
	sortCmd.Flags().BoolVar(&sortAsc, "asc", false, "Sort ascending (default for name)")
	sortCmd.Flags().IntVar(&sortLimit, "limit", 20, "Maximum number of items to display")
	sortCmd.Flags().StringVar(&sortType, "type", "all", "Filter by media type: movies, shows, all")
	sortCmd.Flags().BoolVarP(&sortInteractive, "interactive", "i", false, "Open results in interactive browser")

	// Version command
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("goplexcli v%s\n", version)
		},
	}

	// Update command
	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Update goplexcli to the latest release",
		Long: `Update goplexcli to the latest release published on GitHub.

Downloads the release asset matching your platform and replaces the running
binary in place. Use --check to see whether an update is available without
installing it.`,
		RunE: runUpdate,
	}
	updateCmd.Flags().BoolVar(&updateCheckOnly, "check", false, "Only check whether an update is available; don't install")

	// Hidden subcommand invoked by the fzf preview window. Renders one
	// media item's metadata to stdout. Not intended for direct use.
	previewCmd := &cobra.Command{
		Use:    "__preview <data-file> <index>",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return preview.Run(os.Stdout, args[0], args[1])
		},
	}

	rootCmd.AddCommand(loginCmd, browseCmd, cacheCmd, configCmd, streamCmd, serverCmd, webdavCmd, outplayerCmd, sortCmd, versionCmd, updateCmd, previewCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(errorStyle.Render("Error: " + err.Error()))
		os.Exit(1)
	}
}

// recentlyAddedLimit caps how many items the "Recently Added" hub shows.
const recentlyAddedLimit = 50

// buildContinueWatching returns items with resumable playback progress, most
// recently watched first. Progress reflects cache freshness ('cache reindex'
// refreshes it for older items).
func buildContinueWatching(media []plex.MediaItem) []plex.MediaItem {
	var out []plex.MediaItem
	for i := range media {
		if ui.HasResumableProgress(&media[i]) {
			out = append(out, media[i])
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].LastViewedAt > out[j].LastViewedAt
	})
	return out
}

// buildRecentlyAdded returns the most recently added items, newest first,
// capped at limit.
func buildRecentlyAdded(media []plex.MediaItem, limit int) []plex.MediaItem {
	out := make([]plex.MediaItem, len(media))
	copy(out, media)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].AddedAt > out[j].AddedAt
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// toPlexPathMappings converts configured path mappings into the plex package's
// representation used during cache indexing.
func toPlexPathMappings(mappings []config.PathMapping) []plex.PathMapping {
	if len(mappings) == 0 {
		return nil
	}
	out := make([]plex.PathMapping, len(mappings))
	for i, m := range mappings {
		out[i] = plex.PathMapping{Prefix: m.Prefix, Remote: m.Remote}
	}
	return out
}

// completeServerNames provides shell completion for commands that take a
// [server-name] argument. It returns the names of configured servers that
// have not already been given on the command line.
func completeServerNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfg, err := config.Load()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	already := make(map[string]struct{}, len(args))
	for _, a := range args {
		already[a] = struct{}{}
	}

	var names []string
	for _, s := range cfg.Servers {
		if _, dup := already[s.Name]; dup {
			continue
		}
		names = append(names, s.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
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

// selectMediaFlat handles flat media selection (for movies or "all" media type).
// Returns selected media items, whether user cancelled, and any error.
func selectMediaFlat(media []plex.MediaItem, cfg *config.Config, prompt string) ([]*plex.MediaItem, bool, error) {
	var selectedMediaItems []*plex.MediaItem

	if ui.IsAvailable(cfg.FzfPath) {
		selectedIndices, err := ui.SelectMediaWithPreview(media, prompt, cfg.FzfPath, cfg.PlexURL, cfg.PlexToken)
		if err != nil {
			if errors.Is(err, apperrors.ErrCancelled) {
				return nil, true, nil
			}
			return nil, false, fmt.Errorf("media selection failed: %w", err)
		}

		// Build list of selected media items
		for _, index := range selectedIndices {
			if index >= 0 && index < len(media) {
				selectedMediaItems = append(selectedMediaItems, &media[index])
			} else {
				fmt.Fprintf(os.Stderr, "Warning: invalid index %d ignored\n", index)
			}
		}
	} else {
		// Fallback to manual selection (no fzf required)
		selectedMedia, err := selectMediaManual(media)
		if err != nil {
			return nil, false, err
		}
		selectedMediaItems = []*plex.MediaItem{selectedMedia}
	}

	return selectedMediaItems, false, nil
}

func runSearch(cmd *cobra.Command, args []string) error {
	searchTerm := strings.ToLower(strings.Join(args, " "))

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

	// Search across all cached media
	type searchResult struct {
		label       string
		isMovie     bool
		item        *plex.MediaItem // set for movies
		showName    string          // set for TV shows
		previewItem plex.MediaItem  // what to render in the preview pane
	}

	var results []searchResult

	// Find matching movies
	for i := range mediaCache.Media {
		item := &mediaCache.Media[i]
		if item.Type != "movie" {
			continue
		}
		titleMatch := strings.Contains(strings.ToLower(item.Title), searchTerm)
		descMatch := searchDescriptions && !titleMatch && strings.Contains(strings.ToLower(item.Summary), searchTerm)
		if !titleMatch && !descMatch {
			continue
		}
		yearStr := ""
		if item.Year > 0 {
			yearStr = fmt.Sprintf(" (%d)", item.Year)
		}
		label := fmt.Sprintf("%s%s  ·  Movie", item.Title, yearStr)
		if descMatch {
			label += "  ·  matched description"
		}
		results = append(results, searchResult{
			label:       label,
			isMovie:     true,
			item:        item,
			previewItem: *item,
		})
	}

	// Find matching TV shows (deduplicated by show name). titleEpisodeCount
	// is total episodes for a name-matched show; descEpisodeCount is the
	// number of episodes whose summary matched, used only for shows whose
	// name did NOT match. previewEp is a representative episode used to
	// populate the preview pane (Summary, etc.) for the show.
	titleEpisodeCount := make(map[string]int)
	descEpisodeCount := make(map[string]int)
	titlePreviewEp := make(map[string]plex.MediaItem)
	descPreviewEp := make(map[string]plex.MediaItem)
	for _, item := range mediaCache.Media {
		if item.Type != "episode" || item.ParentTitle == "" {
			continue
		}
		if strings.Contains(strings.ToLower(item.ParentTitle), searchTerm) {
			titleEpisodeCount[item.ParentTitle]++
			if _, ok := titlePreviewEp[item.ParentTitle]; !ok {
				titlePreviewEp[item.ParentTitle] = item
			}
			continue
		}
		if searchDescriptions && strings.Contains(strings.ToLower(item.Summary), searchTerm) {
			descEpisodeCount[item.ParentTitle]++
			if _, ok := descPreviewEp[item.ParentTitle]; !ok {
				descPreviewEp[item.ParentTitle] = item
			}
		}
	}
	// Collect & sort show names for deterministic ordering
	showNameSet := make(map[string]struct{}, len(titleEpisodeCount)+len(descEpisodeCount))
	for s := range titleEpisodeCount {
		showNameSet[s] = struct{}{}
	}
	for s := range descEpisodeCount {
		showNameSet[s] = struct{}{}
	}
	showNames := make([]string, 0, len(showNameSet))
	for s := range showNameSet {
		showNames = append(showNames, s)
	}
	sort.Strings(showNames)
	for _, showName := range showNames {
		var label string
		var ep plex.MediaItem
		if count, ok := titleEpisodeCount[showName]; ok {
			label = fmt.Sprintf("%s  ·  TV Show  ·  %d episodes", showName, count)
			ep = titlePreviewEp[showName]
		} else {
			count := descEpisodeCount[showName]
			label = fmt.Sprintf("%s  ·  TV Show  ·  %d episode(s) matched description", showName, count)
			ep = descPreviewEp[showName]
		}
		// Synthesize a show-level preview item: keep show-relevant fields,
		// drop episode-specific ones (Duration, Rating, etc.) so the preview
		// doesn't misrepresent a single episode as the whole show.
		previewItem := plex.MediaItem{
			Title:      showName,
			Type:       "show",
			Summary:    ep.Summary,
			ServerName: ep.ServerName,
		}
		results = append(results, searchResult{
			label:       label,
			isMovie:     false,
			showName:    showName,
			previewItem: previewItem,
		})
	}

	if len(results) == 0 {
		fmt.Println(warningStyle.Render(fmt.Sprintf("No results found for \"%s\".", strings.Join(args, " "))))
		fmt.Println(infoStyle.Render("Try 'goplexcli cache reindex' if your library has been updated recently."))
		return nil
	}

	fmt.Println(infoStyle.Render(fmt.Sprintf("Found %d result(s) for \"%s\"\n", len(results), strings.Join(args, " "))))

	// Build labels for fzf selection
	labels := make([]string, len(results))
	for i, r := range results {
		labels[i] = r.label
	}

	// Load queue for action handling
	q, err := queue.Load()
	if err != nil {
		return fmt.Errorf("failed to load queue: %w", err)
	}

	// Select a result
	var selectedIdx int
	if ui.IsAvailable(cfg.FzfPath) {
		var idx int
		var err error
		if searchDescriptions {
			previewItems := make([]plex.MediaItem, len(results))
			for i, r := range results {
				previewItems[i] = r.previewItem
			}
			idx, err = ui.SelectMediaWithCustomLabels(previewItems, labels, "Select:", cfg.FzfPath, cfg.PlexURL, cfg.PlexToken)
		} else {
			_, idx, err = ui.SelectWithFzf(labels, "Select:", cfg.FzfPath)
		}
		if err != nil {
			if errors.Is(err, apperrors.ErrCancelled) {
				return nil
			}
			return fmt.Errorf("selection failed: %w", err)
		}
		selectedIdx = idx
	} else {
		fmt.Println(infoStyle.Render("Results:"))
		for i, label := range labels {
			fmt.Printf("  %d. %s\n", i+1, label)
		}
		fmt.Printf("\nSelect (1-%d): ", len(labels))
		var choice int
		if _, err := fmt.Scanln(&choice); err != nil {
			return fmt.Errorf("failed to read selection: %w", err)
		}
		if choice < 1 || choice > len(labels) {
			return fmt.Errorf("invalid selection")
		}
		selectedIdx = choice - 1
	}

	selected := results[selectedIdx]

	if selected.isMovie {
		// Movie: go straight to action
		selectedMediaItems := []*plex.MediaItem{selected.item}
		err = handleMediaAction(cfg, q, selectedMediaItems)
		if err != nil && !errors.Is(err, errAddedToQueue) {
			return err
		}
		return nil
	}

	// TV Show: drill into season/episode selection using full cache
	var allEpisodes []plex.MediaItem
	for _, item := range mediaCache.Media {
		if item.Type == "episode" {
			allEpisodes = append(allEpisodes, item)
		}
	}

	seasons := ui.GetSeasonsForShow(allEpisodes, selected.showName)
	if len(seasons) == 0 {
		fmt.Println(warningStyle.Render("No seasons found for this show."))
		return nil
	}

	fmt.Println(infoStyle.Render(fmt.Sprintf("\n%s has %d seasons...\n", selected.showName, len(seasons))))

	selectedSeason, err := ui.SelectSeason(seasons, selected.showName, cfg.FzfPath)
	if err != nil {
		if errors.Is(err, apperrors.ErrCancelled) {
			return nil
		}
		return fmt.Errorf("season selection failed: %w", err)
	}

	episodesInSeason := ui.GetEpisodesForSeason(allEpisodes, selected.showName, selectedSeason)
	if len(episodesInSeason) == 0 {
		fmt.Println(warningStyle.Render("No episodes found for this season."))
		return nil
	}

	seasonLabel := fmt.Sprintf("Season %d", selectedSeason)
	if selectedSeason == 0 {
		seasonLabel = "Specials"
	}
	fmt.Println(infoStyle.Render(fmt.Sprintf("\n%s has %d episodes...\n", seasonLabel, len(episodesInSeason))))

	selectedMediaItems, cancelled, err := selectMediaFlat(episodesInSeason, cfg, "Select episode(s) (TAB for multi-select):")
	if err != nil {
		return err
	}
	if cancelled {
		return nil
	}

	if len(selectedMediaItems) == 0 {
		return nil
	}

	err = handleMediaAction(cfg, q, selectedMediaItems)
	if err != nil && !errors.Is(err, errAddedToQueue) {
		return err
	}
	return nil
}

func runBrowse(cmd *cobra.Command, args []string) error {
	// Show logo for interactive browse command
	ui.Logo(version)

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

	// Load persistent queue
	q, err := queue.Load()
	if err != nil {
		return fmt.Errorf("failed to load queue: %w", err)
	}

	if q.Len() > 0 {
		fmt.Println(infoStyle.Render(fmt.Sprintf("Queue has %s from previous session", ui.PluralizeItems(q.Len()))))
	}

	// Count items with resumable progress to decide whether to offer the
	// "Continue Watching" hub. This reflects the cache's freshness; run
	// 'cache reindex' to refresh progress on older items.
	continueCount := 0
	for i := range mediaCache.Media {
		if ui.HasResumableProgress(&mediaCache.Media[i]) {
			continueCount++
		}
	}

browseLoop:
	for {
		// Ask user to select media type using fzf if available
		var mediaType string
		if ui.IsAvailable(cfg.FzfPath) {
			var err error
			mediaType, err = ui.SelectMediaTypeWithQueue(cfg.FzfPath, q.Len(), continueCount)
			if err != nil {
				if errors.Is(err, apperrors.ErrCancelled) {
					return nil
				}
				return fmt.Errorf("media type selection failed: %w", err)
			}
		} else {
			// Fallback to manual selection
			var err error
			mediaType, err = selectMediaTypeManualWithQueue(q.Len(), continueCount)
			if err != nil {
				return err
			}
		}

		// Handle queue view
		if mediaType == "queue" {
			result, err := handleQueueView(cfg, q)
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
		case "continue watching":
			filteredMedia = buildContinueWatching(mediaCache.Media)
		case "recently added movies":
			var movies []plex.MediaItem
			for _, item := range mediaCache.Media {
				if item.Type == "movie" {
					movies = append(movies, item)
				}
			}
			filteredMedia = buildRecentlyAdded(movies, recentlyAddedLimit)
		case "recently added tv shows":
			// Keep every episode so the show -> season -> episode drill-down
			// below can resolve seasons and episodes; the recency limit is
			// applied to the show list itself, not the episode pool.
			for _, item := range mediaCache.Media {
				if item.Type == "episode" {
					filteredMedia = append(filteredMedia, item)
				}
			}
		default:
			filteredMedia = mediaCache.Media
		}

		if len(filteredMedia) == 0 {
			fmt.Println(warningStyle.Render("No media found for selected type."))
			continue browseLoop
		}

		// For TV shows, use hierarchical drill-down: Show -> Season -> Episode
		var selectedMediaItems []*plex.MediaItem
		isTVDrillDown := mediaType == "tv shows" || mediaType == "recently added tv shows"
		if isTVDrillDown && ui.IsAvailable(cfg.FzfPath) {
			// Step 1: Select TV show. "Recently Added TV Shows" orders the top
			// level shows by how recently each was updated; "TV Shows" lists
			// them alphabetically.
			var shows []string
			if mediaType == "recently added tv shows" {
				shows = ui.GetRecentlyAddedTVShows(filteredMedia, recentlyAddedLimit)
			} else {
				shows = ui.GetUniqueTVShows(filteredMedia)
			}
			if len(shows) == 0 {
				fmt.Println(warningStyle.Render("No TV shows found."))
				continue browseLoop
			}

			fmt.Println(infoStyle.Render(fmt.Sprintf("\nFound %d TV shows...\n", len(shows))))

			selectedShow, err := ui.SelectTVShow(shows, cfg.FzfPath)
			if err != nil {
				if errors.Is(err, apperrors.ErrCancelled) {
					continue browseLoop
				}
				return fmt.Errorf("show selection failed: %w", err)
			}

			// Step 2: Select season
			seasons := ui.GetSeasonsForShow(filteredMedia, selectedShow)
			if len(seasons) == 0 {
				fmt.Println(warningStyle.Render("No seasons found for this show."))
				continue browseLoop
			}

			fmt.Println(infoStyle.Render(fmt.Sprintf("\n%s has %d seasons...\n", selectedShow, len(seasons))))

			selectedSeason, err := ui.SelectSeason(seasons, selectedShow, cfg.FzfPath)
			if err != nil {
				if errors.Is(err, apperrors.ErrCancelled) {
					continue browseLoop
				}
				return fmt.Errorf("season selection failed: %w", err)
			}

			// Step 3: Select episodes from that season
			episodesInSeason := ui.GetEpisodesForSeason(filteredMedia, selectedShow, selectedSeason)
			if len(episodesInSeason) == 0 {
				fmt.Println(warningStyle.Render("No episodes found for this season."))
				continue browseLoop
			}

			seasonLabel := fmt.Sprintf("Season %d", selectedSeason)
			if selectedSeason == 0 {
				seasonLabel = "Specials"
			}
			fmt.Println(infoStyle.Render(fmt.Sprintf("\n%s has %d episodes...\n", seasonLabel, len(episodesInSeason))))

			var cancelled bool
			selectedMediaItems, cancelled, err = selectMediaFlat(episodesInSeason, cfg, "Select episode(s) (TAB for multi-select):")
			if err != nil {
				return err
			}
			if cancelled {
				continue browseLoop
			}
		} else {
			// For movies or "all", use flat selection
			fmt.Println(infoStyle.Render(fmt.Sprintf("\nBrowsing %d items...\n", len(filteredMedia))))

			var cancelled bool
			var err error
			selectedMediaItems, cancelled, err = selectMediaFlat(filteredMedia, cfg, "Select media (TAB for multi-select):")
			if err != nil {
				return err
			}
			if cancelled {
				continue browseLoop
			}
		}

		if len(selectedMediaItems) == 0 {
			return fmt.Errorf("no media selected")
		}

	// Handle user action
	err = handleMediaAction(cfg, q, selectedMediaItems)
	if err != nil {
		if errors.Is(err, errAddedToQueue) {
			// Items were added to queue, continue browsing
			continue browseLoop
		}
		return err
	}
	// Action completed successfully, continue browsing
	continue browseLoop
	}
}

// errAddedToQueue is a sentinel error to signal that items were added to the queue
var errAddedToQueue = errors.New("items added to queue")

// handleMediaAction prompts the user for an action and dispatches to the appropriate handler.
// Returns errAddedToQueue if items were added to the queue (caller decides whether to continue or return).
// Returns nil for actions that complete successfully.
// Returns other errors for failures.
func handleMediaAction(cfg *config.Config, q *queue.Queue, selectedMediaItems []*plex.MediaItem) error {
	// Ask what to do. "Transfer to Outplayer" is only offered when at least one
	// Outplayer target is enabled (disabling all targets hides the action).
	outplayerCount := len(cfg.GetEnabledOutplayerTargets())
	var action string
	var err error
	if ui.IsAvailable(cfg.FzfPath) {
		action, err = ui.PromptActionWithQueue(cfg.FzfPath, len(selectedMediaItems), q.Len(), outplayerCount)
		if err != nil {
			if errors.Is(err, apperrors.ErrCancelled) {
				return nil
			}
			return err
		}
	} else {
		action, err = promptActionManualWithQueue(len(selectedMediaItems), q.Len(), outplayerCount)
		if err != nil {
			return err
		}
	}

	// "More..." opens a submenu with the less-common playback/streaming options.
	if action == "more" {
		if ui.IsAvailable(cfg.FzfPath) {
			action, err = ui.PromptMoreAction(cfg.FzfPath)
			if err != nil {
				if errors.Is(err, apperrors.ErrCancelled) {
					return nil
				}
				return err
			}
		} else {
			action, err = promptMoreActionManual()
			if err != nil {
				return err
			}
		}
	}

	switch action {
	case "watch":
		return handleWatchMultiple(cfg, selectedMediaItems)
	case "download":
		return handleDownloadMultiple(cfg, selectedMediaItems)
	case "transfer":
		return handleTransferToWebDAV(cfg, selectedMediaItems)
	case "transfer-outplayer":
		return handleTransferToOutplayer(cfg, selectedMediaItems)
	case "senplayer play":
		return handleSenPlayer(cfg, selectedMediaItems, "play")
	case "senplayer download":
		return handleSenPlayer(cfg, selectedMediaItems, "download")
	case "queue":
		// Safety check: confirm if adding many items (likely accidental multi-select)
		if len(selectedMediaItems) > 3 {
			fmt.Printf("You selected %d items. Add all to queue? [y/N]: ", len(selectedMediaItems))
			var confirm string
			// Ignore the error: empty input / EOF leaves confirm == "", which is
			// treated as "no" below.
			_, _ = fmt.Scanln(&confirm)
			if confirm != "y" && confirm != "Y" {
				fmt.Println(warningStyle.Render("Queue add cancelled."))
				return nil
			}
		}
		added := q.Add(selectedMediaItems)
		if err := q.Save(); err != nil {
			return fmt.Errorf("failed to save queue: %w", err)
		}
		skipped := len(selectedMediaItems) - added
		if skipped > 0 {
			fmt.Println(successStyle.Render(fmt.Sprintf("Added %d item(s) to queue (%d duplicate(s) skipped). Queue now has %s.", added, skipped, ui.PluralizeItems(q.Len()))))
		} else {
			fmt.Println(successStyle.Render(fmt.Sprintf("Added %d item(s) to queue. Queue now has %s.", added, ui.PluralizeItems(q.Len()))))
		}
		return errAddedToQueue
	case "stream":
		if len(selectedMediaItems) > 1 {
			fmt.Println(warningStyle.Render("Note: Stream only supports single selection, using first item"))
		}
		return handleStream(cfg, selectedMediaItems[0])
	default:
		return nil
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

	// Check for items with progress
	var itemsWithProgress []*plex.MediaItem
	for _, media := range mediaItems {
		if ui.HasResumableProgress(media) {
			itemsWithProgress = append(itemsWithProgress, media)
		}
	}

	// Determine start positions based on user choice
	startPositions := make([]int, len(mediaItems))
	if len(itemsWithProgress) > 0 {
		if len(itemsWithProgress) == 1 && len(mediaItems) == 1 {
			// Single item with progress - show simple resume prompt
			choice, err := ui.PromptResume(ui.ResumePromptOptions{
				Title:      mediaItems[0].FormatMediaTitle(),
				ViewOffset: mediaItems[0].ViewOffset,
				Duration:   mediaItems[0].Duration,
				FzfPath:    cfg.FzfPath,
			})
			if err != nil {
				if errors.Is(err, apperrors.ErrCancelled) {
					return nil
				}
				// On error, default to start from beginning
				fmt.Println(warningStyle.Render("Resume prompt failed, starting from beginning"))
			} else if choice == ui.ResumeFromPosition {
				// Convert milliseconds to seconds for MPV
				startPositions[0] = mediaItems[0].ViewOffset / 1000
			}
		} else {
			// Multiple items or multiple items with progress - show multi-resume prompt
			choice, err := ui.PromptMultiResume(len(itemsWithProgress), len(mediaItems), cfg.FzfPath)
			if err != nil {
				if errors.Is(err, apperrors.ErrCancelled) {
					return nil
				}
				// On error, default to start from beginning
				fmt.Println(warningStyle.Render("Resume prompt failed, starting all from beginning"))
			} else {
				switch choice {
				case ui.ResumeAll:
					// Set start positions for all items with progress
					for i, media := range mediaItems {
						if ui.HasResumableProgress(media) {
							startPositions[i] = media.ViewOffset / 1000
						}
					}
				case ui.ChooseIndividually:
					// Prompt for each item with progress
					for i, media := range mediaItems {
						if ui.HasResumableProgress(media) {
							itemChoice, err := ui.PromptResume(ui.ResumePromptOptions{
								Title:      media.FormatMediaTitle(),
								ViewOffset: media.ViewOffset,
								Duration:   media.Duration,
								FzfPath:    cfg.FzfPath,
							})
							if err != nil {
								if errors.Is(err, apperrors.ErrCancelled) {
									return nil
								}
								// On error, start this item from beginning
								continue
							}
							if itemChoice == ui.ResumeFromPosition {
								startPositions[i] = media.ViewOffset / 1000
							}
						}
					}
					// case ui.StartAllFromBeginning: all positions remain 0
				}
			}
		}
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

	// Set up progress tracking using Unix socket (macOS/Linux) or named pipe (Windows)
	socketPath := progress.GenerateIPCPath()
	mpvClient := progress.NewMPVClient(socketPath)
	tracker := progress.NewTracker(mediaItems, mpvClient, client)

	// Clean up socket file when done (Unix only, no-op on Windows)
	defer os.Remove(socketPath)

	// Prepare playback options
	// Note: MPV's --start flag only applies to the first file in a playlist.
	// For multi-item playlists, only the first item resumes from saved position;
	// subsequent items start from the beginning.
	startPos := 0
	if len(mediaItems) == 1 && len(startPositions) > 0 {
		startPos = startPositions[0]
	}

	// Warn user about playlist resume limitation if multiple items have progress
	if len(mediaItems) > 1 && len(startPositions) > 0 {
		progressCount := 0
		for _, pos := range startPositions {
			if pos > 0 {
				progressCount++
			}
		}
		if progressCount > 1 {
			fmt.Println(warningStyle.Render("Note: Resume position only applies to first item in playlist"))
		}
	}

	opts := player.PlaybackOptions{
		SocketPath: socketPath,
		StartPos:   startPos,
	}

	fmt.Println(successStyle.Render(fmt.Sprintf("✓ Starting playback of %d items...", len(mediaItems))))
	fmt.Println(infoStyle.Render("Use 'n' in MPV to skip to next item"))

	// Create context that cancels when MPV exits
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start MPV in goroutine
	errCh := make(chan error, 1)
	go func() {
		err := player.PlayMultipleWithOptions(streamURLs, cfg.MPVPath, opts)
		cancel() // Cancel context when MPV exits (stops Connect retries)
		errCh <- err
	}()

	// Connect to MPV IPC and start tracking (with context for early cancellation)
	tracking := false
	if err := mpvClient.ConnectWithContext(ctx); err != nil {
		// Only show warning if it wasn't due to MPV exiting
		if ctx.Err() == nil {
			fmt.Println(warningStyle.Render(fmt.Sprintf("Note: Progress tracking unavailable: %v", err)))
		}
	} else {
		defer func() { _ = mpvClient.Close() }()
		tracker.Start(ctx, 10*time.Second)
		tracking = true
	}

	// Wait for playback to finish
	playbackErr := <-errCh

	// Stop tracking and flush the final position into the local cache so the
	// just-watched item appears in "Continue Watching" immediately, without
	// waiting for a 'cache reindex'.
	if tracking {
		tracker.Stop()
		persistPlaybackProgress(tracker)
	}

	if playbackErr != nil {
		return fmt.Errorf("playback failed: %w", playbackErr)
	}

	fmt.Println(successStyle.Render("✓ Playback finished"))
	return nil
}

// persistPlaybackProgress writes the playback positions captured during this
// session back into the local cache, keyed by media key. This makes
// freshly-watched items appear in the "Continue Watching" hub immediately,
// rather than only after a 'cache reindex'. Best-effort: cache write failures
// are logged but do not fail playback.
func persistPlaybackProgress(tracker *progress.Tracker) {
	offsets := tracker.Progress()
	if len(offsets) == 0 {
		return
	}

	mediaCache, err := cache.Load()
	if err != nil {
		logging.Warn("failed to load cache to persist playback progress", "error", err)
		return
	}

	if !mediaCache.ApplyOffsets(offsets) {
		return
	}

	if err := mediaCache.Save(); err != nil {
		logging.Warn("failed to persist playback progress to cache", "error", err)
	}
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

	// Resolve destination directory (--dest flag > config download_dir > cwd)
	destDir, err := cfg.ResolveDownloadDir(downloadDest)
	if err != nil {
		return fmt.Errorf("failed to resolve download directory: %w", err)
	}

	// Handle dry-run mode
	if dryRun {
		fmt.Println(warningStyle.Render("\n[DRY RUN] Would download the following files:"))
		for _, path := range rclonePaths {
			fmt.Println(infoStyle.Render(fmt.Sprintf("  - %s", path)))
		}
		fmt.Println(warningStyle.Render(fmt.Sprintf("\n[DRY RUN] Total: %d files to %s", len(rclonePaths), destDir)))
		return nil
	}

	// Ensure the destination directory exists
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create download directory %q: %w", destDir, err)
	}

	fmt.Println(successStyle.Render(fmt.Sprintf("\n✓ Starting download of %d items to %s...", len(rclonePaths), destDir)))

	// Download with rclone
	ctx := context.Background()
	if err := download.DownloadMultiple(ctx, rclonePaths, destDir, cfg.RclonePath); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	fmt.Println(successStyle.Render("✓ All downloads complete"))
	return nil
}

// handleTransferToWebDAV discovers gowebdav servers on the LAN via mDNS, lets
// the user pick one, and pushes the selected media to it using rclone's WebDAV
// backend. Credentials are the shared ones stored in config (WebDAVUser/Pass).
func handleTransferToWebDAV(cfg *config.Config, mediaItems []*plex.MediaItem) error {
	if len(mediaItems) == 0 {
		return fmt.Errorf("no media items provided")
	}

	// rclone is used for the actual transfer (same requirement as downloads).
	if !download.IsAvailable(cfg.RclonePath) {
		return fmt.Errorf("rclone is not installed. Please install rclone to transfer media")
	}

	// Collect rclone source paths and validate (mirrors handleDownloadMultiple).
	var rclonePaths []string
	for _, media := range mediaItems {
		if media.RclonePath == "" {
			fmt.Println(warningStyle.Render(fmt.Sprintf("⚠ Skipping %s (no rclone path)", media.FormatMediaTitle())))
			continue
		}
		rclonePaths = append(rclonePaths, media.RclonePath)
	}
	if len(rclonePaths) == 0 {
		return fmt.Errorf("no valid rclone paths available")
	}

	// Discover gowebdav servers advertised on the LAN.
	fmt.Println(infoStyle.Render("\nSearching for gowebdav servers on the local network..."))
	ctx := context.Background()
	targets, err := webdav.Discover(ctx, 3*time.Second)
	if err != nil {
		return fmt.Errorf("discovery failed: %w", err)
	}
	if len(targets) == 0 {
		fmt.Println(warningStyle.Render("No gowebdav servers found on the network"))
		fmt.Println(infoStyle.Render("\nTo run a gowebdav server, on another machine run:"))
		fmt.Println(infoStyle.Render("  gowebdav -name <label> -username <user> -password <pass>"))
		fmt.Println(infoStyle.Render("(use the same credentials you set in goplexcli config)"))
		return nil
	}

	fmt.Println(successStyle.Render(fmt.Sprintf("✓ Found %d server(s)\n", len(targets))))

	// Select a target if more than one was found.
	var selected *webdav.Target
	if len(targets) == 1 {
		selected = targets[0]
		fmt.Println(infoStyle.Render(fmt.Sprintf("Transferring to: %s (%s)", selected.Name, selected.BaseURL())))
	} else {
		var names []string
		for _, t := range targets {
			names = append(names, fmt.Sprintf("%s (%s)", t.Name, t.BaseURL()))
		}
		if ui.IsAvailable(cfg.FzfPath) {
			_, idx, err := ui.SelectWithFzf(names, "Select gowebdav server:", cfg.FzfPath)
			if err != nil {
				if errors.Is(err, apperrors.ErrCancelled) {
					return nil
				}
				return fmt.Errorf("server selection failed: %w", err)
			}
			selected = targets[idx]
		} else {
			fmt.Println(infoStyle.Render("Available servers:"))
			for i, name := range names {
				fmt.Printf("  %d. %s\n", i+1, name)
			}
			fmt.Print("\nSelect server number: ")
			var choice int
			if _, err := fmt.Scanln(&choice); err != nil {
				return fmt.Errorf("failed to read selection: %w", err)
			}
			if choice < 1 || choice > len(targets) {
				return fmt.Errorf("invalid selection")
			}
			selected = targets[choice-1]
		}
	}

	baseURL := selected.BaseURL()
	if baseURL == "" {
		return fmt.Errorf("selected server %q has no reachable address", selected.Name)
	}

	if dryRun {
		fmt.Println(warningStyle.Render(fmt.Sprintf("\n[DRY RUN] Would transfer %d file(s) to %s:", len(rclonePaths), baseURL)))
		for _, p := range rclonePaths {
			fmt.Println(infoStyle.Render("  - " + p))
		}
		return nil
	}

	if cfg.WebDAVUser == "" && cfg.WebDAVPass == "" {
		fmt.Println(warningStyle.Render("Note: no webdav_user/webdav_pass set in config; connecting anonymously."))
	}

	fmt.Println(successStyle.Render(fmt.Sprintf("\n✓ Starting transfer of %d item(s) to %s...", len(rclonePaths), baseURL)))
	if err := download.UploadToWebDAV(ctx, rclonePaths, baseURL, cfg.WebDAVUser, cfg.WebDAVPass, cfg.WebDAVDir, cfg.RclonePath); err != nil {
		return fmt.Errorf("transfer failed: %w", err)
	}

	fmt.Println(successStyle.Render("✓ All transfers complete"))
	return nil
}

// runWebDAVDiscover scans the LAN for gowebdav servers and prints them.
func runWebDAVDiscover(cmd *cobra.Command, args []string) error {
	fmt.Println(titleStyle.Render("gowebdav Discovery"))
	fmt.Println(infoStyle.Render("Searching for gowebdav servers on the local network...\n"))

	ctx := context.Background()
	targets, err := webdav.Discover(ctx, 3*time.Second)
	if err != nil {
		return fmt.Errorf("discovery failed: %w", err)
	}

	if len(targets) == 0 {
		fmt.Println(warningStyle.Render("No gowebdav servers found on the network"))
		fmt.Println(infoStyle.Render("\nStart one on another machine with:"))
		fmt.Println(infoStyle.Render("  gowebdav -name <label> -username <user> -password <pass>"))
		return nil
	}

	fmt.Println(successStyle.Render(fmt.Sprintf("✓ Found %d server(s):\n", len(targets))))
	for _, t := range targets {
		fmt.Printf("  %-20s %s\n", t.Name, t.BaseURL())
	}
	return nil
}

// runWebDAVSetCreds prompts for and saves the shared gowebdav credentials.
func runWebDAVSetCreds(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	fmt.Println(titleStyle.Render("gowebdav Credentials"))
	fmt.Println(infoStyle.Render("These are shared across every gowebdav server on your LAN.\n"))

	fmt.Print("Username: ")
	var username string
	// An empty username (just Enter) is allowed for anonymous servers; ignore
	// the EOF/empty-input error and treat it as blank.
	_, _ = fmt.Scanln(&username)

	fmt.Print("Password: ")
	passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return fmt.Errorf("failed to read password: %w", err)
	}
	fmt.Println()

	fmt.Print("Upload sub-directory (optional, blank = server root): ")
	var dir string
	_, _ = fmt.Scanln(&dir)

	cfg.WebDAVUser = strings.TrimSpace(username)
	cfg.WebDAVPass = string(passwordBytes)
	cfg.WebDAVDir = strings.TrimSpace(dir)

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println(successStyle.Render("✓ Saved gowebdav credentials"))
	return nil
}

// findOutplayerTarget returns the index of the target whose name matches (case
// insensitively), or -1 if none match.
func findOutplayerTarget(cfg *config.Config, name string) int {
	for i, t := range cfg.OutplayerTargets {
		if strings.EqualFold(t.Name, name) {
			return i
		}
	}
	return -1
}

// completeOutplayerNames provides shell completion of configured target names.
func completeOutplayerNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfg, err := config.Load()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	already := make(map[string]struct{}, len(args))
	for _, a := range args {
		already[a] = struct{}{}
	}
	var names []string
	for _, t := range cfg.OutplayerTargets {
		if _, dup := already[t.Name]; dup {
			continue
		}
		names = append(names, t.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// runOutplayerList prints all configured Outplayer targets and their status.
func runOutplayerList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	fmt.Println(titleStyle.Render("Outplayer Targets"))
	if len(cfg.OutplayerTargets) == 0 {
		fmt.Println(warningStyle.Render("No Outplayer targets configured."))
		fmt.Println(infoStyle.Render("Add one with: goplexcli outplayer add"))
		return nil
	}

	for _, t := range cfg.OutplayerTargets {
		dir := t.Dir
		if dir == "" {
			dir = "/"
		}
		status := successStyle.Render("enabled")
		if !t.Enabled {
			status = warningStyle.Render("disabled")
		}
		fmt.Printf("  %-20s %-28s dir=%-12s %s\n", t.Name, t.URL, dir, status)
	}
	return nil
}

// runOutplayerAdd interactively adds a new Outplayer target to the config.
func runOutplayerAdd(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Println(titleStyle.Render("Add Outplayer Target"))
	fmt.Println(infoStyle.Render("In Outplayer, enable Wi-Fi transfer to see the address to use.\n"))

	fmt.Print("Name (e.g. iPhone): ")
	name, _ := reader.ReadString('\n')
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if findOutplayerTarget(cfg, name) != -1 {
		return fmt.Errorf("a target named %q already exists", name)
	}

	fmt.Print("URL (e.g. http://192.168.0.34): ")
	rawURL, _ := reader.ReadString('\n')
	rawURL = strings.TrimSpace(rawURL)
	// Default to http:// when the user omits the scheme.
	if rawURL != "" && !strings.Contains(rawURL, "://") {
		rawURL = "http://" + rawURL
	}
	rawURL = strings.TrimRight(rawURL, "/")

	fmt.Print("Upload folder (optional, blank = root): ")
	dir, _ := reader.ReadString('\n')
	dir = strings.TrimSpace(dir)

	target := config.OutplayerTarget{
		Name:    name,
		URL:     rawURL,
		Dir:     dir,
		Enabled: true,
	}
	if err := target.Validate(); err != nil {
		return fmt.Errorf("invalid target: %w", err)
	}

	// Best-effort connectivity check; a target can still be saved while offline.
	fmt.Println(infoStyle.Render("\nChecking connectivity..."))
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := outplayer.Reachable(ctx, target.URL); err != nil {
		fmt.Println(warningStyle.Render(fmt.Sprintf("⚠ Could not reach target: %v", err)))
		fmt.Println(infoStyle.Render("Saved anyway. Enable Wi-Fi transfer in Outplayer before uploading."))
	} else {
		fmt.Println(successStyle.Render("✓ Target is reachable"))
	}

	cfg.OutplayerTargets = append(cfg.OutplayerTargets, target)
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	fmt.Println(successStyle.Render(fmt.Sprintf("✓ Added Outplayer target %q", name)))
	return nil
}

// runOutplayerRemove deletes a target from the config by name.
func runOutplayerRemove(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	name := args[0]
	idx := findOutplayerTarget(cfg, name)
	if idx == -1 {
		return fmt.Errorf("no Outplayer target named %q", name)
	}
	removed := cfg.OutplayerTargets[idx].Name
	cfg.OutplayerTargets = append(cfg.OutplayerTargets[:idx], cfg.OutplayerTargets[idx+1:]...)
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	fmt.Println(successStyle.Render(fmt.Sprintf("✓ Removed Outplayer target %q", removed)))
	return nil
}

// runOutplayerEnable enables a target so it appears in the transfer menu.
func runOutplayerEnable(cmd *cobra.Command, args []string) error {
	return setOutplayerEnabled(args[0], true)
}

// runOutplayerDisable disables a target so it is hidden from the transfer menu.
func runOutplayerDisable(cmd *cobra.Command, args []string) error {
	return setOutplayerEnabled(args[0], false)
}

// setOutplayerEnabled toggles the Enabled flag of a target by name and saves.
func setOutplayerEnabled(name string, enabled bool) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	idx := findOutplayerTarget(cfg, name)
	if idx == -1 {
		return fmt.Errorf("no Outplayer target named %q", name)
	}
	cfg.OutplayerTargets[idx].Enabled = enabled
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	state := "enabled"
	if !enabled {
		state = "disabled"
	}
	fmt.Println(successStyle.Render(fmt.Sprintf("✓ %s Outplayer target %q", state, cfg.OutplayerTargets[idx].Name)))
	return nil
}

// handleTransferToOutplayer uploads the selected media to a user-configured
// Outplayer Wi-Fi transfer target. The media source is streamed from its rclone
// remote directly into Outplayer's HTTP uploader (see internal/outplayer).
func handleTransferToOutplayer(cfg *config.Config, mediaItems []*plex.MediaItem) error {
	if len(mediaItems) == 0 {
		return fmt.Errorf("no media items provided")
	}

	// rclone provides the source bytes (same requirement as downloads).
	if !download.IsAvailable(cfg.RclonePath) {
		return fmt.Errorf("rclone is not installed. Please install rclone to transfer media")
	}

	// Collect rclone source paths and validate (mirrors handleTransferToWebDAV).
	var rclonePaths []string
	for _, media := range mediaItems {
		if media.RclonePath == "" {
			fmt.Println(warningStyle.Render(fmt.Sprintf("⚠ Skipping %s (no rclone path)", media.FormatMediaTitle())))
			continue
		}
		rclonePaths = append(rclonePaths, media.RclonePath)
	}
	if len(rclonePaths) == 0 {
		return fmt.Errorf("no valid rclone paths available")
	}

	targets := cfg.GetEnabledOutplayerTargets()
	if len(targets) == 0 {
		fmt.Println(warningStyle.Render("No enabled Outplayer targets."))
		fmt.Println(infoStyle.Render("Add one with: goplexcli outplayer add"))
		return nil
	}

	// Select a target if more than one is enabled.
	var selected config.OutplayerTarget
	if len(targets) == 1 {
		selected = targets[0]
		fmt.Println(infoStyle.Render(fmt.Sprintf("Uploading to: %s (%s)", selected.Name, selected.URL)))
	} else {
		names := make([]string, len(targets))
		for i, t := range targets {
			names[i] = fmt.Sprintf("%s (%s)", t.Name, t.URL)
		}
		if ui.IsAvailable(cfg.FzfPath) {
			_, idx, err := ui.SelectWithFzf(names, "Select Outplayer target:", cfg.FzfPath)
			if err != nil {
				if errors.Is(err, apperrors.ErrCancelled) {
					return nil
				}
				return fmt.Errorf("target selection failed: %w", err)
			}
			selected = targets[idx]
		} else {
			fmt.Println(infoStyle.Render("Available targets:"))
			for i, n := range names {
				fmt.Printf("  %d. %s\n", i+1, n)
			}
			fmt.Print("\nSelect target number: ")
			var choice int
			if _, err := fmt.Scanln(&choice); err != nil {
				return fmt.Errorf("failed to read selection: %w", err)
			}
			if choice < 1 || choice > len(targets) {
				return fmt.Errorf("invalid selection")
			}
			selected = targets[choice-1]
		}
	}

	if dryRun {
		fmt.Println(warningStyle.Render(fmt.Sprintf("\n[DRY RUN] Would upload %d file(s) to %s (%s):", len(rclonePaths), selected.Name, selected.URL)))
		for _, p := range rclonePaths {
			fmt.Println(infoStyle.Render("  - " + p))
		}
		return nil
	}

	ctx := context.Background()

	// Fail fast if the target is unreachable (e.g. Wi-Fi transfer is off).
	checkCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	err := outplayer.Reachable(checkCtx, selected.URL)
	cancel()
	if err != nil {
		return fmt.Errorf("cannot reach %s (%s): %w\nMake sure Outplayer's Wi-Fi transfer is enabled and on the same network", selected.Name, selected.URL, err)
	}

	fmt.Println(successStyle.Render(fmt.Sprintf("\n✓ Uploading %d item(s) to %s...", len(rclonePaths), selected.Name)))
	if err := outplayer.Upload(ctx, rclonePaths, selected.URL, selected.Dir, cfg.RclonePath); err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}

	fmt.Println(successStyle.Render("✓ All uploads complete"))
	return nil
}

func handleSenPlayer(cfg *config.Config, mediaItems []*plex.MediaItem, mode string) error {
	if len(mediaItems) == 0 {
		return fmt.Errorf("no media items provided")
	}

	// SenPlayer only supports one item at a time
	if len(mediaItems) > 1 {
		fmt.Println(warningStyle.Render("Note: SenPlayer only supports single selection, using first item"))
	}

	media := mediaItems[0]
	actionText := "Playing"
	if mode == "download" {
		actionText = "Downloading"
	}
	fmt.Println(infoStyle.Render(fmt.Sprintf("\nPreparing for SenPlayer (%s): %s", actionText, media.FormatMediaTitle())))

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

	// Build filename from media title
	filename := media.Title
	if media.Year > 0 {
		filename = fmt.Sprintf("%s (%d)", media.Title, media.Year)
	}
	filename += ".mkv"

	var senplayerURL string
	if mode == "download" {
		// Download format: SenPlayer://x-callback-url/download?url=<url>&name=<filename>
		senplayerURL = fmt.Sprintf("SenPlayer://x-callback-url/download?url=%s&name=%s",
			url.QueryEscape(streamURL),
			url.QueryEscape(filename),
		)
	} else {
		// Play format: SenPlayer://x-callback-url/play?url=<url>&name=<filename>&User-Agent=<ua>
		senplayerURL = fmt.Sprintf("SenPlayer://x-callback-url/play?url=%s&name=%s&User-Agent=%s",
			url.QueryEscape(streamURL),
			url.QueryEscape(filename),
			url.QueryEscape("GoplexCLI/1.0"),
		)
	}

	// On macOS, open the URL directly
	if runtime.GOOS == "darwin" {
		fmt.Println(infoStyle.Render("Opening in SenPlayer..."))
		cmd := exec.Command("open", senplayerURL)
		if err := cmd.Run(); err != nil {
			// If open fails, fall back to showing the URL
			fmt.Println(warningStyle.Render("Could not open SenPlayer automatically"))
			fmt.Println(infoStyle.Render("\nCopy this URL to open in SenPlayer:"))
			fmt.Println(senplayerURL)
		} else {
			fmt.Println(successStyle.Render(fmt.Sprintf("✓ Sent to SenPlayer (%s)", mode)))
		}
	} else {
		// On other platforms, show the URL for manual copying
		fmt.Println(infoStyle.Render("\nCopy this URL to open in SenPlayer:"))
		fmt.Println(senplayerURL)
	}

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
	fmt.Println(warningStyle.Render(fmt.Sprintf("\nStream server running on port %d", stream.DefaultPort)))

	fmt.Println(successStyle.Render("\nClick to open in your player:"))
	fmt.Println()

	playerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#C084FC")).Bold(true).Width(12)
	linkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#60A5FA")).Underline(true)

	fmt.Printf("  %s %s\n\n", playerStyle.Render("Infuse"), linkStyle.Render(fmt.Sprintf("infuse://x-callback-url/play?url=%s", encodedURL)))
	fmt.Printf("  %s %s\n\n", playerStyle.Render("OutPlayer"), linkStyle.Render(fmt.Sprintf("outplayer://x-callback-url/play?url=%s", encodedURL)))
	fmt.Printf("  %s %s\n\n", playerStyle.Render("SenPlayer"), linkStyle.Render(fmt.Sprintf("senplayer://x-callback-url/play?url=%s", encodedURL)))
	fmt.Printf("  %s %s\n\n", playerStyle.Render("VLC"), linkStyle.Render(fmt.Sprintf("vlc://%s", encodedURL)))
	fmt.Printf("  %s %s\n", playerStyle.Render("VidHub"), linkStyle.Render(fmt.Sprintf("open-vidhub://x-callback-url/open?url=%s", encodedURL)))

	fmt.Println()
	fmt.Println(successStyle.Render("Web UI: ") + linkStyle.Render(webURL))
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

// handleQueueView displays queue and handles queue actions
// Returns "done" (after download), "back" (continue browsing), or error
func handleQueueView(cfg *config.Config, q *queue.Queue) (string, error) {
	if q.IsEmpty() {
		fmt.Println(warningStyle.Render("Queue is empty"))
		return "back", nil
	}

	fmt.Println(titleStyle.Render("Download Queue"))
	fmt.Println(infoStyle.Render(fmt.Sprintf("%d item(s) in queue:\n", q.Len())))

	for i, item := range q.Items {
		fmt.Printf("  %d. %s\n", i+1, item.FormatMediaTitle())
	}
	fmt.Println()

	// Prompt for queue action. "Transfer to Outplayer" is only offered when at
	// least one Outplayer target is enabled (mirrors the browse action menu).
	outplayerCount := len(cfg.GetEnabledOutplayerTargets())
	var action string
	var err error

	if ui.IsAvailable(cfg.FzfPath) {
		action, err = ui.PromptQueueAction(cfg.FzfPath, q.Len(), outplayerCount)
		if err != nil {
			if errors.Is(err, apperrors.ErrCancelled) {
				return "back", nil
			}
			return "", err
		}
	} else {
		action, err = promptQueueActionManual(q.Len(), outplayerCount)
		if err != nil {
			return "", err
		}
	}

	switch action {
	case "download":
		// Capture keys of items being downloaded before starting
		// This allows us to remove only these items after download,
		// preserving any new items added by other instances during download
		keysToRemove := make([]string, len(q.Items))
		for i, item := range q.Items {
			keysToRemove[i] = item.Key
		}

		err := handleDownloadMultiple(cfg, q.Items)
		if err != nil {
			return "", err
		}

		// Remove only the downloaded items (preserves items added during download)
		if err := q.RemoveByKeys(keysToRemove); err != nil {
			return "", fmt.Errorf("failed to update queue: %w", err)
		}
		return "done", nil

	case "transfer":
		// Transfers are non-destructive: the queue is left intact (the transfer
		// handler returns nil on soft no-ops like cancelling target selection,
		// so auto-removing here could silently clear the queue). Stay in the
		// queue view so the user can also download or clear afterwards.
		if err := handleTransferToWebDAV(cfg, q.Items); err != nil {
			return "", err
		}
		return "back", nil

	case "transfer-outplayer":
		if err := handleTransferToOutplayer(cfg, q.Items); err != nil {
			return "", err
		}
		return "back", nil

	case "clear":
		if err := q.Clear(); err != nil {
			return "", fmt.Errorf("failed to clear queue: %w", err)
		}
		fmt.Println(successStyle.Render("Queue cleared"))
		return "back", nil

	case "remove":
		if ui.IsAvailable(cfg.FzfPath) {
			indices, err := ui.SelectQueueItemsForRemoval(q.Items, cfg.FzfPath)
			if err != nil {
				if errors.Is(err, apperrors.ErrCancelled) {
					return "back", nil
				}
				return "", err
			}
			q.Remove(indices)
			if err := q.Save(); err != nil {
				return "", fmt.Errorf("failed to save queue: %w", err)
			}
			fmt.Println(successStyle.Render(fmt.Sprintf("Removed %d item(s) from queue", len(indices))))
		} else {
			err := removeFromQueueManual(q)
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

// promptQueueActionManual - fallback for no-fzf queue action selection.
// "Transfer to Outplayer" is only listed when outplayerCount > 0, so the option
// numbering is built dynamically.
func promptQueueActionManual(queueCount, outplayerCount int) (string, error) {
	type option struct {
		label string
		token string
	}
	options := []option{
		{fmt.Sprintf("Download All (%s)", ui.PluralizeItems(queueCount)), "download"},
		{"Transfer to WebDAV", "transfer"},
	}
	if outplayerCount > 0 {
		options = append(options, option{"Transfer to Outplayer", "transfer-outplayer"})
	}
	options = append(options,
		option{"Clear Queue", "clear"},
		option{"Remove Items", "remove"},
		option{"Back to Browse", "back"},
	)

	fmt.Println(infoStyle.Render("\nQueue actions:"))
	for i, opt := range options {
		fmt.Printf("  %d. %s\n", i+1, opt.label)
	}
	fmt.Printf("\nChoice (1-%d): ", len(options))

	var choice int
	if _, err := fmt.Scanln(&choice); err != nil {
		return "", fmt.Errorf("failed to read selection: %w", err)
	}
	if choice < 1 || choice > len(options) {
		return "back", nil
	}
	return options[choice-1].token, nil
}

// removeFromQueueManual - fallback for no-fzf queue item removal
func removeFromQueueManual(q *queue.Queue) error {
	fmt.Println(infoStyle.Render("\nSelect items to remove:"))
	for i, item := range q.Items {
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
			if num >= 1 && num <= q.Len() {
				indices = append(indices, num-1) // Convert to 0-based index
			}
		}
	}

	if len(indices) > 0 {
		q.Remove(indices)
		if err := q.Save(); err != nil {
			return fmt.Errorf("failed to save queue: %w", err)
		}
		fmt.Println(successStyle.Render(fmt.Sprintf("Removed %d item(s) from queue", len(indices))))
	}

	return nil
}

// selectMediaTypeManualWithQueue - fallback for no-fzf with queue option
func selectMediaTypeManualWithQueue(queueCount, continueCount int) (string, error) {
	fmt.Println(infoStyle.Render("\nSelect media type:"))

	type option struct {
		label string
		token string
	}
	var options []option
	if queueCount > 0 {
		options = append(options, option{fmt.Sprintf("View Queue (%s)", ui.PluralizeItems(queueCount)), "queue"})
	}
	if continueCount > 0 {
		options = append(options, option{fmt.Sprintf("Continue Watching (%s)", ui.PluralizeItems(continueCount)), "continue watching"})
	}
	options = append(options,
		option{"Recently Added Movies", "recently added movies"},
		option{"Recently Added TV Shows", "recently added tv shows"},
		option{"Movies", "movies"},
		option{"TV Shows", "tv shows"},
		option{"All", "all"},
	)

	for i, opt := range options {
		fmt.Printf("  %d. %s\n", i+1, opt.label)
	}
	fmt.Printf("\nChoice (1-%d): ", len(options))

	var choice int
	if _, err := fmt.Scanln(&choice); err != nil {
		return "", fmt.Errorf("failed to read selection: %w", err)
	}
	if choice < 1 || choice > len(options) {
		return "", fmt.Errorf("invalid selection")
	}
	return options[choice-1].token, nil
}

// promptActionManualWithQueue - fallback for no-fzf action selection with queue.
// "Transfer to Outplayer" is only listed when outplayerCount > 0, so the option
// numbering is built dynamically.
func promptActionManualWithQueue(selectionCount, queueCount, outplayerCount int) (string, error) {
	queueLabel := fmt.Sprintf("Add (%d) to Queue", selectionCount)
	if queueCount > 0 {
		queueLabel = fmt.Sprintf("Add (%d) to Queue (%d)", selectionCount, queueCount)
	}

	type option struct {
		label string
		token string
	}
	options := []option{
		{"Watch", "watch"},
		{"Download", "download"},
		{queueLabel, "queue"},
		{"Transfer to WebDAV", "transfer"},
	}
	if outplayerCount > 0 {
		options = append(options, option{"Transfer to Outplayer", "transfer-outplayer"})
	}
	options = append(options, option{"More...", "more"}, option{"Cancel", "cancel"})

	fmt.Println(infoStyle.Render("\nSelect action:"))
	for i, opt := range options {
		fmt.Printf("  %d. %s\n", i+1, opt.label)
	}
	fmt.Printf("\nChoice (1-%d): ", len(options))

	var choice int
	if _, err := fmt.Scanln(&choice); err != nil {
		return "", fmt.Errorf("failed to read selection: %w", err)
	}
	if choice < 1 || choice > len(options) {
		return "cancel", nil
	}
	return options[choice-1].token, nil
}

// promptMoreActionManual - fallback for no-fzf selection of the "More..." submenu.
func promptMoreActionManual() (string, error) {
	fmt.Println(infoStyle.Render("\nMore actions:"))
	fmt.Println("  1. SenPlayer Play")
	fmt.Println("  2. SenPlayer Download")
	fmt.Println("  3. Stream")
	fmt.Println("  4. Back")
	fmt.Print("\nChoice (1-4): ")

	var choice int
	if _, err := fmt.Scanln(&choice); err != nil {
		return "", fmt.Errorf("failed to read selection: %w", err)
	}

	switch choice {
	case 1:
		return "senplayer play", nil
	case 2:
		return "senplayer download", nil
	case 3:
		return "stream", nil
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

	// An incremental update fetches only items added since the last cache and
	// merges them in. A full reindex (or an empty/missing cache) fetches
	// everything and replaces the cache.
	var existing *cache.Cache
	incremental := false
	if !fullReindex {
		existing, err = cache.Load()
		if err != nil {
			return fmt.Errorf("failed to load existing cache: %w", err)
		}
		incremental = len(existing.Media) > 0
	}

	action := "Reindexing"
	if !fullReindex {
		action = "Updating"
	}

	fmt.Println(titleStyle.Render(action + " Media Cache"))

	// Newest addedAt already cached, keyed by server name then item type
	// ("movie"/"episode"). Used to fetch only newer items during incremental
	// updates.
	maxAdded := map[string]map[string]int64{}
	if incremental {
		for _, item := range existing.Media {
			byType := maxAdded[item.ServerName]
			if byType == nil {
				byType = map[string]int64{}
				maxAdded[item.ServerName] = byType
			}
			if item.AddedAt > byType[item.Type] {
				byType[item.Type] = item.AddedAt
			}
		}
	}
	// sinceFor maps a library type ("movie"/"show") to the newest addedAt known
	// for the matching item type on the given server.
	sinceFor := func(serverName, libType string) int64 {
		itemType := "movie"
		if libType == "show" {
			itemType = "episode"
		}
		if byType, ok := maxAdded[serverName]; ok {
			return byType[itemType]
		}
		return 0
	}

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

		serverProgress := func(serverName, libraryName string, itemCount, totalItems, totalLibs, currentLib, serverNum, totalServers int) {
			progress := fmt.Sprintf("%d items", itemCount)
			if totalItems > 0 {
				progress = fmt.Sprintf("%d/%d items", itemCount, totalItems)
			}
			fmt.Printf("\r\x1b[K%s [Server %d/%d: %s] [%d/%d] %s: %s",
				infoStyle.Render("Processing"),
				serverNum,
				totalServers,
				serverName,
				currentLib,
				totalLibs,
				libraryName,
				progress,
			)
		}
		mappings := toPlexPathMappings(cfg.PathMappings)
		if incremental {
			media, err = plex.GetNewMediaFromServers(ctx, serverConfigs, mappings, sinceFor, serverProgress)
		} else {
			media, err = plex.GetAllMediaFromServers(ctx, serverConfigs, mappings, serverProgress)
		}
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
		client.SetPathMappings(toPlexPathMappings(cfg.PathMappings))

		// Test connection
		if err := client.Test(); err != nil {
			return fmt.Errorf("failed to connect to plex server: %w", err)
		}

		fmt.Println(successStyle.Render("✓ Connected to Plex server"))
		fmt.Println(infoStyle.Render("Fetching media library..."))

		// Get media with progress
		libraryProgress := func(libraryName string, itemCount, totalItems, totalLibs, currentLib int) {
			progress := fmt.Sprintf("%d items", itemCount)
			if totalItems > 0 {
				progress = fmt.Sprintf("%d/%d items", itemCount, totalItems)
			}
			fmt.Printf("\r\x1b[K%s [%d/%d] %s: %s",
				infoStyle.Render("Processing libraries"),
				currentLib,
				totalLibs,
				libraryName,
				progress,
			)
		}
		if incremental {
			// The client uses serverURL as its server name when none is set,
			// matching how items were tagged when first cached.
			media, err = client.GetMediaSince(ctx, func(libType string) int64 {
				return sinceFor(serverURL, libType)
			}, libraryProgress)
		} else {
			media, err = client.GetAllMedia(ctx, libraryProgress)
		}
		if err != nil {
			return fmt.Errorf("failed to get media: %w", err)
		}
	}

	fmt.Println() // New line after progress

	// For incremental updates, merge the newly fetched items into the existing
	// cache (deduping by server + key); a full reindex replaces it outright.
	finalMedia := media
	if incremental {
		merged, added := mergeMedia(existing.Media, media)
		finalMedia = merged
		if added == 0 {
			fmt.Println(successStyle.Render("✓ Cache is already up to date — no new items"))
		} else {
			fmt.Println(successStyle.Render(fmt.Sprintf("✓ Added %d new item(s)", added)))
		}
	} else {
		fmt.Println(successStyle.Render(fmt.Sprintf("✓ Retrieved %d media items", len(finalMedia))))
	}

	// Save to cache
	mediaCache := &cache.Cache{
		Media: finalMedia,
	}

	if err := mediaCache.Save(); err != nil {
		return fmt.Errorf("failed to save cache: %w", err)
	}

	fmt.Println(successStyle.Render("✓ Cache saved successfully"))

	// Count by type and by server
	movieCount := 0
	episodeCount := 0
	serverCounts := make(map[string]int)

	for _, item := range finalMedia {
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

	fmt.Println(infoStyle.Render(fmt.Sprintf("\nTotal items: %d", len(finalMedia))))
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

// mergeMedia combines newly fetched items into the existing cached items,
// deduplicating by server name and key. Items present in both are replaced
// with the freshly fetched version (picking up metadata changes). It returns
// the merged slice and the number of items that were newly added.
func mergeMedia(existing, fetched []plex.MediaItem) ([]plex.MediaItem, int) {
	keyOf := func(m plex.MediaItem) string { return m.ServerName + "\x00" + m.Key }

	merged := make([]plex.MediaItem, len(existing))
	copy(merged, existing)

	index := make(map[string]int, len(merged))
	for i := range merged {
		index[keyOf(merged[i])] = i
	}

	added := 0
	for _, item := range fetched {
		k := keyOf(item)
		if i, ok := index[k]; ok {
			merged[i] = item
			continue
		}
		index[k] = len(merged)
		merged = append(merged, item)
		added++
	}

	return merged, added
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
	// Safely truncate token display to avoid panic on short tokens
	tokenDisplay := cfg.PlexToken
	if len(tokenDisplay) > 10 {
		tokenDisplay = tokenDisplay[:10] + "..."
	}
	fmt.Println(infoStyle.Render("Token: " + tokenDisplay))

	downloadDir := "(current directory)"
	if cfg.DownloadDir != "" {
		downloadDir = cfg.DownloadDir
	}
	fmt.Println(infoStyle.Render("Download dir: " + downloadDir))

	configPath, _ := config.GetConfigPath()
	fmt.Println(infoStyle.Render("\nConfig file: " + configPath))

	cachePath, _ := cache.GetCachePath()
	fmt.Println(infoStyle.Render("Cache file: " + cachePath))

	return nil
}

func runUpdate(cmd *cobra.Command, args []string) error {
	fmt.Println(titleStyle.Render("Update"))
	ctx := context.Background()
	return update.Run(ctx, update.DefaultRepo, version, updateCheckOnly, os.Stdout)
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
				if errors.Is(err, apperrors.ErrCancelled) {
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
				if errors.Is(err, apperrors.ErrCancelled) {
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

func runServerRemove(cmd *cobra.Command, args []string) error {
	serverName := strings.Join(args, " ")

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	found := false
	remaining := make([]config.PlexServer, 0, len(cfg.Servers))
	for _, server := range cfg.Servers {
		if !found && strings.EqualFold(server.Name, serverName) {
			found = true
			// Clear the legacy single-server field if it pointed at this
			// server, otherwise MigrateLegacy would re-add it on next load.
			if cfg.PlexURL == server.URL {
				cfg.PlexURL = ""
			}
			continue
		}
		remaining = append(remaining, server)
	}

	if !found {
		return fmt.Errorf("server '%s' not found", serverName)
	}

	cfg.Servers = remaining

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println(successStyle.Render(fmt.Sprintf("✓ Removed server '%s'", serverName)))
	fmt.Println(warningStyle.Render("Note: Cached items from this server will remain until next reindex"))

	return nil
}

func runSort(cmd *cobra.Command, args []string) error {
	// Default sort field is "added"
	sortField := "added"
	if len(args) > 0 {
		sortField = strings.ToLower(args[0])
	}

	// Validate sort field
	validFields := map[string]bool{
		"name":     true,
		"added":    true,
		"year":     true,
		"rating":   true,
		"duration": true,
	}
	if !validFields[sortField] {
		return fmt.Errorf("invalid sort field '%s'. Valid fields: name, added, year, rating, duration", sortField)
	}

	// Validate type filter
	normalizedType := strings.ToLower(sortType)
	validTypes := map[string]bool{
		"all":      true,
		"movies":   true,
		"movie":    true,
		"shows":    true,
		"show":     true,
		"tv":       true,
		"episodes": true,
	}
	if !validTypes[normalizedType] {
		return fmt.Errorf("invalid type '%s'. Valid types: movies, shows, all", sortType)
	}

	// Determine sort direction
	// Default: ascending for name, descending for everything else
	ascending := sortField == "name"
	if sortAsc {
		ascending = true
	}
	if sortDesc {
		ascending = false
	}

	// Validate limit
	if sortLimit < 1 {
		sortLimit = 20
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

	// Filter by type (using already normalized type)
	var filteredMedia []plex.MediaItem
	switch normalizedType {
	case "movies", "movie":
		for _, item := range mediaCache.Media {
			if item.Type == "movie" {
				filteredMedia = append(filteredMedia, item)
			}
		}
	case "shows", "show", "tv", "episodes":
		for _, item := range mediaCache.Media {
			if item.Type == "episode" {
				filteredMedia = append(filteredMedia, item)
			}
		}
	default: // "all"
		filteredMedia = append(filteredMedia, mediaCache.Media...)
	}

	if len(filteredMedia) == 0 {
		fmt.Println(warningStyle.Render("No media found matching the filter."))
		return nil
	}

	// Sort the media
	sort.Slice(filteredMedia, func(i, j int) bool {
		switch sortField {
		case "name":
			left := strings.ToLower(filteredMedia[i].Title)
			right := strings.ToLower(filteredMedia[j].Title)
			if ascending {
				return left < right
			}
			return left > right
		case "added":
			left := filteredMedia[i].AddedAt
			right := filteredMedia[j].AddedAt
			if ascending {
				return left < right
			}
			return left > right
		case "year":
			left := filteredMedia[i].Year
			right := filteredMedia[j].Year
			if ascending {
				return left < right
			}
			return left > right
		case "rating":
			left := filteredMedia[i].Rating
			right := filteredMedia[j].Rating
			if ascending {
				return left < right
			}
			return left > right
		case "duration":
			left := filteredMedia[i].Duration
			right := filteredMedia[j].Duration
			if ascending {
				return left < right
			}
			return left > right
		default:
			left := filteredMedia[i].AddedAt
			right := filteredMedia[j].AddedAt
			if ascending {
				return left < right
			}
			return left > right
		}
	})

	// Save total count before applying limit
	totalFiltered := len(filteredMedia)

	// Apply limit
	if sortLimit > 0 && sortLimit < len(filteredMedia) {
		filteredMedia = filteredMedia[:sortLimit]
	}

	// If interactive mode, feed into the browse flow
	if sortInteractive {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("invalid config: %w. Please run 'goplexcli login' first", err)
		}

		selectedMediaItems, cancelled, err := selectMediaFlat(filteredMedia, cfg, "Select media (TAB for multi-select):")
		if err != nil {
			return err
		}
		if cancelled {
			return nil
		}

		if len(selectedMediaItems) == 0 {
			return nil
		}

		// Load queue for action prompt
		q, err := queue.Load()
		if err != nil {
			return fmt.Errorf("failed to load queue: %w", err)
		}

		// Handle user action
		err = handleMediaAction(cfg, q, selectedMediaItems)
		if err != nil {
			if errors.Is(err, errAddedToQueue) {
				// Items were added to queue, return successfully
				return nil
			}
			return err
		}
		return nil
	}

	// Non-interactive: display the sorted list
	directionLabel := "descending"
	if ascending {
		directionLabel = "ascending"
	}

	fmt.Println(titleStyle.Render(fmt.Sprintf("Sorted by %s (%s)", sortField, directionLabel)))
	fmt.Println(infoStyle.Render(fmt.Sprintf("Showing %d of %d items\n", len(filteredMedia), totalFiltered)))

	// Display results
	for i, item := range filteredMedia {
		// Format the sort field value for display
		var fieldValue string
		switch sortField {
		case "name":
			fieldValue = ""
		case "added":
			if item.AddedAt > 0 {
				addedTime := time.Unix(item.AddedAt, 0)
				fieldValue = formatTimeAgo(addedTime)
			} else {
				fieldValue = "Unknown"
			}
		case "year":
			if item.Year > 0 {
				fieldValue = fmt.Sprintf("%d", item.Year)
			} else {
				fieldValue = "N/A"
			}
		case "rating":
			if item.Rating > 0 {
				fieldValue = fmt.Sprintf("%.1f", item.Rating)
			} else {
				fieldValue = "N/A"
			}
		case "duration":
			if item.Duration > 0 {
				mins := item.Duration / 60000
				fieldValue = fmt.Sprintf("%d min", mins)
			} else {
				fieldValue = "N/A"
			}
		}

		// Build the output line
		numStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Width(4)
		titleStr := item.FormatMediaTitle()

		if fieldValue != "" {
			fieldStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#60A5FA"))
			fmt.Printf("%s %s  %s\n", numStyle.Render(fmt.Sprintf("%d.", i+1)), titleStr, fieldStyle.Render(fieldValue))
		} else {
			fmt.Printf("%s %s\n", numStyle.Render(fmt.Sprintf("%d.", i+1)), titleStr)
		}
	}

	return nil
}

// formatTimeAgo returns a human-readable relative time string
func formatTimeAgo(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	switch {
	case diff < time.Minute:
		return "Just now"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	case diff < 24*time.Hour:
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	case diff < 7*24*time.Hour:
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	case diff < 30*24*time.Hour:
		weeks := int(diff.Hours() / 24 / 7)
		if weeks == 1 {
			return "1 week ago"
		}
		return fmt.Sprintf("%d weeks ago", weeks)
	case diff < 365*24*time.Hour:
		months := int(diff.Hours() / 24 / 30)
		if months == 1 {
			return "1 month ago"
		}
		return fmt.Sprintf("%d months ago", months)
	default:
		years := int(diff.Hours() / 24 / 365)
		if years == 1 {
			return "1 year ago"
		}
		return fmt.Sprintf("%d years ago", years)
	}
}
