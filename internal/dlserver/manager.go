// Package dlserver is the headless download daemon behind `goplexcli serve`.
// It accepts download requests over a small authenticated REST API and runs
// them through the shared rclone engine (internal/dlengine), independently of
// any GUI: a client can submit a batch, disconnect, and the transfers keep
// going. Job state persists to serve_downloads.json in the cache dir, so
// interrupted transfers restart when the daemon comes back up — the same
// crash-recovery contract the GUI's local engine has.
package dlserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/joshkerr/goplexcli/internal/config"
	"github.com/joshkerr/goplexcli/internal/dlengine"
)

// DownloadRequest is one file a remote client asks this daemon to download.
// The request carries the rclone source path plus the Plex metadata needed for
// sorted-folder routing — never a destination path: the daemon resolves the
// destination against its own configured download directory, so remote callers
// cannot write to arbitrary folders on this machine.
type DownloadRequest struct {
	// Src is the rclone remote path (e.g. "plexcloudservers2:Media/TV/x.mkv").
	// The daemon's own rclone.conf must know the remote.
	Src string `json:"src"`
	// Name is the destination file name; defaults to the base of Src.
	Name string `json:"name,omitempty"`
	// Type/Show route the file into Movies/ or "TV Shows/<show>/" when the
	// daemon has sorted downloads enabled. Type is the Plex media type
	// ("movie" | "episode"); Show is the show title for episodes.
	Type string `json:"type,omitempty"`
	Show string `json:"show,omitempty"`
	// Title/Year are display metadata persisted with the job history.
	Title string `json:"title,omitempty"`
	Year  int    `json:"year,omitempty"`
	// OnExisting controls what happens when the destination file already
	// exists: "skip" drops the transfer, "replace" deletes the existing file
	// first, and "" overwrites in place.
	OnExisting string `json:"onExisting,omitempty"`
}

// SubmitResult reports what a batch submission did.
type SubmitResult struct {
	Accepted int `json:"accepted"`
	Skipped  int `json:"skipped"`
}

// Options fixes the environment jobs run in. Resolved once at daemon startup
// from this machine's config.
type Options struct {
	RcloneBin   string // rclone binary to invoke
	DownloadDir string // absolute destination root
	Sort        bool   // file into Movies/ and TV Shows/<show>/ subfolders
	SlowDevice  bool   // large sequential write buffers for SD/USB targets
}

// job is a queued transfer.
type job struct {
	id   string
	seq  int64
	src  string
	dest string
	name string
}

// Manager owns the daemon's download queue: one transfer at a time, every
// job's latest state kept for the REST API and persisted across restarts.
// It is the headless counterpart of the GUI's queue in gui/downloads.go.
type Manager struct {
	opts Options

	// runMu serializes transfers so only one file downloads at a time, across
	// all submitted batches; queued jobs report "pending" until their turn.
	runMu sync.Mutex
	seq   atomic.Int64

	// mu guards jobs and cancels.
	mu      sync.Mutex
	jobs    map[string]*dlengine.Progress
	cancels map[string]context.CancelFunc

	// quitting is set during shutdown so killed transfers keep their on-disk
	// "in_progress"/"pending" state (and restart on the next daemon start)
	// instead of being recorded as cancelled.
	quitting atomic.Bool
}

// NewManager creates a Manager. Call Restore before serving to requeue jobs
// interrupted by the last shutdown.
func NewManager(opts Options) *Manager {
	return &Manager{
		opts:    opts,
		jobs:    make(map[string]*dlengine.Progress),
		cancels: make(map[string]context.CancelFunc),
	}
}

