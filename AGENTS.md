# AGENTS.md - Development Guide for AI Agents

This document provides comprehensive guidance for AI agents working in the GoplexCLI codebase. It documents commands, patterns, conventions, and gotchas specific to this project.

---

## Table of Contents

1. [Project Overview](#project-overview)
2. [Essential Commands](#essential-commands)
3. [Project Structure](#project-structure)
4. [Code Patterns & Conventions](#code-patterns--conventions)
5. [Dependencies & External Tools](#dependencies--external-tools)
6. [Testing Strategy](#testing-strategy)
7. [Important Gotchas](#important-gotchas)
8. [Platform-Specific Considerations](#platform-specific-considerations)
9. [Configuration & Security](#configuration--security)
10. [CI/CD Pipeline](#cicd-pipeline)

---

## Project Overview

**GoplexCLI** is a Go-based command-line interface for browsing and streaming media from Plex servers. It integrates with external tools (fzf, mpv, rclone) to provide a rich terminal UI experience.

**Key Technologies:**
- Go 1.24.0
- Cobra (CLI framework)
- Bubble Tea (TUI framework)
- Lipgloss (terminal styling)
- PlexGo SDK (Plex API)
- Custom rclone-golib (rclone integration)

**Platform Support:** macOS, Linux, Windows (AMD64 and ARM64)

---

## Essential Commands

### Building

```bash
# Build both binaries (main + preview helper)
make build

# Build for all platforms (creates ./build/ directory)
make build-all

# Manual build
go build -o goplexcli ./cmd/goplexcli
go build -o goplexcli-preview ./cmd/preview
```

**Output:**
- macOS/Linux: `goplexcli` and `goplexcli-preview`
- Windows: `goplexcli.exe` and `goplexcli-preview.exe`

### Testing

```bash
# Run all tests
make test

# Or directly
go test ./...

# With race detection (not on Windows)
go test -race ./...

# With coverage
go test -v -coverprofile=coverage.txt -covermode=atomic ./...
```

**Note:** Currently no tests exist in the codebase. When adding tests, use standard Go testing patterns.

### Installation

```bash
# Install to /usr/local/bin (requires sudo on Unix)
make install

# Manual installation
sudo cp goplexcli goplexcli-preview /usr/local/bin/
```

### Cleanup

```bash
# Remove build artifacts
make clean
```

### Dependency Management

```bash
# Download and tidy dependencies
make deps

# Or manually
go mod download
go mod tidy
```

### Running the Application

```bash
# Build and run
make run

# Or directly
./goplexcli

# Common workflows
./goplexcli login                # Authenticate with Plex
./goplexcli cache reindex        # Build media cache
./goplexcli browse               # Browse and play media
./goplexcli cache info           # View cache statistics
./goplexcli config               # Show current configuration
```

---

## Project Structure

```
goplexcli/
├── cmd/
│   ├── goplexcli/           # Main CLI application
│   │   └── main.go          # Cobra commands, CLI logic, styling
│   └── preview/             # Preview helper for fzf
│       └── main.go          # Standalone binary for preview window
├── internal/
│   ├── cache/               # Media caching system
│   │   └── cache.go         # JSON-based cache management
│   ├── config/              # Configuration management
│   │   └── config.go        # Platform-specific paths, validation
│   ├── download/            # Download functionality
│   │   └── download.go      # rclone integration with TUI progress
│   ├── player/              # Media playback
│   │   └── player.go        # MPV player wrapper
│   ├── plex/                # Plex API client
│   │   └── client.go        # PlexGo SDK wrapper + custom HTTP
│   └── ui/                  # User interface
│       └── fzf.go           # fzf integration + Bubble Tea browser
├── .github/workflows/
│   ├── ci.yml               # Multi-platform CI testing
│   └── release.yml          # Automated binary releases
├── Makefile                 # Build automation
├── go.mod                   # Go module definition
├── go.sum                   # Dependency checksums
├── README.md                # User documentation
├── PROJECT_SUMMARY.md       # Technical overview
├── CONTRIBUTING.md          # Contributor guidelines
├── QUICKSTART.md            # Quick start guide
├── LICENSE                  # MIT License
└── .gitignore               # Git ignore rules
```

### Two-Binary Architecture

GoplexCLI uses **two separate binaries**:

1. **`goplexcli`** - Main CLI application
2. **`goplexcli-preview`** - Standalone preview helper for fzf

**Why?** fzf's `--preview` flag executes a command for each line. The preview binary must be fast and lightweight. It communicates via a temporary JSON file (for passing auth tokens securely).

---

## Code Patterns & Conventions

### Error Handling

**Consistent wrapping with context:**
```go
if err != nil {
    return fmt.Errorf("failed to fetch media: %w", err)
}
```

**Graceful degradation:**
```go
// Missing tools → user-friendly errors
exec.LookPath("fzf")  // Check if tool exists before using

// Missing config → return empty struct instead of failing
if os.IsNotExist(err) {
    return &Cache{Media: []plex.MediaItem{}, LastUpdated: time.Time{}}, nil
}

// Exit code 130 (Ctrl-C in fzf) → treated as user cancellation
if exitErr.ExitCode() == 130 {
    fmt.Println(warningStyle.Render("Cancelled"))
    return nil
}
```

### Context Usage

**Pass context for cancellable operations:**
```go
func GetAllMedia(ctx context.Context, progressCallback ProgressCallback) ([]MediaItem, error)
func Download(ctx context.Context, remotePath string, localPath string) error
```

**No context for simple file I/O:**
```go
func Load() (*Cache, error)
func Save() error
```

### Nil Safety Helpers

Plex API responses use pointers (`*string`, `*int`). Helper functions handle nil values:

```go
func valueOrEmpty(s *string) string {
    if s == nil { return "" }
    return *s
}

func valueOrZeroInt(v *int) int {
    if v == nil { return 0 }
    return *v
}
```

### Validation Pattern

Explicit `Validate()` methods on types:

```go
func (c *Config) Validate() error {
    if strings.TrimSpace(c.PlexURL) == "" {
        return fmt.Errorf("plex_url is required")
    }
    // ...
}
```

### Player Detection

Auto-detecting media player with configurable override in `internal/player/player.go`:

```go
// Auto-detect best player: iina on macOS if available, otherwise mpv
func DetectPlayer(preference string) (string, string, error) {
    // preference can be: "auto", "iina", "mpv", or custom path
    
    if runtime.GOOS == "darwin" {
        // Try iina-cli first (brew install or IINA.app bundle)
        if path, err := exec.LookPath("iina-cli"); err == nil {
            return path, "iina", nil
        }
    }
    
    // Fallback to mpv (cross-platform)
    return exec.LookPath("mpv")
}
```

**Config usage:**
```json
{
  "player": "auto"  // or "iina", "mpv", "/custom/path"
}
```

**Legacy support:** Old `mpv_path` config field still works for backward compatibility.

### Styling with Lipgloss

Package-level style variables in `cmd/goplexcli/main.go`:

```go
var (
    titleStyle = lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color("205")). // Pink
        MarginBottom(1)

    successStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("42"))   // Green

    errorStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("196")).  // Red
        Bold(true)

    infoStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("86"))   // Cyan

    warningStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("214"))  // Orange
)
```

**Usage:**
```go
fmt.Println(successStyle.Render("✓ Success"))
fmt.Println(errorStyle.Render("Error: something failed"))
```

### Progress Callbacks

Functional pattern for long-running operations:

```go
type ProgressCallback func(libraryName string, itemCount int, totalLibraries int, currentLibrary int)

func (c *Client) GetAllMedia(ctx context.Context, progressCallback ProgressCallback) ([]MediaItem, error) {
    // Call callback with progress updates
    if progressCallback != nil {
        progressCallback(lib.Title, len(media), totalLibs, currentLib)
    }
}
```

### Media Formatting

Consistent across all interfaces:

```go
// Movies: "Title (Year)"
func (m *MediaItem) FormatMediaTitle() string {
    if m.Type == "movie" {
        return fmt.Sprintf("%s (%d)", m.Title, m.Year)
    }
    // Episodes: "ShowName - S01E05 - EpisodeTitle"
    if m.Type == "episode" {
        return fmt.Sprintf("%s - S%02dE%02d - %s", 
            m.ParentTitle, m.ParentIndex, m.Index, m.Title)
    }
}
```

### File Path Handling

**Always use `filepath.Join()` for cross-platform compatibility:**
```go
import "path/filepath"

cachePath := filepath.Join(cacheDir, "media.json")
```

**Temp files:**
```go
tempFile := filepath.Join(os.TempDir(), "goplexcli-preview-data.json")
defer os.Remove(tempFile)  // Cleanup
```

### Async Operations in Bubble Tea

```go
// Command pattern for async work
func downloadPosterCmd(url string) tea.Cmd {
    return func() tea.Msg {
        // Do work
        return posterDownloadedMsg{url: url, data: data}
    }
}

// Handle in Update()
case posterDownloadedMsg:
    m.posterData[msg.url] = msg.data
    return m, renderPosterCmd(msg.data)
```

---

## Dependencies & External Tools

### Go Dependencies

**Production:**
- `github.com/LukeHagar/plexgo` v0.28.1 - Plex API SDK (partially used - see gotchas)
- `github.com/charmbracelet/bubbletea` - TUI framework
- `github.com/charmbracelet/bubbles` - TUI components
- `github.com/charmbracelet/lipgloss` - Terminal styling
- `github.com/spf13/cobra` - CLI framework
- `github.com/joshkerr/rclone-golib` - Custom rclone wrapper with progress bars
- `github.com/sahilm/fuzzy` - Fuzzy search
- `golang.org/x/term` - Secure password input

### External Tools (Required by Users)

Must be in PATH or configured in `config.json`:

- **fzf** - Fuzzy finder for media browsing
- **Media player** - mpv or iina (macOS) for streaming
- **rclone** - File transfer tool for downloads

**Player Detection:**
The application auto-detects the best available player:
- macOS: Prefers iina if installed, falls back to mpv
- Linux/Windows: Uses mpv

Users can override in `config.json`:
```json
{
  "player": "auto"  // or "iina", "mpv", or custom path
}
```

**Detection pattern:**
```go
fzfPath, err := exec.LookPath("fzf")
if err != nil {
    return fmt.Errorf("fzf not found in PATH. Install with: brew install fzf")
}
```

---

## Testing Strategy

### Current State

**No tests currently exist in the codebase.**

### Testing Recommendations for Future Work

When adding tests:

1. **Unit tests** for:
   - `internal/cache` - Cache load/save, formatting
   - `internal/config` - Path resolution, validation
   - Media formatting helpers in `internal/plex`

2. **Integration tests** for:
   - Plex API calls (require test server or mocking)
   - fzf/mpv/rclone integrations (require tools installed)

3. **Test patterns to use:**
   ```go
   func TestCacheLoadEmpty(t *testing.T) {
       // Setup temp dir
       tempDir := t.TempDir()
       
       // Test logic
       
       // Assertions
       if got != want {
           t.Errorf("got %v, want %v", got, want)
       }
   }
   ```

4. **Avoid testing external tools directly** - use interfaces and mocks

---

## Important Gotchas

### 1. Hardcoded Rclone Path Conversion

**Location:** `internal/plex/client.go` (in `convertToRclonePath` function)

```go
// HARDCODED: Strips /home/joshkerr/ prefix specifically
// HARDCODED: plexcloudservers and plexcloudservers2 remote names
func convertToRclonePath(filePath string) string {
    // Removes /home/joshkerr/ prefix
    // Adds : after remote name (e.g., plexcloudservers2:/Media/...)
}
```

**Why?** This is specific to the original developer's Plex setup.

**For contributions:** This should be made configurable via `config.json`:
```json
{
  "plex_path_prefix": "/home/joshkerr",
  "rclone_remote_mapping": {
    "plexcloudservers": "plexcloudservers:",
    "plexcloudservers2": "plexcloudservers2:"
  }
}
```

### 2. PlexGo SDK Limitations

**Location:** `internal/plex/client.go:84-125`

The Plex SDK has unmarshaling issues with library sections. **Solution:** Use direct HTTP requests instead:

```go
// Don't use: c.sdk.Library.GetLibraries()
// Use custom HTTP:
func (c *Client) GetLibraries(ctx context.Context) ([]Library, error) {
    url := fmt.Sprintf("%s/library/sections?X-Plex-Token=%s", c.serverURL, c.token)
    // Manual HTTP request + JSON parsing
}
```

**Pattern:** When SDK fails, fall back to direct HTTP with custom structs.

### 3. Preview Binary Communication

**Location:** `cmd/preview/main.go` and `internal/ui/fzf.go`

The preview binary and main binary communicate via a **temporary JSON file**:

```go
// Main binary writes:
tempFile := filepath.Join(os.TempDir(), "goplexcli-preview-data.json")
os.WriteFile(tempFile, jsonData, 0600)  // 0600 for token security

// Preview binary reads:
data, err := os.ReadFile(tempFile)
```

**Security:** File permissions are `0600` because the JSON contains the Plex auth token.

**Platform-specific wrapper scripts:**
- Unix: Shell script (`.sh`)
- Windows: Batch script (`.bat`)

### 4. Exit Code 130 = User Cancellation

**Location:** `internal/ui/fzf.go`

```go
if exitErr, ok := err.(*exec.ExitError); ok {
    if exitErr.ExitCode() == 130 {
        // User pressed Ctrl-C in fzf
        return "", nil  // Not an error
    }
}
```

Exit code 130 is standard for Ctrl-C interrupts. Don't treat it as an error.

### 5. File Permissions Matter

```go
// Config file (contains token) - MUST be private
os.WriteFile(configPath, data, 0600)

// Cache file (no secrets) - can be readable
os.WriteFile(cachePath, data, 0644)

// Directories
os.MkdirAll(configDir, 0755)
```

### 6. No Global State

**Pattern:** Everything is passed explicitly - no global variables for config or state.

```go
// Good: Pass explicitly
func runBrowse(cmd *cobra.Command, args []string) error {
    cfg, err := config.Load()
    client, err := plex.New(cfg.PlexURL, cfg.PlexToken)
}

// Bad: Don't use global config
var globalConfig *config.Config  // ❌ Avoid
```

### 7. Progress Output Uses `\r` for In-Place Updates

```go
fmt.Printf("\r%s %s (%d items)", 
    infoStyle.Render("Processing"), 
    libName, 
    itemCount)
// \r moves cursor to start of line for overwrite
```

Final line needs `\n` to avoid terminal corruption:
```go
fmt.Printf("\n")  // After progress loop
```

---

## Platform-Specific Considerations

### Config Directories

```go
switch runtime.GOOS {
case "darwin":
    return filepath.Join(home, ".config", "goplexcli")
case "windows":
    appData := os.Getenv("APPDATA")
    return filepath.Join(appData, "goplexcli")
case "linux":
    xdgConfig := os.Getenv("XDG_CONFIG_HOME")
    if xdgConfig != "" {
        return filepath.Join(xdgConfig, "goplexcli")
    }
    return filepath.Join(home, ".config", "goplexcli")
}
```

### Binary Extensions

```go
// Windows detection
if runtime.GOOS == "windows" {
    binaryName = "goplexcli.exe"
}
```

### Path Separators

**Always use `filepath.Join()`** - it handles `/` vs `\` automatically.

```go
// Good
filepath.Join("dir", "subdir", "file.txt")

// Bad
"dir/subdir/file.txt"  // ❌ Breaks on Windows
```

### Makefile Platform Detection

```makefile
ifeq ($(OS),Windows_NT)
	@go build -o goplexcli.exe ./cmd/goplexcli
else
	@go build -o goplexcli ./cmd/goplexcli
endif
```

### Shell Script Escaping

```go
// Unix shell script
script := fmt.Sprintf(`#!/bin/bash
goplexcli-preview "$@"
`)

// Windows batch script
script := fmt.Sprintf(`@echo off
goplexcli-preview.exe %%*
`)
```

---

## Configuration & Security

### Config File Structure

**Location:** Platform-specific (see above)

```json
{
  "plex_url": "http://192.168.1.100:32400",
  "plex_token": "xxxxxxxxxxxxxxxxxxxx",
  "plex_username": "username",
  "mpv_path": "mpv",
  "rclone_path": "rclone",
  "fzf_path": "fzf"
}
```

### Security Practices

1. **No password storage** - only tokens
2. **Config file permissions:** `0600` (user-only read/write)
3. **Hidden password input:** `term.ReadPassword()`
4. **Token display:** Truncated to 10 chars in UI
5. **No secrets in logs**

```go
// Don't log tokens
log.Printf("Token: %s", token)  // ❌

// Truncate for display
truncatedToken := token
if len(token) > 10 {
    truncatedToken = token[:10] + "..."
}
fmt.Printf("Token: %s\n", truncatedToken)  // ✓
```

### Validation

```go
func (c *Config) Validate() error {
    if strings.TrimSpace(c.PlexURL) == "" {
        return fmt.Errorf("plex_url is required")
    }
    if strings.TrimSpace(c.PlexToken) == "" {
        return fmt.Errorf("plex_token is required")
    }
    return nil
}
```

---

## CI/CD Pipeline

### GitHub Actions Workflows

**`.github/workflows/ci.yml`** - Continuous Integration

- **Triggers:** Push to `main`, pull requests
- **Matrix:**
  - OS: Ubuntu, macOS, Windows
  - Go version: 1.24
- **Steps:**
  1. Checkout code
  2. Set up Go
  3. Cache Go modules
  4. Download dependencies
  5. Verify dependencies
  6. Build (`go build ./cmd/goplexcli`)
  7. Test with coverage (race detector on Unix only)
  8. Upload coverage to Codecov (Ubuntu only)
- **Lint:** golangci-lint (Ubuntu only)

**`.github/workflows/release.yml`** - Release Automation

- **Trigger:** Git tags matching `v*` (e.g., `v1.0.0`)
- **Builds for:**
  - darwin-amd64, darwin-arm64
  - linux-amd64, linux-arm64
  - windows-amd64
- **Creates:**
  - `.tar.gz` archives (macOS, Linux)
  - `.zip` archives (Windows)
  - GitHub Release with auto-generated notes
- **Note:** Only builds main binary (`cmd/goplexcli`), not preview binary

### Release Process

```bash
# Create and push tag
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0

# GitHub Actions automatically:
# 1. Builds binaries for all platforms
# 2. Creates archives
# 3. Publishes GitHub Release
```

---

## Common Workflows for Agents

### Adding a New Command

1. Add command in `cmd/goplexcli/main.go`:
   ```go
   newCmd := &cobra.Command{
       Use:   "mycommand",
       Short: "Description",
       RunE:  runMyCommand,
   }
   rootCmd.AddCommand(newCmd)
   ```

2. Implement handler:
   ```go
   func runMyCommand(cmd *cobra.Command, args []string) error {
       // Load config
       cfg, err := config.Load()
       if err != nil {
           return fmt.Errorf("failed to load config: %w", err)
       }
       
       // Your logic
       
       return nil
   }
   ```

3. Use lipgloss styles for output
4. Handle errors gracefully
5. Test manually: `make build && ./goplexcli mycommand`

### Adding New Configuration Options

1. Update `internal/config/config.go`:
   ```go
   type Config struct {
       // Existing fields...
       NewOption string `json:"new_option"`
   }
   ```

2. Update `Validate()` if needed
3. Update README.md config section
4. Regenerate cache if config affects media: `./goplexcli cache reindex`

### Modifying Plex API Calls

1. Check if SDK method works (test first)
2. If SDK fails, use direct HTTP:
   ```go
   url := fmt.Sprintf("%s/some/endpoint?X-Plex-Token=%s", c.serverURL, c.token)
   req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
   // Add headers, do request, parse JSON
   ```
3. Create custom struct for response
4. Use helper functions for nil pointer fields

### Cross-Platform File Operations

```go
// Get config directory (platform-specific)
configDir, err := config.GetConfigDir()

// Build paths
filePath := filepath.Join(configDir, "myfile.json")

// Create directories
os.MkdirAll(configDir, 0755)

// Write files with correct permissions
os.WriteFile(filePath, data, 0644)
```

---

## Documentation Standards

When modifying code:

1. **Update README.md** for user-facing changes
2. **Update this file (AGENTS.md)** for developer patterns
3. **Add inline comments** for non-obvious logic
4. **Update CONTRIBUTING.md** if process changes
5. **Keep PROJECT_SUMMARY.md** in sync with architecture

**Commit message style:**
```
Add feature for sorting media by rating

- Implement sorting in cache module
- Add --sort flag to browse command
- Update documentation
```

---

## Quick Reference

### File Permissions
- Config files: `0600` (contains secrets)
- Cache files: `0644` (no secrets)
- Directories: `0755`
- Temp files with tokens: `0600`

### Color Codes (Lipgloss)
- 205: Pink (titles)
- 42: Green (success)
- 196: Red (errors)
- 86: Cyan (info)
- 214: Orange (warnings)

### Build Targets
- Main binary: `./cmd/goplexcli`
- Preview binary: `./cmd/preview`
- Both must be distributed together

### Go Version
- Minimum: 1.24.0 (specified in `go.mod`)
- CI tests on: 1.24

### External Tool Detection
```go
path, err := exec.LookPath("toolname")
if err != nil {
    return fmt.Errorf("toolname not found")
}
```

---

## Contact & Resources

- **Repository:** https://github.com/joshkerr/goplexcli
- **Issues:** GitHub Issues
- **Plex API Docs:** https://github.com/LukeHagar/plexgo
- **Charm Libraries:** https://charm.sh/

---

*Last updated: December 2024*
