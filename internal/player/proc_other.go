//go:build !windows

package player

import "os/exec"

// configureMPVProc is a no-op on non-Windows platforms.
func configureMPVProc(cmd *exec.Cmd) {}
