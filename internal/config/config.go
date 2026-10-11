// Package config provides configuration management for goplexcli.
// It handles loading, saving, and validating user configuration including
// Plex server credentials, multi-server support, and paths to external tools.
// Configuration is stored in a platform-specific config directory.
package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// PlexServer represents a configured Plex server.
// Multiple servers can be configured, with each individually enabled or disabled.
type PlexServer struct {
	// Name is a human-readable identifier for the server
	Name string `json:"name"`
	// URL is the base URL of the Plex server (e.g., "http://192.168.1.100:32400")
	URL string `json:"url"`
	// Token is this server's access token from plex.tv. Shared (non-owner)
	// accounts cannot use their account token against a server — the server
	// returns 401 — so the per-server token must be used when present. Empty
	// for configs saved before this field existed; callers fall back to the
	// account-wide PlexToken (see Config.TokenForServer).
	Token string `json:"token,omitempty"`
	// Enabled determines whether this server is included when indexing media
	Enabled bool `json:"enabled"`
}

// Config holds all user configuration for goplexcli.
// It supports both legacy single-server configurations and newer multi-server setups.
type Config struct {
	// Legacy single-server fields (maintained for backward compatibility)
	PlexURL      string `json:"plex_url,omitempty"`
	PlexToken    string `json:"plex_token"`
	PlexUsername string `json:"plex_username,omitempty"`

	// Servers holds multi-server configuration. Each server can be independently
	// enabled or disabled for indexing.
	Servers []PlexServer `json:"servers,omitempty"`

	// Tool paths allow overriding the default paths to external binaries.
	// If empty, the system PATH is searched.
	MPVPath    string `json:"mpv_path,omitempty"`
	RclonePath string `json:"rclone_path,omitempty"`
	FzfPath    string `json:"fzf_path,omitempty"`

	// RclonecpPath optionally points at the rclonecp GUI binary used by the
	// GUI's "Send to rclonecp" handoff. If empty, PATH and conventional
	// install locations are searched.
	RclonecpPath string `json:"rclonecp_path,omitempty"`

	// AutoSendRclonecp forwards every completed GUI download to rclonecp
	// automatically, in addition to the manual per-download button.
	AutoSendRclonecp bool `json:"auto_send_rclonecp,omitempty"`

	// DownloadDir is the destination directory for downloads. A leading "~"
	// is expanded to the user's home directory. If empty, downloads go to the
	// current working directory. Can be overridden per-run with --dest.
	DownloadDir string `json:"download_dir,omitempty"`

	// SlowDeviceMode buffers each multi-thread stream's writes into large
	// sequential chunks (--multi-thread-write-buffer-size=128M). SD cards and
	// USB drives collapse under concurrent random writes without this.
	SlowDeviceMode bool `json:"slow_device_mode,omitempty"`

	// SortDownloads files GUI downloads into subfolders of the download
	// directory by Plex type: movies into "Movies", episodes into
	// "TV Shows/<show>". The layout matches what gowebdav's Movies/TV tabs
	// auto-detect, so a shared library folder sorts itself.
	SortDownloads bool `json:"sort_downloads,omitempty"`

	// SyncPeer is the hostname or IP (optionally host:port) of another computer
	// on the LAN to pull the media cache from ("Sync from LAN"). When set, sync
	// goes straight to this host; when empty, mDNS auto-discovery is used.
	SyncPeer string `json:"sync_peer,omitempty"`

	// PathMappings translate Plex on-disk file paths into rclone remote paths
	// during cache indexing. If empty, a legacy heuristic is used.
	PathMappings []PathMapping `json:"path_mappings,omitempty"`

	// WebDAVUser and WebDAVPass are the shared Basic Auth credentials used for
	// every gowebdav server discovered on the LAN (the "transfer to webdav"
	// action). gowebdav servers advertise themselves via mDNS but do not
	// advertise credentials, so the same user/pass is assumed across all of
	// them. Empty values mean connect anonymously.
	WebDAVUser string `json:"webdav_user,omitempty"`
	WebDAVPass string `json:"webdav_pass,omitempty"`
	// WebDAVDir is an optional sub-path under the server root to upload into
	// (e.g. "incoming"). Empty uploads to the server root.
	WebDAVDir string `json:"webdav_dir,omitempty"`

	// OutplayerTargets are user-defined Outplayer "Wi-Fi transfer" destinations.
	// Unlike gowebdav servers they are not discovered on the LAN; each is
	// configured explicitly with a base URL. Individually enabled or disabled;
	// disabled targets are hidden from the transfer menu but kept in config.
	OutplayerTargets []OutplayerTarget `json:"outplayer_targets,omitempty"`

	// WebDAVTargets are user-defined WebDAV upload destinations with their own
	// per-server credentials. Unlike gowebdav servers (discovered via mDNS and
	// sharing WebDAVUser/WebDAVPass), each of these is configured explicitly
	// with a full base URL (scheme, host, port) and its own username/password.
	WebDAVTargets []WebDAVTarget `json:"webdav_targets,omitempty"`

	// ServeListen/ServeName/ServeToken configure this machine's `goplexcli
	// serve` download daemon. Listen defaults to ":47821", name to the
	// hostname. Token is the bearer token remote GUIs must present; it is
	// generated on the first `serve` run and kept stable across restarts so
	// registered clients don't break.
	ServeListen string `json:"serve_listen,omitempty"`
	ServeName   string `json:"serve_name,omitempty"`
	ServeToken  string `json:"serve_token,omitempty"`

	// RemoteServers are `goplexcli serve` daemons on other machines this GUI
	// can send downloads to. Each is registered manually with the URL and
	// token the daemon prints at startup. Individually enabled or disabled;
	// disabled servers are hidden from the download-target picker but kept.
	RemoteServers []RemoteServer `json:"remote_servers,omitempty"`

	// Libraries are local media folders indexed by goplexcli itself, without a
	// Plex server. Each folder is scanned for video files, which are matched
	// against TMDB for metadata. The folder may be an rclone mount; set Remote
	// so downloads/transfers can address the same files via rclone.
	Libraries []LibraryFolder `json:"libraries,omitempty"`

	// LibraryListen is the address the built-in library server (streaming,
	// posters, playback progress for locally indexed items) binds. Defaults
	// to ":47822".
	LibraryListen string `json:"library_listen,omitempty"`
	// LibraryURL is the base URL other devices use to reach this machine's
	// library server; it is stamped on every locally indexed item as its
	// ServerURL, so it must be reachable from wherever the cache is used
	// (including peers that pull it over LAN sync). Defaults to the first
	// LAN IPv4 address plus the library port.
	LibraryURL string `json:"library_url,omitempty"`
	// LibraryName labels locally indexed items (their ServerName). Defaults to
	// the hostname.
	LibraryName string `json:"library_name,omitempty"`
	// LibraryToken, when set, is required as X-Plex-Token on every library
	// server request. Empty means the server is open to the LAN (like the
	// cache sync server).
	LibraryToken string `json:"library_token,omitempty"`
	// LibraryProbe runs ffprobe on each new file during indexing to record the
	// exact duration and audio languages. Off by default because it opens every
	// file, which is slow on cloud mounts; TMDB runtimes are used instead.
	LibraryProbe bool `json:"library_probe,omitempty"`
	// LibraryScanInterval is how often the daemon rescans libraries, as a
	// Go duration string ("1h"). Empty means the daemon default (1h); "0"
	// disables periodic rescans.
	LibraryScanInterval string `json:"library_scan_interval,omitempty"`

	// TMDBAPIKey is a The Movie Database API key (v3 key or v4 read token)
	// used by the local indexer for metadata and artwork. Without it, items
	// are indexed with titles parsed from file names only.
	TMDBAPIKey string `json:"tmdb_api_key,omitempty"`

	// Artwork controls cover-art embedding for downloads: "" or "auto"
	// embeds art (Apple TV first, then TMDB, then Netflix) into every
	// completed download; "off" disables it.
	Artwork string `json:"artwork,omitempty"`
	// ArtworkOrientation picks "horizontal" (16:9 art with the title
	// treatment, the default) or "vertical" (2:3 poster).
	ArtworkOrientation string `json:"artwork_orientation,omitempty"`
	// AppleStorefront is the Apple TV catalog storefront id (default US,
	// 143441).
	AppleStorefront string `json:"apple_storefront,omitempty"`
	// FFmpegPath, FFprobePath and MkvPropEditPath override tool locations
	// for artwork embedding; empty searches PATH.
	FFmpegPath      string `json:"ffmpeg_path,omitempty"`
	FFprobePath     string `json:"ffprobe_path,omitempty"`
	MkvPropEditPath string `json:"mkvpropedit_path,omitempty"`
}

