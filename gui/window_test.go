package main

import "testing"

// TestUpdateWindowState checks the fold of live geometry into persisted state:
// a maximized close must keep the previous restore size, and junk readings
// below the app minimum are ignored.
func TestUpdateWindowState(t *testing.T) {
	cases := []struct {
		name      string
		prev      windowState
		maximized bool
		w, h      int
		want      windowState
	}{
		{
			"normal close records size",
			windowState{Maximized: true, Width: 1000, Height: 700},
			false, 1200, 800,
			windowState{Maximized: false, Width: 1200, Height: 800},
		},
		{
			"maximized close keeps previous size",
			windowState{Width: 1000, Height: 700},
			true, 2560, 1400,
			windowState{Maximized: true, Width: 1000, Height: 700},
		},
		{
			"sub-minimum reading ignored",
			windowState{Width: 1000, Height: 700},
			false, 0, 0,
			windowState{Maximized: false, Width: 1000, Height: 700},
		},
		{
			"first run maximized has no size",
			windowState{},
			true, 2560, 1400,
			windowState{Maximized: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := updateWindowState(tc.prev, tc.maximized, tc.w, tc.h); got != tc.want {
				t.Errorf("updateWindowState = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestWindowStatePersistence round-trips window.json and checks the
// first-run/missing-file path.
func TestWindowStatePersistence(t *testing.T) {
	isolateHistory(t)

	if _, ok := loadWindowState(); ok {
		t.Fatal("loadWindowState reported ok with no file")
	}
	want := windowState{Maximized: true, Width: 1100, Height: 750}
	if err := saveWindowState(want); err != nil {
		t.Fatalf("saveWindowState: %v", err)
	}
	got, ok := loadWindowState()
	if !ok || got != want {
		t.Errorf("loadWindowState = %+v, %v; want %+v, true", got, ok, want)
	}
}
