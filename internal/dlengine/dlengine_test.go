package dlengine

import (
	"path/filepath"
	"testing"
)

// TestStatsRegexSpeed checks that the rclone stats parser extracts the transfer
// rate and ETA (and stays correct when rclone omits either).
func TestStatsRegexSpeed(t *testing.T) {
	cases := []struct {
		line      string
		pct       string
		wantSpeed int64  // bytes/sec, 0 = none
		wantETA   string // "" = none (absent or the "-" placeholder)
	}{
		{"Transferred:   \t  1.234 GiB / 5.678 GiB, 22%, 10 MiB/s, ETA 7m30s", "22", 10 << 20, "7m30s"},
		{"Transferred:        512 KiB / 100 MiB, 0%, 0 B/s, ETA -", "0", 0, ""},
		{"Transferred:   \t  2.0 GiB / 2.0 GiB, 100%, 45 MiB/s, ETA 0s", "100", 45 << 20, "0s"},
		{"Transferred:   \t  1.5 MiB / 900 MiB, 0%", "0", 0, ""}, // no speed or ETA field at all
		{"Transferred:   \t  3 GiB / 12 GiB, 25%, 8 MiB/s, ETA 1h2m3s", "25", 8 << 20, "1h2m3s"},
	}
	for _, tc := range cases {
		m := statsRegex.FindStringSubmatch(tc.line)
		if len(m) < 6 {
			t.Fatalf("line did not match: %q", tc.line)
		}
		if m[5] != tc.pct {
			t.Errorf("percent = %q, want %q (%q)", m[5], tc.pct, tc.line)
		}
		var speed int64
		if len(m) >= 8 && m[6] != "" {
			speed = parseSize(m[6], m[7])
		}
		if speed != tc.wantSpeed {
			t.Errorf("speed = %d, want %d (%q)", speed, tc.wantSpeed, tc.line)
		}
		var eta string
		if len(m) >= 9 && m[8] != "-" {
			eta = m[8]
		}
		if eta != tc.wantETA {
			t.Errorf("eta = %q, want %q (%q)", eta, tc.wantETA, tc.line)
		}
	}
}

// TestSubdir checks the sorted-download destination routing: movies into
// Movies/, episodes into TV Shows/<show>/ with unsafe folder characters
// cleaned, and everything else (or sorting disabled) into the root.
func TestSubdir(t *testing.T) {
	cases := []struct {
		name      string
		mediaType string
		show      string
		sorted    bool
		want      string
	}{
		{"disabled", "movie", "", false, ""},
		{"movie", "movie", "", true, "Movies"},
		{"episode", "episode", "Severance", true, filepath.Join("TV Shows", "Severance")},
		{"episode show name sanitized", "episode", "What If...?", true, filepath.Join("TV Shows", "What If")},
		{"episode with colon", "episode", "Star Trek: Picard", true, filepath.Join("TV Shows", "Star Trek- Picard")},
		{"episode without show", "episode", "", true, "TV Shows"},
		{"other type", "track", "", true, ""},
	}
	for _, tc := range cases {
		if got := Subdir(tc.mediaType, tc.show, tc.sorted); got != tc.want {
			t.Errorf("%s: Subdir = %q, want %q", tc.name, got, tc.want)
		}
	}
}
