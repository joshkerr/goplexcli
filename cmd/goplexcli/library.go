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

	"github.com/joshkerr/goplexcli/internal/browse"
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
	libraryMatchTMDB  int
	libraryMatchTV    bool
	libraryMatchYear  int
	libraryMatchAuto  bool
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

	searchCmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search TMDB for the right title before fixing a match",
		Long: `Search TMDB for a movie (default) or TV show and print candidate ids
to use with 'library match'. Uses this machine's tmdb_api_key, or asks the
library server that holds your local items when no key is configured here.`,
		Args: cobra.MinimumNArgs(1),
		RunE: runLibrarySearch,
	}
	searchCmd.Flags().BoolVar(&libraryMatchTV, "tv", false, "Search TV shows instead of movies")
	searchCmd.Flags().IntVar(&libraryMatchYear, "year", 0, "Restrict to a release / first-air year")

	matchCmd := &cobra.Command{
		Use:   "match <title or key> --tmdb <id>",
		Short: "Pin a movie or show to a specific TMDB title (fix a wrong match)",
		Long: `Fix a wrong match: pin a locally indexed movie, or a whole show, to the
TMDB id you choose (find it with 'library search'). The items are rebuilt
right away on the library server that owns them, and later scans keep the
choice. Use --auto to drop the pin and go back to automatic matching.

  goplexcli library search "Accused" --tv
  goplexcli library match "Accused" --tv --tmdb 193744
  goplexcli library match "/local/metadata/1a2b3c…" --tmdb 603
  goplexcli library match "Accused" --tv --auto`,
		Args: cobra.MinimumNArgs(1),
		RunE: runLibraryMatch,
	}
	matchCmd.Flags().IntVar(&libraryMatchTMDB, "tmdb", 0, "TMDB id to pin the title to")
	matchCmd.Flags().BoolVar(&libraryMatchTV, "tv", false, "The title is a TV show (match by show name)")
	matchCmd.Flags().BoolVar(&libraryMatchAuto, "auto", false, "Remove the manual match and re-run automatic matching")

	matchesCmd := &cobra.Command{
		Use:   "matches",
		Short: "List manual match fixes on this machine",
		RunE:  runLibraryMatches,
	}

	libraryCmd.AddCommand(addCmd, listCmd, removeCmd, enableCmd, disableCmd, scanCmd, searchCmd, matchCmd, matchesCmd)
	return libraryCmd
}

