package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/joshkerr/goplexcli/internal/cache"
	"github.com/joshkerr/goplexcli/internal/config"
	"github.com/joshkerr/goplexcli/internal/favorites"
	"github.com/joshkerr/goplexcli/internal/lansync"
	"github.com/joshkerr/goplexcli/internal/library"
	"github.com/joshkerr/goplexcli/internal/libserver"
	"github.com/joshkerr/goplexcli/internal/plex"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// Flags for the library and daemon commands.
var (
	libraryAddName    string
	libraryAddType    string
	libraryAddRemote  string
	libraryScanFull   bool
	daemonScanEvery   time.Duration
	daemonNoDownloads bool
	daemonNoSync      bool
	daemonNoLibrary   bool
	daemonScanOnStart bool
)

// newLibraryCmd builds the `library` command group: manage the local folders
// goplexcli indexes itself, without a Plex server.
func newLibraryCmd() *cobra.Command {
	libraryCmd := &cobra.Command{
		Use:   "library",
		Short: "Index local media folders without a Plex server",
		Long: `Manage the media folders goplexcli indexes on its own. Each folder is
scanned for video files, titles are read from Plex-style names
("Title (Year) [imdb-tt…]" folders, "Show/Season 01/Show.S01E02.mkv"), and
metadata and artwork come from TMDB (set tmdb_api_key in config).

Locally indexed items live in the same cache as Plex items, so browse, the
GUI, LAN sync and downloads all work unchanged. Playback and posters are
served by this machine's library server — run 'goplexcli daemon' to keep it
(and periodic rescans) running.

  goplexcli library add /mnt/media/Movies --type movie --remote gdrive:Media/Movies
  goplexcli library add /mnt/media/TV --type show --name "TV"
  goplexcli library list
  goplexcli library scan          # index now (incremental)
  goplexcli library scan --full   # re-enrich everything from TMDB`,
	}

	addCmd := &cobra.Command{
		Use:   "add <path>",
		Short: "Add a folder to index",
		Args:  cobra.ExactArgs(1),
		RunE:  runLibraryAdd,
	}
	addCmd.Flags().StringVar(&libraryAddName, "name", "", "Library name (default: folder name)")
	addCmd.Flags().StringVar(&libraryAddType, "type", "movie", "Library type: movie or show")
	addCmd.Flags().StringVar(&libraryAddRemote, "remote", "", "rclone path that mirrors this folder (e.g. gdrive:Media/Movies) so downloads can fetch files via rclone")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List configured library folders",
		RunE:  runLibraryList,
	}
	removeCmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a library folder (its items drop from the cache on the next scan)",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runLibraryRemove,
	}
	enableCmd := &cobra.Command{
		Use:   "enable <name>",
		Short: "Include a library folder in scans",
		Args:  cobra.MinimumNArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return setLibraryEnabled(strings.Join(args, " "), true) },
	}
	disableCmd := &cobra.Command{
		Use:   "disable <name>",
		Short: "Exclude a library folder from scans",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return setLibraryEnabled(strings.Join(args, " "), false)
		},
	}
	scanCmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan library folders and update the cache",
		Long: `Scan every enabled library folder and merge the results into the media
cache. Files that haven't changed keep their metadata; new files are matched
on TMDB. With --full every title is re-enriched (playback progress is kept).
This is also what 'cache update' and 'cache reindex' do for local libraries.`,
		RunE: runLibraryScan,
	}
	scanCmd.Flags().BoolVar(&libraryScanFull, "full", false, "Re-enrich every title from TMDB instead of only new or changed files")

	libraryCmd.AddCommand(addCmd, listCmd, removeCmd, enableCmd, disableCmd, scanCmd)
	return libraryCmd
}

