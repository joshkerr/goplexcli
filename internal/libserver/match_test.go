package libserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/joshkerr/goplexcli/internal/plex"
)

// matchResolver is a fakeResolver that also fixes matches.
type matchResolver struct {
	fakeResolver
	lastKey  string
	lastTMDB int
}

func (m *matchResolver) SearchTitles(_ context.Context, kind, query string, year int) ([]Candidate, error) {
	if query == "boom" {
		return nil, errors.New("tmdb down")
	}
	return []Candidate{{ID: 42, Kind: kind, Title: query + "!", Year: year, Poster: ThumbPath("/p.jpg")}}, nil
}

func (m *matchResolver) ApplyMatch(_ context.Context, key string, tmdbID int) ([]plex.MediaItem, error) {
	m.lastKey, m.lastTMDB = key, tmdbID
	if tmdbID == 13 {
		return nil, errors.New("unlucky")
	}
	return []plex.MediaItem{{Key: key, Title: "Fixed", ServerName: "vault"}}, nil
}

func TestMatchEndpointsAndClient(t *testing.T) {
	res := &matchResolver{}
	srv := httptest.NewServer(New(res, Options{ImageCacheDir: t.TempDir(), Token: "secret"}))
	defer srv.Close()
	ctx := context.Background()

	cands, err := SearchRemote(ctx, srv.URL, "secret", "tv", "Accused", 2023)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].ID != 42 || cands[0].Kind != "tv" || cands[0].Title != "Accused!" || cands[0].Year != 2023 || cands[0].Poster != "/local/tmdb/w500/p.jpg" {
		t.Errorf("SearchRemote = %+v", cands)
	}
	if _, err := SearchRemote(ctx, srv.URL, "secret", "movie", "boom", 0); err == nil || !strings.Contains(err.Error(), "tmdb down") {
		t.Errorf("upstream error not surfaced: %v", err)
	}
	if _, err := SearchRemote(ctx, srv.URL, "wrong", "movie", "x", 0); err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Errorf("token not enforced: %v", err)
	}

	items, err := MatchRemote(ctx, srv.URL, "secret", ItemKey("abc"), 603)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "Fixed" || res.lastKey != ItemKey("abc") || res.lastTMDB != 603 {
		t.Errorf("MatchRemote = %+v (resolver saw %s %d)", items, res.lastKey, res.lastTMDB)
	}
	if _, err := MatchRemote(ctx, srv.URL, "secret", ItemKey("abc"), 13); err == nil || !strings.Contains(err.Error(), "unlucky") {
		t.Errorf("apply error not surfaced: %v", err)
	}
	if _, err := MatchRemote(ctx, srv.URL, "secret", "/library/metadata/1", 1); err == nil {
		t.Error("non-local key accepted")
	}

	// A plain resolver answers 503.
	plain := httptest.NewServer(New(&fakeResolver{}, Options{ImageCacheDir: t.TempDir()}))
	defer plain.Close()
	resp, err := http.Get(plain.URL + SearchPath + "?q=x")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("plain resolver search status %d", resp.StatusCode)
	}
}
