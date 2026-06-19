package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/joshkerr/goplexcli/internal/cache"
	"github.com/joshkerr/goplexcli/internal/player"
	"github.com/joshkerr/goplexcli/internal/plex"
	"github.com/joshkerr/goplexcli/internal/progress"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Play streams one or more cached items in MPV as a playlist, tracking progress
// back to Plex and flushing resume positions into the local cache on exit. It
// mirrors the CLI's playback path (cmd/goplexcli/main.go) but emits Wails
// events instead of writing to a terminal.
//
// keys are Plex metadata keys (as returned in MediaDTO.Key). resume, when true,
// starts the first item from its saved position. Play returns once MPV exits.
func (a *App) Play(keys []string, resume bool) error {
	if len(keys) == 0 {
		return fmt.Errorf("no items to play")
	}

	cfg := a.config()
	if !player.IsAvailable(cfg.MPVPath) {
		return fmt.Errorf("mpv is not installed - install mpv to play media")
	}

	c := a.media()
	if c == nil {
		return fmt.Errorf("media cache is empty - build your library first")
	}

	// Resolve items in the requested order.
	items, err := resolveItems(c, keys)
	if err != nil {
		return err
	}

	// All playlist items must share a server for stream-URL generation and
	// progress reporting; use the first item's server.
	client, err := plex.NewWithName(items[0].ServerURL, cfg.PlexToken, items[0].ServerName)
	if err != nil {
		return fmt.Errorf("failed to create Plex client: %w", err)
	}

	var streamURLs []string
	for _, it := range items {
		itemClient := client
		if it.ServerURL != items[0].ServerURL {
			if c2, e := plex.NewWithName(it.ServerURL, cfg.PlexToken, it.ServerName); e == nil {
				itemClient = c2
			}
		}
		url, e := itemClient.GetStreamURL(it.Key)
		if e != nil {
			return fmt.Errorf("failed to get stream URL for %s: %w", it.FormatMediaTitle(), e)
		}
		streamURLs = append(streamURLs, url)
	}

	socketPath := progress.GenerateIPCPath()
	mpvClient := progress.NewMPVClient(socketPath)
	tracker := progress.NewTracker(items, mpvClient, client)
	defer os.Remove(socketPath)

	startPos := 0
	if resume && len(items) == 1 && items[0].ViewOffset > 0 {
		startPos = items[0].ViewOffset / 1000
	}

	opts := player.PlaybackOptions{SocketPath: socketPath, StartPos: startPos}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a.emit("playback:started", map[string]interface{}{"count": len(items), "title": items[0].FormatMediaTitle()})

	errCh := make(chan error, 1)
	go func() {
		err := player.PlayMultipleWithOptions(streamURLs, cfg.MPVPath, opts)
		cancel()
		errCh <- err
	}()

	tracking := false
	if err := mpvClient.ConnectWithContext(ctx); err == nil {
		tracker.Start(ctx, 10*time.Second)
		tracking = true
		defer func() { _ = mpvClient.Close() }()
	}

	playbackErr := <-errCh

	if tracking {
		tracker.Stop()
		persistProgress(tracker)
		// The on-disk cache now has updated resume offsets; drop the in-memory
		// copy so Continue Watching reflects them on the next browse.
		a.invalidateMedia()
	}

	a.emit("playback:stopped", map[string]interface{}{})

	if playbackErr != nil {
		return fmt.Errorf("playback failed: %w", playbackErr)
	}
	return nil
}

// resolveItems looks up cached items by key, preserving the requested order.
func resolveItems(c *cache.Cache, keys []string) ([]*plex.MediaItem, error) {
	index := map[string]*plex.MediaItem{}
	for i := range c.Media {
		index[c.Media[i].Key] = &c.Media[i]
	}
	var items []*plex.MediaItem
	for _, k := range keys {
		if it, ok := index[k]; ok {
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("none of the requested items were found in the cache")
	}
	return items, nil
}

// persistProgress flushes tracked playback offsets into the local cache so
// just-watched items appear in Continue Watching immediately. Best-effort.
func persistProgress(tracker *progress.Tracker) {
	offsets := tracker.Progress()
	if len(offsets) == 0 {
		return
	}
	c, err := cache.Load()
	if err != nil {
		return
	}
	if c.ApplyOffsets(offsets) {
		_ = c.Save()
	}
}

// emit is a nil-safe wrapper around the Wails event emitter.
func (a *App) emit(event string, data interface{}) {
	if a.ctx == nil {
		return
	}
	wruntime.EventsEmit(a.ctx, event, data)
}
