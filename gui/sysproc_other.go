//go:build !windows

package main

import "os/exec"

// configureSysProc is a no-op on non-Windows platforms (no console-window
// problem to suppress).
func configureSysProc(cmd *exec.Cmd) {}
