package ui

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	
	"github.com/joshkerr/goplexcli/internal/plex"
)

// SelectWithFzf presents items in fzf and returns the selected item
func SelectWithFzf(items []string, prompt string, fzfPath string) (string, int, error) {
	if len(items) == 0 {
		return "", -1, fmt.Errorf("no items to select from")
	}
	
	if fzfPath == "" {
		fzfPath = "fzf"
	}
	
	// Check if fzf is available
	if _, err := exec.LookPath(fzfPath); err != nil {
		return "", -1, fmt.Errorf("fzf not found in PATH. Please install fzf or specify the path in config")
	}
	
	// Join items with newlines
	input := strings.Join(items, "\n")
	
	// Build fzf command
	args := []string{
		"--height=90%",
		"--reverse",
		"--border",
		"--prompt=" + prompt + " ",
	}
	
	cmd := exec.Command(fzfPath, args...)
	
	// Set up pipes
	cmd.Stdin = strings.NewReader(input)
	cmd.Stderr = os.Stderr
	
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	
	// Run fzf
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Exit code 130 means user cancelled with Ctrl-C
			if exitErr.ExitCode() == 130 {
				return "", -1, fmt.Errorf("cancelled by user")
			}
		}
		return "", -1, fmt.Errorf("fzf failed: %w", err)
	}
	
	// Get selected item
	selected := strings.TrimSpace(outBuf.String())
	if selected == "" {
		return "", -1, fmt.Errorf("no selection made")
	}
	
	// Find the index of the selected item
	index := -1
	for i, item := range items {
		if item == selected {
			index = i
			break
		}
	}
	
	return selected, index, nil
}

// SelectMediaWithPreview presents media in fzf with preview window showing metadata and poster
func SelectMediaWithPreview(media []plex.MediaItem, prompt string, fzfPath string, plexURL string, plexToken string) (int, error) {
	if len(media) == 0 {
		return -1, fmt.Errorf("no items to select from")
	}
	
	if fzfPath == "" {
		fzfPath = "fzf"
	}
	
	// Check if fzf is available
	if _, err := exec.LookPath(fzfPath); err != nil {
		return -1, fmt.Errorf("fzf not found in PATH. Please install fzf or specify the path in config")
	}
	
	// Create formatted items with index prefix for preview script
	var items []string
	for i, item := range media {
		items = append(items, fmt.Sprintf("%d\t%s", i, item.FormatMediaTitle()))
	}
	input := strings.Join(items, "\n")
	
	// Create a temporary preview script and data file
	previewScript, err := createPreviewScript(media, plexURL, plexToken)
	if err != nil {
		return -1, fmt.Errorf("failed to create preview script: %w", err)
	}
	defer os.Remove(previewScript)
	
	// Also clean up the data file containing the token
	dataPath := filepath.Join(os.TempDir(), "goplexcli-preview-data.json")
	defer os.Remove(dataPath)
	
	// Build fzf command with preview
	args := []string{
		"--height=90%",
		"--reverse",
		"--border",
		"--delimiter=\t",
		"--with-nth=2..",
		"--prompt=" + prompt + " ",
		"--preview=" + previewScript + " {1}",
		"--preview-window=right:50%:wrap:hidden",
		"--bind=ctrl-p:toggle-preview",
		"--bind=ctrl-/:toggle-preview",
	}
	
	cmd := exec.Command(fzfPath, args...)
	
	// Set up pipes
	cmd.Stdin = strings.NewReader(input)
	cmd.Stderr = os.Stderr
	
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	
	// Run fzf
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Exit code 130 means user cancelled with Ctrl-C
			if exitErr.ExitCode() == 130 {
				return -1, fmt.Errorf("cancelled by user")
			}
		}
		return -1, fmt.Errorf("fzf failed: %w", err)
	}
	
	// Get selected item and extract index
	selected := strings.TrimSpace(outBuf.String())
	if selected == "" {
		return -1, fmt.Errorf("no selection made")
	}
	
	// Parse the index from the selected line
	parts := strings.SplitN(selected, "\t", 2)
	if len(parts) < 1 {
		return -1, fmt.Errorf("invalid selection format")
	}
	
	var index int
	if _, err := fmt.Sscanf(parts[0], "%d", &index); err != nil {
		return -1, fmt.Errorf("failed to parse selection index: %w", err)
	}
	
	if index < 0 || index >= len(media) {
		return -1, fmt.Errorf("invalid selection index")
	}
	
	return index, nil
}

