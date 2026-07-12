//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// CREATE_NO_WINDOW prevents Windows from allocating a console window for a
// console-mode child process spawned by a GUI app. Without it, every rclone
// invocation pops up a black console window.
const createNoWindow = 0x08000000

func configureSysProc(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}

// createNewProcessGroup lets a spawned process outlive its parent's process
// group, so the update helper keeps running after the GUI quits.
const createNewProcessGroup = 0x00000200

// detachSysProc configures cmd so it survives this process exiting (used to
// launch the self-update helper before the GUI quits), with no console window.
func detachSysProc(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow | createNewProcessGroup,
	}
}
