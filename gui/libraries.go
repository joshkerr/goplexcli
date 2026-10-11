package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshkerr/goplexcli/internal/cache"
	"github.com/joshkerr/goplexcli/internal/config"
	"github.com/joshkerr/goplexcli/internal/library"
	"github.com/joshkerr/goplexcli/internal/libserver"
	"github.com/joshkerr/goplexcli/internal/plex"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// LibraryDTO is a locally indexed folder as shown in Settings.
type LibraryDTO struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Type    string `json:"type"` // "movie" | "show"
	Remote  string `json:"remote"`
	Enabled bool   `json:"enabled"`
	Items   int    `json:"items"` // indexed items under this folder
}

// LibraryInfoDTO summarizes the local-library setup for Settings and Setup.
type LibraryInfoDTO struct {
	Libraries      []LibraryDTO `json:"libraries"`
	ServerName     string       `json:"serverName"`
	ServerURL      string       `json:"serverURL"`
	ServerPort     int          `json:"serverPort"`
	TMDBConfigured bool         `json:"tmdbConfigured"`
	RcloneRemotes  []string     `json:"rcloneRemotes"`
}

// ---- Bound methods: local libraries ----

// GetLibraryInfo returns the configured library folders with item counts.
func (a *App) GetLibraryInfo() LibraryInfoDTO {
	cfg := a.config()
	info := LibraryInfoDTO{
		ServerName:     library.ServerName(cfg),
		ServerURL:      library.ServerURL(cfg.LibraryURL, cfg.LibraryListen),
		TMDBConfigured: strings.TrimSpace(cfg.TMDBAPIKey) != "",
		RcloneRemotes:  a.rcloneRemotes(),
	}
	if a.lib != nil {
		info.ServerPort = a.lib.Port()
	}
	counts := map[string]int{}
	if c := a.media(); c != nil {
		server := info.ServerName
		for i := range c.Media {
			it := &c.Media[i]
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
	info.Libraries = make([]LibraryDTO, 0, len(cfg.Libraries))
	for _, lib := range cfg.Libraries {
		info.Libraries = append(info.Libraries, LibraryDTO{
			Name: lib.Name, Path: lib.Path, Type: lib.Type, Remote: lib.Remote, Enabled: lib.Enabled, Items: counts[lib.Name],
		})
	}
	return info
}

// PickLibraryFolder opens a native folder picker and returns the chosen path
// ("" if cancelled).
func (a *App) PickLibraryFolder() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("window not ready")
	}
	return wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{
		Title:                "Choose a media folder",
		CanCreateDirectories: false,
	})
}

// AddLibrary registers a folder to index. name defaults to the folder's base
// name; remote is an optional rclone path mirroring the folder.
func (a *App) AddLibrary(path, libType, name, remote string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("choose a folder")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	// With a remote, the folder may be a mount that isn't up yet: scanning
	// lists the remote and streaming falls back to rclone.
	st, err := os.Stat(abs)
	if err != nil {
		if strings.TrimSpace(remote) == "" {
			return fmt.Errorf("cannot read %s: %v", abs, err)
		}
	} else if !st.IsDir() {
		return fmt.Errorf("%s is not a folder", abs)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = filepath.Base(abs)
	}
	cfg := a.reloadConfig()
	if _, _, exists := cfg.FindLibrary(name); exists {
		return fmt.Errorf("a library named %q already exists", name)
	}
	lib := config.LibraryFolder{Name: name, Path: abs, Type: strings.TrimSpace(libType), Remote: strings.TrimSpace(remote), Enabled: true}
	if err := lib.Validate(); err != nil {
		return err
	}
	cfg.Libraries = append(cfg.Libraries, lib)
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	a.reloadConfig()
	return nil
}

// RemoveLibrary forgets a folder. Its items stay cached until the next index.
func (a *App) RemoveLibrary(name string) error {
	cfg := a.reloadConfig()
	_, i, ok := cfg.FindLibrary(name)
	if !ok {
		return fmt.Errorf("no library named %q", name)
	}
	cfg.Libraries = append(cfg.Libraries[:i], cfg.Libraries[i+1:]...)
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	a.reloadConfig()
	return nil
}

// SetLibraryEnabled includes or excludes a folder from indexing.
func (a *App) SetLibraryEnabled(name string, enabled bool) error {
	cfg := a.reloadConfig()
	_, i, ok := cfg.FindLibrary(name)
	if !ok {
		return fmt.Errorf("no library named %q", name)
	}
	cfg.Libraries[i].Enabled = enabled
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	a.reloadConfig()
	return nil
}

