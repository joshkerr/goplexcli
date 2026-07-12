//go:build windows

package update

import (
	"os/exec"
	"syscall"
)

// createNoWindow stops Windows from allocating a console window for a
// console-mode child spawned by a GUI process.
const createNoWindow = 0x08000000

// hideConsoleWindow configures cmd so spawning it doesn't flash a console
// window — needed when the desktop GUI shells out to `gh` during update checks.
func hideConsoleWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
