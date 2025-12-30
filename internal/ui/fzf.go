package ui

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
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
		"--height=40%",
		"--reverse",
		"--border",
		"--prompt=" + prompt + " ",
		"--preview-window=down:3:wrap",
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
