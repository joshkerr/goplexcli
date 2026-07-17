package main

import (
	"context"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

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
	if w < 800 {
		w = 800
	}
	if h < 520 {
		h = 520
	}
	wruntime.WindowSetSize(ctx, w, h)
	wruntime.WindowCenter(ctx)
}
