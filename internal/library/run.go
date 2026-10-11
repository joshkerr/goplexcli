package library

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/joshkerr/goplexcli/internal/config"
	"github.com/joshkerr/goplexcli/internal/plex"
)

// tmdbCacheTTL bounds how long cached TMDB responses are trusted; artwork
// and ratings drift slowly, so a month keeps reindexes cheap without going
// stale forever.
const tmdbCacheTTL = 30 * 24 * time.Hour

// ServerName returns the label stamped on this machine's local items.
func ServerName(cfg *config.Config) string {
	if n := strings.TrimSpace(cfg.LibraryName); n != "" {
		return n
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "local"
	}
	return host
}

// Run indexes every enabled library in cfg and returns the cache contents
// with this machine's local items replaced by the fresh set (items from Plex
// servers or other machines are left alone). full forces re-enrichment of
// unchanged files. It returns the existing items untouched when no library
// is enabled.
func Run(ctx context.Context, cfg *config.Config, existing []plex.MediaItem, full bool, progress ProgressFunc, logf func(string, ...any)) ([]plex.MediaItem, *Result, error) {
	libs := cfg.GetEnabledLibraries()
	if len(libs) == 0 {
		return existing, &Result{}, nil
	}
	cacheDir, err := config.GetCacheDir()
	if err != nil {
		return nil, nil, err
	}

	client, err := NewTMDBClient(cfg, cacheDir)
	if err != nil {
		return nil, nil, err
	}
	if client == nil && logf != nil {
		logf("no tmdb_api_key configured: indexing titles from file names only")
	}

	overrides, err := LoadOverrides(cacheDir)
	if err != nil {
		return nil, nil, err
	}
	serverName := ServerName(cfg)
	opts := Options{
		Overrides:    overrides,
		TMDB:         client,
		ServerName:   serverName,
		ServerURL:    ServerURL(cfg.LibraryURL, cfg.LibraryListen),
		PathMappings: cfg.PathMappings,
		Scan:         ScanOptions{RcloneBin: cfg.RclonePath},
		Probe:        cfg.LibraryProbe,
		Force:        full,
		Logf:         logf,
	}
	if opts.Probe {
		if _, err := exec.LookPath("ffprobe"); err != nil {
			opts.Probe = false
			if logf != nil {
				logf("library_probe is on but ffprobe was not found; using TMDB runtimes")
			}
		}
	}

	res, err := Index(ctx, libs, existing, opts, progress)
	if err != nil {
		return nil, nil, err
	}
	SortItems(res.Items)
	res.KeyMigration = KeyMigration(existing, res.Items)
	kept, dropped := DropDisabledServers(cfg, existing)
	if dropped > 0 && logf != nil {
		logf("dropped %d item(s) from disabled Plex servers", dropped)
	}
	return ReplaceLocal(kept, res.Items, serverName), res, nil
}

// ReplaceLocal returns existing with every item belonging to serverName's
// library server swapped for the fresh set, preserving the order of the rest.
func ReplaceLocal(existing, fresh []plex.MediaItem, serverName string) []plex.MediaItem {
	out := make([]plex.MediaItem, 0, len(existing)+len(fresh))
	for _, it := range existing {
		if it.ServerName == serverName {
			continue
		}
		out = append(out, it)
	}
	return append(out, fresh...)
}

// KeyMigration maps the keys of items that were replaced by local items for
// the same file (matched by rclone path, else by file path) to the new keys,
// so favorites and other per-key state can follow the switch from a Plex
// server to a local library.
func KeyMigration(old, fresh []plex.MediaItem) map[string]string {
	byPath := make(map[string]string, len(fresh))
	for i := range fresh {
		it := &fresh[i]
		if it.RclonePath != "" {
			byPath["r:"+it.RclonePath] = it.Key
		}
		if it.FilePath != "" {
			byPath["f:"+it.FilePath] = it.Key
		}
	}
	out := map[string]string{}
	for i := range old {
		it := &old[i]
		var newKey string
		if it.RclonePath != "" {
			newKey = byPath["r:"+it.RclonePath]
		}
		if newKey == "" && it.FilePath != "" {
			newKey = byPath["f:"+it.FilePath]
		}
		if newKey != "" && newKey != it.Key {
			out[it.Key] = newKey
		}
	}
	return out
}

// DropDisabledServers removes items that came from Plex servers which are
// configured but disabled, so turning a server off (for instance after
// moving its folders to a local library) also retires its cached items on
// the next update. Items from unknown servers are left alone.
func DropDisabledServers(cfg *config.Config, items []plex.MediaItem) ([]plex.MediaItem, int) {
	disabled := map[string]bool{}
	for _, s := range cfg.Servers {
		if !s.Enabled {
			disabled[s.Name] = true
			disabled[strings.TrimRight(s.URL, "/")] = true
		}
	}
	if len(disabled) == 0 {
		return items, 0
	}
	out := items[:0:0]
	dropped := 0
	for _, it := range items {
		if disabled[it.ServerName] || disabled[strings.TrimRight(it.ServerURL, "/")] {
			dropped++
			continue
		}
		out = append(out, it)
	}
	return out, dropped
}
