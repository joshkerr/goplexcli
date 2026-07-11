package main

import "testing"

// TestStatsRegexSpeed checks that the rclone stats parser extracts the transfer
// rate (and stays correct when rclone omits it).
func TestStatsRegexSpeed(t *testing.T) {
	cases := []struct {
		line      string
		pct       string
		wantSpeed int64 // bytes/sec, 0 = none
	}{
		{"Transferred:   \t  1.234 GiB / 5.678 GiB, 22%, 10 MiB/s, ETA 7m30s", "22", 10 << 20},
		{"Transferred:        512 KiB / 100 MiB, 0%, 0 B/s, ETA -", "0", 0},
		{"Transferred:   \t  2.0 GiB / 2.0 GiB, 100%, 45 MiB/s, ETA 0s", "100", 45 << 20},
		{"Transferred:   \t  1.5 MiB / 900 MiB, 0%", "0", 0}, // no speed field at all
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
	}
}