// createPreviewScript creates a preview binary and returns its path
func createPreviewScript(media []plex.MediaItem, plexURL string, plexToken string) (string, error) {
	tmpDir := os.TempDir()
	
	// Create JSON data file for the preview to read
	dataPath := filepath.Join(tmpDir, "goplexcli-preview-data.json")
	
	type PreviewData struct {
		Media     []plex.MediaItem `json:"media"`
		PlexURL   string           `json:"plex_url"`
		PlexToken string           `json:"plex_token"`
	}
	
	data := PreviewData{
		Media:     media,
		PlexURL:   plexURL,
		PlexToken: plexToken,
	}
	
	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	
	// Use restrictive permissions (0600) to protect the Plex token
	if err := os.WriteFile(dataPath, jsonData, 0600); err != nil {
		return "", err
	}
	
	// First, try to find in PATH
	var previewBinary string
	var previewBinaryName string
	
	// On Windows, look for .exe extension
	if runtime.GOOS == "windows" {
		previewBinaryName = "goplexcli-preview.exe"
	} else {
		previewBinaryName = "goplexcli-preview"
	}
	
	if pathBinary, err := exec.LookPath(previewBinaryName); err == nil {
		previewBinary = pathBinary
	} else {
		// Look for the preview binary in common locations
		// Get current working directory
		cwd, _ := os.Getwd()
		
		possiblePaths := []string{
			filepath.Join(cwd, previewBinaryName),            // Current directory
		}
		
		// Add Unix-specific paths on non-Windows systems
		if runtime.GOOS != "windows" {
			possiblePaths = append(possiblePaths,
				"/usr/local/bin/goplexcli-preview",
				filepath.Join(os.Getenv("HOME"), "bin", "goplexcli-preview"),
			)
		}
		
		for _, path := range possiblePaths {
			if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
				previewBinary, _ = filepath.Abs(path)
				break
			}
		}
	}
	
	// If not found, return error with helpful message
	if previewBinary == "" {
		var scriptPath string
		var script string
		
		if runtime.GOOS == "windows" {
			scriptPath = filepath.Join(tmpDir, "goplexcli-preview.bat")
			script = `@echo off
echo Preview binary not found!
echo.
echo Please run 'make build' or 'go build -o goplexcli-preview.exe ./cmd/preview'
echo Or install it to a location in your PATH
echo.
echo Searched locations:
echo   - PATH (goplexcli-preview.exe)
echo   - .\goplexcli-preview.exe
`
		} else {
			scriptPath = filepath.Join(tmpDir, "goplexcli-preview.sh")
			script = `#!/bin/bash
echo "Preview binary not found!"
echo ""
echo "Please run 'make build' or 'go build -o goplexcli-preview ./cmd/preview'"
echo "Or install it to a location in your PATH"
echo ""
echo "Searched locations:"
echo "  - PATH (goplexcli-preview)"
echo "  - ./goplexcli-preview"
echo "  - /usr/local/bin/goplexcli-preview"
echo "  - ~/bin/goplexcli-preview"
`
		}
		_ = os.WriteFile(scriptPath, []byte(script), 0755) // Ignore error - will fail in wrapper script anyway
		return scriptPath, nil
	}
	
	// Create wrapper script that calls the binary
	var scriptPath string
	var script string
	
	if runtime.GOOS == "windows" {
		// Windows batch file
		scriptPath = filepath.Join(tmpDir, "goplexcli-preview.bat")
		// Escape special characters for batch files
		// In batch files, % needs to be escaped as %%, and quotes are handled by outer quotes
		escapedBinary := strings.ReplaceAll(previewBinary, "%", "%%")
		escapedDataPath := strings.ReplaceAll(dataPath, "%", "%%")
		script = fmt.Sprintf(`@echo off
"%s" "%s" %%1
`, escapedBinary, escapedDataPath)
	} else {
		// Unix shell script
		scriptPath = filepath.Join(tmpDir, "goplexcli-preview.sh")
		// Use single quotes and escape any single quotes in the paths for shell safety
		escapedBinary := strings.ReplaceAll(previewBinary, "'", "'\"'\"'")
		escapedDataPath := strings.ReplaceAll(dataPath, "'", "'\"'\"'")
		script = fmt.Sprintf(`#!/bin/bash
'%s' '%s' "$1"
`, escapedBinary, escapedDataPath)
	}
	
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return "", err
	}
	
	return scriptPath, nil
}

// IsAvailable checks if fzf is available on the system
func IsAvailable(fzfPath string) bool {
	if fzfPath == "" {
		fzfPath = "fzf"
	}
	
	_, err := exec.LookPath(fzfPath)
	return err == nil
}

// PromptAction asks the user what action to take
func PromptAction(fzfPath string) (string, error) {
	actions := []string{
		"Watch",
		"Download",
		"Cancel",
	}
	
	selected, _, err := SelectWithFzf(actions, "Select action:", fzfPath)
	if err != nil {
		return "", err
	}
	
	return strings.ToLower(selected), nil
}

// SelectMediaType asks user to select Movies or TV Shows
func SelectMediaType(fzfPath string) (string, error) {
	types := []string{
		"Movies",
		"TV Shows",
		"All",
	}
	
	selected, _, err := SelectWithFzf(types, "Select media type:", fzfPath)
	if err != nil {
		return "", err
	}
	
	return strings.ToLower(selected), nil
}

// SelectMedia presents media items in fzf and returns the selected item
func SelectMedia(media []plex.MediaItem, prompt string, fzfPath string) (*plex.MediaItem, error) {
	if len(media) == 0 {
		return nil, fmt.Errorf("no media to select from")
	}
	
	// Format media items for display
	var items []string
	for _, item := range media {
		items = append(items, item.FormatMediaTitle())
	}
	
	// Use fzf to select
	_, index, err := SelectWithFzf(items, prompt, fzfPath)
	if err != nil {
		return nil, err
	}
	
	if index < 0 || index >= len(media) {
		return nil, fmt.Errorf("invalid selection")
	}
	
	return &media[index], nil
}

// DownloadPoster downloads the poster image and returns the local path
func DownloadPoster(plexURL, thumbPath, token string) string {
	if thumbPath == "" {
		return ""
	}
	
	// Create cache directory
	cacheDir := filepath.Join(os.TempDir(), "goplexcli-posters")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return ""
	}
	
	// Create filename from hash of thumb path
	hash := md5.Sum([]byte(thumbPath))
	posterFile := filepath.Join(cacheDir, fmt.Sprintf("%x.jpg", hash))
	
	// Check if already downloaded
	if _, err := os.Stat(posterFile); err == nil {
		return posterFile
	}
	
	// Download poster
	url := plexURL + thumbPath + "?X-Plex-Token=" + token
	resp, err := http.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		return ""
	}
	
	// Save to file
	out, err := os.Create(posterFile)
	if err != nil {
		return ""
	}
	defer out.Close()
	
	if _, err := io.Copy(out, resp.Body); err != nil {
		os.Remove(posterFile)
		return ""
	}
	
	return posterFile
}
