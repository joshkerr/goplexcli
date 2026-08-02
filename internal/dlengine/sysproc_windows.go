//go:build windows

package dlengine

import (
	"os/exec"
	"syscall"
)

// CREATE_NO_WINDOW prevents Windows from allocating a console window for a
// console-mode child process spawned by a GUI app. Without it, every rclone
// invocation pops up a black console window.
const createNoWindow = 0x08000000

// ConfigureSysProc hides the console window rclone would otherwise pop up when
// spawned from a GUI process. Harmless for console hosts (the serve daemon).
func ConfigureSysProc(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
