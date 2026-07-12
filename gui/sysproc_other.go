//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// configureSysProc is a no-op on non-Windows platforms (no console-window
// problem to suppress).
func configureSysProc(cmd *exec.Cmd) {}

// detachSysProc puts the spawned process in its own session so it survives the
// GUI exiting (used to launch the self-update helper before the app quits).
func detachSysProc(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
