// Command goplexcli-gui is a cross-platform desktop GUI for GoplexCLI.
//
// It is a Wails v2 application: this Go package is the backend and exposes the
// methods on *App to a React/TypeScript frontend (see ./frontend). All real
// work is delegated to the same internal packages the CLI uses, so the GUI and
// CLI share one implementation of Plex access, caching, playback and downloads.
package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Launched from Finder/Dock the process gets the minimal system PATH;
	// widen it so mpv/rclone installed via Homebrew & co. are found.
	augmentPath()

	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "GoplexCLI",
		Width:     1320,
		Height:    860,
		MinWidth:  800,
		MinHeight: 520,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 9, G: 11, B: 17, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
		},
		Mac: &mac.Options{
			TitleBar:             mac.TitleBarHiddenInset(),
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