// newDaemonCmd builds the `daemon` command: one long-running process that
// serves locally indexed media, syncs the cache over the LAN, accepts remote
// downloads, and rescans libraries on a schedule.
func newDaemonCmd() *cobra.Command {
	daemonCmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run the library server, LAN cache sync, download server and scheduled scans",
		Long: `Run goplexcli as an always-on server. In one process it:

  • serves locally indexed media to players and other goplexcli instances
    (streaming, posters, playback progress) on port 47822;
  • advertises and serves the media cache on the LAN (like 'sync serve');
  • accepts download requests from remote GUIs (like 'serve');
  • rescans the library folders on an interval (default 1h, or
    library_scan_interval in config) so new files show up on every device
    that syncs from this machine.

Typical use is a NAS or home server with the media folders (or rclone
mounts) attached:

  goplexcli library add /mnt/gdrive/Media/Movies --type movie --remote gdrive:Media/Movies
  goplexcli daemon`,
		RunE: runDaemon,
	}
	daemonCmd.Flags().DurationVar(&daemonScanEvery, "scan-interval", 0, "How often to rescan libraries (default library_scan_interval from config, else 1h; 0 disables)")
	daemonCmd.Flags().BoolVar(&daemonScanOnStart, "scan-on-start", true, "Run a library scan when the daemon starts")
	daemonCmd.Flags().BoolVar(&daemonNoDownloads, "no-downloads", false, "Don't run the remote download server")
	daemonCmd.Flags().BoolVar(&daemonNoSync, "no-sync", false, "Don't advertise the cache on the LAN")
	daemonCmd.Flags().BoolVar(&daemonNoLibrary, "no-library", false, "Don't serve local media or scan libraries")
	return daemonCmd
}

func runLibraryAdd(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	p, err := filepath.Abs(expandHome(args[0]))
	if err != nil {
		return err
	}
	st, err := os.Stat(p)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", p, err)
	}
	if !st.IsDir() {
		return fmt.Errorf("%s is not a directory", p)
	}
	name := strings.TrimSpace(libraryAddName)
	if name == "" {
		name = filepath.Base(p)
	}
	if _, _, exists := cfg.FindLibrary(name); exists {
		return fmt.Errorf("a library named %q already exists (use --name to pick another)", name)
	}
	lib := config.LibraryFolder{
		Name:    name,
		Path:    p,
		Type:    strings.ToLower(strings.TrimSpace(libraryAddType)),
		Remote:  strings.TrimSpace(libraryAddRemote),
		Enabled: true,
	}
	switch lib.Type {
	case "movies", "film", "films":
		lib.Type = "movie"
	case "shows", "tv", "series":
		lib.Type = "show"
	}
	if err := lib.Validate(); err != nil {
		return err
	}
	cfg.Libraries = append(cfg.Libraries, lib)
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	fmt.Println(successStyle.Render(fmt.Sprintf("✓ Added %s library %q at %s", lib.Type, lib.Name, lib.Path)))
	if lib.Remote != "" {
		fmt.Println(infoStyle.Render("  rclone: " + lib.Remote))
	}
	if strings.TrimSpace(cfg.TMDBAPIKey) == "" {
		fmt.Println(warningStyle.Render("No tmdb_api_key in config: titles will come from file names only. Add one for summaries, ratings and artwork."))
	}
	fmt.Println(infoStyle.Render("Run 'goplexcli library scan' to index it, or 'goplexcli daemon' to keep it indexed and served."))
	return nil
}

func runLibraryList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	fmt.Println(titleStyle.Render("Local Libraries"))
	if len(cfg.Libraries) == 0 {
		fmt.Println(warningStyle.Render("No libraries configured. Add one with 'goplexcli library add <path>'."))
		return nil
	}
	counts := map[string]int{}
	if c, err := cache.Load(); err == nil {
		server := library.ServerName(cfg)
		for _, it := range c.Media {
			if it.ServerName != server || !libserver.IsLocalKey(it.Key) {
				continue
			}
			for _, lib := range cfg.Libraries {
				if strings.HasPrefix(it.FilePath, strings.TrimRight(lib.Path, string(filepath.Separator))+string(filepath.Separator)) {
					counts[lib.Name]++
					break
				}
			}
		}
	}
	for i, lib := range cfg.Libraries {
		status := warningStyle.Render("disabled")
		if lib.Enabled {
			status = successStyle.Render("enabled")
		}
		fmt.Printf("%d. %s (%s) - %s [%s]", i+1, lib.Name, lib.Type, lib.Path, status)
		if n, ok := counts[lib.Name]; ok {
			fmt.Printf("  %d items", n)
		}
		fmt.Println()
		if lib.Remote != "" {
			fmt.Println(infoStyle.Render("     rclone: " + lib.Remote))
		}
	}
	fmt.Println(infoStyle.Render(fmt.Sprintf("\nServer name: %s   Server URL: %s", library.ServerName(cfg), library.ServerURL(cfg.LibraryURL, cfg.LibraryListen))))
	if strings.TrimSpace(cfg.TMDBAPIKey) == "" {
		fmt.Println(warningStyle.Render("tmdb_api_key is not set; metadata and artwork are unavailable."))
	}
	return nil
}

