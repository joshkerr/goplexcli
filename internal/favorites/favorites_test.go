package favorites

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func storeAt(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "favorites.json")
	return NewStoreAt(path), path
}

// TestV1Migration checks that a pre-sync flat-array file is read intact — no
// favorites are lost — and that the first write upgrades it to v2.
func TestV1Migration(t *testing.T) {
	st, path := storeAt(t)
	if err := os.WriteFile(path, []byte(`["m1","show:Severance"]`), 0o644); err != nil {
		t.Fatal(err)
	}

	keys, err := st.Keys()
	if err != nil {
		t.Fatalf("Keys: %v", err)
	}
	if len(keys) != 2 || keys[0] != "m1" || keys[1] != "show:Severance" {
		t.Fatalf("migrated keys = %v; want [m1 show:Severance]", keys)
	}

	// A write persists v2 and keeps the migrated favorites.
	if _, err := st.Toggle("m2"); err != nil {
		t.Fatalf("Toggle: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var s Set
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("saved file is not a v2 object: %v", err)
	}
	if s.Version != 2 || !s.Items["m1"].Fav || !s.Items["show:Severance"].Fav || !s.Items["m2"].Fav {
		t.Errorf("v2 file lost favorites: %+v", s)
	}
}

func TestToggleRoundTrip(t *testing.T) {
	st, _ := storeAt(t)
	if fav, err := st.Toggle("m1"); err != nil || !fav {
		t.Fatalf("Toggle on = %v, %v; want true, nil", fav, err)
	}
	if fav, err := st.Toggle("m1"); err != nil || fav {
		t.Fatalf("Toggle off = %v, %v; want false, nil", fav, err)
	}
	if _, err := st.Toggle(""); err == nil {
		t.Error("Toggle(\"\") should fail")
	}
	keys, _ := st.Keys()
	if len(keys) != 0 {
		t.Errorf("keys after off = %v; want empty", keys)
	}
	// The tombstone is kept so the removal can win a future merge.
	snap, _ := st.Snapshot()
	if snap["m1"] {
		t.Error("snapshot still reports m1 favorited")
	}
}

// TestMerge checks last-writer-wins in both directions, tombstone propagation,
// the fav-wins tie-break, and that merging is commutative.
func TestMerge(t *testing.T) {
	mk := func(items map[string]Entry) *Set {
		s := NewSet()
		for k, v := range items {
			s.Items[k] = v
		}
		return s
	}
	a := mk(map[string]Entry{
		"keep-local": {Fav: true, TS: 200},  // newer than remote's remove
		"removed":    {Fav: true, TS: 100},  // remote removed later
		"tie":        {Fav: false, TS: 300}, // equal TS: fav wins
		"local-only": {Fav: true, TS: 100},
	})
	b := mk(map[string]Entry{
		"keep-local":  {Fav: false, TS: 150},
		"removed":     {Fav: false, TS: 250},
		"tie":         {Fav: true, TS: 300},
		"remote-only": {Fav: true, TS: 100},
	})

	merged := mk(nil)
	merged.Merge(a)
	if !merged.Merge(b) {
		t.Fatal("merge reported no change")
	}
	want := []string{"keep-local", "local-only", "remote-only", "tie"}
	got := merged.Keys()
	if len(got) != len(want) {
		t.Fatalf("merged keys = %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("merged keys = %v; want %v", got, want)
		}
	}

	// Commutative: b then a gives the same result.
	other := mk(nil)
	other.Merge(b)
	other.Merge(a)
	for k, e := range merged.Items {
		if other.Items[k] != e {
			t.Errorf("merge not commutative at %q: %+v vs %+v", k, e, other.Items[k])
		}
	}

	// Idempotent: merging again changes nothing.
	if merged.Merge(b) {
		t.Error("re-merge reported a change")
	}
}

func TestMergeData(t *testing.T) {
	st, _ := storeAt(t)
	if _, err := st.Toggle("m1"); err != nil {
		t.Fatal(err)
	}

	remote := NewSet()
	remote.Items["m2"] = Entry{Fav: true, TS: time.Now().Unix()}
	data, _ := json.Marshal(remote)

	changed, err := st.MergeData(data)
	if err != nil || !changed {
		t.Fatalf("MergeData = %v, %v; want true, nil", changed, err)
	}
	keys, _ := st.Keys()
	if len(keys) != 2 || keys[0] != "m1" || keys[1] != "m2" {
		t.Errorf("keys after merge = %v; want [m1 m2]", keys)
	}
	if changed, _ := st.MergeData(data); changed {
		t.Error("second MergeData reported a change")
	}
}

func TestPruneTombstones(t *testing.T) {
	s := NewSet()
	old := time.Now().Add(-tombstoneTTL - time.Hour).Unix()
	s.Items["stale-remove"] = Entry{Fav: false, TS: old}
	s.Items["old-fav"] = Entry{Fav: true, TS: old} // favorites are never pruned
	s.Items["fresh-remove"] = Entry{Fav: false, TS: time.Now().Unix()}
	s.prune(time.Now())
	if _, ok := s.Items["stale-remove"]; ok {
		t.Error("stale tombstone not pruned")
	}
	if _, ok := s.Items["old-fav"]; !ok {
		t.Error("old favorite was pruned")
	}
	if _, ok := s.Items["fresh-remove"]; !ok {
		t.Error("fresh tombstone was pruned")
	}
}
