package main

import (
	"path/filepath"
	"testing"
)

func TestAppBundleRoot(t *testing.T) {
	cases := map[string]string{
		// Standard macOS layout: exe lives in .app/Contents/MacOS/.
		"/Applications/GoplexCLI.app/Contents/MacOS/goplexcli-gui":             "/Applications/GoplexCLI.app",
		"/Users/j/Applications/goplexcli-gui.app/Contents/MacOS/goplexcli-gui": "/Users/j/Applications/goplexcli-gui.app",
		// Not inside a bundle (e.g. a bare binary or Windows exe) -> "".
		"/usr/local/bin/goplexcli-gui": "",
		"/tmp/goplexcli-gui":           "",
	}
	for exe, want := range cases {
		if got := appBundleRoot(filepath.FromSlash(exe)); got != filepath.FromSlash(want) {
			t.Errorf("appBundleRoot(%q) = %q, want %q", exe, got, want)
		}
	}
}

func TestGuiAssetNameNonEmptyOnDesktop(t *testing.T) {
	// The updater only supports the desktop platforms it publishes bundles for.
	// On the test host (one of darwin/linux/windows) darwin+windows return a
	// name; linux returns "" (no GUI bundle published). Just assert the mapping
	// is stable for the two supported ones via direct string checks.
	if got := desktopAsset("darwin"); got != "goplexcli-gui-darwin-universal.zip" {
		t.Errorf("darwin asset = %q", got)
	}
	if got := desktopAsset("windows"); got != "goplexcli-gui-windows-amd64.zip" {
		t.Errorf("windows asset = %q", got)
	}
	if got := desktopAsset("linux"); got != "" {
		t.Errorf("linux asset = %q, want empty", got)
	}
}
