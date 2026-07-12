package main

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/joshkerr/goplexcli/internal/update"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// updateRepo is the GitHub "owner/name" the GUI self-updater pulls releases from.
const updateRepo = "joshkerr/goplexcli"

// AppVersion returns the running GUI build's version ("dev" for an unstamped
// local build, which disables self-update).
func (a *App) AppVersion() string { return version }

// UpdateInfoDTO reports whether a newer GUI release is available for install.
type UpdateInfoDTO struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	NotesURL  string `json:"notesURL"`
	Error     string `json:"error"`
}

// guiAssetName is the release asset holding the GUI bundle for this platform, as
// produced by the release workflow's gui-* jobs. Empty on unsupported platforms.
func guiAssetName() string { return desktopAsset(runtime.GOOS) }

// desktopAsset maps a GOOS to its published GUI bundle asset name ("" if none).
func desktopAsset(goos string) string {
	switch goos {
	case "darwin":
		return "goplexcli-gui-darwin-universal.zip"
	case "windows":
		return "goplexcli-gui-windows-amd64.zip"
	default:
		return ""
	}
}

// CheckUpdate queries the latest release and reports whether a newer GUI build
// is available for this platform. Development builds never report an update.
func (a *App) CheckUpdate() UpdateInfoDTO {
	info := UpdateInfoDTO{Current: version}
	if version == "dev" || version == "" {
		return info
	}
	assetName := guiAssetName()
	if assetName == "" {
		info.Error = "GUI updates aren't available on this platform"
		return info
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rel, _, err := update.ResolveLatest(ctx, updateRepo)
	if err != nil {
		info.Error = err.Error()
		return info
	}
	info.Latest = strings.TrimPrefix(rel.TagName, "v")
	info.NotesURL = rel.HTMLURL
	if _, ok := rel.FindAsset(assetName); !ok {
		// Newer release exists but has no GUI bundle for this platform — treat as
		// not-available rather than offering a broken install.
		return info
	}
	info.Available = update.CompareVersions(version, rel.TagName) < 0
	return info
}

// ApplyUpdate downloads the latest GUI bundle and hands off to a detached helper
// that waits for this process to exit, swaps the app in place, and relaunches it.
// Replacing a running app in place is unreliable, so the swap deliberately
// happens after the GUI quits. Progress is emitted on "gui-update:progress".
func (a *App) ApplyUpdate() error {
	if version == "dev" || version == "" {
		return fmt.Errorf("this is a development build; self-update is disabled")
	}
	assetName := guiAssetName()
	if assetName == "" {
		return fmt.Errorf("GUI updates aren't available on this platform")
	}

	a.emitUpdate("Checking for the latest release…")
	rctx, rcancel := context.WithTimeout(context.Background(), 20*time.Second)
	rel, token, err := update.ResolveLatest(rctx, updateRepo)
	rcancel()
	if err != nil {
		return err
	}
	if update.CompareVersions(version, rel.TagName) >= 0 {
		return fmt.Errorf("already up to date")
	}
	asset, ok := rel.FindAsset(assetName)
	if !ok {
		return fmt.Errorf("release %s has no GUI bundle for this platform", rel.TagName)
	}

	// Replace ourselves in place — resolve where we're installed.
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot locate the running app: %w", err)
	}
	if resolved, rerr := filepath.EvalSymlinks(exePath); rerr == nil {
		exePath = resolved
	}

	work, err := os.MkdirTemp("", "goplexcli-gui-update-")
	if err != nil {
		return err
	}
	// work is left for the detached helper to finish with (we may have quit).

	zipPath := filepath.Join(work, "bundle.zip")
	a.emitUpdate("Downloading update…")
	dctx, dcancel := context.WithTimeout(context.Background(), 10*time.Minute)
	err = update.DownloadAsset(dctx, asset, token, zipPath)
	dcancel()
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	a.emitUpdate("Extracting…")
	extractDir := filepath.Join(work, "new")
	if err := unzip(zipPath, extractDir); err != nil {
		return fmt.Errorf("extract failed: %w", err)
	}
	_ = os.Remove(zipPath) // free the download; keep the extracted bundle

	newArtifact, target, err := resolveUpdateTarget(extractDir, exePath)
	if err != nil {
		return err
	}

	a.emitUpdate("Installing — the app will relaunch…")
	if err := launchUpdateHelper(newArtifact, target, work); err != nil {
		return fmt.Errorf("failed to start the updater: %w", err)
	}

	// Give the UI a moment to show the message, then quit so the helper can swap.
	go func() {
		time.Sleep(1500 * time.Millisecond)
		wruntime.Quit(a.ctx)
	}()
	return nil
}

