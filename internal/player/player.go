package player

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// DetectPlayer finds the best available media player based on preference
// preference can be: "auto", "iina", "mpv", or a custom path
func DetectPlayer(preference string) (string, string, error) {
	// If preference is a custom path or specific player name, try it first
	if preference != "" && preference != "auto" {
		// Check if it's a full path
		if strings.Contains(preference, "/") || strings.Contains(preference, "\\") {
			if _, err := exec.LookPath(preference); err == nil {
				return preference, getPlayerType(preference), nil
			}
			return "", "", fmt.Errorf("specified player not found: %s", preference)
		}
		
		// Try to find the named player
		if path, err := exec.LookPath(preference); err == nil {
			return path, preference, nil
		}
		
		// On macOS, check for IINA.app if "iina" was specified
		if preference == "iina" && runtime.GOOS == "darwin" {
			iinaPath := "/Applications/IINA.app/Contents/MacOS/iina-cli"
			if _, err := exec.LookPath(iinaPath); err == nil {
				return iinaPath, "iina", nil
			}
		}
		
		return "", "", fmt.Errorf("player '%s' not found in PATH", preference)
	}
	
	// Auto-detect: prefer iina on macOS, otherwise mpv
	if runtime.GOOS == "darwin" {
		// Try iina-cli first (installed via brew or in IINA.app)
		if path, err := exec.LookPath("iina-cli"); err == nil {
			return path, "iina", nil
		}
		
		// Try IINA.app bundle
		iinaPath := "/Applications/IINA.app/Contents/MacOS/iina-cli"
		if _, err := exec.LookPath(iinaPath); err == nil {
			return iinaPath, "iina", nil
		}
	}
	
	// Fallback to mpv (cross-platform)
	mpvPath := "mpv"
	if runtime.GOOS == "windows" {
		mpvPath = "mpv.exe"
	}
	
	if path, err := exec.LookPath(mpvPath); err == nil {
		return path, "mpv", nil
	}
	
	return "", "", fmt.Errorf("no media player found (tried: iina, mpv). Please install mpv or iina")
}

// getPlayerType extracts player type from path
func getPlayerType(path string) string {
	lowerPath := strings.ToLower(path)
	if strings.Contains(lowerPath, "iina") {
		return "iina"
	}
	if strings.Contains(lowerPath, "mpv") {
		return "mpv"
	}
	if strings.Contains(lowerPath, "vlc") {
		return "vlc"
	}
	return "unknown"
}

// Play launches the detected media player to play the given URL
func Play(streamURL, playerPreference string) error {
	playerPath, playerType, err := DetectPlayer(playerPreference)
	if err != nil {
		return err
	}
	
	var args []string
	
	// Build command based on player type
	switch playerType {
	case "iina":
		args = []string{
			"--no-stdin",
			"--keep-running=no",
			streamURL,
		}
	case "mpv":
		args = []string{
			"--force-seekable=yes",
			"--hr-seek=yes",
			"--no-resume-playback",
			streamURL,
		}
	default:
		// Generic player, just pass URL
		args = []string{streamURL}
	}
	
	cmd := exec.Command(playerPath, args...)
	
	// Inherit stdin, stdout, stderr for interactive playback
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	
	// Start player
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start %s: %w", playerType, err)
	}
	
	// Wait for player to finish
	if err := cmd.Wait(); err != nil {
		// Players return non-zero exit codes for various reasons (user quit, etc.)
		// Don't treat this as an error
		return nil
	}
	
	return nil
}

// IsAvailable checks if a media player is available on the system
func IsAvailable(playerPreference string) bool {
	_, _, err := DetectPlayer(playerPreference)
	return err == nil
}

// GetDefaultPath returns the default player preference for the current platform
func GetDefaultPath() string {
	return "auto"
}
