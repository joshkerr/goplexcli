package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type PlexServer struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Enabled  bool   `json:"enabled"` // Whether to index this server
}

type Config struct {
	// Legacy single-server fields (for backward compatibility)
	PlexURL      string `json:"plex_url,omitempty"`
	PlexToken    string `json:"plex_token"`
	PlexUsername string `json:"plex_username,omitempty"`
	
	// Multi-server support
	Servers    []PlexServer `json:"servers,omitempty"`
	
	// Tool paths
	Player     string `json:"player,omitempty"`     // "auto", "iina", "mpv", "vlc", or custom path
	MPVPath    string `json:"mpv_path,omitempty"`   // Deprecated: use Player instead
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

// Validate checks if the config has required fields
func (c *Config) Validate() error {
	// Check for either legacy or new format
	if c.PlexURL == "" && len(c.Servers) == 0 {
		return fmt.Errorf("at least one Plex server is required")
	}
	if c.PlexToken == "" {
		return fmt.Errorf("plex_token is required")
	}
	return nil
}
