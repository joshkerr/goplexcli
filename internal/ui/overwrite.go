package ui

import "fmt"

// OverwriteChoice represents the user's choice when destination files already exist.
type OverwriteChoice int

const (
	// ReplaceAll indicates deleting each existing destination file and downloading everything.
	ReplaceAll OverwriteChoice = iota
	// SkipExisting indicates downloading only the files that don't already exist.
	SkipExisting
	// CancelBatch indicates aborting the whole batch without downloading anything.
	CancelBatch
)

// PromptMultiOverwrite displays a prompt when destination files already exist,
// asking whether to replace them, skip them, or cancel the batch.
func PromptMultiOverwrite(conflictCount int, totalItems int, fzfPath string) (OverwriteChoice, error) {
	options := []string{
		"> Replace all (delete existing files, download everything)",
		"  Skip existing (download only the rest)",
		"  Cancel",
	}

	header := fmt.Sprintf("%d of %d files already exist at the destination", conflictCount, totalItems)

	selected, err := runFzfWithHeader(options, fzfPath, header)
	if err != nil {
		return CancelBatch, err
	}

	switch selected {
	case options[0]:
		return ReplaceAll, nil
	case options[1]:
		return SkipExisting, nil
	case options[2]:
		return CancelBatch, nil
	default:
		return CancelBatch, nil
	}
}
