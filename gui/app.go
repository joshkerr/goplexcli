package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/joshkerr/goplexcli/internal/cache"
	"github.com/joshkerr/goplexcli/internal/config"
	"github.com/joshkerr/goplexcli/internal/download"
	"github.com/joshkerr/goplexcli/internal/player"
	"github.com/joshkerr/goplexcli/internal/plex"
)

// App is the Wails backend. Its exported methods are bound into the frontend as
// async JS functions. It holds the Wails context (needed to emit events) and a
// cached copy of the user config. All heavy lifting is delegated to the shared
// internal packages.
type App struct {
	ctx context.Context

	mu  sync.RWMutex
	cfg *config.Config

	// pendingToken/pendingUser hold the auth token captured by Login until the
	// user confirms a server selection via SaveServers.
	pendingToken string
	pendingUser  string

	// busy guards long-running, mutually-exclusive operations (reindex) so the
	// UI can't kick off two at once.
	busy sync.Mutex

	// mediaMu/media memoize the parsed media cache in memory. The on-disk
	// media.json can be tens of MB for large libraries (20k+ items), so loading
	// and JSON-decoding it on every browse/search call would make the UI
	// unresponsive. We parse it once and reuse it, refreshing only after a
	// reindex or an explicit invalidation.
	mediaMu      sync.RWMutex
	mediaCache   *cache.Cache
}

// NewApp creates a new App. Config is loaded lazily in startup.
func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if cfg, err := config.Load(); err == nil {
		a.mu.Lock()
		a.cfg = cfg
		a.mu.Unlock()
	}
}

func (a *App) shutdown(ctx context.Context) {}

// config returns the in-memory config, loading it from disk if needed.
func (a *App) config() *config.Config {
	a.mu.RLock()
	cfg := a.cfg
	a.mu.RUnlock()
	if cfg != nil {
		return cfg
	}
	loaded, err := config.Load()
	if err != nil || loaded == nil {
		loaded = &config.Config{}
	}
	a.mu.Lock()
	a.cfg = loaded
	a.mu.Unlock()
	return loaded
}

// reloadConfig forces a fresh read from disk into memory.
func (a *App) reloadConfig() *config.Config {
	cfg, err := config.Load()
	if err != nil || cfg == nil {
		cfg = &config.Config{}
	}
	a.mu.Lock()
	a.cfg = cfg
	a.mu.Unlock()
	return cfg
}

// ---- DTOs exposed to the frontend ----

// StatusDTO describes the app's readiness on launch so the frontend can route
// to login, first-run indexing, or the library.
type StatusDTO struct {
	Configured      bool   `json:"configured"`
	HasCache        bool   `json:"hasCache"`
	CacheCount      int    `json:"cacheCount"`
	LastUpdated     string `json:"lastUpdated"`
	MovieCount      int    `json:"movieCount"`
	ShowCount       int    `json:"showCount"`
	EpisodeCount    int    `json:"episodeCount"`
	MPVAvailable    bool   `json:"mpvAvailable"`
	RcloneAvailable bool   `json:"rcloneAvailable"`
	ServerNames     []string `json:"serverNames"`
}

// ServerDTO is a Plex server discovered during login.
type ServerDTO struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Local bool   `json:"local"`
	Owned bool   `json:"owned"`
}

// ServerSelection is a server the user chose to enable, sent back on save.
type ServerSelection struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// ConfigDTO is the subset of config the Settings view edits.
type ConfigDTO struct {
	DownloadDir string `json:"downloadDir"`
	MPVPath     string `json:"mpvPath"`
	RclonePath  string `json:"rclonePath"`
}

// ---- Bound methods: status & config ----

// GetStatus reports configuration and cache readiness for routing on launch.
func (a *App) GetStatus() StatusDTO {
	cfg := a.reloadConfig()

	dto := StatusDTO{
		MPVAvailable:    player.IsAvailable(cfg.MPVPath),
		RcloneAvailable: download.IsAvailable(cfg.RclonePath),
	}
	dto.Configured = cfg.Validate() == nil
	for _, s := range cfg.Servers {
		dto.ServerNames = append(dto.ServerNames, s.Name)
	}

	if mediaCache := a.media(); mediaCache != nil {
		dto.CacheCount = len(mediaCache.Media)
		dto.HasCache = dto.CacheCount > 0
		if !mediaCache.LastUpdated.IsZero() {
			dto.LastUpdated = mediaCache.LastUpdated.Format("Jan 2, 2006 3:04 PM")
		}
		shows := map[string]struct{}{}
		for i := range mediaCache.Media {
			switch mediaCache.Media[i].Type {
			case "movie":
				dto.MovieCount++
			case "episode":
				dto.EpisodeCount++
				if mediaCache.Media[i].ParentTitle != "" {
					shows[mediaCache.Media[i].ParentTitle] = struct{}{}
				}
			}
		}
		dto.ShowCount = len(shows)
	}

	return dto
}

// Login authenticates with Plex and returns the available servers. It does not
// persist anything; the frontend follows up with SaveServers.
func (a *App) Login(username, password string) ([]ServerDTO, error) {
	if username == "" || password == "" {
		return nil, fmt.Errorf("username and password are required")
	}
	token, servers, err := plex.Authenticate(username, password)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.pendingToken = token
	a.pendingUser = username
	a.mu.Unlock()

	out := make([]ServerDTO, 0, len(servers))
	for _, s := range servers {
		out = append(out, ServerDTO{Name: s.Name, URL: s.URL, Local: s.Local, Owned: s.Owned})
	}
	return out, nil
}

// SaveServers persists the chosen servers (all enabled) plus the auth token
// captured during the most recent Login.
func (a *App) SaveServers(selections []ServerSelection) error {
	a.mu.RLock()
	token := a.pendingToken
	user := a.pendingUser
	a.mu.RUnlock()
	if token == "" {
		return fmt.Errorf("not logged in - call Login first")
	}
	if len(selections) == 0 {
		return fmt.Errorf("select at least one server")
	}

	cfg := a.config()
	cfg.PlexToken = token
	if user != "" {
		cfg.PlexUsername = user
	}
	cfg.Servers = cfg.Servers[:0]
	for _, s := range selections {
		cfg.Servers = append(cfg.Servers, config.PlexServer{Name: s.Name, URL: s.URL, Enabled: true})
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	a.reloadConfig()
	return nil
}

// GetConfig returns the editable settings.
func (a *App) GetConfig() ConfigDTO {
	cfg := a.config()
	return ConfigDTO{
		DownloadDir: cfg.DownloadDir,
		MPVPath:     cfg.MPVPath,
		RclonePath:  cfg.RclonePath,
	}
}

// SaveConfig updates the editable settings and persists them.
func (a *App) SaveConfig(dto ConfigDTO) error {
	cfg := a.config()
	cfg.DownloadDir = dto.DownloadDir
	cfg.MPVPath = dto.MPVPath
	cfg.RclonePath = dto.RclonePath
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	a.reloadConfig()
	return nil
}
