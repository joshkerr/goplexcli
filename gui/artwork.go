package main

import (
	"context"
	"fmt"

	"github.com/joshkerr/goplexcli/internal/artwork"
	"github.com/joshkerr/goplexcli/internal/dlengine"
)

// artworkProcessor returns the shared post-download artwork processor, or
// nil when artwork embedding is turned off in config.
func (a *App) artworkProcessor() *artwork.Processor {
	a.artMu.Lock()
	defer a.artMu.Unlock()
	cfg := a.config()
	if !cfg.ArtworkEnabled() {
		a.art = nil
		return nil
	}
	if a.art == nil {
		a.art = artwork.NewProcessor(cfg, func(format string, args ...any) {
			fmt.Printf("artwork: "+format+"\n", args...)
		})
	}
	return a.art
}

// invalidateArtworkProcessor drops the processor so Settings changes
// (orientation, tool paths, TMDB key) take effect on the next download.
func (a *App) invalidateArtworkProcessor() {
	a.artMu.Lock()
	a.art = nil
	a.artMu.Unlock()
}

// embedArtwork runs after a local download completes: it looks the title up
// (Apple TV, then TMDB, then Netflix) and embeds the art into the file, off
// the transfer goroutine so the next download starts right away.
func (a *App) embedArtwork(j downloadJob) {
	proc := a.artworkProcessor()
	if proc == nil {
		return
	}
	rec := dlengine.Progress{Dest: j.dest, Title: j.title, Year: j.year, MediaType: j.mediaType}
	go func() {
		out := proc.ProcessDownload(context.Background(), rec)
		switch {
		case out.Err != nil:
			a.emitToast("error", out.String())
		case out.Result.Skipped != "":
			// Quiet: nothing to embed (unsupported container, art already
			// present, nothing found).
		default:
			a.emitToast("info", fmt.Sprintf("Added %s cover art to %s", out.Result.Source, j.name))
		}
	}()
}
