// Package errors provides domain-specific error types for goplexcli.
// These errors provide structured context for debugging and enable
// callers to handle errors appropriately using errors.As().
package errors

import (
	"errors"
	"fmt"
)

// Common sentinel errors for error checking
var (
	// ErrNotFound indicates a requested resource was not found
	ErrNotFound = errors.New("not found")

	// ErrInvalidConfig indicates the configuration is invalid
	ErrInvalidConfig = errors.New("invalid configuration")

	// ErrAuthRequired indicates authentication is required
	ErrAuthRequired = errors.New("authentication required")

	// ErrConnectionFailed indicates a connection to a server failed
	ErrConnectionFailed = errors.New("connection failed")

	// ErrCancelled indicates the operation was cancelled by the user
	ErrCancelled = errors.New("cancelled by user")
)

// PlexError represents an error that occurred while interacting with the Plex API.
type PlexError struct {
	Op         string // Operation being performed (e.g., "GetAllMedia", "Authenticate")
	Server     string // Server URL or name
	StatusCode int    // HTTP status code, if applicable
	Err        error  // Underlying error
}

func (e *PlexError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("plex %s on %s: HTTP %d: %v", e.Op, e.Server, e.StatusCode, e.Err)
	}
	return fmt.Sprintf("plex %s on %s: %v", e.Op, e.Server, e.Err)
}

func (e *PlexError) Unwrap() error {
	return e.Err
}

// NewPlexError creates a new PlexError.
func NewPlexError(op, server string, err error) *PlexError {
	return &PlexError{Op: op, Server: server, Err: err}
}

// NewPlexErrorWithStatus creates a new PlexError with an HTTP status code.
func NewPlexErrorWithStatus(op, server string, statusCode int, err error) *PlexError {
	return &PlexError{Op: op, Server: server, StatusCode: statusCode, Err: err}
}

// ConfigError represents an error related to configuration.
type ConfigError struct {
	Field   string // Config field that has an issue
	Message string // Human-readable description
	Err     error  // Underlying error, if any
}

func (e *ConfigError) Error() string {
	if e.Field != "" {
		if e.Err != nil {
			return fmt.Sprintf("config %s: %s: %v", e.Field, e.Message, e.Err)
		}
		return fmt.Sprintf("config %s: %s", e.Field, e.Message)
	}
	if e.Err != nil {
		return fmt.Sprintf("config: %s: %v", e.Message, e.Err)
	}
	return fmt.Sprintf("config: %s", e.Message)
}

func (e *ConfigError) Unwrap() error {
	return e.Err
}

// NewConfigError creates a new ConfigError.
func NewConfigError(field, message string) *ConfigError {
	return &ConfigError{Field: field, Message: message}
}

// NewConfigErrorWithCause creates a new ConfigError with an underlying cause.
func NewConfigErrorWithCause(field, message string, err error) *ConfigError {
	return &ConfigError{Field: field, Message: message, Err: err}
}

// DownloadError represents an error that occurred during a download operation.
type DownloadError struct {
	Op     string // Operation (e.g., "Download", "DownloadMultiple")
	Path   string // File path or rclone path involved
	Reason string // Human-readable reason
	Err    error  // Underlying error
}

func (e *DownloadError) Error() string {
	if e.Path != "" {
		if e.Err != nil {
			return fmt.Sprintf("download %s %s: %s: %v", e.Op, e.Path, e.Reason, e.Err)
		}
		return fmt.Sprintf("download %s %s: %s", e.Op, e.Path, e.Reason)
	}
	if e.Err != nil {
		return fmt.Sprintf("download %s: %s: %v", e.Op, e.Reason, e.Err)
	}
	return fmt.Sprintf("download %s: %s", e.Op, e.Reason)
}

func (e *DownloadError) Unwrap() error {
	return e.Err
}

// NewDownloadError creates a new DownloadError.
func NewDownloadError(op, path, reason string, err error) *DownloadError {
	return &DownloadError{Op: op, Path: path, Reason: reason, Err: err}
}

// PlayerError represents an error that occurred during media playback.
type PlayerError struct {
	Op     string // Operation (e.g., "Play", "PlayMultiple")
	Player string // Player name (e.g., "mpv")
	Reason string // Human-readable reason
	Err    error  // Underlying error
}

func (e *PlayerError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s %s: %s: %v", e.Player, e.Op, e.Reason, e.Err)
	}
	return fmt.Sprintf("%s %s: %s", e.Player, e.Op, e.Reason)
}

func (e *PlayerError) Unwrap() error {
	return e.Err
}

// NewPlayerError creates a new PlayerError.
func NewPlayerError(op, player, reason string, err error) *PlayerError {
	return &PlayerError{Op: op, Player: player, Reason: reason, Err: err}
}

// QueueError represents an error that occurred during queue operations.
type QueueError struct {
	Op     string // Operation (e.g., "Load", "Save", "Add")
	Reason string // Human-readable reason
	Err    error  // Underlying error
}

func (e *QueueError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("queue %s: %s: %v", e.Op, e.Reason, e.Err)
	}
	return fmt.Sprintf("queue %s: %s", e.Op, e.Reason)
}

func (e *QueueError) Unwrap() error {
	return e.Err
}

// NewQueueError creates a new QueueError.
func NewQueueError(op, reason string, err error) *QueueError {
	return &QueueError{Op: op, Reason: reason, Err: err}
}

// CacheError represents an error that occurred during cache operations.
type CacheError struct {
	Op     string // Operation (e.g., "Load", "Save")
	Path   string // Cache file path, if relevant
	Reason string // Human-readable reason
	Err    error  // Underlying error
}

func (e *CacheError) Error() string {
	if e.Path != "" {
		if e.Err != nil {
			return fmt.Sprintf("cache %s %s: %s: %v", e.Op, e.Path, e.Reason, e.Err)
		}
		return fmt.Sprintf("cache %s %s: %s", e.Op, e.Path, e.Reason)
	}
	if e.Err != nil {
		return fmt.Sprintf("cache %s: %s: %v", e.Op, e.Reason, e.Err)
	}
	return fmt.Sprintf("cache %s: %s", e.Op, e.Reason)
}

func (e *CacheError) Unwrap() error {
	return e.Err
}

// NewCacheError creates a new CacheError.
func NewCacheError(op, path, reason string, err error) *CacheError {
	return &CacheError{Op: op, Path: path, Reason: reason, Err: err}
}

// StreamError represents an error that occurred during stream operations.
type StreamError struct {
	Op     string // Operation (e.g., "Publish", "Discover")
	Reason string // Human-readable reason
	Err    error  // Underlying error
}

func (e *StreamError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("stream %s: %s: %v", e.Op, e.Reason, e.Err)
	}
	return fmt.Sprintf("stream %s: %s", e.Op, e.Reason)
}

func (e *StreamError) Unwrap() error {
	return e.Err
}

// NewStreamError creates a new StreamError.
func NewStreamError(op, reason string, err error) *StreamError {
	return &StreamError{Op: op, Reason: reason, Err: err}
}