// ArtworkEnabled reports whether downloads get cover art embedded.
func (c *Config) ArtworkEnabled() bool {
	return !strings.EqualFold(strings.TrimSpace(c.Artwork), "off")
}

// LibraryFolder is one locally indexed media folder.
type LibraryFolder struct {
	// Name is a human-readable label (e.g. "Movies", "TV 4K"). It also keys
	// item identity, so renaming a library changes its items' keys.
	Name string `json:"name"`
	// Path is the local folder to scan (absolute; may be an rclone mount).
	Path string `json:"path"`
	// Type is "movie" or "show" and decides how files are interpreted.
	Type string `json:"type"`
	// Remote is the optional rclone path that corresponds to Path
	// (e.g. "gdrive:Media/Movies"), used to build each item's RclonePath so
	// downloads and transfers can fetch the file via rclone. When set and
	// rclone is available, the scan lists the remote directly (much faster
	// than walking a FUSE mount for cloud storage). Empty means the folder is
	// only reachable through this machine's library server.
	Remote string `json:"remote,omitempty"`
	// Enabled determines whether this folder is included when indexing.
	Enabled bool `json:"enabled"`
}

// RemoteServer is a registered `goplexcli serve` download daemon on another
// machine. Downloads sent to it run there independently of this GUI.
type RemoteServer struct {
	// Name is a human-readable identifier shown in the download-target picker
	// (e.g. "media-server"). It must not contain "!", which namespaces remote
	// job IDs ("<name>!<id>") in the merged Downloads list.
	Name string `json:"name"`
	// URL is the daemon's base URL as printed in its startup banner,
	// e.g. "http://192.168.1.50:47821".
	URL string `json:"url"`
	// Token is the daemon's bearer token, also printed in its startup banner.
	Token string `json:"token"`
	// Enabled determines whether this server appears in the target picker.
	Enabled bool `json:"enabled"`
}

