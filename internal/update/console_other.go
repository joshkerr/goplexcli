//go:build !windows

package update

import "os/exec"

// hideConsoleWindow is a no-op off Windows (no console-window flash to suppress).
func hideConsoleWindow(cmd *exec.Cmd) {}
