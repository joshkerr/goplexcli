package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// augmentPath prepends the usual third-party binary directories to PATH.
//
// GUI apps launched from Finder/Dock on macOS (and from some Linux desktops)
// inherit only the minimal system PATH (/usr/bin:/bin:/usr/sbin:/sbin), not
// the user's shell PATH — so exec.LookPath can't find Homebrew/MacPorts
// installs of mpv and rclone even though they work fine from a terminal.
func augmentPath() {
	if runtime.GOOS == "windows" {
		return
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		"/opt/homebrew/bin", // Homebrew on Apple Silicon
		"/usr/local/bin",    // Homebrew on Intel macs; common on Linux
		"/opt/local/bin",    // MacPorts
	}
	if home != "" {
		candidates = append(candidates, filepath.Join(home, ".local", "bin"))
	}

	path := os.Getenv("PATH")
	existing := map[string]bool{}
	for _, d := range strings.Split(path, string(os.PathListSeparator)) {
		existing[d] = true
	}
	var add []string
	for _, d := range candidates {
		if existing[d] {
			continue
		}
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			add = append(add, d)
		}
	}
	if len(add) == 0 {
		return
	}
	os.Setenv("PATH", strings.Join(add, string(os.PathListSeparator))+string(os.PathListSeparator)+path)
}
