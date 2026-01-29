// Package player provides media playback functionality using external players.
// It supports playing single files or multiple files as a playlist using mpv.
package player

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

// MPVPlayer implements the Player interface using mpv media player.
// It provides high-quality media playback with seeking support.
type MPVPlayer struct {
	// Path is the path to the mpv executable. If empty, "mpv" is used.
	Path string
}

// NewMPVPlayer creates a new MPVPlayer with the specified path.
// If path is empty, the system PATH will be searched for mpv.
func NewMPVPlayer(path string) *MPVPlayer {
	return &MPVPlayer{Path: path}
}

// Play plays a single media URL.
func (p *MPVPlayer) Play(ctx context.Context, url string) error {
	return p.PlayMultiple(ctx, []string{url})
}

// PlayMultiple plays multiple URLs as a playlist.
// Users can navigate between items using 'n' (next) in mpv.
func (p *MPVPlayer) PlayMultiple(ctx context.Context, urls []string) error {
	if len(urls) == 0 {
		return fmt.Errorf("no stream URLs provided")
	}
	return playWithMPV(p.getPath(), urls)
}

// IsAvailable checks if mpv is available on the system.
func (p *MPVPlayer) IsAvailable() bool {
	_, err := exec.LookPath(p.getPath())
	return err == nil
}

// getPath returns the mpv path, defaulting to "mpv" if not set.
func (p *MPVPlayer) getPath() string {
	if p.Path == "" {
		return "mpv"
	}
	return p.Path
}

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

// Play launches MPV to play the given URL.
// This is a convenience function that uses the default player.
func Play(streamURL, mpvPath string) error {
	return playWithMPV(mpvPath, []string{streamURL})
}

// PlayMultiple launches MPV to play multiple URLs sequentially.
// This is a convenience function that uses the default player.
func PlayMultiple(streamURLs []string, mpvPath string) error {
	if len(streamURLs) == 0 {
		return fmt.Errorf("no stream URLs provided")
	}

	return playWithMPV(mpvPath, streamURLs)
}

// IsAvailable checks if MPV is available on the system.
// This is a convenience function for checking availability.
func IsAvailable(mpvPath string) bool {
	if mpvPath == "" {
		mpvPath = "mpv"
	}

	_, err := exec.LookPath(mpvPath)
	return err == nil
}

// GetDefaultPath returns the default MPV path for the current platform.
func GetDefaultPath() string {
	switch runtime.GOOS {
	case "windows":
		return "mpv.exe"
	default:
		return "mpv"
	}
}