func runLibraryRemove(cmd *cobra.Command, args []string) error {
	name := strings.Join(args, " ")
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	_, i, ok := cfg.FindLibrary(name)
	if !ok {
		return fmt.Errorf("no library named %q", name)
	}
	cfg.Libraries = append(cfg.Libraries[:i], cfg.Libraries[i+1:]...)
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	fmt.Println(successStyle.Render(fmt.Sprintf("✓ Removed library %q", name)))
	fmt.Println(warningStyle.Render("Its items stay in the cache until the next scan."))
	return nil
}

func setLibraryEnabled(name string, enabled bool) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	_, i, ok := cfg.FindLibrary(name)
	if !ok {
		return fmt.Errorf("no library named %q", name)
	}
	cfg.Libraries[i].Enabled = enabled
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	state := "enabled"
	if !enabled {
		state = "disabled"
	}
	fmt.Println(successStyle.Render(fmt.Sprintf("✓ Library %q %s", cfg.Libraries[i].Name, state)))
	return nil
}

func runLibraryScan(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if len(cfg.GetEnabledLibraries()) == 0 {
		return fmt.Errorf("no enabled libraries - add one with 'goplexcli library add <path>'")
	}
	existing, err := cache.Load()
	if err != nil {
		return fmt.Errorf("failed to load cache: %w", err)
	}
	fmt.Println(titleStyle.Render("Scanning Local Libraries"))
	media, res, err := indexLibrariesWithProgress(context.Background(), cfg, existing.Media, libraryScanFull)
	if err != nil {
		return err
	}
	if err := (&cache.Cache{Media: media}).Save(); err != nil {
		return fmt.Errorf("failed to save cache: %w", err)
	}
	printLibraryResult(res, len(media))
	migrateFavorites(res)
	return nil
}

// indexLibrariesWithProgress runs the local indexer with a one-line terminal
// progress display and warnings printed as they happen.
func indexLibrariesWithProgress(ctx context.Context, cfg *config.Config, existing []plex.MediaItem, full bool) ([]plex.MediaItem, *library.Result, error) {
	var mu sync.Mutex
	var warnings []string
	logf := func(format string, args ...any) {
		mu.Lock()
		warnings = append(warnings, fmt.Sprintf(format, args...))
		mu.Unlock()
	}
	// On a terminal the progress line redraws in place; when output is a
	// pipe or a journal, print a line every few seconds instead so logs
	// stay readable.
	tty := term.IsTerminal(int(os.Stdout.Fd()))
	var lastLine time.Time
	var lastPhase string
	progress := func(p library.Progress) {
		phase := fmt.Sprintf("%d:%s", p.LibNum, p.Phase)
		if !tty && phase == lastPhase && time.Since(lastLine) < 10*time.Second && p.Done != p.Total {
			return
		}
		lastLine, lastPhase = time.Now(), phase
		prefix, suffix := "\r\x1b[K", ""
		if !tty {
			prefix, suffix = "", "\n"
		}
		switch p.Phase {
		case "scan":
			fmt.Printf("%s%s [%d/%d] %s: scanning… %d files%s",
				prefix, infoStyle.Render("Indexing"), p.LibNum, p.Libraries, p.Library, p.Done, suffix)
		case "match":
			fmt.Printf("%s%s [%d/%d] %s: matching %d/%d%s",
				prefix, infoStyle.Render("Indexing"), p.LibNum, p.Libraries, p.Library, p.Done, p.Total, suffix)
		}
	}
	media, res, err := library.Run(ctx, cfg, existing, full, progress, logf)
	if tty {
		fmt.Println()
	}
	mu.Lock()
	defer mu.Unlock()
	const show = 20
	for i, w := range warnings {
		if i == show {
			fmt.Println(warningStyle.Render(fmt.Sprintf("  … and %d more", len(warnings)-show)))
			break
		}
		fmt.Println(warningStyle.Render("  " + w))
	}
	return media, res, err
}

