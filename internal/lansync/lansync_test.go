package lansync

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/joshkerr/goplexcli/internal/cache"
	"github.com/joshkerr/goplexcli/internal/favorites"
	"github.com/joshkerr/goplexcli/internal/plex"
)

// TestRoundTrip exercises serve → meta → pull end to end against a temporary
// cache dir (APPDATA/HOME are redirected so the real cache is untouched). mDNS
// isn't needed: the peer address is supplied directly, mirroring what Discover
// would produce. It verifies the gzip transfer, atomic write, freshness sidecar,
// and that LastUpdated is preserved across the wire.
func TestRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("APPDATA", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("HOME", tmp)

	src := &cache.Cache{Media: []plex.MediaItem{
		{Key: "a", Type: "movie", Title: "Alpha"},
		{Key: "b", Type: "movie", Title: "Beta"},
		{Key: "c", Type: "episode", Title: "Ep", ParentTitle: "Show"},
	}}
	if err := src.Save(); err != nil { // stamps LastUpdated and writes the sidecar
		t.Fatalf("seed cache: %v", err)
	}

	// The server reports freshness from the sidecar, like the headless daemon.
	srv := NewServer(CacheMetaFunc())
	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer srv.Close(context.Background())
	if srv.Port() == 0 {
		t.Skip("could not bind LAN sync server in this environment")
	}

	peer := Peer{Instance: "host-1", Addr: "127.0.0.1:" + strconv.Itoa(srv.Port())}
	ctx := context.Background()

	meta, err := FetchMeta(ctx, peer)
	if err != nil {
		t.Fatalf("FetchMeta: %v", err)
	}
	if meta.Count != 3 {
		t.Errorf("meta count = %d, want 3", meta.Count)
	}
	if meta.LastUpdated.IsZero() {
		t.Error("meta LastUpdated is zero")
	}

	loaded, err := Pull(ctx, peer)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(loaded.Media) != 3 || loaded.Media[0].Title != "Alpha" {
		t.Errorf("pulled cache wrong: %+v", loaded)
	}
	if !loaded.LastUpdated.Equal(meta.LastUpdated) {
		t.Errorf("LastUpdated not preserved: got %v, want %v", loaded.LastUpdated, meta.LastUpdated)
	}
}

// TestFavoritesSync exercises the /favorites endpoints end to end: the client
// pulls the server's set, merges it locally, pushes the merged set back, and
// both sides converge — each keeping its own favorites plus the other's.
func TestFavoritesSync(t *testing.T) {
	serverStore := favorites.NewStoreAt(filepath.Join(t.TempDir(), "favorites.json"))
	clientStore := favorites.NewStoreAt(filepath.Join(t.TempDir(), "favorites.json"))
	if _, err := serverStore.Toggle("server-movie"); err != nil {
		t.Fatal(err)
	}
	if _, err := clientStore.Toggle("show:Client Show"); err != nil {
		t.Fatal(err)
	}

	notified := 0
	srv := NewServer(nil)
	srv.ServeFavorites(serverStore, func() { notified++ })
	if err := srv.StartOn(0); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer srv.Close(context.Background())
	if srv.Port() == 0 {
		t.Skip("could not bind LAN sync server in this environment")
	}

	peer := Peer{Instance: "host-1", Addr: "127.0.0.1:" + strconv.Itoa(srv.Port())}
	if !SyncFavoritesWith(context.Background(), clientStore, []Peer{peer}, nil) {
		t.Fatal("client set should have changed")
	}

	want := []string{"server-movie", "show:Client Show"}
	for name, st := range map[string]*favorites.Store{"client": clientStore, "server": serverStore} {
		keys, err := st.Keys()
		if err != nil {
			t.Fatalf("%s keys: %v", name, err)
		}
		if len(keys) != 2 || keys[0] != want[0] || keys[1] != want[1] {
			t.Errorf("%s keys = %v; want %v", name, keys, want)
		}
	}
	if notified != 1 {
		t.Errorf("onChange called %d times; want 1", notified)
	}

	// A second sync is a no-op on both sides.
	if SyncFavoritesWith(context.Background(), clientStore, []Peer{peer}, nil) {
		t.Error("second sync reported a change")
	}
	if notified != 1 {
		t.Errorf("onChange after no-op sync = %d; want still 1", notified)
	}
}

func TestHostFromInstance(t *testing.T) {
	cases := map[string]string{
		"office-1234": "office",
		"my-pc-99":    "my-pc",
		"plain":       "plain",
	}
	for in, want := range cases {
		if got := hostFromInstance(in); got != want {
			t.Errorf("hostFromInstance(%q) = %q, want %q", in, got, want)
		}
	}
}
