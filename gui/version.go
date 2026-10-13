package main

// version is the GUI build's version, stamped at build time via ldflags
// (-X main.version=X.Y.Z), matching how the CLI is versioned. For a plain
// `wails dev`/`go build` without ldflags it stays "dev", which disables
// self-update (see gui/update.go).
var version = "dev"

// build is the build number, set via ldflags: -X main.build=$(BUILD) (the
// git commit count). Empty for a plain `wails dev`/`go build`.
var build = ""

// versionLabel is the version as Settings shows it: "0.4.0 (build 412)". The
// updater and the library server keep the bare version, which they compare
// and report.
func versionLabel() string {
	if build == "" {
		return version
	}
	return version + " (build " + build + ")"
}
