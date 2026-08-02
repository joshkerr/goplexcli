//go:build !windows

package dlengine

import "os/exec"

// ConfigureSysProc is a no-op on non-Windows platforms (no console-window
// problem to suppress).
func ConfigureSysProc(cmd *exec.Cmd) {}