func (a *App) emitUpdate(msg string) {
	if a.ctx == nil {
		return
	}
	wruntime.EventsEmit(a.ctx, "gui-update:progress", map[string]any{"message": msg})
}

// resolveUpdateTarget locates the freshly extracted artifact and the install
// path it should replace: the running .exe on Windows, or the enclosing .app
// bundle on macOS.
func resolveUpdateTarget(extractDir, exePath string) (newArtifact, target string, err error) {
	switch runtime.GOOS {
	case "windows":
		art := filepath.Join(extractDir, "goplexcli-gui.exe")
		if !pathExists(art) {
			if art, err = findByExt(extractDir, ".exe"); err != nil {
				return "", "", err
			}
		}
		return art, exePath, nil
	case "darwin":
		art := filepath.Join(extractDir, "goplexcli-gui.app")
		if !pathExists(art) {
			if art, err = findByExt(extractDir, ".app"); err != nil {
				return "", "", err
			}
		}
		root := appBundleRoot(exePath)
		if root == "" {
			return "", "", fmt.Errorf("the app isn't running from a .app bundle; reinstall it manually")
		}
		return art, root, nil
	default:
		return "", "", fmt.Errorf("unsupported platform")
	}
}

// appBundleRoot returns the ".app" bundle directory containing exePath, or ""
// if the executable isn't inside one. A macOS exePath looks like
// ".../GoplexCLI.app/Contents/MacOS/goplexcli-gui".
func appBundleRoot(exePath string) string {
	dir := filepath.Dir(exePath)
	for i := 0; i < 4; i++ {
		if strings.HasSuffix(dir, ".app") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// findByExt returns the first top-level entry in dir whose name ends with ext.
func findByExt(dir, ext string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if strings.HasSuffix(strings.ToLower(e.Name()), ext) {
			return filepath.Join(dir, e.Name()), nil
		}
	}
	return "", fmt.Errorf("no %s found in the downloaded bundle", ext)
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// unzip extracts src into destDir, preserving file modes and symlinks (macOS
// .app bundles need the inner executable to stay executable). It guards against
// path traversal ("zip slip").
func unzip(src, destDir string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	for _, f := range r.File {
		target := filepath.Join(destDir, f.Name)
		// Reject entries that escape destDir.
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) &&
			target != filepath.Clean(destDir) {
			return fmt.Errorf("unsafe path in archive: %q", f.Name)
		}

		info := f.FileInfo()
		switch {
		case info.IsDir():
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case info.Mode()&os.ModeSymlink != 0:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			rc, err := f.Open()
			if err != nil {
				return err
			}
			linkBytes, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(string(linkBytes), target); err != nil {
				return err
			}
		default:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := writeZipFile(f, target); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeZipFile(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, f.Mode())
	if err != nil {
		return err
	}
	_, err = io.Copy(out, rc)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// launchUpdateHelper writes a small detached script into work that waits for
// this process (pid) to exit, swaps newArtifact into target (backing the old one
// up and rolling back if the move fails), and relaunches the app. It returns
// once the helper has started; the helper keeps running past our exit.
func launchUpdateHelper(newArtifact, target, work string) error {
	pid := os.Getpid()

	if runtime.GOOS == "windows" {
		script := filepath.Join(work, "update.cmd")
		content := fmt.Sprintf(`@echo off
:wait
tasklist /FI "PID eq %d" 2>NUL | find "%d" >NUL
if not errorlevel 1 (
  ping -n 2 127.0.0.1 >NUL
  goto wait
)
if exist "%[3]s.bak" rmdir /S /Q "%[3]s.bak" >NUL 2>&1
if exist "%[3]s.bak" del /Q "%[3]s.bak" >NUL 2>&1
move /Y "%[3]s" "%[3]s.bak" >NUL 2>&1
move /Y "%[4]s" "%[3]s" >NUL && (del /Q "%[3]s.bak" >NUL 2>&1) || move /Y "%[3]s.bak" "%[3]s" >NUL
start "" "%[3]s"
`, pid, pid, target, newArtifact)
		if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
			return err
		}
		cmd := exec.Command("cmd", "/c", script)
		detachSysProc(cmd)
		return cmd.Start()
	}

	// macOS / other unix.
	script := filepath.Join(work, "update.sh")
	content := fmt.Sprintf(`#!/bin/sh
while kill -0 %d 2>/dev/null; do sleep 0.5; done
rm -rf "%[2]s.bak"
mv "%[2]s" "%[2]s.bak" 2>/dev/null
if mv "%[3]s" "%[2]s"; then
  rm -rf "%[2]s.bak"
else
  mv "%[2]s.bak" "%[2]s"
fi
open "%[2]s"
`, pid, target, newArtifact)
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		return err
	}
	cmd := exec.Command("/bin/sh", script)
	detachSysProc(cmd)
	return cmd.Start()
}