// Validate checks that a remote server has the required fields and a usable
// URL. It is called when saving so misconfiguration is caught early.
func (r RemoteServer) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("name is required")
	}
	if strings.Contains(r.Name, "!") {
		return fmt.Errorf("name must not contain %q", "!")
	}
	if r.URL == "" {
		return fmt.Errorf("URL is required")
	}
	if err := validateServerURL(r.URL); err != nil {
		return err
	}
	return nil
}

// WebDAVTarget represents an explicitly configured WebDAV server used as a
// transfer destination. Uploads go through rclone's WebDAV backend, so any
// standard WebDAV server works.
type WebDAVTarget struct {
	// Name is a human-readable identifier for the target (e.g. "office-nas").
	Name string `json:"name"`
	// URL is the WebDAV base URL including scheme and port,
	// e.g. "http://192.168.1.50:5005".
	URL string `json:"url"`
	// User and Pass are this server's Basic Auth credentials. Pass is stored
	// in plaintext (same as the shared WebDAVPass); empty values connect
	// anonymously.
	User string `json:"user,omitempty"`
	Pass string `json:"pass,omitempty"`
	// Dir is an optional sub-path under the server root to upload into.
	Dir string `json:"dir,omitempty"`
	// Vendor is the rclone WebDAV vendor ("other", "nextcloud", "owncloud",
	// "sharepoint", ...). Empty means "other", which suits generic servers.
	Vendor string `json:"vendor,omitempty"`
	// Enabled determines whether this target appears in the transfer menu.
	Enabled bool `json:"enabled"`
}

// Validate checks that a WebDAV target has the required fields and a usable
// URL. It is called when adding a target so misconfiguration is caught early.
func (t WebDAVTarget) Validate() error {
	if t.Name == "" {
		return fmt.Errorf("name is required")
	}
	if t.URL == "" {
		return fmt.Errorf("URL is required")
	}
	if err := validateServerURL(t.URL); err != nil {
		return err
	}
	return nil
}

// OutplayerTarget represents an Outplayer "Wi-Fi transfer" destination.
// Outplayer is an iOS media player whose Wi-Fi transfer feature runs a small
// HTTP server (GCDWebUploader) that accepts multipart file uploads. Multiple
// targets can be configured and each individually enabled or disabled.
type OutplayerTarget struct {
	// Name is a human-readable identifier for the target (e.g. "iPhone").
	Name string `json:"name"`
	// URL is the base URL of the Outplayer Wi-Fi transfer server, as shown in
	// the app (e.g. "http://192.168.0.34").
	URL string `json:"url"`
	// Dir is the destination folder on the target to upload into. Empty means
	// the server root. Note that some built-in folders (e.g. "Inbox") are not
	// writable, so the root is the safe default.
	Dir string `json:"dir,omitempty"`
	// Enabled determines whether this target appears in the transfer menu.
	Enabled bool `json:"enabled"`
}

// Validate checks that an Outplayer target has the required fields and a usable
// URL. It is called when adding a target so misconfiguration is caught early.
func (t OutplayerTarget) Validate() error {
	if t.Name == "" {
		return fmt.Errorf("name is required")
	}
	if t.URL == "" {
		return fmt.Errorf("URL is required")
	}
	if err := validateServerURL(t.URL); err != nil {
		return err
	}
	return nil
}

