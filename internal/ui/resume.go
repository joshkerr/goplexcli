package ui

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/joshkerr/goplexcli/internal/plex"
	"github.com/joshkerr/goplexcli/internal/progress"
)

// ResumeChoice represents the user's choice for resuming playback.
type ResumeChoice int

const (
	// ResumeFromPosition indicates the user wants to resume from saved position.
	ResumeFromPosition ResumeChoice = iota
	// StartFromBeginning indicates the user wants to start from the beginning.
	StartFromBeginning
)

// MultiResumeChoice represents the user's choice when multiple items have progress.
type MultiResumeChoice int

const (
	// ResumeAll indicates resuming all items from their saved positions.
	ResumeAll MultiResumeChoice = iota
	// StartAllFromBeginning indicates starting all items from the beginning.
	StartAllFromBeginning
	// ChooseIndividually indicates the user wants to choose for each item.
	ChooseIndividually
)

// HasResumableProgress returns true if the media has progress that can be resumed.
// Returns false if no progress or if >=95% complete (treated as watched).
func HasResumableProgress(media *plex.MediaItem) bool {
	if media.ViewOffset <= 0 || media.Duration <= 0 {
		return false
	}

	// If >=95% complete, treat as watched
	percentComplete := float64(media.ViewOffset) / float64(media.Duration)
	if percentComplete >= 0.95 {
		return false
	}

	return true
}

// CountItemsWithProgress counts how many items in the list have resumable progress.
func CountItemsWithProgress(items []*plex.MediaItem) int {
	count := 0
	for _, item := range items {
		if HasResumableProgress(item) {
			count++
		}
	}
	return count
}

// formatResumeOption formats the resume option text.
func formatResumeOption(viewOffset int) string {
	return fmt.Sprintf("Resume from %s", progress.FormatDuration(viewOffset))
}

// formatResumeHeader formats the header text for the resume prompt.
func formatResumeHeader(title string, viewOffset int, duration int) string {
	percent := 0
	if duration > 0 {
		percent = viewOffset * 100 / duration
	}
	return fmt.Sprintf("%q has saved progress at %s / %s (%d%%)",
		title,
		progress.FormatDuration(viewOffset),
		progress.FormatDuration(duration),
		percent,
	)
}

// ResumePromptOptions contains the options for the resume prompt.
type ResumePromptOptions struct {
	Title      string
	ViewOffset int // milliseconds
	Duration   int // milliseconds
	FzfPath    string
}

// PromptResume displays a resume prompt using fzf and returns the user's choice.
func PromptResume(opts ResumePromptOptions) (ResumeChoice, error) {
	resumeText := fmt.Sprintf("> %s", formatResumeOption(opts.ViewOffset))
	beginningText := "  Start from beginning"

	options := []string{resumeText, beginningText}
	header := formatResumeHeader(opts.Title, opts.ViewOffset, opts.Duration)

	selected, err := runFzfWithHeader(options, opts.FzfPath, header)
	if err != nil {
		return StartFromBeginning, err
	}

	if selected == resumeText {
		return ResumeFromPosition, nil
	}
	return StartFromBeginning, nil
}

// PromptMultiResume displays a prompt when multiple items have progress.
func PromptMultiResume(itemsWithProgress int, totalItems int, fzfPath string) (MultiResumeChoice, error) {
	options := []string{
		"> Resume all from saved positions",
		"  Start all from beginning",
		"  Choose individually for each",
	}

	header := fmt.Sprintf("%d of %d videos have saved progress", itemsWithProgress, totalItems)

	selected, err := runFzfWithHeader(options, fzfPath, header)
	if err != nil {
		return StartAllFromBeginning, err
	}

	switch selected {
	case options[0]:
		return ResumeAll, nil
	case options[1]:
		return StartAllFromBeginning, nil
	case options[2]:
		return ChooseIndividually, nil
	default:
		return StartAllFromBeginning, nil
	}
}

// runFzfWithHeader runs fzf with the given options and a header, returning the selected item.
func runFzfWithHeader(options []string, fzfPath string, header string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("no options to select from")
	}

	if fzfPath == "" {
		fzfPath = "fzf"
	}

	// Check if fzf is available
	if _, err := exec.LookPath(fzfPath); err != nil {
		return "", fmt.Errorf("fzf not found in PATH. Please install fzf or specify the path in config")
	}

	// Join options with newlines
	input := strings.Join(options, "\n")

	// Build fzf command with header
	args := []string{
		"--height=10",
		"--layout=reverse",
		"--no-multi",
		"--ansi",
		"--no-sort",
	}

	if header != "" {
		args = append(args, "--header", header)
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
				return "", fmt.Errorf("cancelled by user")
			}
		}
		return "", fmt.Errorf("fzf failed: %w", err)
	}

	// Get selected item
	selected := strings.TrimSpace(outBuf.String())
	if selected == "" {
		return "", fmt.Errorf("no selection made")
	}

	return selected, nil
}
