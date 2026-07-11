package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// DownloadProgress is emitted on "download:progress" for each active transfer.
type DownloadProgress struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Percent float64 `json:"percent"`
	Status  string  `json:"status"` // pending | in_progress | completed | failed
	Bytes   int64   `json:"bytes"`
	Total   int64   `json:"total"`
	Speed   int64   `json:"speed"` // bytes/sec, as reported by rclone (0 if unknown)
	Error   string  `json:"error"`
}

// downloadJob is a single file transfer.
type downloadJob struct {
	id   string
	src  string
	dest string
	name string
}

// Download copies the given cached items (by Plex key) to the configured (or
// overridden) download directory using rclone, emitting "download:progress"
// events as each transfer advances.
//
// It runs rclone directly (rather than via rclone-golib's executor) so it can
// (a) honor the configured rclone path, (b) suppress the console window that
// Windows otherwise pops up for a console subprocess of a GUI app, and
// (c) surface failures in the UI instead of a silent black console.
func (a *App) Download(keys []string, destOverride string) error {
	if len(keys) == 0 {
		return fmt.Errorf("no items to download")
	}

	cfg := a.config()
	c := a.media()
	if c == nil {
		return fmt.Errorf("media cache is empty")
	}

	items, err := resolveItems(c, keys)
	if err != nil {
		return err
	}

	destDir, err := cfg.ResolveDownloadDir(destOverride)
	if err != nil {
		return err
	}

	rcloneBin := cfg.RclonePath
	if rcloneBin == "" {
		rcloneBin = "rclone"
	}
	if _, err := exec.LookPath(rcloneBin); err != nil {
		return fmt.Errorf("rclone not found (%q). Install rclone or set its path in Settings", rcloneBin)
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("failed to create download directory: %w", err)
	}

	var jobs []downloadJob
	for _, it := range items {
		if it.RclonePath == "" {
			continue // no rclone path; skip silently
		}
		name := filepath.Base(it.RclonePath)
		jobs = append(jobs, downloadJob{
			id:   fmt.Sprintf("dl_%d_%s", a.dlSeq.Add(1), name),
			src:  it.RclonePath,
			dest: filepath.Join(destDir, name),
			name: name,
		})
	}
	if len(jobs) == 0 {
		return fmt.Errorf("none of the selected items have a downloadable path")
	}

	// Show every job as queued right away; each waits for dlMu below so only
	// one transfer runs at a time, across all Download() calls.
	for _, j := range jobs {
		a.emitDownload(DownloadProgress{ID: j.id, Name: j.name, Status: "pending"})
	}

	var firstErr error
	for _, j := range jobs {
		a.dlMu.Lock()
		err := a.runRclone(rcloneBin, j)
		a.dlMu.Unlock()
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return fmt.Errorf("download failed: %w", firstErr)
	}
	return nil
}

// statsRegex matches rclone's "Transferred:" progress lines (printed to stderr
// with -v), e.g. "Transferred: 1.234 GiB / 5.678 GiB, 22%, 10 MiB/s, ETA 1m30s".
// The trailing rate (group 6/7) is optional — rclone may omit it early on or
// print a non-numeric placeholder.
var statsRegex = regexp.MustCompile(`Transferred:\s+([0-9.]+)\s*([kKMGTP]i?[Bb]?)\s*/\s*([0-9.]+)\s*([kKMGTP]i?[Bb]?),\s*([0-9]+)%(?:,\s*([0-9.]+)\s*([kKMGTP]?i?[Bb])/s)?`)

// runRclone executes a single transfer, parsing progress from stderr and
// emitting events. The rclone subprocess is started with the OS-specific
// attributes from configureSysProc (no console window on Windows).
func (a *App) runRclone(bin string, j downloadJob) error {
	a.emitDownload(DownloadProgress{ID: j.id, Name: j.name, Status: "in_progress"})

	args := []string{"copyto", "-v", "--stats", "500ms", "--ignore-checksum", j.src, j.dest}
	cmd := exec.CommandContext(context.Background(), bin, args...)
	configureSysProc(cmd)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return a.failDownload(j, fmt.Errorf("failed to capture rclone output: %w", err))
	}
	if err := cmd.Start(); err != nil {
		return a.failDownload(j, fmt.Errorf("failed to start rclone: %w", err))
	}

	var lastBytes, lastTotal int64
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
			lastBytes = parseSize(m[1], m[2])
			lastTotal = parseSize(m[3], m[4])
			var speed int64
			if len(m) >= 8 && m[6] != "" {
				speed = parseSize(m[6], m[7])
			}
			a.emitDownload(DownloadProgress{
				ID: j.id, Name: j.name, Status: "in_progress",
				Percent: pct, Bytes: lastBytes, Total: lastTotal, Speed: speed,
			})
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
		return a.failDownload(j, fmt.Errorf("%s", msg))
	}

	a.emitDownload(DownloadProgress{
		ID: j.id, Name: j.name, Status: "completed",
		Percent: 100, Bytes: lastTotal, Total: lastTotal,
	})
	return nil
}

func (a *App) failDownload(j downloadJob, err error) error {
	a.emitDownload(DownloadProgress{ID: j.id, Name: j.name, Status: "failed", Error: err.Error()})
	return err
}

func (a *App) emitDownload(dp DownloadProgress) {
	if a.ctx == nil {
		return
	}
	wruntime.EventsEmit(a.ctx, "download:progress", dp)
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