// rcloneRemotes lists configured rclone remotes (with trailing colons) to
// offer in the library form; empty when rclone is unavailable.
func (a *App) rcloneRemotes() []string {
	bin := a.config().RclonePath
	if bin == "" {
		bin = "rclone"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := execOutput(ctx, bin, "listremotes")
	if err != nil {
		return nil
	}
	var remotes []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			remotes = append(remotes, line)
		}
	}
	return remotes
}

// ---- library server embedded in the GUI ----

// startLibraryServer serves this machine's locally indexed items (streaming,
// posters, playback progress) while the GUI is open, so other devices that
// sync the cache from here can play them.
func (a *App) startLibraryServer() {
	cfg := a.config()
	cacheDir, err := config.GetCacheDir()
	if err != nil {
		return
	}
	a.lib = library.NewService(library.ServiceOptions{
		Listen:        cfg.LibraryListen,
		Token:         cfg.LibraryToken,
		ImageCacheDir: library.ImageCacheDir(cacheDir),
		Version:       version,
		RcloneBin:     cfg.RclonePath,
		Timeline:      a.localTimeline,
	})
	if err := a.lib.Start(); err != nil {
		fmt.Printf("library server unavailable: %v\n", err)
		a.lib = nil
		return
	}
	if c := a.media(); c != nil {
		a.lib.SetItems(c.Media, library.ServerName(cfg))
	}
}

// refreshLibraryServer re-points the library server at the current cache.
func (a *App) refreshLibraryServer() {
	if a.lib == nil {
		return
	}
	if c := a.media(); c != nil {
		a.lib.SetItems(c.Media, library.ServerName(a.config()))
	}
}

// localTimeline records playback progress reported to the library server
// (by this GUI's own mpv tracker or another device) into the cache.
func (a *App) localTimeline(key, state string, timeMs, durationMs int) {
	c := a.media()
	if c == nil {
		return
	}
	server := library.ServerName(a.config())
	a.mediaMu.Lock()
	changed := false
	for i := range c.Media {
		it := &c.Media[i]
		if it.Key == key && it.ServerName == server {
			changed = library.ApplyTimeline(it, state, timeMs, durationMs, time.Now())
			break
		}
	}
	a.mediaMu.Unlock()
	if !changed {
		return
	}
	a.scheduleCacheSave(state == "stopped")
}

// scheduleCacheSave persists the in-memory cache after playback progress
// changes, coalescing bursts of timeline reports into one write.
func (a *App) scheduleCacheSave(immediate bool) {
	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	if a.saveTimer != nil {
		a.saveTimer.Stop()
	}
	delay := 15 * time.Second
	if immediate {
		delay = 0
	}
	a.saveTimer = time.AfterFunc(delay, func() {
		c := a.media()
		if c == nil {
			return
		}
		a.mediaMu.RLock()
		snapshot := &cache.Cache{Media: c.Media, LastUpdated: c.LastUpdated}
		a.mediaMu.RUnlock()
		if err := snapshot.Save(); err != nil {
			fmt.Printf("failed to save playback progress: %v\n", err)
		}
	})
}

// indexLocalLibraries runs the local indexer against the current items and
// returns the merged set, relaying progress as reindex:progress events.
func (a *App) indexLocalLibraries(cfg *config.Config, existing []plex.MediaItem, full bool) ([]plex.MediaItem, *library.Result, error) {
	progress := func(p library.Progress) {
		if a.ctx == nil {
			return
		}
		phase := "Scanning"
		if p.Phase == "match" {
			phase = "Matching"
		}
		wruntime.EventsEmit(a.ctx, "reindex:progress", ReindexProgress{
			Server:    library.ServerName(cfg),
			Library:   p.Library + " · " + phase,
			Items:     p.Done,
			Total:     p.Total,
			ServerNum: 1,
			Servers:   1,
			LibNum:    p.LibNum,
			Libraries: p.Libraries,
		})
	}
	return library.Run(context.Background(), cfg, existing, full, progress, func(format string, args ...any) {
		fmt.Printf("library: "+format+"\n", args...)
	})
}

// execOutput runs a command and returns its stdout.
func execOutput(ctx context.Context, bin string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, bin, args...).Output()
	return string(out), err
}

// migrateFavorites carries stars from items a local index replaced onto the
// new keys and refreshes the frontend's stars when any moved.
func (a *App) migrateFavorites(res *library.Result) {
	if res == nil || len(res.KeyMigration) == 0 {
		return
	}
	moved, err := a.fav.Remap(res.KeyMigration)
	if err != nil {
		fmt.Printf("favorites migration failed: %v\n", err)
		return
	}
	if moved > 0 {
		a.emitFavoritesChanged()
	}
}
