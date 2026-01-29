package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty config",
			config:  Config{},
			wantErr: true,
			errMsg:  "plex_token is required",
		},
		{
			name: "token only - no server",
			config: Config{
				PlexToken: "test-token",
			},
			wantErr: true,
			errMsg:  "at least one Plex server is required",
		},
		{
			name: "valid legacy config",
			config: Config{
				PlexURL:   "http://192.168.1.100:32400",
				PlexToken: "test-token",
			},
			wantErr: false,
		},
		{
			name: "valid multi-server config",
			config: Config{
				PlexToken: "test-token",
				Servers: []PlexServer{
					{Name: "Server1", URL: "http://192.168.1.100:32400", Enabled: true},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid URL scheme",
			config: Config{
				PlexURL:   "ftp://192.168.1.100:32400",
				PlexToken: "test-token",
			},
			wantErr: true,
			errMsg:  "must use http or https",
		},
		{
			name: "invalid URL - no host",
			config: Config{
				PlexURL:   "http://",
				PlexToken: "test-token",
			},
			wantErr: true,
			errMsg:  "must include a host",
		},
		{
			name: "server without name",
			config: Config{
				PlexToken: "test-token",
				Servers: []PlexServer{
					{URL: "http://192.168.1.100:32400", Enabled: true},
				},
			},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name: "server without URL",
			config: Config{
				PlexToken: "test-token",
				Servers: []PlexServer{
					{Name: "Server1", Enabled: true},
				},
			},
			wantErr: true,
			errMsg:  "URL is required",
		},
		{
			name: "https URL is valid",
			config: Config{
				PlexURL:   "https://plex.example.com",
				PlexToken: "test-token",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.errMsg)
					return
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %q, want containing %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestMigrateLegacy(t *testing.T) {
	tests := []struct {
		name           string
		config         Config
		wantServers    int
		wantServerName string
	}{
		{
			name: "migrate legacy to servers",
			config: Config{
				PlexURL:   "http://192.168.1.100:32400",
				PlexToken: "test-token",
			},
			wantServers:    1,
			wantServerName: "Default Server",
		},
		{
			name: "no migration when servers exist",
			config: Config{
				PlexURL:   "http://old-url:32400",
				PlexToken: "test-token",
				Servers: []PlexServer{
					{Name: "Existing", URL: "http://new-url:32400", Enabled: true},
				},
			},
			wantServers:    1,
			wantServerName: "Existing",
		},
		{
			name: "no migration when no legacy URL",
			config: Config{
				PlexToken: "test-token",
			},
			wantServers: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.config
			err := cfg.MigrateLegacy()
			if err != nil {
				t.Errorf("MigrateLegacy() unexpected error: %v", err)
				return
			}

			if len(cfg.Servers) != tt.wantServers {
				t.Errorf("MigrateLegacy() servers = %d, want %d", len(cfg.Servers), tt.wantServers)
			}

			if tt.wantServers > 0 && cfg.Servers[0].Name != tt.wantServerName {
				t.Errorf("MigrateLegacy() server name = %q, want %q", cfg.Servers[0].Name, tt.wantServerName)
			}
		})
	}
}

func TestGetEnabledServers(t *testing.T) {
	cfg := Config{
		Servers: []PlexServer{
			{Name: "Server1", URL: "http://s1:32400", Enabled: true},
			{Name: "Server2", URL: "http://s2:32400", Enabled: false},
			{Name: "Server3", URL: "http://s3:32400", Enabled: true},
		},
	}

	enabled := cfg.GetEnabledServers()
	if len(enabled) != 2 {
		t.Errorf("GetEnabledServers() = %d servers, want 2", len(enabled))
	}

	// Check that only enabled servers are returned
	for _, s := range enabled {
		if !s.Enabled {
			t.Errorf("GetEnabledServers() returned disabled server: %s", s.Name)
		}
	}
}

func TestSaveLoad(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "goplexcli-config-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create config file directly in temp dir
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := Config{
		PlexURL:      "http://192.168.1.100:32400",
		PlexToken:    "test-token-12345",
		PlexUsername: "testuser",
		Servers: []PlexServer{
			{Name: "TestServer", URL: "http://192.168.1.100:32400", Enabled: true},
		},
		MPVPath:    "/usr/bin/mpv",
		RclonePath: "/usr/bin/rclone",
		FzfPath:    "/usr/bin/fzf",
	}

	// Save to temp file
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Read it back
	data, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("Failed to unmarshal config: %v", err)
	}

	// Verify fields
	if loaded.PlexURL != cfg.PlexURL {
		t.Errorf("PlexURL = %q, want %q", loaded.PlexURL, cfg.PlexURL)
	}
	if loaded.PlexToken != cfg.PlexToken {
		t.Errorf("PlexToken = %q, want %q", loaded.PlexToken, cfg.PlexToken)
	}
	if len(loaded.Servers) != 1 {
		t.Errorf("Servers count = %d, want 1", len(loaded.Servers))
	}
}

// contains checks if s contains substr
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
