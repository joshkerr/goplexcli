package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/joshkerr/goplexcli/internal/config"
	"github.com/joshkerr/goplexcli/internal/dlengine"
	"github.com/joshkerr/goplexcli/internal/plex"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// DownloadProgress is emitted on "download:progress" for each active transfer.
// It is the shared engine's Progress record — the same shape the serve daemon
// returns over REST, which is what lets remote jobs merge into the local
// Downloads panel unchanged.
type DownloadProgress = dlengine.Progress

// downloadJob is a single file transfer.
type downloadJob struct {
	id    string
	seq   int64
	src   string
	dest  string
	name  string
	title string
	year  int
}

// DownloadConflict describes a requested download whose final destination file
// already exists on disk. Returned by CheckDownloadConflicts so the frontend
// can ask the user whether to replace, skip, or cancel before starting.
type DownloadConflict struct {
	Name string `json:"name"`
	Dest string `json:"dest"`
}

// downloadTarget pairs a cached item with the exact destination path Download
// will write to.
type downloadTarget struct {
	item *plex.MediaItem
	name string
	dest string
}

// resolveDownloadTargets resolves the given Plex keys to their source items and
// final destination paths. Both Download and CheckDownloadConflicts build their
// paths through this helper so the two can never disagree. Items without an
// rclone path are silently dropped, matching Download's historical behavior.
func (a *App) resolveDownloadTargets(keys []string, destOverride string) ([]downloadTarget, string, error) {
	cfg := a.config()
	c := a.media()
	if c == nil {
		return nil, "", fmt.Errorf("media cache is empty")
	}

	items, missing, err := resolveItems(c, keys)
	if err != nil {
		return nil, "", err
	}
	if len(missing) > 0 {
		return nil, "", fmt.Errorf("%d of %d items not found in cache", len(missing), len(keys))
	}

	// With nothing configured, ResolveDownloadDir falls back to the process
	// working directory — which for a Finder-launched app is "/", the
	// read-only system volume. Default to ~/Downloads instead.
	if destOverride == "" && cfg.DownloadDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, "", fmt.Errorf("no download directory configured and no home directory found: %w", err)
		}
		destOverride = filepath.Join(home, "Downloads")
	}

	destDir, err := cfg.ResolveDownloadDir(destOverride)
	if err != nil {
		return nil, "", err
	}

	var targets []downloadTarget
	for _, it := range items {
		if it.RclonePath == "" {
			continue // no rclone path; skip silently
		}
		name := filepath.Base(it.RclonePath)
		targets = append(targets, downloadTarget{
			item: it,
			name: name,
			dest: filepath.Join(destDir, downloadSubdir(it, cfg.SortDownloads), name),
		})
	}
	return targets, destDir, nil
}

// CheckDownloadConflicts reports which of the given items already have their
// final destination file on disk, using exactly the paths Download would write
// to. The frontend calls this before Download so it can prompt the user.
func (a *App) CheckDownloadConflicts(keys []string, destOverride string) ([]DownloadConflict, error) {
	if len(keys) == 0 {
		return []DownloadConflict{}, nil
	}
	targets, _, err := a.resolveDownloadTargets(keys, destOverride)
	if err != nil {
		return nil, err
	}
	conflicts := []DownloadConflict{}
	for _, t := range targets {
		if _, err := os.Stat(t.dest); err == nil {
			conflicts = append(conflicts, DownloadConflict{Name: t.name, Dest: t.dest})
		}
	}
	return conflicts, nil
}

