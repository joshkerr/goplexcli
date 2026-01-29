package player

import (
	"fmt"
	"os/exec"
	"runtime"
)

// playWithMPV is a helper function that executes mpv with the given arguments
func playWithMPV(mpvPath string, streamURLs []string) error {
	if mpvPath == "" {
		mpvPath = "mpv"
	}

	// Check if mpv is available
	if _, err := exec.LookPath(mpvPath); err != nil {
		return fmt.Errorf("mpv not found in PATH. Please install mpv or specify the path in config")
	}

	// Build mpv command
	args := []string{
		"--force-seekable=yes",
		"--hr-seek=yes",
		"--no-resume-playback",
	}
	args = append(args, streamURLs...)

	cmd := exec.Command(mpvPath, args...)

	// Inherit stdin, stdout, stderr for interactive playback
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	// Start mpv
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start mpv: %w", err)
	}

	// Wait for mpv to finish
	if err := cmd.Wait(); err != nil {
		// mpv returns non-zero exit codes for various reasons (user quit, etc.)
		// Don't treat this as an error
		return nil
	}

	return nil
}

// Play launches MPV to play the given URL
func Play(streamURL, mpvPath string) error {
	return playWithMPV(mpvPath, []string{streamURL})
}

// PlayMultiple launches MPV to play multiple URLs sequentially
func PlayMultiple(streamURLs []string, mpvPath string) error {
	if len(streamURLs) == 0 {
		return fmt.Errorf("no stream URLs provided")
	}

	return playWithMPV(mpvPath, streamURLs)
}

// IsAvailable checks if MPV is available on the system
func IsAvailable(mpvPath string) bool {
	if mpvPath == "" {
		mpvPath = "mpv"
	}

	_, err := exec.LookPath(mpvPath)
	return err == nil
}

// GetDefaultPath returns the default MPV path for the current platform
func GetDefaultPath() string {
	switch runtime.GOOS {
	case "windows":
		return "mpv.exe"
	default:
		return "mpv"
	}
}