func runLibrarySearch(cmd *cobra.Command, args []string) error {
	query := strings.Join(args, " ")
	kind := "movie"
	if libraryMatchTV {
		kind = "tv"
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	ctx := context.Background()
	var results []libserver.Candidate
	if strings.TrimSpace(cfg.TMDBAPIKey) != "" {
		cacheDir, err := config.GetCacheDir()
		if err != nil {
			return err
		}
		client, err := library.NewTMDBClient(cfg, cacheDir)
		if err != nil {
			return err
		}
		results, err = library.SearchTitles(ctx, client, kind, query, libraryMatchYear)
		if err != nil {
			return err
		}
	} else {
		serverURL, token, ok := anyLibraryServer(cfg)
		if !ok {
			return fmt.Errorf("no tmdb_api_key configured and no local library items in the cache to ask a library server")
		}
		results, err = libserver.SearchRemote(ctx, serverURL, token, kind, query, libraryMatchYear)
		if err != nil {
			return err
		}
	}
	if len(results) == 0 {
		fmt.Println(warningStyle.Render("No results."))
		return nil
	}
	fmt.Println(titleStyle.Render(fmt.Sprintf("TMDB %s results for %q", kind, query)))
	for _, c := range results {
		year := ""
		if c.Year > 0 {
			year = fmt.Sprintf(" (%d)", c.Year)
		}
		fmt.Printf("%s  %s%s\n", successStyle.Render(fmt.Sprintf("%8d", c.ID)), c.Title, year)
		if ov := strings.TrimSpace(c.Overview); ov != "" {
			if len(ov) > 140 {
				ov = ov[:137] + "…"
			}
			fmt.Println(infoStyle.Render("          " + ov))
		}
	}
	fmt.Println(infoStyle.Render("\nApply one with:  goplexcli library match \"<title>\" --tmdb <id>" + map[bool]string{true: " --tv", false: ""}[libraryMatchTV]))
	return nil
}

func runLibraryMatch(cmd *cobra.Command, args []string) error {
	target := strings.Join(args, " ")
	if !libraryMatchAuto && libraryMatchTMDB <= 0 {
		return fmt.Errorf("pass --tmdb <id> (see 'goplexcli library search') or --auto")
	}
	tmdbID := libraryMatchTMDB
	if libraryMatchAuto {
		tmdbID = 0
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	c, err := cache.Load()
	if err != nil {
		return fmt.Errorf("failed to load cache: %w", err)
	}
	item, err := findLocalItem(c.Media, target, libraryMatchTV)
	if err != nil {
		return err
	}
	// MergeByKey below rewrites the slice in place, so remember the old
	// names before the item pointer starts showing the rebuilt entry.
	oldTitle, oldShow := item.Title, item.ParentTitle
	ctx := context.Background()
	// The library server that owns the item (usually a daemon, possibly on
	// this very machine) rebuilds it so its served cache stays in step. If
	// it's our own library and nothing is serving it, rebuild in place.
	rebuilt, err := libserver.MatchRemote(ctx, item.ServerURL, cfg.TokenForURL(item.ServerURL), item.Key, tmdbID)
	if err != nil {
		if item.ServerName != library.ServerName(cfg) || len(cfg.GetEnabledLibraries()) == 0 {
			return fmt.Errorf("library server %s: %w", item.ServerURL, err)
		}
		fmt.Println(infoStyle.Render("No library server reachable; rebuilding locally."))
		cacheDir, err := config.GetCacheDir()
		if err != nil {
			return err
		}
		rebuilt, err = library.Rematch(ctx, cfg, cacheDir, c.Media, item.Key, tmdbID, func(format string, args ...any) {
			fmt.Println(warningStyle.Render(fmt.Sprintf(format, args...)))
		})
		if err != nil {
			return err
		}
	}
	c.Media = library.MergeByKey(c.Media, rebuilt)
	if err := c.Save(); err != nil {
		return fmt.Errorf("failed to save cache: %w", err)
	}
	if len(rebuilt) == 0 {
		fmt.Println(warningStyle.Render("Nothing was rebuilt."))
		return nil
	}
	first := rebuilt[0]
	if first.Type == "episode" {
		if first.ParentTitle != oldShow {
			// Shows are favorited by name; follow the rename.
			_, _ = favorites.NewStore().Remap(map[string]string{"show:" + oldShow: "show:" + first.ParentTitle})
		}
		fmt.Println(successStyle.Render(fmt.Sprintf("✓ %q now matches %q (%d episodes rebuilt)", oldShow, first.ParentTitle, len(rebuilt))))
	} else {
		fmt.Println(successStyle.Render(fmt.Sprintf("✓ %q now matches %q (%d)", oldTitle, first.Title, first.Year)))
	}
	return nil
}

func runLibraryMatches(cmd *cobra.Command, args []string) error {
	cacheDir, err := config.GetCacheDir()
	if err != nil {
		return err
	}
	ov, err := library.LoadOverrides(cacheDir)
	if err != nil {
		return err
	}
	list := ov.List()
	fmt.Println(titleStyle.Render("Manual Matches"))
	if len(list) == 0 {
		fmt.Println(infoStyle.Render("None. Fix one with 'goplexcli library match'."))
		return nil
	}
	for _, m := range list {
		fmt.Printf("%s %s → TMDB %d   %s\n", successStyle.Render(m.Kind), m.Title, m.TMDB, infoStyle.Render(m.Key))
	}
	return nil
}

// findLocalItem resolves a title or key to a locally indexed item: an exact
// key, else a case-insensitive title match (show name when tv is set).
func findLocalItem(items []plex.MediaItem, target string, tv bool) (*plex.MediaItem, error) {
	if libserver.IsLocalKey(target) {
		for i := range items {
			if items[i].Key == target {
				return &items[i], nil
			}
		}
		return nil, fmt.Errorf("no item with key %s in the cache", target)
	}
	want := strings.ToLower(strings.TrimSpace(target))
	var matches []*plex.MediaItem
	seen := map[string]bool{}
	for i := range items {
		it := &items[i]
		if !libserver.IsLocalKey(it.Key) {
			continue
		}
		if tv {
			if it.Type == "episode" && strings.ToLower(it.ParentTitle) == want && !seen[it.ServerName+"|"+it.ParentTitle] {
				seen[it.ServerName+"|"+it.ParentTitle] = true
				matches = append(matches, it)
			}
		} else if it.Type == "movie" && strings.ToLower(it.Title) == want {
			matches = append(matches, it)
		}
	}
	switch len(matches) {
	case 0:
		kind := "movie"
		if tv {
			kind = "show"
		}
		return nil, fmt.Errorf("no locally indexed %s titled %q (titles are matched exactly on the current name; pass the item key instead — 'goplexcli library matches' lists keys of pinned titles)", kind, target)
	case 1:
		return matches[0], nil
	}
	fmt.Println(warningStyle.Render(fmt.Sprintf("%d items share that title; pass a key to pick one:", len(matches))))
	for _, m := range matches {
		fmt.Printf("  %s  %s (%d)  %s\n", m.Key, m.Title, m.Year, infoStyle.Render(m.FilePath))
	}
	return nil, fmt.Errorf("ambiguous title")
}

// anyLibraryServer returns the server URL of the first locally indexed item
// in the cache, so machines without a TMDB key can search through it.
func anyLibraryServer(cfg *config.Config) (string, string, bool) {
	c, err := cache.Load()
	if err != nil {
		return "", "", false
	}
	for i := range c.Media {
		if libserver.IsLocalKey(c.Media[i].Key) && c.Media[i].ServerURL != "" {
			u := c.Media[i].ServerURL
			return u, cfg.TokenForURL(u), true
		}
	}
	return "", "", false
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
	// With a remote, the folder may be a mount that isn't up yet: scanning
	// lists the remote and streaming falls back to rclone, so only warn.
	st, err := os.Stat(p)
	if err != nil {
		if strings.TrimSpace(libraryAddRemote) == "" {
			return fmt.Errorf("cannot read %s: %w", p, err)
		}
		fmt.Println(warningStyle.Render(fmt.Sprintf("Note: %s is not accessible right now; the rclone remote will be used until it is mounted.", p)))
	} else if !st.IsDir() {
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
	// scanning serializes scans so a signal or timer can't start a second
	// one while the first is still running.
	scanning sync.Mutex
	// diskMod is the cache file's mtime after our own last load or save;
	// a different mtime means another process (a manual 'library scan', a
	// LAN sync pull) rewrote it and the in-memory copy should be reloaded.
	diskMod time.Time
}

// noteDisk records the cache file's current mtime as "ours".
func (d *daemonState) noteDisk() {
	if p, err := cache.GetCachePath(); err == nil {
		if st, err := os.Stat(p); err == nil {
			d.diskMod = st.ModTime()
		}
	}
}

// diskChanged reports whether the cache file was rewritten by someone else.
func (d *daemonState) diskChanged() bool {
	p, err := cache.GetCachePath()
	if err != nil {
		return false
	}
	st, err := os.Stat(p)
	if err != nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return !st.ModTime().Equal(d.diskMod)
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
	d.noteDisk()
}

// replace installs a freshly indexed cache (already saved to disk).
func (d *daemonState) replace(c *cache.Cache) {
	d.mu.Lock()
	d.cache = c
	d.dirty = false
	d.saveAt = time.Time{}
	d.noteDisk()
	d.mu.Unlock()
	if d.service != nil {
		d.service.SetItems(c.Media, d.server)
	}
}

// rematch pins one movie or show to a TMDB id (0 = automatic), rebuilds its
// items, and installs them in the served cache.
func (d *daemonState) rematch(ctx context.Context, cacheDir, key string, tmdbID int) ([]plex.MediaItem, error) {
	if !d.scanning.TryLock() {
		return nil, fmt.Errorf("a library scan is running; try again when it finishes")
	}
	defer d.scanning.Unlock()
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cache == nil {
		return nil, fmt.Errorf("no cache loaded")
	}
	rebuilt, err := library.Rematch(ctx, cfg, cacheDir, d.cache.Media, key, tmdbID, func(format string, args ...any) {
		fmt.Println(warningStyle.Render(fmt.Sprintf(format, args...)))
	})
	if err != nil {
		return nil, err
	}
	d.cache.Media = library.MergeByKey(d.cache.Media, rebuilt)
	if err := d.cache.Save(); err != nil {
		return nil, fmt.Errorf("save cache: %w", err)
	}
	d.dirty = false
	d.noteDisk()
	if d.service != nil {
		d.service.SetItems(d.cache.Media, d.server)
	}
	what := "automatic match"
	if tmdbID > 0 {
		what = fmt.Sprintf("TMDB %d", tmdbID)
	}
	if len(rebuilt) > 0 {
		title := rebuilt[0].Title
		if rebuilt[0].Type == "episode" {
			title = rebuilt[0].ParentTitle
		}
		fmt.Println(successStyle.Render(fmt.Sprintf("[%s] Rematched %q to %s (%d item(s) rebuilt)", time.Now().Format("15:04"), title, what, len(rebuilt))))
	}
	return rebuilt, nil
}

// view runs fn with the served items under the state lock (browse API).
func (d *daemonState) view(fn func(items []plex.MediaItem)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cache == nil {
		fn(nil)
		return
	}
	fn(d.cache.Media)
}

// updated reports when the served cache was last indexed.
func (d *daemonState) updated() time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cache == nil {
		return time.Time{}
	}
	return d.cache.LastUpdated
}

// setWatched marks a local item watched (counted, resume cleared) or
// unwatched, and saves soon after.
func (d *daemonState) setWatched(key string, watched bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cache == nil {
		return browse.ErrNotFound
	}
	for i := range d.cache.Media {
		it := &d.cache.Media[i]
		if it.Key != key || it.ServerName != d.server {
			continue
		}
		it.ViewOffset = 0
		if watched {
			if it.ViewCount == 0 {
				it.ViewCount = 1
			}
			it.LastViewedAt = time.Now().Unix()
		} else {
			it.ViewCount = 0
		}
		d.dirty = true
		d.saveAt = time.Now()
		return nil
	}
	return browse.ErrNotFound
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

	// One favorites store, shared by the LAN sync server and the browse API,
	// so a star set from the Android app reaches peers and vice versa.
	favStore := favorites.NewStore()

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
			Search: func(ctx context.Context, kind, query string, year int) ([]libserver.Candidate, error) {
				cfg, err := config.Load()
				if err != nil {
					return nil, err
				}
				client, err := library.NewTMDBClient(cfg, cacheDir)
				if err != nil {
					return nil, err
				}
				return library.SearchTitles(ctx, client, kind, query, year)
			},
			Match: func(ctx context.Context, key string, tmdbID int) ([]plex.MediaItem, error) {
				return state.rematch(ctx, cacheDir, key, tmdbID)
			},
			Browse: browse.NewHandler(browse.Source{
				View:       state.view,
				Favorites:  favStore,
				SetWatched: state.setWatched,
				Name:       state.server,
				Version:    version,
				Updated:    state.updated,
			}),
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
		srv.ServeFavorites(favStore, nil)
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
	reload := func(reason string) {
		fresh, err := cache.Load()
		if err != nil {
			fmt.Println(warningStyle.Render("Failed to reload cache: " + err.Error()))
			return
		}
		state.replace(fresh)
		fmt.Println(infoStyle.Render(fmt.Sprintf("[%s] %s: now serving %d item(s), %d local", time.Now().Format("15:04"), reason, len(fresh.Media), state.service.Count())))
	}
	scanMode := func(reason string, full bool) {
		if !state.scanning.TryLock() {
			fmt.Println(infoStyle.Render("A scan is already running; request ignored."))
			return
		}
		defer state.scanning.Unlock()
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
		if err := updateCache(full); err != nil {
			fmt.Println(warningStyle.Render("Scan failed: " + err.Error()))
			return
		}
		reload("Scan finished")
	}
	scan := func(reason string) { scanMode(reason, false) }
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
	// Flush playback progress shortly after it changes, and pick up a cache
	// rewritten by another process (a manual 'library scan' or a LAN sync
	// pull) so the library server resolves the new items without a restart.
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				state.flush(false)
				if !daemonNoLibrary && state.diskChanged() && state.scanning.TryLock() {
					reload("Cache changed on disk")
					state.scanning.Unlock()
				}
			}
		}
	}()

	fmt.Println(infoStyle.Render("\nPress Ctrl+C to stop. SIGHUP rescans now, SIGUSR1 re-enriches everything (systemctl --user kill -s HUP goplexcli-daemon).\n"))
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	full := fullRescanSignal()
	for {
		select {
		case <-hup:
			if daemonNoLibrary {
				continue
			}
			go scan("Rescan requested (SIGHUP)…")
		case <-full:
			if daemonNoLibrary {
				continue
			}
			go scanMode("Full re-enrichment requested (SIGUSR1)…", true)
		case <-sigChan:
			fmt.Println(infoStyle.Render("\nStopping daemon..."))
			cancel()
			state.flush(true)
			return nil
		}
	}
}
