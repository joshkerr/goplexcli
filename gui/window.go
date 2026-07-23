package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/joshkerr/goplexcli/internal/config"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Minimum window size, mirrored in the wails.Run options in main.go.
const (
	minWindowW = 800
	minWindowH = 520
)

// windowState is the window geometry persisted across launches. Width/Height
// hold the last un-maximized size, so un-maximizing after a maximized launch
// restores something sensible.
type windowState struct {
	Maximized bool `json:"maximized"`
	Width     int  `json:"width"`
	Height    int  `json:"height"`
}

// windowStatePath returns the JSON file holding the window state, alongside
// the media cache.
func windowStatePath() (string, error) {
	dir, err := config.GetCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "window.json"), nil
}

// loadWindowState reads the persisted state; ok is false on first run (or an
// unreadable file), in which case the caller keeps the built-in defaults.
func loadWindowState() (windowState, bool) {
	path, err := windowStatePath()
	if err != nil {
		return windowState{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return windowState{}, false
	}
	var ws windowState
	if err := json.Unmarshal(data, &ws); err != nil {
		return windowState{}, false
	}
	return ws, true
}

func saveWindowState(ws windowState) error {
	path, err := windowStatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// updateWindowState folds the current window geometry into the persisted
// state. A maximized (or fullscreen) window keeps the previously saved
// Width/Height — the maximized dimensions must never become the restore size.
// Implausibly small sizes (below the app minimum) are ignored as readings
// taken mid-teardown.
func updateWindowState(prev windowState, maximized bool, w, h int) windowState {
	prev.Maximized = maximized
	if !maximized && w >= minWindowW && h >= minWindowH {
		prev.Width, prev.Height = w, h
	}
	return prev
}

// captureWindowState records the window geometry for the next launch. Called
// from OnBeforeClose, while the native window still exists — by OnShutdown it
// may already be gone on some platforms.
func (a *App) captureWindowState(ctx context.Context) {
	prev, _ := loadWindowState()
	maximized := wruntime.WindowIsMaximised(ctx) || wruntime.WindowIsFullscreen(ctx)
	w, h := wruntime.WindowGetSize(ctx)
	_ = saveWindowState(updateWindowState(prev, maximized, w, h))
}

// restoreWindowState applies the persisted geometry on startup: re-maximize if
// the app was closed maximized, otherwise restore the last size (clamped to
// the current screen by fitWindowToScreen, which also handles the first-run
// default).
func (a *App) restoreWindowState(ctx context.Context) {
	ws, ok := loadWindowState()
	if ok && ws.Maximized {
		wruntime.WindowMaximise(ctx)
		return
	}
	if ok && ws.Width >= minWindowW && ws.Height >= minWindowH {
		wruntime.WindowSetSize(ctx, ws.Width, ws.Height)
		wruntime.WindowCenter(ctx)
	}
	a.fitWindowToScreen(ctx)
}

// fitWindowToScreen shrinks and centers the window when the default size from
// main.go is larger than the screen it opened on (small laptops, high display
// scaling). Without this the window's edges land off-screen and there is no
// way to reach the resize handles. Sizes are in logical pixels, matching the
// units used by wails.Run options and WindowSetSize.
func (a *App) fitWindowToScreen(ctx context.Context) {
	screens, err := wruntime.ScreenGetAll(ctx)
	if err != nil {
		return
	}
	var screen *wruntime.Screen
	for i := range screens {
		if screens[i].IsCurrent {
			screen = &screens[i]
			break
		}
		if screen == nil && screens[i].IsPrimary {
			screen = &screens[i]
		}
	}
	if screen == nil || screen.Size.Width <= 0 || screen.Size.Height <= 0 {
		return
	}

	// Screen.Size is the full screen, not the work area, so leave headroom
	// for the taskbar/dock and window chrome.
	maxW := screen.Size.Width - 40
	maxH := screen.Size.Height - 100

	w, h := wruntime.WindowGetSize(ctx)
	if w <= maxW && h <= maxH {
		return
	}
	if w > maxW {
		w = maxW
	}
	if h > maxH {
		h = maxH
	}
	// Never shrink below the app minimum; on screens smaller than even that,
	// a min-sized centered window keeps the title bar reachable.
	if w < minWindowW {
		w = minWindowW
	}
	if h < minWindowH {
		h = minWindowH
	}
	wruntime.WindowSetSize(ctx, w, h)
	wruntime.WindowCenter(ctx)
}
