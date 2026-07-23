//go:build !windows && !darwin

package main

// setTaskbarProgress is a no-op on platforms without a taskbar/dock progress
// API (Linux would need the Unity LauncherEntry DBus protocol, which few
// desktops still implement).
func setTaskbarProgress(float64) {}