// Submit validates a batch of requests, records them as pending, and starts
// working through them in the background. It returns as soon as the batch is
// queued — the HTTP handler answers 202 while the transfers run.
func (m *Manager) Submit(reqs []DownloadRequest) (SubmitResult, error) {
	var res SubmitResult
	var jobs []job
	queuedAt := time.Now().UnixMilli()
	for _, r := range reqs {
		if r.Src == "" {
			return res, fmt.Errorf("request is missing src")
		}
		name := r.Name
		if name == "" {
			name = filepath.Base(r.Src)
		}
		dest := filepath.Join(m.opts.DownloadDir, dlengine.Subdir(r.Type, r.Show, m.opts.Sort), name)
		if _, statErr := os.Stat(dest); statErr == nil {
			switch r.OnExisting {
			case "skip":
				res.Skipped++
				continue
			case "replace":
				if err := os.Remove(dest); err != nil {
					return res, fmt.Errorf("failed to remove existing file %q: %w", dest, err)
				}
			}
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return res, fmt.Errorf("failed to create download directory: %w", err)
		}
		seq := m.seq.Add(1)
		j := job{id: fmt.Sprintf("dl_%d_%s", seq, name), seq: seq, src: r.Src, dest: dest, name: name}
		m.record(dlengine.Progress{
			ID: j.id, Seq: j.seq, Name: j.name, Status: "pending", QueuedAt: queuedAt,
			Src: j.src, Dest: j.dest, Title: r.Title, Year: r.Year,
		})
		jobs = append(jobs, j)
		res.Accepted++
	}
	m.start(jobs)
	return res, nil
}

// start works through jobs in the background, one transfer at a time across
// all batches.
func (m *Manager) start(jobs []job) {
	if len(jobs) == 0 {
		return
	}
	go func() {
		for _, j := range jobs {
			m.runMu.Lock()
			m.run(j)
			m.runMu.Unlock()
		}
	}()
}

// run executes a single transfer via the shared engine. Mirrors the GUI's
// runRclone, minus pause support (remote clients can only cancel).
func (m *Manager) run(j job) {
	// During shutdown, leave queued jobs untouched: their on-disk state is
	// still "pending", so they restart on the next daemon start.
	if m.quitting.Load() {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Register the cancel func so Cancel can reach this transfer — unless the
	// job was already cancelled while it sat in the queue.
	m.mu.Lock()
	if e, ok := m.jobs[j.id]; ok && e.Status == "cancelled" {
		m.mu.Unlock()
		return
	}
	m.cancels[j.id] = cancel
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.cancels, j.id)
		m.mu.Unlock()
	}()

	m.record(dlengine.Progress{ID: j.id, Seq: j.seq, Name: j.name, Status: "in_progress"})

	last, err := dlengine.Run(ctx, m.opts.RcloneBin, j.src, j.dest,
		dlengine.RunOptions{SlowDevice: m.opts.SlowDevice},
		func(s dlengine.Stats) {
			m.record(dlengine.Progress{
				ID: j.id, Seq: j.seq, Name: j.name, Status: "in_progress",
				Percent: s.Percent, Bytes: s.Bytes, Total: s.Total, Speed: s.Speed, ETA: s.ETA,
			})
		})
	if err != nil {
		if ctx.Err() != nil {
			// A user cancel, not a failure. A shutdown-triggered kill leaves
			// the persisted "in_progress" state alone so the transfer restarts
			// on the next daemon start.
			if !m.quitting.Load() {
				m.record(dlengine.Progress{
					ID: j.id, Seq: j.seq, Name: j.name, Status: "cancelled",
					Percent: last.Percent, Bytes: last.Bytes, Total: last.Total,
				})
			}
			return
		}
		m.record(dlengine.Progress{ID: j.id, Seq: j.seq, Name: j.name, Status: "failed", Error: err.Error()})
		return
	}

	m.record(dlengine.Progress{
		ID: j.id, Seq: j.seq, Name: j.name, Status: "completed",
		Percent: 100, Bytes: last.Total, Total: last.Total,
	})
}

// record stores the latest state for the REST API, carrying forward the
// identifying fields the initial "pending" record set, and persists on every
// status transition (not every 500ms progress tick).
func (m *Manager) record(p dlengine.Progress) {
	m.mu.Lock()
	prev := m.jobs[p.ID]
	if prev != nil {
		if p.Src == "" {
			p.Src = prev.Src
		}
		if p.Dest == "" {
			p.Dest = prev.Dest
		}
		if p.Title == "" {
			p.Title = prev.Title
		}
		if p.Year == 0 {
			p.Year = prev.Year
		}
		if p.QueuedAt == 0 {
			p.QueuedAt = prev.QueuedAt
		}
	}
	statusChanged := prev == nil || prev.Status != p.Status
	cp := p
	m.jobs[p.ID] = &cp
	m.mu.Unlock()
	if statusChanged {
		if err := m.saveHistory(); err != nil {
			fmt.Printf("failed to save download history: %v\n", err)
		}
	}
}

