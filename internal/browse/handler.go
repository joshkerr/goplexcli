package browse

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joshkerr/goplexcli/internal/favorites"
	"github.com/joshkerr/goplexcli/internal/plex"
)

// APIPrefix is where the browse API is mounted on the library server.
const APIPrefix = "/api/v1/"

// APIVersion is bumped when the JSON shapes change incompatibly.
const APIVersion = 1

// Source supplies the items and side effects the API needs.
type Source struct {
	// View runs fn with the current items while holding whatever lock
	// guards them; fn must not retain the slice.
	View func(fn func(items []plex.MediaItem))
	// Local reports whether this server streams an item (nil = local key).
	Local func(*plex.MediaItem) bool
	// Favorites is the shared favorites store (nil disables favorites).
	Favorites *favorites.Store
	// SetWatched marks an item watched or unwatched (nil disables it).
	SetWatched func(key string, watched bool) error
	// Name and Version identify the server in /status.
	Name, Version string
	// Updated reports when the library was last indexed (may be nil).
	Updated func() time.Time
}

// Handler serves the browse API.
type Handler struct {
	src Source

	mu    sync.Mutex
	index *Index
	head  *plex.MediaItem
	n     int
}

// NewHandler builds the API handler.
func NewHandler(src Source) *Handler { return &Handler{src: src} }

// withIndex runs fn with an index over the current items, rebuilding it
// when the cache was replaced (a new backing slice or length).
func (h *Handler) withIndex(fn func(ix *Index)) {
	h.src.View(func(items []plex.MediaItem) {
		var head *plex.MediaItem
		if len(items) > 0 {
			head = &items[0]
		}
		h.mu.Lock()
		if h.index == nil || h.head != head || h.n != len(items) {
			h.index = NewIndex(items, h.src.Local)
			h.head, h.n = head, len(items)
		}
		ix := h.index
		h.mu.Unlock()
		fn(ix)
	})
}

func (h *Handler) favSnapshot() map[string]bool {
	if h.src.Favorites == nil {
		return map[string]bool{}
	}
	m, err := h.src.Favorites.Snapshot()
	if err != nil || m == nil {
		return map[string]bool{}
	}
	return m
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func errJSON(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ServeHTTP routes /api/v1/*. CORS is open so a browser-hosted client (dev
// server, WebView with a different origin) can call it; authorization is
// enforced by the library server before requests get here.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Plex-Token")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	route := strings.TrimPrefix(r.URL.Path, APIPrefix)
	q := r.URL.Query()
	fav := h.favSnapshot

	switch route {
	case "status":
		h.withIndex(func(ix *Index) {
			resp := map[string]any{
				"name": h.src.Name, "version": h.src.Version, "api": APIVersion,
				"status":     ix.Status(),
				"categories": Categories,
				"features":   h.features(),
			}
			if h.src.Updated != nil {
				if t := h.src.Updated(); !t.IsZero() {
					resp["lastUpdated"] = t.UTC().Format(time.RFC3339)
				}
			}
			writeJSON(w, http.StatusOK, resp)
		})

	case "browse":
		query := Query{
			Category: q.Get("category"),
			Sort:     q.Get("sort"),
			Desc:     q.Get("desc") == "1" || q.Get("desc") == "true",
			Genre:    q.Get("genre"),
		}
		query.Offset, _ = strconv.Atoi(q.Get("offset"))
		query.Limit, _ = strconv.Atoi(q.Get("limit"))
		if !validCategory(query.Category) {
			errJSON(w, http.StatusBadRequest, "unknown category")
			return
		}
		f := fav()
		h.withIndex(func(ix *Index) { writeJSON(w, http.StatusOK, ix.Browse(query, f)) })

	case "search":
		limit, _ := strconv.Atoi(q.Get("limit"))
		f := fav()
		h.withIndex(func(ix *Index) {
			writeJSON(w, http.StatusOK, map[string]any{"items": ix.Search(q.Get("q"), limit, f)})
		})

	case "item":
		key := q.Get("key")
		if strings.HasPrefix(key, "show:") {
			h.serveShow(w, strings.TrimPrefix(key, "show:"), fav())
			return
		}
		f := fav()
		h.withIndex(func(ix *Index) {
			m, ok := ix.Item(key, f)
			if !ok {
				errJSON(w, http.StatusNotFound, "not found")
				return
			}
			writeJSON(w, http.StatusOK, m)
		})

	case "show":
		h.serveShow(w, q.Get("title"), fav())

	case "episodes":
		season, err := strconv.Atoi(q.Get("season"))
		if err != nil {
			errJSON(w, http.StatusBadRequest, "season is required")
			return
		}
		f := fav()
		h.withIndex(func(ix *Index) {
			writeJSON(w, http.StatusOK, map[string]any{"items": ix.Episodes(q.Get("show"), season, f)})
		})

	case "genres":
		h.withIndex(func(ix *Index) { writeJSON(w, http.StatusOK, map[string]any{"genres": ix.Genres()}) })

	case "favorite":
		if r.Method != http.MethodPost {
			errJSON(w, http.StatusMethodNotAllowed, "POST required")
			return
		}
		if h.src.Favorites == nil {
			errJSON(w, http.StatusServiceUnavailable, "favorites are not available")
			return
		}
		var body struct {
			Key      string `json:"key"`
			Favorite bool   `json:"favorite"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&body); err != nil || body.Key == "" {
			errJSON(w, http.StatusBadRequest, "key is required")
			return
		}
		cur := fav()[body.Key]
		if cur != body.Favorite {
			if _, err := h.src.Favorites.Toggle(body.Key); err != nil {
				errJSON(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"key": body.Key, "favorite": body.Favorite})

	case "watched":
		if r.Method != http.MethodPost {
			errJSON(w, http.StatusMethodNotAllowed, "POST required")
			return
		}
		if h.src.SetWatched == nil {
			errJSON(w, http.StatusServiceUnavailable, "watched state is not available")
			return
		}
		var body struct {
			Key     string `json:"key"`
			Watched bool   `json:"watched"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&body); err != nil || body.Key == "" {
			errJSON(w, http.StatusBadRequest, "key is required")
			return
		}
		if err := h.src.SetWatched(body.Key, body.Watched); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, ErrNotFound) {
				status = http.StatusNotFound
			}
			errJSON(w, status, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"key": body.Key, "watched": body.Watched})

	default:
		errJSON(w, http.StatusNotFound, "not found")
	}
}

// ErrNotFound is returned by SetWatched implementations for unknown keys.
var ErrNotFound = errors.New("item not found")

func (h *Handler) serveShow(w http.ResponseWriter, title string, f map[string]bool) {
	h.withIndex(func(ix *Index) {
		s, ok := ix.Show(title, f)
		if !ok {
			errJSON(w, http.StatusNotFound, "show not found")
			return
		}
		writeJSON(w, http.StatusOK, s)
	})
}

func (h *Handler) features() []string {
	out := []string{"browse", "search", "stream", "timeline"}
	if h.src.Favorites != nil {
		out = append(out, "favorites")
	}
	if h.src.SetWatched != nil {
		out = append(out, "watched")
	}
	return out
}

func validCategory(c string) bool {
	for _, v := range Categories {
		if v == c {
			return true
		}
	}
	return false
}
