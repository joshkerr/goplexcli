package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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
	
	// Create a temporary preview script
	previewScript, err := createPreviewScript(media, plexURL, plexToken)
	if err != nil {
		return -1, fmt.Errorf("failed to create preview script: %w", err)
	}
	defer os.Remove(previewScript)
	
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
		"--bind=i:toggle-preview",
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
	
	if err := os.WriteFile(dataPath, jsonData, 0644); err != nil {
		return "", err
	}
	
	// Build the preview binary
	previewBinary := filepath.Join(tmpDir, "goplexcli-preview")
	buildCmd := exec.Command("go", "build", "-o", previewBinary, "github.com/joshkerr/goplexcli/cmd/preview")
	if err := buildCmd.Run(); err != nil {
		// If build fails, create a simple shell script instead
		return createFallbackPreviewScript(dataPath)
	}
	
	// Create wrapper script that calls the binary
	scriptPath := filepath.Join(tmpDir, "goplexcli-preview.sh")
	script := fmt.Sprintf(`#!/bin/bash
%s %s "$1"
`, previewBinary, dataPath)
	
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return "", err
	}
	
	return scriptPath, nil
}

// createFallbackPreviewScript creates a simple bash script if Go build fails
func createFallbackPreviewScript(dataPath string) (string, error) {
	tmpDir := os.TempDir()
	scriptPath := filepath.Join(tmpDir, "goplexcli-preview.sh")
	
	script := `#!/bin/bash
echo "Preview functionality requires rebuild. Showing basic info..."
echo "Index: $1"
`
	
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

// downloadPoster downloads the poster image for preview
func downloadPoster(posterURL string) (string, error) {
	resp, err := http.Get(posterURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download poster: status %d", resp.StatusCode)
	}
	
	// Save to temp file
	tmpFile := filepath.Join(os.TempDir(), "goplexcli-poster.jpg")
	out, err := os.Create(tmpFile)
	if err != nil {
		return "", err
	}
	defer out.Close()
	
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return "", err
	}
	
	return tmpFile, nil
}
