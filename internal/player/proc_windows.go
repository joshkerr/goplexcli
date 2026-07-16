//go:build windows

package player

import (
	"os/exec"
	"syscall"
)

// CREATE_NO_WINDOW stops Windows from allocating a console window for a
// console-mode child process (e.g. mpv launched via a scoop shim) when the
// parent is a GUI application.
const createNoWindow = 0x08000000

func configureMPVProc(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}

// exitSignal always returns "" on Windows, which has no POSIX signals.
func exitSignal(ee *exec.ExitError) string {
	return ""
}
