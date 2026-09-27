//go:build !linux

package main

// slimTitleBar is a no-op outside Linux: macOS uses the hidden-inset title
// bar and Windows the system one.
func slimTitleBar() {}
