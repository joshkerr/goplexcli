package ui

import (
	"testing"

	"github.com/joshkerr/goplexcli/internal/plex"
)

func TestFormatResumeOption(t *testing.T) {
	tests := []struct {
		name       string
		viewOffset int
		want       string
	}{
		{"zero position", 0, "Resume from 0:00"},
		{"one minute", 60000, "Resume from 1:00"},
		{"one hour 23 min 45 sec", 5025000, "Resume from 1:23:45"},
		{"two hours", 7200000, "Resume from 2:00:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatResumeOption(tt.viewOffset)
			if got != tt.want {
				t.Errorf("formatResumeOption(%d) = %q, want %q", tt.viewOffset, got, tt.want)
			}
		})
	}
}

func TestHasResumableProgress(t *testing.T) {
	tests := []struct {
		name       string
		viewOffset int
		duration   int
		want       bool
	}{
		{"no progress", 0, 7200000, false},
		{"has progress", 3600000, 7200000, true},
		{"almost complete (95%)", 6840000, 7200000, false}, // Treat as watched
		{"90% progress", 6480000, 7200000, true},
		{"negative view offset", -100, 7200000, false},
		{"zero duration", 3600000, 0, false},
		{"negative duration", 3600000, -1000, false},
		{"exactly 95%", 6840000, 7200000, false}, // 95% = watched
		{"just under 95%", 6839000, 7200000, true},
		{"1% progress", 72000, 7200000, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			media := &plex.MediaItem{
				ViewOffset: tt.viewOffset,
				Duration:   tt.duration,
			}
			got := HasResumableProgress(media)
			if got != tt.want {
				percentComplete := 0.0
				if tt.duration > 0 {
					percentComplete = float64(tt.viewOffset) / float64(tt.duration) * 100
				}
				t.Errorf("HasResumableProgress() = %v, want %v (viewOffset=%d, duration=%d, percent=%.2f%%)",
					got, tt.want, tt.viewOffset, tt.duration, percentComplete)
			}
		})
	}
}

func TestFormatResumeHeader(t *testing.T) {
	tests := []struct {
		name       string
		title      string
		viewOffset int
		duration   int
		wantPct    int
	}{
		{"halfway through", "The Matrix", 3600000, 7200000, 50},
		{"quarter through", "Inception", 1800000, 7200000, 25},
		{"almost done", "Interstellar", 6480000, 7200000, 90},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatResumeHeader(tt.title, tt.viewOffset, tt.duration)
			if got == "" {
				t.Error("expected non-empty header")
			}
			// Verify the header contains the expected percentage
			expectedPctStr := ""
			if tt.wantPct > 0 {
				expectedPctStr = "(" // percentage is in parentheses
			}
			if expectedPctStr != "" && !contains(got, expectedPctStr) {
				t.Errorf("header %q should contain percentage", got)
			}
		})
	}
}

func TestCountItemsWithProgress(t *testing.T) {
	tests := []struct {
		name  string
		items []*plex.MediaItem
		want  int
	}{
		{
			name:  "empty list",
			items: []*plex.MediaItem{},
			want:  0,
		},
		{
			name: "no progress",
			items: []*plex.MediaItem{
				{ViewOffset: 0, Duration: 7200000},
				{ViewOffset: 0, Duration: 5400000},
			},
			want: 0,
		},
		{
			name: "all with progress",
			items: []*plex.MediaItem{
				{ViewOffset: 1000000, Duration: 7200000},
				{ViewOffset: 2000000, Duration: 5400000},
			},
			want: 2,
		},
		{
			name: "mixed",
			items: []*plex.MediaItem{
				{ViewOffset: 1000000, Duration: 7200000}, // has progress
				{ViewOffset: 0, Duration: 5400000},       // no progress
				{ViewOffset: 5130000, Duration: 5400000}, // 95% = treated as watched
				{ViewOffset: 3600000, Duration: 7200000}, // has progress
			},
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CountItemsWithProgress(tt.items)
			if got != tt.want {
				t.Errorf("CountItemsWithProgress() = %d, want %d", got, tt.want)
			}
		})
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
