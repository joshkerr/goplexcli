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
)

// PlexServer represents a configured Plex server.
// Multiple servers can be configured, with each individually enabled or disabled.
type PlexServer struct {
	// Name is a human-readable identifier for the server
	Name string `json:"name"`
	// URL is the base URL of the Plex server (e.g., "http://192.168.1.100:32400")
	URL string `json:"url"`
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
	case "linux":
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
