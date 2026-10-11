package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/joshkerr/goplexcli/internal/config"
	"github.com/joshkerr/goplexcli/internal/libserver"
	"github.com/joshkerr/goplexcli/internal/plex"
	"github.com/joshkerr/goplexcli/internal/tmdb"
)

// Override pins a movie file or a show folder to a chosen TMDB title when
// the automatic match is wrong. Movies are keyed by item key, shows by
// ShowOverrideKey (the show folder), so every episode, including ones added
// later, follows the fix.
type Override struct {
	TMDB  int    `json:"tmdb"`
	Kind  string `json:"kind"` // "movie" | "tv"
	Title string `json:"title,omitempty"`
	SetAt int64  `json:"setAt"`
	// Applied is set once the items were rebuilt with this override; until
	// then the next scan rebuilds them even if the files are unchanged.
	Applied bool `json:"applied,omitempty"`
}

// Overrides is the on-disk set of manual matches.
type Overrides struct {
	path string
	mu   sync.RWMutex
	m    map[string]Override
}

// OverridesPath is where manual matches live under the goplexcli cache dir.
func OverridesPath(cacheDir string) string {
	return filepath.Join(cacheDir, "library", "matches.json")
}

// LoadOverrides reads the manual matches (an absent file is an empty set).
func LoadOverrides(cacheDir string) (*Overrides, error) {
	o := &Overrides{path: OverridesPath(cacheDir), m: map[string]Override{}}
	data, err := os.ReadFile(o.path)
	if err != nil {
		if os.IsNotExist(err) {
			return o, nil
		}
		return nil, err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &o.m); err != nil {
			return nil, fmt.Errorf("%s: %w", o.path, err)
		}
	}
	return o, nil
}

// Get returns the override for a key.
func (o *Overrides) Get(key string) (Override, bool) {
	if o == nil {
		return Override{}, false
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	ov, ok := o.m[key]
	return ov, ok
}

// Set records an override; Delete removes one.
func (o *Overrides) Set(key string, ov Override) {
	o.mu.Lock()
	o.m[key] = ov
	o.mu.Unlock()
}

// Delete removes an override.
func (o *Overrides) Delete(key string) {
	o.mu.Lock()
	delete(o.m, key)
	o.mu.Unlock()
}

// MarkApplied flags every override as applied; it reports whether any
// changed (so the caller knows to Save).
func (o *Overrides) MarkApplied() bool {
	if o == nil {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	changed := false
	for k, v := range o.m {
		if !v.Applied {
			v.Applied = true
			o.m[k] = v
			changed = true
		}
	}
	return changed
}

// Len returns how many overrides are set.
func (o *Overrides) Len() int {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return len(o.m)
}

// Save writes the set atomically.
func (o *Overrides) Save() error {
	o.mu.RLock()
	data, err := json.MarshalIndent(o.m, "", "  ")
	o.mu.RUnlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(o.path), 0o755); err != nil {
		return err
	}
	tmp := o.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, o.path)
}

// ShowOverrideKey keys a show by its library and group (folder) key.
func ShowOverrideKey(libraryName, groupKey string) string {
	return "show:" + libraryName + "|" + groupKey
}

// TMDBBaseURL overrides the TMDB API base URL (tests point it at a fake).
var TMDBBaseURL string

// NewTMDBClient builds the TMDB client the indexer uses, or nil when no key
// is configured.
func NewTMDBClient(cfg *config.Config, cacheDir string) (*tmdb.Client, error) {
	key := strings.TrimSpace(cfg.TMDBAPIKey)
	if key == "" {
		return nil, nil
	}
	store, err := tmdb.NewDiskStore(TMDBCacheDir(cacheDir), tmdbCacheTTL)
	if err != nil {
		return nil, fmt.Errorf("tmdb cache: %w", err)
	}
	c := tmdb.New(key, store)
	if TMDBBaseURL != "" {
		c.SetBaseURL(TMDBBaseURL)
	}
	return c, nil
}

// SearchTitles looks up candidate titles on TMDB for a manual match. kind is
// "movie" or "tv"; year narrows the search when > 0. Poster paths are
// returned as library-server thumb paths.
func SearchTitles(ctx context.Context, client *tmdb.Client, kind, query string, year int) ([]libserver.Candidate, error) {
	if client == nil {
		return nil, errors.New("no tmdb_api_key configured")
	}
	var results []tmdb.SearchResult
	var err error
	switch kind {
	case "tv", "show":
		kind = "tv"
		results, err = client.SearchShows(ctx, query, year)
	default:
		kind = "movie"
		results, err = client.SearchMovies(ctx, query, year)
	}
	if err != nil {
		return nil, err
	}
	out := make([]libserver.Candidate, 0, len(results))
	for _, r := range results {
		c := libserver.Candidate{ID: r.ID, Kind: kind, Title: r.Title, Year: r.Year, Overview: r.Overview, Rating: r.VoteAverage}
		if r.PosterPath != "" {
			c.Poster = libserver.ThumbPath(r.PosterPath)
		}
		out = append(out, c)
	}
	return out, nil
}

