// Package logging provides structured logging for goplexcli using slog.
// It supports configurable log levels and can be enabled via --verbose flag.
package logging

import (
	"io"
	"log/slog"
	"os"
	"sync"
)

var (
	// logger is the global logger instance
	logger *slog.Logger

	// logLevel controls the minimum log level
	logLevel = new(slog.LevelVar)

	// once ensures initialization happens only once
	once sync.Once

	// output is the destination for log output (default: stderr)
	output io.Writer = os.Stderr
)

// Level constants for convenience
const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
)

// Init initializes the logger with the specified options.
// This should be called early in main() before any logging occurs.
// If not called, a default logger will be created on first use.
func Init(opts ...Option) {
	once.Do(func() {
		cfg := &config{
			level:  LevelInfo,
			output: os.Stderr,
		}

		for _, opt := range opts {
			opt(cfg)
		}

		logLevel.Set(cfg.level)
		output = cfg.output

		handler := slog.NewTextHandler(output, &slog.HandlerOptions{
			Level: logLevel,
		})

		logger = slog.New(handler)
	})
}

// config holds logger configuration
type config struct {
	level  slog.Level
	output io.Writer
}

// Option is a functional option for configuring the logger
type Option func(*config)

// WithLevel sets the minimum log level
func WithLevel(level slog.Level) Option {
	return func(c *config) {
		c.level = level
	}
}

// WithOutput sets the output destination
func WithOutput(w io.Writer) Option {
	return func(c *config) {
		c.output = w
	}
}

// WithVerbose enables verbose (debug) logging
func WithVerbose(verbose bool) Option {
	return func(c *config) {
		if verbose {
			c.level = LevelDebug
		}
	}
}

// getLogger returns the logger, initializing with defaults if needed
func getLogger() *slog.Logger {
	if logger == nil {
		Init()
	}
	return logger
}

// SetLevel changes the log level at runtime
func SetLevel(level slog.Level) {
	logLevel.Set(level)
}

// SetVerbose is a convenience function to enable/disable verbose logging
func SetVerbose(verbose bool) {
	if verbose {
		logLevel.Set(LevelDebug)
	} else {
		logLevel.Set(LevelInfo)
	}
}

// Debug logs a debug message
func Debug(msg string, args ...any) {
	getLogger().Debug(msg, args...)
}

// Info logs an info message
func Info(msg string, args ...any) {
	getLogger().Info(msg, args...)
}

// Warn logs a warning message
func Warn(msg string, args ...any) {
	getLogger().Warn(msg, args...)
}

// Error logs an error message
func Error(msg string, args ...any) {
	getLogger().Error(msg, args...)
}

// With returns a new logger with the given attributes
func With(args ...any) *slog.Logger {
	return getLogger().With(args...)
}

// Logger returns the underlying slog.Logger for advanced use cases
func Logger() *slog.Logger {
	return getLogger()
}

// Enabled returns true if the given level is enabled
func Enabled(level slog.Level) bool {
	return logLevel.Level() <= level
}

// IsVerbose returns true if verbose (debug) logging is enabled
func IsVerbose() bool {
	return Enabled(LevelDebug)
}
