package libserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/joshkerr/goplexcli/internal/plex"
)

const (
	// SearchPath answers title searches for manual match fixes.
	SearchPath = "/local/search"
	// MatchPath applies (or clears) a manual match.
	MatchPath = "/local/match"
)

// Candidate is one possible title for a manual match.
type Candidate struct {
	ID       int     `json:"id"`
	Kind     string  `json:"kind"` // "movie" | "tv"
	Title    string  `json:"title"`
	Year     int     `json:"year"`
	Overview string  `json:"overview"`
	Poster   string  `json:"poster"` // library thumb path ("/local/tmdb/w500/…") or ""
	Rating   float64 `json:"rating"`
}

// Matcher is implemented by resolvers that can fix wrong title matches. The
// daemon and the GUI implement it; the endpoints answer 503 otherwise.
type Matcher interface {
	// SearchTitles returns candidates for a query; kind is "movie" or "tv".
	SearchTitles(ctx context.Context, kind, query string, year int) ([]Candidate, error)
	// ApplyMatch pins the movie (key) or the show containing the episode
	// (key) to tmdbID and rebuilds its items; tmdbID 0 restores the
	// automatic match. It returns the rebuilt items.
	ApplyMatch(ctx context.Context, key string, tmdbID int) ([]plex.MediaItem, error)
}

// MatchRequest is the body of POST /local/match.
type MatchRequest struct {
	Key  string `json:"key"`
	TMDB int    `json:"tmdb"`
}

// MatchResponse is the reply of POST /local/match.
type MatchResponse struct {
	Items []plex.MediaItem `json:"items"`
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	m, ok := s.res.(Matcher)
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "match fixing is not available on this server"})
		return
	}
	q := r.URL.Query()
	query := strings.TrimSpace(q.Get("q"))
	if query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "q is required"})
		return
	}
	year, _ := strconv.Atoi(q.Get("year"))
	results, err := m.SearchTitles(r.Context(), q.Get("kind"), query, year)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if results == nil {
		results = []Candidate{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *Server) handleMatch(w http.ResponseWriter, r *http.Request) {
	m, ok := s.res.(Matcher)
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "match fixing is not available on this server"})
		return
	}
	var req MatchRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if _, ok := IDFromKey(req.Key); !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key must be a local item key"})
		return
	}
	if req.TMDB < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tmdb must be >= 0"})
		return
	}
	items, err := m.ApplyMatch(r.Context(), req.Key, req.TMDB)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	if items == nil {
		items = []plex.MediaItem{}
	}
	writeJSON(w, http.StatusOK, MatchResponse{Items: items})
}

// ---- client side ----

var matchClient = &http.Client{Timeout: 5 * time.Minute}

// SearchRemote asks a library server for title candidates.
func SearchRemote(ctx context.Context, serverURL, token, kind, query string, year int) ([]Candidate, error) {
	q := url.Values{"kind": {kind}, "q": {query}}
	if year > 0 {
		q.Set("year", strconv.Itoa(year))
	}
	if token != "" {
		q.Set("X-Plex-Token", token)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(serverURL, "/")+SearchPath+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Results []Candidate `json:"results"`
	}
	if err := doJSON(req, &out); err != nil {
		return nil, err
	}
	return out.Results, nil
}

// MatchRemote asks a library server to pin an item to a TMDB id (0 clears)
// and returns the rebuilt items.
func MatchRemote(ctx context.Context, serverURL, token, key string, tmdbID int) ([]plex.MediaItem, error) {
	body, _ := json.Marshal(MatchRequest{Key: key, TMDB: tmdbID})
	u := strings.TrimRight(serverURL, "/") + MatchPath
	if token != "" {
		u += "?X-Plex-Token=" + url.QueryEscape(token)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	var out MatchResponse
	if err := doJSON(req, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func doJSON(req *http.Request, out any) error {
	resp, err := matchClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return fmt.Errorf("%s", e.Error)
		}
		return fmt.Errorf("library server returned %s", resp.Status)
	}
	return json.Unmarshal(data, out)
}
