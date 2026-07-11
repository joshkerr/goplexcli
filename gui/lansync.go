package main

import (
	"context"
	"fmt"

	"github.com/joshkerr/goplexcli/internal/lansync"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// newSyncServer builds the LAN cache-sync server for the GUI. Its freshness is
// read from the in-memory cache (cheap — no re-parse of the large media.json).
func (a *App) newSyncServer() *lansync.Server {
	return lansync.NewServer(func() lansync.Meta {
		m := lansync.Meta{}
		if c := a.media(); c != nil {
			m.Count = len(c.Media)
			m.LastUpdated = c.LastUpdated
		}
		return m
	})
}

// SyncFromLAN discovers peers, pulls the newest cache if one is newer than ours,
// and hot-swaps it into memory. It emits "sync:progress" events throughout and a
// final "sync:done" ({updated, upToDate, count, source} or {error}). It shares
// the busy lock with Reindex/Update so the three can't run at once.
func (a *App) SyncFromLAN() error {
	if a.lan == nil {
		return fmt.Errorf("lan sync unavailable")
	}
	if !a.busy.TryLock() {
		return fmt.Errorf("an index operation is already running")
	}
	defer a.busy.Unlock()

	local := lansync.Meta{}
	if c := a.media(); c != nil {
		local = lansync.Meta{Count: len(c.Media), LastUpdated: c.LastUpdated}
	}

	res, err := lansync.SyncFromLAN(context.Background(), a.lan.Instance(), local, a.emitSyncProgress)
	if err != nil {
		a.emitSyncDone(map[string]any{"error": err.Error()})
		return err
	}
	if res.UpToDate {
		a.emitSyncDone(map[string]any{"updated": false, "upToDate": true, "count": local.Count})
		return nil
	}

	// Serve the freshly-synced library from memory without re-reading disk.
	a.setMedia(res.Cache)
	a.emitSyncDone(map[string]any{"updated": true, "count": len(res.Cache.Media), "source": res.Source})
	return nil
}

func (a *App) emitSyncProgress(msg string) {
	if a.ctx == nil {
		return
	}
	wruntime.EventsEmit(a.ctx, "sync:progress", map[string]any{"message": msg})
}

func (a *App) emitSyncDone(payload map[string]any) {
	if a.ctx == nil {
		return
	}
	wruntime.EventsEmit(a.ctx, "sync:done", payload)
}
