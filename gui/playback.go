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
	items, missing, err := resolveItems(c, keys)
	if err != nil {
		return err
	}

	a.emitPlaybackStatus("preparing", items, "")
	if len(missing) > 0 {
		a.emitPlaybackStatus("warning", items, fmt.Sprintf(
			"%d of %d items not in cache — playing the rest", len(missing), len(keys)))
	}

	// Progress reporting uses the first item's server; items from other
	// servers get their own client for stream-URL generation.
	client, err := plex.NewWithName(items[0].ServerURL, cfg.TokenForURL(items[0].ServerURL), items[0].ServerName)
	if err != nil {
		return fmt.Errorf("failed to create Plex client: %w", err)
	}

	var streamURLs []string
	for _, it := range items {
		itemClient := client
		if it.ServerURL != items[0].ServerURL {
			c2, e := plex.NewWithName(it.ServerURL, cfg.TokenForURL(it.ServerURL), it.ServerName)
			if e != nil {
				return fmt.Errorf("failed to create Plex client for %s (server %s): %w",
					it.FormatMediaTitle(), it.ServerName, e)
			}
			itemClient = c2
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

	a.emitPlaybackStatus("starting", items, "")

	started := time.Now()
	var outcome *player.PlayOutcome
	errCh := make(chan error, 1)
	go func() {
		o, err := player.PlayMultipleWithOptions(streamURLs, cfg.MPVPath, opts)
		outcome = o // synchronized by the errCh send below
		cancel()
		errCh <- err
	}()

	tracking := false
	if err := mpvClient.ConnectWithContext(ctx); err == nil {
		a.emitPlaybackStatus("playing", items, "")
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

	if playbackErr == nil {
		if w := silentExitWarning(tracking, time.Since(started), outcome); w != "" {
			a.emitPlaybackStatus("warning", items, w)
		}
	}
	a.emitPlaybackStatus("stopped", items, "")

	if playbackErr != nil {
		return fmt.Errorf("playback failed: %w", playbackErr)
	}
	return nil
}

// silentExitWarning returns a user-facing message when mpv exited "cleanly"
// without playback ever starting: no IPC connection and a near-instant exit.
// This catches streams mpv treats as played despite never showing a frame
// (e.g. an instant EOF from a broken transcode). Empty means all is well.
func silentExitWarning(tracked bool, ran time.Duration, outcome *player.PlayOutcome) string {
	if tracked || ran > 10*time.Second || outcome == nil {
		return ""
	}
	msg := fmt.Sprintf("mpv exited after %.1fs without starting playback (exit code %d)",
		ran.Seconds(), outcome.ExitCode)
	if outcome.ErrorLine != "" {
		msg += ": " + outcome.ErrorLine
	}
	return msg
}

// emitPlaybackStatus emits a playback:status Wails event. Stages: preparing,
// warning (detail holds the message), starting, playing, stopped. Errors are
// not emitted — they flow through Play's returned error instead.
func (a *App) emitPlaybackStatus(stage string, items []*plex.MediaItem, detail string) {
	title := ""
	if len(items) > 0 {
		title = items[0].FormatMediaTitle()
	}
	a.emit("playback:status", map[string]interface{}{
		"stage":  stage,
		"title":  title,
		"count":  len(items),
		"detail": detail,
	})
}

// resolveItems looks up cached items by key, preserving the requested order.
// Keys absent from the cache are returned in missing so the caller can warn
// rather than dropping them silently; it is an error only when nothing resolves.
func resolveItems(c *cache.Cache, keys []string) (items []*plex.MediaItem, missing []string, err error) {
	index := map[string]*plex.MediaItem{}
	for i := range c.Media {
		index[c.Media[i].Key] = &c.Media[i]
	}
	for _, k := range keys {
		if it, ok := index[k]; ok {
			items = append(items, it)
		} else {
			missing = append(missing, k)
		}
	}
	if len(items) == 0 {
		return nil, missing, fmt.Errorf("none of the requested items were found in the cache")
	}
	return items, missing, nil
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
