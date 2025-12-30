package download

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	rclone "github.com/joshkerr/rclone-golib"
)

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
		return fmt.Errorf("download failed: %w", err)
	}
	
	manager.Complete(transferID)
	
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
