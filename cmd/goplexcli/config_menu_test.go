package main

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/joshkerr/goplexcli/internal/config"
)

// isolateConfig points the config dir at a temp dir. USERPROFILE and APPDATA
// must be overridden too — HOME alone doesn't redirect GetConfigDir on
// Windows.
func isolateConfig(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("APPDATA", tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("XDG_CONFIG_HOME", "")
}

// TestConfigMenuEditAndSave scripts a session: set the download directory,
// toggle sorted folders, set the serve name, save. The saved config must
// round-trip the edits.
func TestConfigMenuEditAndSave(t *testing.T) {
	isolateConfig(t)
	in := bufio.NewReader(strings.NewReader(
		"1\nD:/Media\n" + // download directory
			"2\n" + // toggle sort on
			"8\nserver-box\n" + // serve name
			"s\n", // save & exit
	))
	if err := configMenuLoop(in, &config.Config{}); err != nil {
		t.Fatalf("configMenuLoop: %v", err)
	}

	saved, err := config.Load()
	if err != nil {
		t.Fatalf("Load after save: %v", err)
	}
	if saved.DownloadDir != "D:/Media" {
		t.Errorf("DownloadDir = %q, want D:/Media", saved.DownloadDir)
	}
	if !saved.SortDownloads {
		t.Error("SortDownloads = false, want true after toggle")
	}
	if saved.ServeName != "server-box" {
		t.Errorf("ServeName = %q, want server-box", saved.ServeName)
	}
}

// TestConfigMenuQuitDiscards checks that quitting (with the confirm prompt)
// writes nothing to disk.
func TestConfigMenuQuitDiscards(t *testing.T) {
	isolateConfig(t)
	in := bufio.NewReader(strings.NewReader(
		"2\n" + // make a change so quit asks to confirm
			"q\ny\n", // quit, confirm discard
	))
	if err := configMenuLoop(in, &config.Config{}); err != nil {
		t.Fatalf("configMenuLoop: %v", err)
	}
	path, err := config.GetConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("config file written despite quitting without saving")
	}
}

// TestConfigMenuTokenRegen checks that option 9 generates and saves a serve
// token after confirmation.
func TestConfigMenuTokenRegen(t *testing.T) {
	isolateConfig(t)
	in := bufio.NewReader(strings.NewReader("9\ny\ns\n"))
	if err := configMenuLoop(in, &config.Config{}); err != nil {
		t.Fatalf("configMenuLoop: %v", err)
	}
	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.ServeToken) != 64 {
		t.Errorf("ServeToken length = %d, want 64 hex chars", len(saved.ServeToken))
	}
}

// TestConfigMenuClearValue checks the "-" convention: clearing a set value
// empties it back to the default.
func TestConfigMenuClearValue(t *testing.T) {
	isolateConfig(t)
	in := bufio.NewReader(strings.NewReader("1\n-\ns\n"))
	if err := configMenuLoop(in, &config.Config{DownloadDir: "D:/Old"}); err != nil {
		t.Fatalf("configMenuLoop: %v", err)
	}
	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.DownloadDir != "" {
		t.Errorf("DownloadDir = %q, want cleared", saved.DownloadDir)
	}
}

// TestConfigMenuEOFLeaves checks that EOF (closed stdin) exits cleanly
// without saving.
func TestConfigMenuEOFLeaves(t *testing.T) {
	isolateConfig(t)
	in := bufio.NewReader(strings.NewReader("2\n")) // change, then EOF
	if err := configMenuLoop(in, &config.Config{}); err != nil {
		t.Fatalf("configMenuLoop: %v", err)
	}
	path, _ := config.GetConfigPath()
	if _, err := os.Stat(path); err == nil {
		t.Error("config file written on EOF without explicit save")
	}
}