// List returns every known download (live and historical), newest first.
func (m *Manager) List() []dlengine.Progress {
	m.mu.Lock()
	out := make([]dlengine.Progress, 0, len(m.jobs))
	for _, e := range m.jobs {
		out = append(out, *e)
	}
	m.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Seq > out[j].Seq })
	return out
}

// Cancel aborts a queued or in-flight download. Queued jobs are skipped when
// their turn comes; the in-flight job's rclone process is killed via its
// context. Cancelling an already-finished job is a no-op.
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	e, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("unknown download %q", id)
	}
	switch e.Status {
	case "pending":
		p := *e
		p.Status = "cancelled"
		m.mu.Unlock()
		m.record(p)
	case "in_progress":
		cancel := m.cancels[id]
		m.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	default:
		m.mu.Unlock()
	}
	return nil
}

// ClearFinished removes all completed/failed/cancelled entries, keeping
// active jobs, and persists the result.
func (m *Manager) ClearFinished() error {
	m.mu.Lock()
	for id, e := range m.jobs {
		if dlengine.IsTerminal(e.Status) {
			delete(m.jobs, id)
		}
	}
	m.mu.Unlock()
	return m.saveHistory()
}

// Shutdown kills any in-flight transfer while preserving its persisted
// "in_progress" state, so it restarts on the next daemon start.
func (m *Manager) Shutdown() {
	m.quitting.Store(true)
	m.mu.Lock()
	for _, cancel := range m.cancels {
		cancel()
	}
	m.mu.Unlock()
}

// ---- History persistence ----

// maxHistory caps the persisted history so serve_downloads.json can't grow
// without bound; the newest entries win.
const maxHistory = 200

// historyPath returns the JSON file holding the daemon's download history,
// alongside the media cache. Distinct from the GUI's downloads.json so a
// machine can run both without the two queues clobbering each other.
func historyPath() (string, error) {
	dir, err := config.GetCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "serve_downloads.json"), nil
}

func (m *Manager) saveHistory() error {
	list := m.List() // newest first
	if len(list) > maxHistory {
		list = list[:maxHistory]
	}
	path, err := historyPath()
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

// Restore loads persisted history and requeues the jobs that were still queued
// or transferring when the daemon last stopped, oldest first. Entries missing
// their src/dest can't be requeued and are marked failed instead. It returns
// the number of restarted jobs.
func (m *Manager) Restore() int {
	path, err := historyPath()
	if err != nil {
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0 // no history yet (or unreadable) — start empty
	}
	var list []dlengine.Progress
	if err := json.Unmarshal(data, &list); err != nil {
		return 0
	}
	var requeue []job
	m.mu.Lock()
	for i := range list {
		e := list[i]
		if e.Status == "pending" || e.Status == "in_progress" {
			if e.Src != "" && e.Dest != "" {
				// rclone can't resume a partial file, so the job restarts
				// from zero.
				e.Status = "pending"
				e.Percent, e.Bytes, e.Speed = 0, 0, 0
				e.Error = ""
				requeue = append(requeue, job{id: e.ID, seq: e.Seq, src: e.Src, dest: e.Dest, name: e.Name})
			} else {
				e.Status = "failed"
				e.Error = "interrupted — the server stopped during the download"
			}
		}
		m.jobs[e.ID] = &e
		// Keep new job IDs/order strictly after everything restored.
		if e.Seq > m.seq.Load() {
			m.seq.Store(e.Seq)
		}
	}
	m.mu.Unlock()
	sort.Slice(requeue, func(i, j int) bool { return requeue[i].seq < requeue[j].seq })
	m.start(requeue)
	return len(requeue)
}

// GenerateToken returns a fresh bearer token for a first-time `serve` run:
// 32 random bytes, hex-encoded.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
