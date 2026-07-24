package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
)

// "Send to rclonecp" hands a completed download to the rclonecp GUI (a sibling
// app that embeds cover art and copies files onward). rclonecp holds a Wails
// single-instance lock, so a plain exec covers both cases: if it's running,
// the new process forwards its command line to the running instance and exits;
// if not, the app launches and ingests its startup args. The item's Plex
// title/year ride along as --title/--year so rclonecp seeds its poster search
// with the exact name instead of re-parsing the filename.

// rclonecpBinName is the rclonecp GUI binary's base name (its wails.json
// outputfilename).
const rclonecpBinName = "rclonecp"

// findRclonecp locates the rclonecp GUI binary: the configured override first,
// then PATH, then conventional install locations.
func (a *App) findRclonecp() (string, error) {
	if p := a.config().RclonecpPath; p != "" {
		// LookPath handles both forms the setting accepts: a bare name is
		// resolved against PATH, an explicit path is checked directly.
		if found, err := exec.LookPath(p); err == nil {
			return found, nil
		}
		return "", fmt.Errorf("rclonecp not found at configured path %q", p)
	}
	// Conventional GUI install locations come BEFORE the PATH probe: the
	// rclonecp CLI is also named "rclonecp" and typically lives on PATH
	// (go/bin), so a bare-name lookup would silently hand the file to a
	// hidden terminal app instead of the GUI. `make gui-install` currently
	// installs with a -gui suffix; both spellings are tried.
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			"/Applications/rclonecp.app/Contents/MacOS/" + rclonecpBinName,
			"/Applications/rclonecp-gui.app/Contents/MacOS/rclonecp-gui",
		}
	case "windows":
		if lad := os.Getenv("LOCALAPPDATA"); lad != "" {
			candidates = append(candidates,
				filepath.Join(lad, "Programs", "rclonecp", rclonecpBinName+".exe"),
				filepath.Join(lad, "Programs", "rclonecp-gui", "rclonecp-gui.exe"))
		}
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	if p, err := exec.LookPath("rclonecp-gui"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("rclonecp not found — install it or set its path in Settings")
}

// SendToRclonecp forwards a completed download's file to rclonecp.
func (a *App) SendToRclonecp(id string) error {
	a.dlStateMu.Lock()
	e, ok := a.dlHist[id]
	var dp DownloadProgress
	if ok {
		dp = *e
	}
	a.dlStateMu.Unlock()
	if !ok {
		return fmt.Errorf("unknown download %q", id)
	}
	if dp.Status != "completed" {
		return fmt.Errorf("%q hasn't finished downloading", dp.Name)
	}
	if dp.Dest == "" {
		return fmt.Errorf("%q has no recorded destination", dp.Name)
	}
	if _, err := os.Stat(dp.Dest); err != nil {
		return fmt.Errorf("downloaded file no longer exists: %s", dp.Dest)
	}
	return a.launchRclonecp(dp.Dest, dp.Title, dp.Year)
}

// launchRclonecp starts (or forwards to) the rclonecp GUI with one file.
func (a *App) launchRclonecp(path, title string, year int) error {
	bin, err := a.findRclonecp()
	if err != nil {
		return err
	}
	var args []string
	if title != "" {
		args = append(args, "--title", title)
		if year > 0 {
			args = append(args, "--year", strconv.Itoa(year))
		}
	}
	args = append(args, path)
	// No configureSysProc here: that sets HideWindow for console children
	// (rclone), but rclonecp is a GUI app — inheriting SW_HIDE makes a
	// cold-started rclonecp window invisible. It has no console to suppress.
	cmd := exec.Command(bin, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to launch rclonecp: %w", err)
	}
	// Reap the process: the forwarder variant exits almost immediately, a cold
	// launch lives as long as the rclonecp window.
	go func() { _ = cmd.Wait() }()
	return nil
}

// maybeAutoSendToRclonecp forwards a just-completed download when the
// auto-send preference is on. Both outcomes surface as frontend toasts —
// running from a background goroutine, there is no bound-call return path,
// and a silent failure would look like the feature simply not working. The
// manual per-download button remains as the retry path.
func (a *App) maybeAutoSendToRclonecp(id string) {
	if !a.config().AutoSendRclonecp {
		return
	}
	go func() {
		if err := a.SendToRclonecp(id); err != nil {
			a.emitToast("error", fmt.Sprintf("Auto-send to rclonecp failed: %v", err))
			return
		}
		a.emitToast("info", "Sent to rclonecp")
	}()
}
