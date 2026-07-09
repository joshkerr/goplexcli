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

	// DownloadDir is the destination directory for downloads. A leading "~"
	// is expanded to the user's home directory. If empty, downloads go to the
	// current working directory. Can be overridden per-run with --dest.
	DownloadDir string `json:"download_dir,omitempty"`

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

// Validate checks if the config has all required fields and valid values.
// It returns an error describing what's wrong if validation fails.
// Call this after Load() to ensure the configuration is usable.
func (c *Config) Validate() error {
	// Check for authentication token
	if c.PlexToken == "" {
		return fmt.Errorf("plex_token is required - run 'goplexcli login'")
	}

	// Check for at least one server (legacy or new format)
	if c.PlexURL == "" && len(c.Servers) == 0 {
		return fmt.Errorf("at least one Plex server is required - run 'goplexcli login'")
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
