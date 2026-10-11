package tmdb

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Store caches raw JSON responses keyed by request path+query (never
// containing the API key).
type Store interface {
	Get(key string) ([]byte, bool)
	Put(key string, data []byte)
}

// DiskStore is a Store backed by one file per entry under dir (sha1 of key as
// filename, .json). Entries older than ttl are treated as misses. A ttl <= 0
// means entries never expire. Safe for concurrent use.
type DiskStore struct {
	dir string
	ttl time.Duration
	mu  sync.RWMutex
}

// NewDiskStore creates dir if needed and returns a DiskStore rooted there.
func NewDiskStore(dir string, ttl time.Duration) (*DiskStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &DiskStore{dir: dir, ttl: ttl}, nil
}

func (s *DiskStore) path(key string) string {
	sum := sha1.Sum([]byte(key))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:])+".json")
}

// Get returns the cached bytes for key if present and not expired.
func (s *DiskStore) Get(key string) ([]byte, bool) {
	p := s.path(key)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ttl > 0 {
		fi, err := os.Stat(p)
		if err != nil {
			return nil, false
		}
		if time.Since(fi.ModTime()) > s.ttl {
			return nil, false
		}
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	return data, true
}

// Put stores data for key, writing to a temp file and renaming so readers
// never observe a partial entry.
func (s *DiskStore) Put(key string, data []byte) {
	p := s.path(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	tmp, err := os.CreateTemp(s.dir, ".tmp-*")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, p); err != nil {
		os.Remove(tmpName)
	}
}
