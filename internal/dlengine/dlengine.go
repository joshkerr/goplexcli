// Package dlengine is the shared rclone download engine: it runs a single
// rclone transfer, parses live progress from its stderr, and defines the
// Progress record every download surface uses. Both the Wails GUI and the
// headless `goplexcli serve` daemon build their queues on top of it, so the
// rclone invocation (args, tuning flags, stats parsing) exists exactly once.
package dlengine

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Progress is the state of one download. It is simultaneously the GUI's
// "download:progress" event payload, the serve daemon's REST response record,
// and the persisted history entry — one shape everywhere so remote and local
// jobs can share a single Downloads list.
type Progress struct {
	ID      string  `json:"id"`
	Seq     int64   `json:"seq"` // monotonically increasing; higher = added later
	Name    string  `json:"name"`
	Percent float64 `json:"percent"`
	Status  string  `json:"status"` // pending | in_progress | paused | completed | failed | cancelled
	Bytes   int64   `json:"bytes"`
	Total   int64   `json:"total"`
	Speed   int64   `json:"speed"` // bytes/sec, as reported by rclone (0 if unknown)
	ETA     string  `json:"eta"`   // rclone's remaining-time estimate ("" if unknown); transient, only set on in_progress records
	Error   string  `json:"error"`

	// QueuedAt is the wall-clock time the job was queued (unix ms). Seq only
	// orders jobs from one machine; QueuedAt orders them across the local
	// engine and remote serve daemons in the GUI's merged Downloads list.
	QueuedAt int64 `json:"queuedAt,omitempty"`

	// Origin names the remote server a job runs on ("" = this process). Set by
	// the GUI when it merges jobs fetched from remote serve daemons; never set
	// on records a process stores about its own transfers.
	Origin string `json:"origin,omitempty"`

	// Src/Dest are the rclone source and local destination, persisted so an
	// interrupted download can be restarted on the next launch. Not shown in
	// the UI.
	Src  string `json:"src,omitempty"`
	Dest string `json:"dest,omitempty"`

	// Title/Year carry the item's Plex metadata (show title for episodes) so
	// "Send to rclonecp" can seed rclonecp's poster search with the exact name
	// instead of re-parsing the filename. Persisted with the history so the
	// handoff still works for downloads finished in an earlier session.
	Title string `json:"title,omitempty"`
	Year  int    `json:"year,omitempty"`
}

// IsTerminal reports whether a status is final (no further transitions).
func IsTerminal(status string) bool {
	switch status {
	case "completed", "failed", "cancelled":
		return true
	}
	return false
}

// Stats is a live progress sample parsed from one of rclone's stderr
// "Transferred:" lines.
type Stats struct {
	Percent float64
	Bytes   int64
	Total   int64
	Speed   int64  // bytes/sec (0 if unknown)
	ETA     string // "" if unknown
}

// RunOptions tunes a transfer.
type RunOptions struct {
	// SlowDevice buffers each multi-thread stream's writes into large
	// sequential chunks (--multi-thread-write-buffer-size=128M). SD cards and
	// USB drives collapse under concurrent random writes without this.
	SlowDevice bool
}

// statsRegex matches rclone's "Transferred:" progress lines (printed to stderr
// with -v), e.g. "Transferred: 1.234 GiB / 5.678 GiB, 22%, 10 MiB/s, ETA 1m30s".
// The trailing rate (group 6/7) and ETA (group 8) are optional — rclone may
// omit them early on or print a non-numeric placeholder ("ETA -").
var statsRegex = regexp.MustCompile(`Transferred:\s+([0-9.]+)\s*([kKMGTP]i?[Bb]?)\s*/\s*([0-9.]+)\s*([kKMGTP]i?[Bb]?),\s*([0-9]+)%(?:,\s*([0-9.]+)\s*([kKMGTP]?i?[Bb])/s)?(?:,\s*ETA\s+(\S+))?`)

