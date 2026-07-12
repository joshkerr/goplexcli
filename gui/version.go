package main

// version is the GUI build's version, stamped at build time via ldflags
// (-X main.version=X.Y.Z), matching how the CLI is versioned. For a plain
// `wails dev`/`go build` without ldflags it stays "dev", which disables
// self-update (see gui/update.go).
var version = "dev"
