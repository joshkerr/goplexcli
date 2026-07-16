//go:build !windows

package player

import (
	"os/exec"
	"syscall"
)

// configureMPVProc is a no-op on non-Windows platforms.
func configureMPVProc(cmd *exec.Cmd) {}

// exitSignal returns the name of the signal that killed the process, or ""
// for a normal exit.
func exitSignal(ee *exec.ExitError) string {
	if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return ws.Signal().String()
	}
	return ""
}
