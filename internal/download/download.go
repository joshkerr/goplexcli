package download

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	rclone "github.com/joshkerr/rclone-golib"
)

// generateTransferID creates a unique transfer ID using crypto/rand
func generateTransferID(index int, filename string) string {
	b := make([]byte, 8)
	// crypto/rand.Read always returns n == len(b) and err == nil for valid byte slices
	// The only way it can fail is if the random source is unreadable, which is a fatal system error
	// We ignore the error as recommended in crypto/rand documentation for this use case
	_, _ = rand.Read(b)
	return fmt.Sprintf("download_%s_%d_%s", hex.EncodeToString(b), index, filename)
}

// Download downloads a file from rclone remote to the current directory
func Download(ctx context.Context, rclonePath, destinationDir, rcloneBinary string) error {
	if rclonePath == "" {
		return fmt.Errorf("rclone path is empty")
	}

	if rcloneBinary == "" {
		rcloneBinary = "rclone"
	}

	// Check if rclone is available
	if _, err := exec.LookPath(rcloneBinary); err != nil {
		return fmt.Errorf("rclone not found in PATH. Please install rclone or specify the path in config")
	}

	// Get the filename from the rclone path
	filename := filepath.Base(rclonePath)

	// Set destination to current directory if not specified
	if destinationDir == "" {
		var err error
		destinationDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(destinationDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	destinationPath := filepath.Join(destinationDir, filename)

	// Create transfer manager and executor
	manager := rclone.NewManager()
	transferID := fmt.Sprintf("download_%d", time.Now().UnixNano())

	// Add transfer to manager
	manager.Add(transferID, rclonePath, destinationPath)

	// Start the Bubble Tea UI for progress in a goroutine
	var wg sync.WaitGroup
	var uiErr error
	uiReady := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		p := tea.NewProgram(rclone.NewModel(manager))
		// Signal that UI is ready
		close(uiReady)
		if _, err := p.Run(); err != nil {
			uiErr = err
		}
	}()

	// Wait for UI to be ready before proceeding
	<-uiReady

	// Create executor
	executor := rclone.NewExecutor(manager)

	// Mark as started
	manager.Start(transferID)

	// Configure rclone options
	opts := rclone.RcloneOptions{
		Command:       rclone.RcloneCopyTo,
		Source:        rclonePath,
		Destination:   destinationPath,
		StatsInterval: "500ms",
		Context:       ctx,
	}

	// Execute the transfer
	err := executor.Execute(transferID, opts)
	if err != nil {
		manager.Fail(transferID, err)
		wg.Wait() // Wait for UI to finish
		return fmt.Errorf("download failed: %w", err)
	}

	manager.Complete(transferID)

	// Set modification time to now instead of preserving server time
	now := time.Now()
	if err := os.Chtimes(destinationPath, now, now); err != nil {
		// Log but don't fail the download for this
		fmt.Fprintf(os.Stderr, "warning: could not set modification time: %v\n", err)
	}

	// Wait for UI to finish
	wg.Wait()

	if uiErr != nil {
		return fmt.Errorf("UI error: %w", uiErr)
	}

	return nil
}

// DownloadMultiple downloads multiple files from rclone remote to the current directory
func DownloadMultiple(ctx context.Context, rclonePaths []string, destinationDir, rcloneBinary string) error {
	if len(rclonePaths) == 0 {
		return fmt.Errorf("no rclone paths provided")
	}

	if rcloneBinary == "" {
		rcloneBinary = "rclone"
	}

	// Check if rclone is available
	if _, err := exec.LookPath(rcloneBinary); err != nil {
		return fmt.Errorf("rclone not found in PATH. Please install rclone or specify the path in config")
	}

	// Set destination to current directory if not specified
	if destinationDir == "" {
		var err error
		destinationDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(destinationDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Create transfer manager and executor
	manager := rclone.NewManager()

	// Add all transfers to manager
	var transferIDs []string
	for i, rclonePath := range rclonePaths {
		filename := filepath.Base(rclonePath)
		destinationPath := filepath.Join(destinationDir, filename)
		transferID := generateTransferID(i, filename)
		transferIDs = append(transferIDs, transferID)
		manager.Add(transferID, rclonePath, destinationPath)
	}

	// Start the Bubble Tea UI for progress in a goroutine
	var wg sync.WaitGroup
	var uiErr error
	uiReady := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		p := tea.NewProgram(rclone.NewModel(manager))
		// Signal that UI is ready
		close(uiReady)
		if _, err := p.Run(); err != nil {
			uiErr = err
		}
	}()

	// Wait for UI to be ready before proceeding
	<-uiReady

	// Create executor
	executor := rclone.NewExecutor(manager)

	// Execute transfers sequentially
	var firstErr error
	for i, transferID := range transferIDs {
		manager.Start(transferID)

		opts := rclone.RcloneOptions{
			Command:       rclone.RcloneCopyTo,
			Source:        rclonePaths[i],
			Destination:   filepath.Join(destinationDir, filepath.Base(rclonePaths[i])),
			StatsInterval: "500ms",
			Context:       ctx,
		}

		err := executor.Execute(transferID, opts)
		if err != nil {
			manager.Fail(transferID, err)
			if firstErr == nil {
				firstErr = err
			}
		} else {
			manager.Complete(transferID)
			// Set modification time to now instead of preserving server time
			destPath := filepath.Join(destinationDir, filepath.Base(rclonePaths[i]))
			now := time.Now()
			if chErr := os.Chtimes(destPath, now, now); chErr != nil {
				fmt.Fprintf(os.Stderr, "warning: could not set modification time for %s: %v\n", destPath, chErr)
			}
		}
	}

	// Wait for UI to finish
	wg.Wait()

	if uiErr != nil {
		return fmt.Errorf("UI error: %w", uiErr)
	}

	if firstErr != nil {
		return fmt.Errorf("download failed: %w", firstErr)
	}

	return nil
}

// IsAvailable checks if rclone is available on the system
func IsAvailable(rclonePath string) bool {
	if rclonePath == "" {
		rclonePath = "rclone"
	}

	_, err := exec.LookPath(rclonePath)
	return err == nil
}
