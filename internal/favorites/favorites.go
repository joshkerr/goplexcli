// Package favorites stores the user's favorited items — movie Plex keys and
// synthetic "show:<title>" keys — and merges sets between machines.
//
// The on-disk format (v2) keeps one timestamped entry per key; removals are
// kept as tombstones (fav=false) rather than deleted. Merging two sets is
// per-key last-writer-wins, which is commutative and idempotent: any number of
// machines converge no matter who syncs with whom or in which order, and a
// removal on one machine beats an older add on another. The original v1 format
// (a flat JSON array of favorited keys) is migrated on load, stamping every
// key with the current time, so pre-sync favorites are preserved.
//
// It is GUI-agnostic: the Wails GUI and the CLI sync commands share it, with
// Store serializing all file access within a process.
package favorites

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/joshkerr/goplexcli/internal/config"
)

// tombstoneTTL is how long a removal is remembered. A tombstone only needs to
// outlive the last machine that still has the item favorited; half a year is
// far beyond any realistic sync gap, and pruning keeps the file from growing
// forever.
const tombstoneTTL = 180 * 24 * time.Hour

// Entry records one key's state and when it last changed (unix seconds).
type Entry struct {
	Fav bool  `json:"fav"`
	TS  int64 `json:"ts"`
}

// Set is the full favorites state, keyed by card key.
type Set struct {
	Version int              `json:"version"`
	Items   map[string]Entry `json:"items"`
}

// NewSet returns an empty v2 set.
func NewSet() *Set {
	return &Set{Version: 2, Items: map[string]Entry{}}
}

// Path returns the JSON file holding the favorites, alongside the media cache.
func Path() (string, error) {
	dir, err := config.GetCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "favorites.json"), nil
}

// Decode parses either format: the v2 object, or the v1 flat array of
// favorited keys, which is migrated with every key stamped at now. Empty or
// missing data yields an empty set.
func Decode(data []byte, now time.Time) (*Set, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return NewSet(), nil
	}
	if data[0] == '[' {
		var keys []string
		if err := json.Unmarshal(data, &keys); err != nil {
			return nil, fmt.Errorf("parse favorites (v1): %w", err)
		}
		s := NewSet()
		for _, k := range keys {
			if k != "" {
				s.Items[k] = Entry{Fav: true, TS: now.Unix()}
			}
		}
		return s, nil
	}
	var s Set
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse favorites: %w", err)
	}
	if s.Items == nil {
		s.Items = map[string]Entry{}
	}
	s.Version = 2
	return &s, nil
}

// Keys returns the favorited keys (tombstones excluded), sorted.
func (s *Set) Keys() []string {
	keys := make([]string, 0, len(s.Items))
	for k, e := range s.Items {
		if e.Fav {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// Merge folds remote into s, per-key last-writer-wins. A tie on timestamp
// prefers fav=true so the result is the same regardless of merge order.
// Returns whether s changed.
func (s *Set) Merge(remote *Set) bool {
	if remote == nil {
		return false
	}
	changed := false
	for k, r := range remote.Items {
		if k == "" {
			continue
		}
		local, ok := s.Items[k]
		if !ok || r.TS > local.TS || (r.TS == local.TS && r.Fav && !local.Fav) {
			if !ok || r != local {
				s.Items[k] = r
				changed = true
			}
		}
	}
	return changed
}

// prune drops tombstones older than tombstoneTTL.
func (s *Set) prune(now time.Time) {
	cutoff := now.Add(-tombstoneTTL).Unix()
	for k, e := range s.Items {
		if !e.Fav && e.TS < cutoff {
			delete(s.Items, k)
		}
	}
}

// Store serializes access to the favorites file within a process. Every
// operation is a read-modify-write against disk — the file is a few KB, and
// disk being the single source of truth means a merge arriving over the LAN
// and a local toggle can never overwrite each other.
type Store struct {
	mu   sync.Mutex
	path string // empty = resolve the default path per call
}

// NewStore returns a Store on the default favorites path.
func NewStore() *Store { return &Store{} }

// NewStoreAt returns a Store on an explicit path (used by tests).
func NewStoreAt(path string) *Store { return &Store{path: path} }

// load reads the current set. Callers must hold mu. A missing or unreadable
// file yields an empty set rather than an error — favoriting starts fresh.
func (st *Store) load(now time.Time) (*Set, string, error) {
	path := st.path
	if path == "" {
		p, err := Path()
		if err != nil {
			return nil, "", err
		}
		path = p
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return NewSet(), path, nil
	}
	s, err := Decode(data, now)
	if err != nil {
		// A corrupt file shouldn't brick favoriting; start fresh rather than
		// failing every operation forever.
		return NewSet(), path, nil
	}
	return s, path, nil
}

// save writes the set. Callers must hold mu.
func (st *Store) save(s *Set, path string, now time.Time) error {
	s.prune(now)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Toggle flips a key and persists the change. Returns the new state: true if
// the item is now a favorite.
func (st *Store) Toggle(key string) (bool, error) {
	if key == "" {
		return false, fmt.Errorf("empty key")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	now := time.Now()
	s, path, err := st.load(now)
	if err != nil {
		return false, err
	}
	fav := !s.Items[key].Fav
	s.Items[key] = Entry{Fav: fav, TS: now.Unix()}
	if err := st.save(s, path, now); err != nil {
		return fav, fmt.Errorf("failed to save favorites: %w", err)
	}
	return fav, nil
}

// Keys returns the favorited keys, sorted.
func (st *Store) Keys() ([]string, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	s, _, err := st.load(time.Now())
	if err != nil {
		return nil, err
	}
	return s.Keys(), nil
}

// Snapshot returns the favorited keys as a set for membership checks.
func (st *Store) Snapshot() (map[string]bool, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	s, _, err := st.load(time.Now())
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(s.Items))
	for k, e := range s.Items {
		if e.Fav {
			out[k] = true
		}
	}
	return out, nil
}

// Export returns the current set as v2 JSON, for serving to a peer. A v1 file
// is migrated in-memory, so peers always receive mergeable v2 data.
func (st *Store) Export() ([]byte, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	s, _, err := st.load(time.Now())
	if err != nil {
		return nil, err
	}
	return json.Marshal(s)
}

// MergeData merges a peer's favorites JSON (either format) into the local set
// and persists the result if it changed. Returns whether it changed.
func (st *Store) MergeData(data []byte) (bool, error) {
	now := time.Now()
	remote, err := Decode(data, now)
	if err != nil {
		return false, err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	s, path, err := st.load(now)
	if err != nil {
		return false, err
	}
	if !s.Merge(remote) {
		return false, nil
	}
	if err := st.save(s, path, now); err != nil {
		return true, fmt.Errorf("failed to save favorites: %w", err)
	}
	return true, nil
}