func printLibraryResult(res *library.Result, total int) {
	fmt.Println(successStyle.Render(fmt.Sprintf("✓ Indexed %d local item(s) (%d unchanged, %d unmatched on TMDB)", len(res.Items), res.Reused, res.Unmatched)))
	fmt.Println(successStyle.Render(fmt.Sprintf("✓ Cache saved with %d items", total)))
}

// migrateFavorites carries stars from items that a local index replaced
// (e.g. the Plex entries for the same files) onto the new keys.
func migrateFavorites(res *library.Result) {
	if res == nil || len(res.KeyMigration) == 0 {
		return
	}
	moved, err := favorites.NewStore().Remap(res.KeyMigration)
	if err != nil {
		fmt.Println(warningStyle.Render("Favorites migration failed: " + err.Error()))
		return
	}
	if moved > 0 {
		fmt.Println(successStyle.Render(fmt.Sprintf("✓ Carried %d favorite(s) over to local items", moved)))
	}
}

// expandHome expands a leading ~ in a path.
func expandHome(p string) string {
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

// ---- daemon ----

// daemonState is the daemon's in-memory copy of the cache, which it owns
// while running: scans replace it and playback reports mutate it, and it is
// flushed to disk shortly after each change so LAN-sync peers see progress.
type daemonState struct {
	mu      sync.Mutex
	cache   *cache.Cache
	dirty   bool
	saveAt  time.Time
	service *library.Service
	server  string
}

func (d *daemonState) timeline(key, state string, timeMs, durationMs int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cache == nil {
		return
	}
	for i := range d.cache.Media {
		it := &d.cache.Media[i]
		if it.Key != key || it.ServerName != d.server {
			continue
		}
		if library.ApplyTimeline(it, state, timeMs, durationMs, time.Now()) {
			d.dirty = true
			delay := 15 * time.Second
			if state == "stopped" {
				delay = 0
				// A stop is the report that matters (resume point or
				// watched); write it straight away rather than on the
				// next tick.
				go d.flush(true)
			}
			if d.saveAt.IsZero() || d.saveAt.After(time.Now().Add(delay)) {
				d.saveAt = time.Now().Add(delay)
			}
		}
		return
	}
}

// flush writes the cache if playback reports changed it.
func (d *daemonState) flush(force bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.dirty || d.cache == nil {
		return
	}
	if !force && time.Now().Before(d.saveAt) {
		return
	}
	if err := d.cache.Save(); err != nil {
		fmt.Println(warningStyle.Render("Failed to save playback progress: " + err.Error()))
		return
	}
	d.dirty = false
	d.saveAt = time.Time{}
}

// replace installs a freshly indexed cache (already saved to disk).
func (d *daemonState) replace(c *cache.Cache) {
	d.mu.Lock()
	d.cache = c
	d.dirty = false
	d.saveAt = time.Time{}
	d.mu.Unlock()
	if d.service != nil {
		d.service.SetItems(c.Media, d.server)
	}
}

func (d *daemonState) snapshot() []plex.MediaItem {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cache == nil {
		return nil
	}
	return d.cache.Media
}

func runDaemon(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	fmt.Println(titleStyle.Render("GoplexCLI Daemon"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Download server (remote GUIs submit rclone transfers here).
	if !daemonNoDownloads {
		stop, err := startDownloadServer(cfg)
		if err != nil {
			fmt.Println(warningStyle.Render("Download server not started: " + err.Error()))
		} else {
			defer stop()
		}
	}

	// Library server: streams local files and proxies artwork.
	state := &daemonState{server: library.ServerName(cfg)}
	libs := cfg.GetEnabledLibraries()
	if !daemonNoLibrary {
		cacheDir, err := config.GetCacheDir()
		if err != nil {
			return err
		}
		svc := library.NewService(library.ServiceOptions{
			Listen:        cfg.LibraryListen,
			Token:         cfg.LibraryToken,
			ImageCacheDir: library.ImageCacheDir(cacheDir),
			Version:       version,
			RcloneBin:     cfg.RclonePath,
			Timeline:      state.timeline,
		})
		if err := svc.Start(); err != nil {
			return err
		}
		defer svc.Close(context.Background())
		state.service = svc

		existing, err := cache.Load()
		if err != nil {
			return fmt.Errorf("failed to load cache: %w", err)
		}
		state.replace(existing)

		fmt.Println(successStyle.Render(fmt.Sprintf("✓ Library server on port %d serving %d local item(s) as %q", svc.Port(), svc.Count(), state.server)))
		fmt.Println(infoStyle.Render("  Items are addressed at " + library.ServerURL(cfg.LibraryURL, cfg.LibraryListen) + " (library_url in config)"))
		if len(libs) == 0 {
			fmt.Println(warningStyle.Render("  No library folders configured; add some with 'goplexcli library add <path>'."))
		} else {
			for _, lib := range libs {
				fmt.Println(infoStyle.Render(fmt.Sprintf("  %s (%s): %s", lib.Name, lib.Type, lib.Path)))
			}
		}
		if cfg.LibraryToken == "" {
			fmt.Println(infoStyle.Render("  Open to the LAN (set library_token in config to require a token)."))
		}
	}

	// LAN cache sync (peers pull the cache; favorites rendezvous).
	if !daemonNoSync {
		srv := lansync.NewServer(lansync.CacheMetaFunc())
		srv.ServeFavorites(favorites.NewStore(), nil)
		if err := srv.StartOn(lansync.DefaultPort); err != nil {
			fmt.Println(warningStyle.Render("LAN sync not started: " + err.Error()))
		} else {
			defer srv.Close(context.Background())
			if err := srv.AdvertiseError(); err != nil {
				fmt.Println(warningStyle.Render("  mDNS advertising failed; peers must use --peer or sync_peer: " + err.Error()))
			}
			fmt.Println(successStyle.Render(fmt.Sprintf("✓ Serving cache to LAN peers on port %d", srv.Port())))
		}
	}

	// Scheduled scans.
	interval := daemonScanEvery
	if interval == 0 && !cmd.Flags().Changed("scan-interval") {
		interval = time.Hour
		if s := strings.TrimSpace(cfg.LibraryScanInterval); s != "" {
			if d, err := time.ParseDuration(s); err == nil {
				interval = d
			} else {
				fmt.Println(warningStyle.Render("Ignoring invalid library_scan_interval " + s))
			}
		}
	}
	scan := func(reason string) {
		cfg, err := config.Load() // pick up libraries added while running
		if err != nil {
			fmt.Println(warningStyle.Render("Scan skipped, config unreadable: " + err.Error()))
			return
		}
		if len(cfg.GetEnabledLibraries()) == 0 && !cfg.HasPlex() {
			return
		}
		fmt.Println(infoStyle.Render(fmt.Sprintf("\n[%s] %s", time.Now().Format("15:04"), reason)))
		state.flush(true)
		if err := updateCache(false); err != nil {
			fmt.Println(warningStyle.Render("Scan failed: " + err.Error()))
			return
		}
		fresh, err := cache.Load()
		if err != nil {
			fmt.Println(warningStyle.Render("Failed to reload cache: " + err.Error()))
			return
		}
		state.replace(fresh)
	}
	if !daemonNoLibrary && (len(libs) > 0 || cfg.HasPlex()) {
		if daemonScanOnStart {
			go scan("Scanning libraries…")
		}
		if interval > 0 {
			fmt.Println(infoStyle.Render(fmt.Sprintf("Rescanning every %s.", interval)))
			go func() {
				ticker := time.NewTicker(interval)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						scan("Scheduled library scan…")
					}
				}
			}()
		}
	}
	// Flush playback progress shortly after it changes.
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				state.flush(false)
			}
		}
	}()

	fmt.Println(infoStyle.Render("\nPress Ctrl+C to stop.\n"))
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan
	fmt.Println(infoStyle.Render("\nStopping daemon..."))
	cancel()
	state.flush(true)
	return nil
}
