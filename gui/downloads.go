package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/joshkerr/goplexcli/internal/config"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// DownloadProgress is emitted on "download:progress" for each active transfer.
type DownloadProgress struct {
	ID      string  `json:"id"`
	Seq     int64   `json:"seq"` // monotonically increasing; higher = added later
	Name    string  `json:"name"`
	Percent float64 `json:"percent"`
	Status  string  `json:"status"` // pending | in_progress | completed | failed | cancelled
	Bytes   int64   `json:"bytes"`
	Total   int64   `json:"total"`
	Speed   int64   `json:"speed"` // bytes/sec, as reported by rclone (0 if unknown)
	Error   string  `json:"error"`

	// Src/Dest are the rclone source and local destination, persisted so an
	// interrupted download can be restarted on the next launch. Not shown in
	// the UI.
	Src  string `json:"src,omitempty"`
	Dest string `json:"dest,omitempty"`
}

// downloadJob is a single file transfer.
type downloadJob struct {
	id   string
	seq  int64
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
		seq := a.dlSeq.Add(1)
		jobs = append(jobs, downloadJob{
			id:   fmt.Sprintf("dl_%d_%s", seq, name),
			seq:  seq,
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
		a.recordDownload(DownloadProgress{
			ID: j.id, Seq: j.seq, Name: j.name, Status: "pending",
			Src: j.src, Dest: j.dest,
		})
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
// attributes from configureSysProc (no console window on Windows). The
// transfer can be aborted via CancelDownload, which cancels the context and
// kills the subprocess.
func (a *App) runRclone(bin string, j downloadJob) error {
	// During shutdown, leave queued jobs untouched: their on-disk state is
	// still "pending", so they restart on the next launch.
	if a.quitting.Load() {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Register the cancel func so CancelDownload can reach this transfer —
	// unless the job was already cancelled while it sat in the queue.
	a.dlStateMu.Lock()
	if e, ok := a.dlHist[j.id]; ok && e.Status == "cancelled" {
		a.dlStateMu.Unlock()
		return nil
	}
	a.dlCancels[j.id] = cancel
	a.dlStateMu.Unlock()
	defer func() {
		a.dlStateMu.Lock()
		delete(a.dlCancels, j.id)
		a.dlStateMu.Unlock()
	}()

	a.recordDownload(DownloadProgress{ID: j.id, Seq: j.seq, Name: j.name, Status: "in_progress"})

	args := []string{"copyto", "-v", "--stats", "500ms", "--ignore-checksum", j.src, j.dest}
	cmd := exec.CommandContext(ctx, bin, args...)
	configureSysProc(cmd)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return a.failDownload(j, fmt.Errorf("failed to capture rclone output: %w", err))
	}
	if err := cmd.Start(); err != nil {
		if ctx.Err() != nil {
			if !a.quitting.Load() {
				a.cancelledDownload(j, 0, 0, 0)
			}
			return nil
		}
		return a.failDownload(j, fmt.Errorf("failed to start rclone: %w", err))
	}

	var lastPct float64
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
			lastPct = pct
			lastBytes = parseSize(m[1], m[2])
			lastTotal = parseSize(m[3], m[4])
			var speed int64
			if len(m) >= 8 && m[6] != "" {
				speed = parseSize(m[6], m[7])
			}
			a.recordDownload(DownloadProgress{
				ID: j.id, Seq: j.seq, Name: j.name, Status: "in_progress",
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
		// A cancelled transfer is not a failure — report it as such and don't
		// bubble an error up to the Download() caller. If the cancel came from
		// app shutdown rather than the user, leave the on-disk "in_progress"
		// entry alone so the download restarts on the next launch.
		if ctx.Err() != nil {
			if !a.quitting.Load() {
				a.cancelledDownload(j, lastPct, lastBytes, lastTotal)
			}
			return nil
		}
		msg := strings.Join(errLines, "; ")
		if msg == "" {
			msg = err.Error()
		}
		return a.failDownload(j, fmt.Errorf("%s", msg))
	}

	a.recordDownload(DownloadProgress{
		ID: j.id, Seq: j.seq, Name: j.name, Status: "completed",
		Percent: 100, Bytes: lastTotal, Total: lastTotal,
	})
	return nil
}

func (a *App) failDownload(j downloadJob, err error) error {
	a.recordDownload(DownloadProgress{ID: j.id, Seq: j.seq, Name: j.name, Status: "failed", Error: err.Error()})
	return err
}

func (a *App) cancelledDownload(j downloadJob, pct float64, bytes, total int64) {
	a.recordDownload(DownloadProgress{
		ID: j.id, Seq: j.seq, Name: j.name, Status: "cancelled",
		Percent: pct, Bytes: bytes, Total: total,
	})
}

// recordDownload stores the latest state for the Downloads panel, emits the
// "download:progress" event, and persists history on every status transition
// (not every 500ms progress tick). Persisting queued/in-flight jobs — with
// their src/dest carried over from the initial "pending" record — is what
// lets an interrupted queue restart after a crash or quit.
func (a *App) recordDownload(dp DownloadProgress) {
	a.dlStateMu.Lock()
	prev := a.dlHist[dp.ID]
	if prev != nil {
		if dp.Src == "" {
			dp.Src = prev.Src
		}
		if dp.Dest == "" {
			dp.Dest = prev.Dest
		}
	}
	statusChanged := prev == nil || prev.Status != dp.Status
	cp := dp
	a.dlHist[dp.ID] = &cp
	a.dlStateMu.Unlock()
	a.emitDownload(dp)
	if statusChanged {
		if err := a.saveDownloadHistory(); err != nil {
			fmt.Printf("failed to save download history: %v\n", err)
		}
	}
}

func (a *App) emitDownload(dp DownloadProgress) {
	if a.ctx == nil {
		return
	}
	wruntime.EventsEmit(a.ctx, "download:progress", dp)
}

// ---- Bound methods: download list / cancel / history ----

// ListDownloads returns every known download (live and historical), newest
// first, so the Downloads panel can restore its state on launch.
func (a *App) ListDownloads() []DownloadProgress {
	a.dlStateMu.Lock()
	out := make([]DownloadProgress, 0, len(a.dlHist))
	for _, e := range a.dlHist {
		out = append(out, *e)
	}
	a.dlStateMu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Seq > out[j].Seq })
	return out
}

// CancelDownload aborts a queued or in-flight download. Queued jobs are
// skipped when their turn comes; the in-flight job's rclone process is killed
// via its context.
func (a *App) CancelDownload(id string) error {
	a.dlStateMu.Lock()
	e, ok := a.dlHist[id]
	if !ok {
		a.dlStateMu.Unlock()
		return fmt.Errorf("unknown download %q", id)
	}
	switch e.Status {
	case "pending":
		dp := *e
		dp.Status = "cancelled"
		a.dlStateMu.Unlock()
		a.recordDownload(dp)
	case "in_progress":
		cancel := a.dlCancels[id]
		a.dlStateMu.Unlock()
		if cancel != nil {
			cancel()
		}
	default:
		// Already finished; nothing to do.
		a.dlStateMu.Unlock()
	}
	return nil
}

// ClearDownloadHistory removes all finished (completed/failed/cancelled)
// entries, keeping active jobs, and persists the result.
func (a *App) ClearDownloadHistory() error {
	a.dlStateMu.Lock()
	for id, e := range a.dlHist {
		switch e.Status {
		case "completed", "failed", "cancelled":
			delete(a.dlHist, id)
		}
	}
	a.dlStateMu.Unlock()
	return a.saveDownloadHistory()
}

// ---- History persistence ----

// maxDownloadHistory caps the persisted history so downloads.json can't grow
// without bound; the newest entries win.
const maxDownloadHistory = 200

// downloadHistoryPath returns the JSON file holding download history,
// alongside the media cache.
func downloadHistoryPath() (string, error) {
	dir, err := config.GetCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "downloads.json"), nil
}

func (a *App) saveDownloadHistory() error {
	list := a.ListDownloads() // newest first
	if len(list) > maxDownloadHistory {
		list = list[:maxDownloadHistory]
	}
	path, err := downloadHistoryPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// loadDownloadHistory restores persisted history at startup and returns the
// jobs that were still queued or transferring when the app last quit, oldest
// first, so the caller can restart them. Interrupted entries missing their
// src/dest (pre-restart-support history) can't be requeued and are marked
// failed instead.
func (a *App) loadDownloadHistory() []downloadJob {
	path, err := downloadHistoryPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil // no history yet (or unreadable) — start empty
	}
	var list []DownloadProgress
	if err := json.Unmarshal(data, &list); err != nil {
		return nil
	}
	var requeue []downloadJob
	a.dlStateMu.Lock()
	for i := range list {
		e := list[i]
		if e.Status == "pending" || e.Status == "in_progress" {
			if e.Src != "" && e.Dest != "" {
				// rclone can't resume a partial file, so the job restarts
				// from zero.
				e.Status = "pending"
				e.Percent, e.Bytes, e.Speed = 0, 0, 0
				e.Error = ""
				requeue = append(requeue, downloadJob{
					id: e.ID, seq: e.Seq, src: e.Src, dest: e.Dest, name: e.Name,
				})
			} else {
				e.Status = "failed"
				e.Error = "interrupted — the app quit during the download"
			}
		}
		a.dlHist[e.ID] = &e
		// Keep new job IDs/order strictly after everything we restored.
		if e.Seq > a.dlSeq.Load() {
			a.dlSeq.Store(e.Seq)
		}
	}
	a.dlStateMu.Unlock()
	sort.Slice(requeue, func(i, j int) bool { return requeue[i].seq < requeue[j].seq })
	return requeue
}

// resumeDownloads restarts downloads that were interrupted by the last quit
// or crash. It runs in its own goroutine and takes the same per-transfer
// dlMu as Download(), so restarted jobs and newly requested ones interleave
// one at a time as usual.
func (a *App) resumeDownloads(jobs []downloadJob) {
	cfg := a.config()
	bin := cfg.RclonePath
	if bin == "" {
		bin = "rclone"
	}
	if _, err := exec.LookPath(bin); err != nil {
		for _, j := range jobs {
			_ = a.failDownload(j, fmt.Errorf("cannot restart: rclone not found (%q)", bin))
		}
		return
	}
	for _, j := range jobs {
		a.dlMu.Lock()
		_ = a.runRclone(bin, j) // failures are already recorded per job
		a.dlMu.Unlock()
	}
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