// Download copies the given cached items (by Plex key) to the configured (or
// overridden) download directory using rclone, emitting "download:progress"
// events as each transfer advances.
//
// onExisting controls what happens to items whose destination file already
// exists: "skip" drops those transfers, "replace" deletes the existing file
// before downloading, and "" keeps the historical behavior (rclone overwrites
// in place). The frontend picks the policy via CheckDownloadConflicts.
//
// It runs rclone directly (rather than via rclone-golib's executor) so it can
// (a) honor the configured rclone path, (b) suppress the console window that
// Windows otherwise pops up for a console subprocess of a GUI app, and
// (c) surface failures in the UI instead of a silent black console.
func (a *App) Download(keys []string, destOverride string, onExisting string) error {
	if len(keys) == 0 {
		return fmt.Errorf("no items to download")
	}

	cfg := a.config()
	targets, destDir, err := a.resolveDownloadTargets(keys, destOverride)
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
	skipped := 0
	for _, t := range targets {
		if _, statErr := os.Stat(t.dest); statErr == nil {
			switch onExisting {
			case "skip":
				skipped++
				continue
			case "replace":
				if err := os.Remove(t.dest); err != nil {
					return fmt.Errorf("failed to remove existing file %q: %w", t.dest, err)
				}
			}
		}
		it := t.item
		// Episodes carry the show title: that's the name rclonecp's poster
		// search needs (TMDB is searched by show, not by episode).
		title := it.Title
		if it.Type == "episode" && it.ParentTitle != "" {
			title = it.ParentTitle
		}
		if err := os.MkdirAll(filepath.Dir(t.dest), 0o755); err != nil {
			return fmt.Errorf("failed to create download directory: %w", err)
		}
		seq := a.dlSeq.Add(1)
		jobs = append(jobs, downloadJob{
			id:    fmt.Sprintf("dl_%d_%s", seq, t.name),
			seq:   seq,
			src:   it.RclonePath,
			dest:  t.dest,
			name:  t.name,
			title: title,
			year:  it.Year,
		})
	}
	if len(jobs) == 0 {
		if skipped > 0 {
			// Every requested file already exists and the user chose to skip.
			return nil
		}
		return fmt.Errorf("none of the selected items have a downloadable path")
	}

	// Show every job as queued right away; each waits for dlMu below so only
	// one transfer runs at a time, across all Download() calls. QueuedAt orders
	// these against jobs running on remote serve daemons in the merged list.
	queuedAt := time.Now().UnixMilli()
	for _, j := range jobs {
		a.recordDownload(DownloadProgress{
			ID: j.id, Seq: j.seq, Name: j.name, Status: "pending", QueuedAt: queuedAt,
			Src: j.src, Dest: j.dest, Title: j.title, Year: j.year,
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

// downloadSubdir returns the subfolder (relative to the download directory) an
// item is filed into when sorted downloads are enabled: movies into "Movies",
// episodes into "TV Shows/<show>" — the layout gowebdav's Movies/TV tabs
// auto-detect, with the show folder naming the show. Plex metadata decides, so
// no filename guessing is involved; items of any other type (and episodes
// missing a show title) land in the download directory itself.
func downloadSubdir(it *plex.MediaItem, sorted bool) string {
	if it == nil {
		return ""
	}
	return dlengine.Subdir(it.Type, it.ParentTitle, sorted)
}

// runRclone executes a single transfer via the shared engine, recording
// progress as events. The transfer can be aborted via CancelDownload, which
// cancels the context and kills the subprocess.
func (a *App) runRclone(bin string, j downloadJob) error {
	// During shutdown, leave queued jobs untouched: their on-disk state is
	// still "pending", so they restart on the next launch.
	if a.quitting.Load() {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Register the cancel func so CancelDownload can reach this transfer —
	// unless the job was already cancelled or paused while it sat in the queue.
	a.dlStateMu.Lock()
	if e, ok := a.dlHist[j.id]; ok && (e.Status == "cancelled" || e.Status == "paused") {
		a.dlStateMu.Unlock()
		return nil
	}
	a.dlCancels[j.id] = cancel
	a.dlStateMu.Unlock()
	defer func() {
		a.dlStateMu.Lock()
		delete(a.dlCancels, j.id)
		delete(a.dlPauseReq, j.id)
		a.dlStateMu.Unlock()
	}()

	a.recordDownload(DownloadProgress{ID: j.id, Seq: j.seq, Name: j.name, Status: "in_progress"})

	last, err := dlengine.Run(ctx, bin, j.src, j.dest,
		dlengine.RunOptions{SlowDevice: a.config().SlowDeviceMode},
		func(s dlengine.Stats) {
			a.recordDownload(DownloadProgress{
				ID: j.id, Seq: j.seq, Name: j.name, Status: "in_progress",
				Percent: s.Percent, Bytes: s.Bytes, Total: s.Total, Speed: s.Speed, ETA: s.ETA,
			})
		})
	if err != nil {
		// A cancelled transfer is not a failure — report it as such and don't
		// bubble an error up to the Download() caller. A kill triggered by
		// PauseDownload records "paused" (resumable) instead. If the cancel
		// came from app shutdown rather than the user, leave the on-disk
		// "in_progress" entry alone so the download restarts on the next launch.
		if ctx.Err() != nil {
			if a.consumePauseReq(j.id) {
				a.pausedDownload(j, last.Percent, last.Bytes, last.Total)
			} else if !a.quitting.Load() {
				a.cancelledDownload(j, last.Percent, last.Bytes, last.Total)
			}
			return nil
		}
		return a.failDownload(j, err)
	}

	a.recordDownload(DownloadProgress{
		ID: j.id, Seq: j.seq, Name: j.name, Status: "completed",
		Percent: 100, Bytes: last.Total, Total: last.Total,
	})
	a.maybeAutoSendToRclonecp(j.id)
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

// pausedDownload records a paused transfer, keeping the progress it reached so
// the panel can show where it stopped. (The numbers are informational: rclone
// can't continue a partial file, so a resume restarts from zero.)
func (a *App) pausedDownload(j downloadJob, pct float64, bytes, total int64) {
	a.recordDownload(DownloadProgress{
		ID: j.id, Seq: j.seq, Name: j.name, Status: "paused",
		Percent: pct, Bytes: bytes, Total: total,
	})
}

// consumePauseReq reports whether the job's context cancellation was a pause
// request, clearing the flag.
func (a *App) consumePauseReq(id string) bool {
	a.dlStateMu.Lock()
	defer a.dlStateMu.Unlock()
	if a.dlPauseReq[id] {
		delete(a.dlPauseReq, id)
		return true
	}
	return false
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
		if dp.Title == "" {
			dp.Title = prev.Title
		}
		if dp.Year == 0 {
			dp.Year = prev.Year
		}
		if dp.QueuedAt == 0 {
			dp.QueuedAt = prev.QueuedAt
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
	a.updateTaskbarProgress()
}

// updateTaskbarProgress mirrors the download queue on the OS taskbar/dock
// icon: the in-flight transfer's percent while one is running, an empty bar
// while jobs are only queued, and cleared when the queue is idle. Downloads
// run one at a time (dlMu), so the in-flight percent is the natural overall
// signal.
func (a *App) updateTaskbarProgress() {
	a.dlStateMu.Lock()
	running, pending := 0, 0
	var sum float64
	for _, e := range a.dlHist {
		switch e.Status {
		case "in_progress":
			running++
			sum += e.Percent
		case "pending":
			pending++
		}
	}
	a.dlStateMu.Unlock()

	switch {
	case running > 0:
		setTaskbarProgress(sum / float64(running) / 100)
	case pending > 0:
		setTaskbarProgress(0)
	default:
		setTaskbarProgress(-1)
	}
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

// CancelDownload aborts a queued, paused, or in-flight download. Queued jobs
// are skipped when their turn comes; the in-flight job's rclone process is
// killed via its context.
func (a *App) CancelDownload(id string) error {
	a.dlStateMu.Lock()
	e, ok := a.dlHist[id]
	if !ok {
		a.dlStateMu.Unlock()
		return fmt.Errorf("unknown download %q", id)
	}
	switch e.Status {
	case "pending", "paused":
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

// PauseDownload pauses a queued or in-flight download. Paused entries survive
// quitting the app (they persist as "paused" in downloads.json and are not
// auto-restarted on launch) and resume only when the user asks. Pausing the
// in-flight transfer kills its rclone process; the resumed file restarts from
// zero because rclone cannot continue a partial transfer.
func (a *App) PauseDownload(id string) error {
	a.dlStateMu.Lock()
	e, ok := a.dlHist[id]
	if !ok {
		a.dlStateMu.Unlock()
		return fmt.Errorf("unknown download %q", id)
	}
	switch e.Status {
	case "pending":
		dp := *e
		dp.Status = "paused"
		a.dlStateMu.Unlock()
		a.recordDownload(dp)
	case "in_progress":
		// Mark the kill as a pause before cancelling so runRclone's exit path
		// can't observe the cancel first and record "cancelled".
		a.dlPauseReq[id] = true
		cancel := a.dlCancels[id]
		a.dlStateMu.Unlock()
		if cancel != nil {
			cancel()
		}
	default:
		// Already finished (or already paused); nothing to do.
		a.dlStateMu.Unlock()
	}
	return nil
}

// ResumeDownload requeues a paused download behind whatever is already
// transferring. The file restarts from the beginning (rclone cannot continue
// a partial file), so progress is reset rather than continued.
func (a *App) ResumeDownload(id string) error {
	a.dlStateMu.Lock()
	e, ok := a.dlHist[id]
	if !ok {
		a.dlStateMu.Unlock()
		return fmt.Errorf("unknown download %q", id)
	}
	if e.Status != "paused" {
		// Already resumed, finished, or never paused — nothing to do.
		a.dlStateMu.Unlock()
		return nil
	}
	if e.Src == "" || e.Dest == "" {
		a.dlStateMu.Unlock()
		return fmt.Errorf("download %q is missing its source and cannot be resumed", id)
	}
	// Flip to pending under the lock so a rapid double-resume can't queue the
	// job twice.
	e.Status = "pending"
	e.Percent, e.Bytes, e.Speed = 0, 0, 0
	e.Error = ""
	dp := *e
	j := downloadJob{id: e.ID, seq: e.Seq, src: e.Src, dest: e.Dest, name: e.Name}
	a.dlStateMu.Unlock()

	a.emitDownload(dp)
	if err := a.saveDownloadHistory(); err != nil {
		fmt.Printf("failed to save download history: %v\n", err)
	}

	cfg := a.config()
	bin := cfg.RclonePath
	if bin == "" {
		bin = "rclone"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return a.failDownload(j, fmt.Errorf("cannot resume: rclone not found (%q)", bin))
	}
	go func() {
		a.dlMu.Lock()
		_ = a.runRclone(bin, j)
		a.dlMu.Unlock()
	}()
	return nil
}

// ClearDownloadHistory removes all finished (completed/failed/cancelled)
// entries, keeping active and paused jobs, and persists the result.
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
// first, so the caller can restart them. Paused entries are restored as-is —
// they wait for an explicit ResumeDownload. Interrupted entries missing their
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
