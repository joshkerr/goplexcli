package lansync

import (
	"context"
	"strconv"
	"testing"

	"github.com/joshkerr/goplexcli/internal/cache"
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
