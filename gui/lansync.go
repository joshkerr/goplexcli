package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/joshkerr/goplexcli/internal/lansync"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// newSyncServer builds the LAN cache-sync server for the GUI. Its freshness is
// read from the in-memory cache (cheap — no re-parse of the large media.json).
// It also serves this machine's favorites, and merges in sets pushed by peers,
// refreshing the frontend's stars when that happens.
func (a *App) newSyncServer() *lansync.Server {
	srv := lansync.NewServer(func() lansync.Meta {
		m := lansync.Meta{}
		if c := a.media(); c != nil {
			m.Count = len(c.Media)
			m.LastUpdated = c.LastUpdated
		}
		return m
	})
	srv.ServeFavorites(a.fav, a.emitFavoritesChanged)
	return srv
}

// syncFavoritesAtStartup merges favorites with LAN peers in the background
// shortly after launch, so machines converge without waiting for an explicit
// Sync. Favorites are a few KB, so this is cheap; failures are silent — the
// next explicit sync (or a peer's push) will catch up.
func (a *App) syncFavoritesAtStartup() {
	ctx := context.Background()
	var peers []lansync.Peer
	if peer := strings.TrimSpace(a.config().SyncPeer); peer != "" {
		peers = []lansync.Peer{{Addr: lansync.NormalizePeerAddr(peer)}}
	} else {
		discovered, err := lansync.Discover(ctx, a.lan.Instance())
		if err != nil {
			return
		}
		peers = discovered
	}
	if lansync.SyncFavoritesWith(ctx, a.fav, peers, nil) {
		a.emitFavoritesChanged()
	}
}

// SyncFromLAN pulls the newest media cache from another computer and hot-swaps
// it into memory. If a sync peer is configured (Settings), it goes straight to
// that host; otherwise it auto-discovers peers via mDNS. It emits "sync:progress"
// events throughout and a final "sync:done" ({updated, upToDate, count, source}
// or {error}). It shares the busy lock with Reindex/Update so the three can't run
// at once.
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

	var res lansync.Result
	var err error
	if peer := strings.TrimSpace(a.config().SyncPeer); peer != "" {
		res, err = lansync.SyncFromPeer(context.Background(), lansync.NormalizePeerAddr(peer), local, a.fav, a.emitSyncProgress)
	} else {
		res, err = lansync.SyncFromLAN(context.Background(), a.lan.Instance(), local, a.fav, a.emitSyncProgress)
	}
	// Favorites merge before the cache transfer, so honor the flag even when
	// the cache part failed.
	if res.FavoritesChanged {
		a.emitFavoritesChanged()
	}
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

// emitFavoritesChanged tells the frontend the favorites set changed outside a
// user toggle (a peer's push, or a background/explicit sync) so it refreshes
// its stars and any open favorites grid.
func (a *App) emitFavoritesChanged() {
	if a.ctx == nil {
		return
	}
	wruntime.EventsEmit(a.ctx, "favorites:changed", nil)
}