// Rematch rebuilds one movie or one whole show with a chosen TMDB id and
// records it as an override so later scans keep it. tmdbID 0 clears the
// override and goes back to the automatic match. key is the cache key of the
// movie or of any episode of the show. It returns the rebuilt items (same
// keys as before) for the caller to merge into the cache.
func Rematch(ctx context.Context, cfg *config.Config, cacheDir string, items []plex.MediaItem, key string, tmdbID int, logf func(string, ...any)) ([]plex.MediaItem, error) {
	serverName := ServerName(cfg)
	var target *plex.MediaItem
	for i := range items {
		if items[i].Key == key && items[i].ServerName == serverName {
			target = &items[i]
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("item %s is not in this machine's library", key)
	}
	lib, ok := libraryFor(cfg, target.FilePath)
	if !ok {
		return nil, fmt.Errorf("no library folder contains %s", target.FilePath)
	}
	client, err := NewTMDBClient(cfg, cacheDir)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("no tmdb_api_key configured")
	}
	overrides, err := LoadOverrides(cacheDir)
	if err != nil {
		return nil, err
	}
	opts := Options{
		TMDB:         client,
		ServerName:   serverName,
		ServerURL:    ServerURL(cfg.LibraryURL, cfg.LibraryListen),
		PathMappings: cfg.PathMappings,
		Probe:        cfg.LibraryProbe,
		Force:        true,
		Overrides:    overrides,
		Logf:         logf,
	}
	ix := newIndexer(opts, items)

	var rebuilt []plex.MediaItem
	if target.Type == "episode" {
		// Regroup every episode of this library so the whole show is
		// rebuilt together and the override lands on its folder key.
		var files []ScannedFile
		for i := range items {
			it := &items[i]
			if it.ServerName == serverName && it.Type == "episode" && strings.HasPrefix(it.FilePath, lib.Path) {
				files = append(files, scannedFromItem(lib, it))
			}
		}
		groups := groupEpisodes(files)
		var grp *showGroup
		for _, g := range groups {
			for _, ef := range g.files {
				if ef.file.Path == target.FilePath {
					grp = g
					break
				}
			}
			if grp != nil {
				break
			}
		}
		if grp == nil {
			return nil, fmt.Errorf("could not locate the show folder for %s", target.FilePath)
		}
		okey := ShowOverrideKey(lib.Name, grp.key)
		if tmdbID > 0 {
			overrides.Set(okey, Override{TMDB: tmdbID, Kind: "tv", Title: grp.name.Title, SetAt: time.Now().Unix(), Applied: true})
		} else {
			overrides.Delete(okey)
		}
		rebuilt, err = ix.showItems(ctx, lib, grp, &showTitles{seen: map[string]int{}})
	} else {
		if tmdbID > 0 {
			overrides.Set(target.Key, Override{TMDB: tmdbID, Kind: "movie", Title: target.Title, SetAt: time.Now().Unix(), Applied: true})
		} else {
			overrides.Delete(target.Key)
		}
		var item plex.MediaItem
		item, err = ix.movieItem(ctx, lib, scannedFromItem(lib, target), 1)
		rebuilt = []plex.MediaItem{item}
	}
	if err != nil {
		return nil, err
	}
	if err := overrides.Save(); err != nil {
		return nil, fmt.Errorf("save overrides: %w", err)
	}
	return rebuilt, nil
}

// MergeByKey replaces items in dst that share a key with one in updates and
// appends the rest. It returns the merged slice.
func MergeByKey(dst, updates []plex.MediaItem) []plex.MediaItem {
	idx := make(map[string]int, len(updates))
	for i := range updates {
		idx[updates[i].ServerName+"\x00"+updates[i].Key] = i
	}
	used := make([]bool, len(updates))
	for i := range dst {
		if j, ok := idx[dst[i].ServerName+"\x00"+dst[i].Key]; ok {
			dst[i] = updates[j]
			used[j] = true
		}
	}
	for j := range updates {
		if !used[j] {
			dst = append(dst, updates[j])
		}
	}
	return dst
}

// libraryFor finds the configured library whose path contains file (longest
// match wins).
func libraryFor(cfg *config.Config, file string) (config.LibraryFolder, bool) {
	var best config.LibraryFolder
	found := false
	for _, lib := range cfg.Libraries {
		root := strings.TrimRight(filepath.Clean(lib.Path), string(filepath.Separator)) + string(filepath.Separator)
		if strings.HasPrefix(file, root) && (!found || len(lib.Path) > len(best.Path)) {
			best, found = lib, true
		}
	}
	return best, found
}

// scannedFromItem reconstructs the scan record of an indexed file.
func scannedFromItem(lib config.LibraryFolder, it *plex.MediaItem) ScannedFile {
	rel, err := filepath.Rel(filepath.Clean(lib.Path), it.FilePath)
	if err != nil {
		rel = filepath.Base(it.FilePath)
	}
	return ScannedFile{
		Path:    it.FilePath,
		Rel:     path.Clean(filepath.ToSlash(rel)),
		ModTime: time.Unix(it.AddedAt, 0),
	}
}

// OverrideSummary lists the manual matches for display.
type OverrideSummary struct {
	Key   string
	Title string
	Kind  string
	TMDB  int
}

// List returns the overrides sorted by key.
func (o *Overrides) List() []OverrideSummary {
	o.mu.RLock()
	defer o.mu.RUnlock()
	out := make([]OverrideSummary, 0, len(o.m))
	for k, v := range o.m {
		out = append(out, OverrideSummary{Key: k, Title: v.Title, Kind: v.Kind, TMDB: v.TMDB})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
