package player

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// DetectPlayer finds the best available media player based on preference
// preference can be: "auto", "iina", "mpv", "vlc", or a custom path
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
		
		// Try to find the named player in PATH
		if path, err := exec.LookPath(preference); err == nil {
			return path, preference, nil
		}
		
		// Platform-specific app bundle checks
		if runtime.GOOS == "darwin" {
			// Check for IINA.app if "iina" was specified
			if preference == "iina" {
				iinaPath := "/Applications/IINA.app/Contents/MacOS/iina-cli"
				if _, err := exec.LookPath(iinaPath); err == nil {
					return iinaPath, "iina", nil
				}
			}
			// Check for VLC.app if "vlc" was specified
			if preference == "vlc" {
				vlcPath := "/Applications/VLC.app/Contents/MacOS/VLC"
				if _, err := exec.LookPath(vlcPath); err == nil {
					return vlcPath, "vlc", nil
				}
			}
		}
		
		return "", "", fmt.Errorf("player '%s' not found in PATH", preference)
	}
	
	// Auto-detect: prefer iina on macOS, then try mpv and vlc
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
	
	// Try mpv (cross-platform)
	mpvPath := "mpv"
	if runtime.GOOS == "windows" {
		mpvPath = "mpv.exe"
	}
	
	if path, err := exec.LookPath(mpvPath); err == nil {
		return path, "mpv", nil
	}
	
	// Try VLC as final fallback (cross-platform)
	vlcPaths := getVLCPaths()
	for _, vlcPath := range vlcPaths {
		if path, err := exec.LookPath(vlcPath); err == nil {
			return path, "vlc", nil
		}
	}
	
	return "", "", fmt.Errorf("no media player found (tried: iina, mpv, vlc). Please install mpv, vlc, or iina")
}

// getVLCPaths returns platform-specific VLC executable paths
func getVLCPaths() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"vlc",                                  // Homebrew or PATH
			"/Applications/VLC.app/Contents/MacOS/VLC", // Standard app bundle
		}
	case "windows":
		return []string{
			"vlc.exe",                              // PATH
			"C:\\Program Files\\VideoLAN\\VLC\\vlc.exe",
			"C:\\Program Files (x86)\\VideoLAN\\VLC\\vlc.exe",
		}
	default: // Linux and others
		return []string{
			"vlc",     // Standard binary name
			"cvlc",    // Command-line VLC (no GUI)
		}
	}
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
	case "vlc":
		args = []string{
			"--play-and-exit",     // Exit after playback
			"--no-video-title-show", // Don't show filename overlay
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