// PathMapping translates a Plex on-disk file path prefix into an rclone remote.
// A file path beginning with Prefix has that prefix replaced by Remote. For
// example {Prefix: "/home/joshkerr/plexcloudservers2/", Remote:
// "plexcloudservers2:"} turns
// "/home/joshkerr/plexcloudservers2/Media/TV/x.mkv" into
// "plexcloudservers2:Media/TV/x.mkv".
type PathMapping struct {
	Prefix string `json:"prefix"`
	Remote string `json:"remote"`
}

// GetConfigDir returns the platform-specific config directory
func GetConfigDir() (string, error) {
	// An explicit override wins on every platform: it lets a second instance
	// (tests, a daemon under another account) keep its own config and cache.
	if dir := os.Getenv("GOPLEXCLI_CONFIG_DIR"); dir != "" {
		return dir, nil
	}

	var baseDir string

	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		baseDir = filepath.Join(home, ".config")
	case "windows":
		baseDir = os.Getenv("APPDATA")
		if baseDir == "" {
			return "", fmt.Errorf("APPDATA environment variable not set")
		}
	case "linux", "android":
		xdgConfig := os.Getenv("XDG_CONFIG_HOME")
		if xdgConfig != "" {
			baseDir = xdgConfig
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			baseDir = filepath.Join(home, ".config")
		}
	default:
		return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	configDir := filepath.Join(baseDir, "goplexcli")
	return configDir, nil
}

// GetCacheDir returns the cache directory path
func GetCacheDir() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "cache"), nil
}

// GetConfigPath returns the full path to the config file
func GetConfigPath() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "config.json"), nil
}

// Load reads the config file and returns a Config struct
func Load() (*Config, error) {
	configPath, err := GetConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	// Migrate legacy single-server config to multi-server
	if err := config.MigrateLegacy(); err != nil {
		return nil, err
	}

	return &config, nil
}

// Save writes the config to disk
func (c *Config) Save() error {
	configDir, err := GetConfigDir()
	if err != nil {
		return err
	}

	// Create config directory if it doesn't exist
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	configPath, err := GetConfigPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0600)
}

// MigrateLegacy converts old single-server config to multi-server format
func (c *Config) MigrateLegacy() error {
	// If we have legacy PlexURL but no servers, migrate
	if c.PlexURL != "" && len(c.Servers) == 0 {
		c.Servers = []PlexServer{
			{
				Name:    "Default Server",
				URL:     c.PlexURL,
				Enabled: true,
			},
		}
		// Keep legacy field for backward compatibility
		// c.PlexURL = "" // Don't clear it yet
	}
	return nil
}

// ResolveDownloadDir returns the directory downloads should be written to.
// Precedence: the override argument (e.g. from a --dest flag), then the
// configured DownloadDir, then the current working directory. A leading "~"
// in either configured path is expanded to the user's home directory.
func (c *Config) ResolveDownloadDir(override string) (string, error) {
	dir := override
	if dir == "" {
		dir = c.DownloadDir
	}
	if dir == "" {
		return os.Getwd()
	}

	if dir == "~" || strings.HasPrefix(dir, "~/") || strings.HasPrefix(dir, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot expand ~ in download dir: %w", err)
		}
		dir = filepath.Join(home, dir[1:])
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("invalid download dir %q: %w", dir, err)
	}
	return abs, nil
}

// TokenForServer returns the token to use when talking to a specific server:
// the server's own access token when present, otherwise the account-wide
// PlexToken. Owners can use their account token directly, but shared users
// get a 401 from the server unless the per-server token is used.
func (c *Config) TokenForServer(s PlexServer) string {
	if s.Token != "" {
		return s.Token
	}
	return c.PlexToken
}

// TokenForURL returns the token to use for the server at the given URL,
// matching configured servers while ignoring trailing slashes. It falls back
// to the account-wide PlexToken when no configured server matches or the
// matching server has no token of its own.
func (c *Config) TokenForURL(serverURL string) string {
	target := strings.TrimRight(serverURL, "/")
	for _, s := range c.Servers {
		if strings.TrimRight(s.URL, "/") == target && s.Token != "" {
			return s.Token
		}
	}
	return c.PlexToken
}

// HasPlex reports whether any Plex server is configured for indexing
// (multi-server or legacy single-URL).
func (c *Config) HasPlex() bool {
	return c.PlexToken != "" && (c.PlexURL != "" || len(c.GetEnabledServers()) > 0)
}