// Run executes a single rclone transfer of src to dest, invoking onStats for
// each progress line until the process exits. It returns the last observed
// stats and, on failure, an error carrying the tail of rclone's diagnostic
// output. Cancelling ctx kills the subprocess; the caller distinguishes
// user-cancel from failure by checking ctx.Err() when Run returns an error.
//
// rclone is run directly (rather than via rclone-golib's executor) so callers
// can (a) honor a configured rclone path, (b) suppress the console window that
// Windows otherwise pops up for a console subprocess of a GUI app, and
// (c) surface failures to their own UI instead of a silent black console.
func Run(ctx context.Context, bin, src, dest string, opts RunOptions, onStats func(Stats)) (Stats, error) {
	// 16 streams because a single TCP stream caps around 3 MiB/s on
	// high-latency links; 32M cutoff so mid-size files multi-thread too.
	args := []string{"copyto", "-v", "--stats", "500ms", "--ignore-checksum", "--multi-thread-streams", "16", "--multi-thread-cutoff", "32M"}
	if opts.SlowDevice {
		args = append(args, "--multi-thread-write-buffer-size", "128M")
	}
	args = append(args, src, dest)
	cmd := exec.CommandContext(ctx, bin, args...)
	ConfigureSysProc(cmd)

	var last Stats
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return last, fmt.Errorf("failed to capture rclone output: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return last, fmt.Errorf("failed to start rclone: %w", err)
	}

	var errLines []string
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	scanner.Split(splitCROrLF)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if m := statsRegex.FindStringSubmatch(line); len(m) >= 6 {
			pct, _ := strconv.ParseFloat(m[5], 64)
			last.Percent = pct
			last.Bytes = parseSize(m[1], m[2])
			last.Total = parseSize(m[3], m[4])
			last.Speed = 0
			if len(m) >= 8 && m[6] != "" {
				last.Speed = parseSize(m[6], m[7])
			}
			last.ETA = ""
			if len(m) >= 9 && m[8] != "-" {
				last.ETA = m[8]
			}
			if onStats != nil {
				onStats(last)
			}
			continue
		}
		// Keep a short tail of diagnostic lines for error reporting.
		if strings.Contains(line, "ERROR") || strings.Contains(line, "Failed") ||
			strings.Contains(line, "error") || strings.Contains(line, "can't") {
			errLines = append(errLines, line)
			if len(errLines) > 5 {
				errLines = errLines[len(errLines)-5:]
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		msg := strings.Join(errLines, "; ")
		if msg == "" {
			msg = err.Error()
		}
		return last, fmt.Errorf("%s", msg)
	}
	return last, nil
}

// Subdir returns the subfolder (relative to the download directory) an item is
// filed into when sorted downloads are enabled: movies into "Movies", episodes
// into "TV Shows/<show>" — the layout gowebdav's Movies/TV tabs auto-detect,
// with the show folder naming the show. Plex metadata decides, so no filename
// guessing is involved; items of any other type (and episodes missing a show
// title) land in the download directory itself.
func Subdir(mediaType, show string, sorted bool) string {
	if !sorted {
		return ""
	}
	switch mediaType {
	case "movie":
		return "Movies"
	case "episode":
		if s := SanitizeDirName(show); s != "" {
			return filepath.Join("TV Shows", s)
		}
		return "TV Shows"
	}
	return ""
}

// SanitizeDirName makes a Plex title safe as a folder name: characters Windows
// forbids become "-", control characters are dropped, and edge dots/spaces
// (invalid on Windows) are trimmed along with any dashes those replacements
// left dangling ("What If...?" -> "What If").
func SanitizeDirName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return '-'
		}
		if r < 0x20 {
			return -1
		}
		return r
	}, name)
	return strings.Trim(name, " .-")
}

// splitCROrLF is a bufio.SplitFunc that treats both \r and \n as line
// terminators, so rclone's in-place \r progress updates are read as they arrive.
func splitCROrLF(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := strings.IndexAny(string(data), "\r\n"); i >= 0 {
		advance = i + 1
		if advance < len(data) && data[i] == '\r' && data[advance] == '\n' {
			advance++
		}
		return advance, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// parseSize converts an rclone size value + unit (e.g. "1.234", "GiB") to bytes.
func parseSize(value, unit string) int64 {
	val, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	unit = strings.ToUpper(strings.TrimSpace(unit))
	unit = strings.TrimSuffix(unit, "B")
	unit = strings.TrimSuffix(unit, "I")
	multiplier := int64(1)
	switch unit {
	case "K":
		multiplier = 1 << 10
	case "M":
		multiplier = 1 << 20
	case "G":
		multiplier = 1 << 30
	case "T":
		multiplier = 1 << 40
	case "P":
		multiplier = 1 << 50
	}
	return int64(val * float64(multiplier))
}
