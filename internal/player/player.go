// Package player provides media playback functionality using external players.
// It supports playing single files or multiple files as a playlist using mpv.
package player

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

var plexTokenPattern = regexp.MustCompile(`(?i)(X-Plex-Token=)[^&\s]+`)

// PlaybackError reports that mpv exited with one of its documented failure
// codes (1 = fatal error, 2 = file could not be played, 3 = some files failed)
// or was killed by a signal. A clean exit — including the user quitting — is
// not a PlaybackError.
type PlaybackError struct {
	ExitCode int    // mpv's exit code; -1 when killed by a signal
	Signal   string // signal name when killed by a signal, "" otherwise
	Detail   string // most relevant stderr line, "" if mpv wrote nothing useful
}

func (e *PlaybackError) Error() string {
	cause := fmt.Sprintf("mpv exited %d", e.ExitCode)
	if e.Signal != "" {
		cause = "mpv died: " + e.Signal
	}
	if e.Detail == "" {
		return cause
	}
	return cause + ": " + e.Detail
}

// PlayOutcome describes how an mpv run ended, error or not, so callers can
// spot streams that "played" without ever starting (e.g. an instant EOF that
// exits 0).
type PlayOutcome struct {
	ExitCode  int    // 0 on clean exit; -1 when killed by a signal
	Signal    string // signal name when killed by a signal, "" otherwise
	ErrorLine string // most relevant stderr line, token-redacted
}

// stderrTailLines bounds how much of mpv's stderr is retained for error
// reporting; mpv's status line churn would otherwise grow without limit.
const stderrTailLines = 40

// stderrTail is an io.Writer that keeps the last stderrTailLines lines written
// to it. mpv redraws its status line with \r, so both \r and \n end a line.
type stderrTail struct {
	mu   sync.Mutex
	line []byte
	tail []string
}

func (t *stderrTail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, b := range p {
		if b == '\n' || b == '\r' {
			t.push()
			continue
		}
		t.line = append(t.line, b)
	}
	return len(p), nil
}

func (t *stderrTail) push() {
	if len(t.line) > 0 {
		t.tail = append(t.tail, string(t.line))
		if len(t.tail) > stderrTailLines {
			t.tail = t.tail[1:]
		}
	}
	t.line = t.line[:0]
}

// Lines returns the retained stderr lines, including any unterminated final line.
func (t *stderrTail) Lines() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.push()
	return append([]string(nil), t.tail...)
}

// errorLineFromStderr picks the stderr line most likely to explain a playback
// failure: the last line mentioning an error, or failing that the last
// non-empty line.
func errorLineFromStderr(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.ToLower(lines[i])
		if strings.Contains(l, "error") || strings.Contains(l, "failed") ||
			strings.Contains(l, "cannot") || strings.Contains(l, "unable") {
			return redactPlexToken(lines[i])
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return redactPlexToken(lines[i])
		}
	}
	return ""
}

func redactPlexToken(line string) string {
	return plexTokenPattern.ReplaceAllString(line, "${1}[REDACTED]")
}

// PlaybackOptions configures MPV playback behavior.
type PlaybackOptions struct {
	SocketPath string // IPC socket path for progress tracking (Unix socket or Windows named pipe, empty to disable)
	StartPos   int    // Start position in seconds (0 to start from beginning)
}

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
	_, err := playWithMPV(p.getPath(), urls, PlaybackOptions{})
	return err
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

// buildMPVArgs constructs the argument list for MPV.
func buildMPVArgs(urls []string, socketPath string, startPos int) []string {
	args := []string{
		"--force-seekable=yes",
		"--hr-seek=yes",
	}

	// Add IPC server if specified (Unix socket on macOS/Linux, named pipe on Windows)
	if socketPath != "" {
		args = append(args, fmt.Sprintf("--input-ipc-server=%s", socketPath))
	} else {
		// Only disable resume playback if we're not tracking
		args = append(args, "--no-resume-playback")
	}

	// Add start position if specified
	if startPos > 0 {
		args = append(args, fmt.Sprintf("--start=%d", startPos))
	}

	args = append(args, urls...)
	return args
}

// playWithMPV executes mpv and reports how the run ended. The outcome is
// non-nil whenever mpv actually ran, error or not.
func playWithMPV(mpvPath string, streamURLs []string, opts PlaybackOptions) (*PlayOutcome, error) {
	if mpvPath == "" {
		mpvPath = "mpv"
	}

	// Check if mpv is available
	if _, err := exec.LookPath(mpvPath); err != nil {
		return nil, fmt.Errorf("mpv not found in PATH. Please install mpv or specify the path in config")
	}

	// Build mpv command using buildMPVArgs
	args := buildMPVArgs(streamURLs, opts.SocketPath, opts.StartPos)

	cmd := exec.Command(mpvPath, args...)

	// Keep the tail of stderr so a failing mpv can explain itself.
	tail := &stderrTail{}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = tail

	// On Windows, prevent a stray console window from appearing when launched
	// from a GUI app (or via a console-mode shim). No-op on other platforms.
	// mpv's own video window is unaffected.
	configureMPVProc(cmd)

	// Start mpv
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start mpv: %w", err)
	}

	// Wait for mpv to finish. Exit codes 1-3 are mpv's documented failure
	// modes and a signal death is a crash; any other exit counts as the user
	// ending playback and is not an error — but the outcome still carries the
	// diagnostics.
	waitErr := cmd.Wait()
	outcome := &PlayOutcome{ErrorLine: errorLineFromStderr(tail.Lines())}
	if waitErr != nil {
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			outcome.ExitCode = ee.ExitCode()
			if sig := exitSignal(ee); sig != "" {
				outcome.Signal = sig
				return outcome, &PlaybackError{ExitCode: -1, Signal: sig, Detail: outcome.ErrorLine}
			}
			if code := ee.ExitCode(); code >= 1 && code <= 3 {
				return outcome, &PlaybackError{ExitCode: code, Detail: outcome.ErrorLine}
			}
		}
	}
	return outcome, nil
}

// Play launches MPV to play the given URL.
// This is a convenience function that uses the default player.
func Play(streamURL, mpvPath string) error {
	_, err := playWithMPV(mpvPath, []string{streamURL}, PlaybackOptions{})
	return err
}

// PlayMultiple launches MPV to play multiple URLs sequentially.
// This is a convenience function that uses the default player.
func PlayMultiple(streamURLs []string, mpvPath string) error {
	if len(streamURLs) == 0 {
		return fmt.Errorf("no stream URLs provided")
	}

	_, err := playWithMPV(mpvPath, streamURLs, PlaybackOptions{})
	return err
}

// PlayMultipleWithOptions launches MPV with custom options and reports how the
// run ended. The outcome is non-nil whenever mpv actually ran, error or not.
func PlayMultipleWithOptions(streamURLs []string, mpvPath string, opts PlaybackOptions) (*PlayOutcome, error) {
	if len(streamURLs) == 0 {
		return nil, fmt.Errorf("no stream URLs provided")
	}
	return playWithMPV(mpvPath, streamURLs, opts)
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