// GetEnabledLibraries returns the local library folders that should be indexed.
func (c *Config) GetEnabledLibraries() []LibraryFolder {
	var enabled []LibraryFolder
	for _, lib := range c.Libraries {
		if lib.Enabled {
			enabled = append(enabled, lib)
		}
	}
	return enabled
}

// FindLibrary returns the library folder with the given name (case-insensitive).
func (c *Config) FindLibrary(name string) (LibraryFolder, int, bool) {
	for i, lib := range c.Libraries {
		if strings.EqualFold(lib.Name, name) {
			return lib, i, true
		}
	}
	return LibraryFolder{}, -1, false
}

// Validate checks a library folder definition.
func (l LibraryFolder) Validate() error {
	if strings.TrimSpace(l.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(l.Path) == "" {
		return fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(l.Path) {
		return fmt.Errorf("path must be absolute: %q", l.Path)
	}
	switch l.Type {
	case "movie", "show":
	default:
		return fmt.Errorf("type must be \"movie\" or \"show\", got %q", l.Type)
	}
	if l.Remote != "" && !strings.Contains(l.Remote, ":") {
		return fmt.Errorf("remote must be an rclone path like \"remote:folder\", got %q", l.Remote)
	}
	return nil
}

// GetEnabledServers returns all servers that should be indexed
func (c *Config) GetEnabledServers() []PlexServer {
	var enabled []PlexServer
	for _, server := range c.Servers {
		if server.Enabled {
			enabled = append(enabled, server)
		}
	}
	return enabled
}

// GetEnabledOutplayerTargets returns all Outplayer targets that are enabled and
// should be offered as transfer destinations.
func (c *Config) GetEnabledOutplayerTargets() []OutplayerTarget {
	var enabled []OutplayerTarget
	for _, t := range c.OutplayerTargets {
		if t.Enabled {
			enabled = append(enabled, t)
		}
	}
	return enabled
}

// GetEnabledRemoteServers returns all registered remote download servers that
// are enabled and should be offered in the download-target picker.
func (c *Config) GetEnabledRemoteServers() []RemoteServer {
	var enabled []RemoteServer
	for _, r := range c.RemoteServers {
		if r.Enabled {
			enabled = append(enabled, r)
		}
	}
	return enabled
}

// GetEnabledWebDAVTargets returns all configured WebDAV targets that are
// enabled and should be offered as transfer destinations.
func (c *Config) GetEnabledWebDAVTargets() []WebDAVTarget {
	var enabled []WebDAVTarget
	for _, t := range c.WebDAVTargets {
		if t.Enabled {
			enabled = append(enabled, t)
		}
	}
	return enabled
}

// Validate checks if the config has all required fields and valid values.
// It returns an error describing what's wrong if validation fails.
// Call this after Load() to ensure the configuration is usable.
func (c *Config) Validate() error {
	// Local libraries need no Plex credentials at all; a config is usable
	// with either source (or both).
	if len(c.GetEnabledLibraries()) > 0 {
		for i, lib := range c.Libraries {
			if err := lib.Validate(); err != nil {
				return fmt.Errorf("libraries[%d]: %w", i, err)
			}
		}
		if !c.HasPlex() {
			return nil
		}
	}

	// Check for authentication token
	if c.PlexToken == "" {
		return fmt.Errorf("plex_token is required - run 'goplexcli login' or add a local library with 'goplexcli library add'")
	}

	// Check for at least one server (legacy or new format)
	if c.PlexURL == "" && len(c.Servers) == 0 {
		return fmt.Errorf("at least one Plex server is required - run 'goplexcli login' or add a local library with 'goplexcli library add'")
	}

	// Validate legacy URL if present
	if c.PlexURL != "" {
		if err := validateServerURL(c.PlexURL); err != nil {
			return fmt.Errorf("invalid plex_url: %w", err)
		}
	}

	// Validate each configured server
	for i, server := range c.Servers {
		if server.Name == "" {
			return fmt.Errorf("server[%d]: name is required", i)
		}
		if server.URL == "" {
			return fmt.Errorf("server[%d] (%s): URL is required", i, server.Name)
		}
		if err := validateServerURL(server.URL); err != nil {
			return fmt.Errorf("server[%d] (%s): %w", i, server.Name, err)
		}
	}

	return nil
}

// validateServerURL checks if a URL is valid for a Plex server.
func validateServerURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Require http or https scheme
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("URL must use http or https scheme, got %q", parsed.Scheme)
	}

	// Require a host
	if parsed.Host == "" {
		return fmt.Errorf("URL must include a host")
	}

	return nil
}
