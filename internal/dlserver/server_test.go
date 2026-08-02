package dlserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshkerr/goplexcli/internal/dlengine"
)

// isolate points the config/cache dir at a temp dir so history writes don't
// touch the real config. USERPROFILE and APPDATA must be overridden too —
// HOME alone doesn't redirect os.UserHomeDir/GetConfigDir on Windows.
func isolate(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("APPDATA", tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("XDG_CONFIG_HOME", "")
}

// noSuchBin is an rclone binary that can't exist, so submitted jobs fail fast
// instead of actually transferring during tests.
const noSuchBin = "goplexcli-test-no-such-rclone"

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	return NewManager(Options{RcloneBin: noSuchBin, DownloadDir: t.TempDir()})
}

// TestPingAndAuth checks that /ping identifies the daemon without auth and
// that every other endpoint rejects missing or wrong bearer tokens.
func TestPingAndAuth(t *testing.T) {
	isolate(t)
	srv := httptest.NewServer(New("box", "1.2.3", "sekrit", newTestManager(t)).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/ping")
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ping status = %d, want 200", resp.StatusCode)
	}
	var ping PingResponse
	if err := json.NewDecoder(resp.Body).Decode(&ping); err != nil {
		t.Fatalf("ping decode: %v", err)
	}
	if ping.Name != "box" || ping.Version != "1.2.3" || ping.Protocol != Protocol {
		t.Errorf("ping = %+v, want name box, version 1.2.3, protocol %d", ping, Protocol)
	}

	get := func(token string) int {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/downloads", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	if got := get(""); got != http.StatusUnauthorized {
		t.Errorf("no token status = %d, want 401", got)
	}
	if got := get("wrong"); got != http.StatusUnauthorized {
		t.Errorf("wrong token status = %d, want 401", got)
	}
	if got := get("sekrit"); got != http.StatusOK {
		t.Errorf("right token status = %d, want 200", got)
	}
}

// TestSubmitSkipExisting checks the existing-file policy: with "skip", a
// request whose destination file exists is dropped and reported as skipped.
func TestSubmitSkipExisting(t *testing.T) {
	isolate(t)
	destDir := t.TempDir()
	mgr := NewManager(Options{RcloneBin: noSuchBin, DownloadDir: destDir})
	if err := os.WriteFile(filepath.Join(destDir, "x.mkv"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(New("box", "dev", "tok", mgr).Handler())
	defer srv.Close()

	body, _ := json.Marshal([]DownloadRequest{{Src: "remote:media/x.mkv", OnExisting: "skip"}})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/downloads", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("submit status = %d, want 202", resp.StatusCode)
	}
	var res SubmitResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("submit decode: %v", err)
	}
	if res.Accepted != 0 || res.Skipped != 1 {
		t.Errorf("submit = %+v, want accepted 0, skipped 1", res)
	}
	if jobs := mgr.List(); len(jobs) != 0 {
		t.Errorf("jobs = %+v, want none for a fully skipped batch", jobs)
	}
}

// TestSubmitRoutesSortedDest checks that a submitted episode is routed into
// TV Shows/<show>/ under the daemon's own download dir when sorting is on.
func TestSubmitRoutesSortedDest(t *testing.T) {
	isolate(t)
	destDir := t.TempDir()
	mgr := NewManager(Options{RcloneBin: noSuchBin, DownloadDir: destDir, Sort: true})

	res, err := mgr.Submit([]DownloadRequest{{
		Src: "remote:media/tv/ep1.mkv", Type: "episode", Show: "Severance", Title: "Severance", Year: 2022,
	}})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if res.Accepted != 1 {
		t.Fatalf("accepted = %d, want 1", res.Accepted)
	}
	jobs := mgr.List()
	if len(jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(jobs))
	}
	want := filepath.Join(destDir, "TV Shows", "Severance", "ep1.mkv")
	if jobs[0].Dest != want {
		t.Errorf("dest = %q, want %q", jobs[0].Dest, want)
	}
	if jobs[0].Name != "ep1.mkv" || jobs[0].QueuedAt == 0 {
		t.Errorf("job = %+v, want name ep1.mkv and a queuedAt stamp", jobs[0])
	}
}

// TestCancelPending checks that cancelling a queued job marks it cancelled and
// that run then skips it without spawning a process.
func TestCancelPending(t *testing.T) {
	isolate(t)
	mgr := newTestManager(t)
	j := job{id: "dl_1_x.mkv", seq: 1, src: "r:x.mkv", dest: "/tmp/x.mkv", name: "x.mkv"}
	mgr.record(dlengine.Progress{ID: j.id, Seq: j.seq, Name: j.name, Status: "pending", Src: j.src, Dest: j.dest})

	if err := mgr.Cancel(j.id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if got := mgr.List()[0].Status; got != "cancelled" {
		t.Fatalf("status = %q, want cancelled", got)
	}
	// A cancelled queued job must not launch rclone: the missing binary would
	// flip the status to failed if run proceeded.
	mgr.run(j)
	if got := mgr.List()[0].Status; got != "cancelled" {
		t.Errorf("status after run = %q, want cancelled (job should be skipped)", got)
	}

	if err := mgr.Cancel("nope"); err == nil {
		t.Errorf("Cancel(unknown) = nil, want error")
	}
}

// TestClearFinished checks that clearing removes terminal entries but keeps
// active ones.
func TestClearFinished(t *testing.T) {
	isolate(t)
	mgr := newTestManager(t)
	mgr.record(dlengine.Progress{ID: "done", Seq: 1, Status: "completed"})
	mgr.record(dlengine.Progress{ID: "dead", Seq: 2, Status: "failed"})
	mgr.record(dlengine.Progress{ID: "gone", Seq: 3, Status: "cancelled"})
	mgr.record(dlengine.Progress{ID: "live", Seq: 4, Status: "in_progress"})
	if err := mgr.ClearFinished(); err != nil {
		t.Fatalf("ClearFinished: %v", err)
	}
	list := mgr.List()
	if len(list) != 1 || list[0].ID != "live" {
		t.Fatalf("after clear = %+v, want just the live entry", list)
	}
}

// TestHistoryRestore checks that terminal entries round-trip through
// serve_downloads.json, interrupted jobs are requeued (or failed when they
// can't be), and the sequence counter resumes past restored entries.
func TestHistoryRestore(t *testing.T) {
	isolate(t)
	m1 := newTestManager(t)
	m1.record(dlengine.Progress{ID: "dl_1_a.mkv", Seq: 1, Name: "a.mkv", Status: "completed", Percent: 100})
	m1.record(dlengine.Progress{
		ID: "dl_2_b.mkv", Seq: 2, Name: "b.mkv", Status: "in_progress", Percent: 40,
		Src: "remote:media/b.mkv", Dest: filepath.Join(t.TempDir(), "b.mkv"),
	})
	// A legacy interrupted entry with no src/dest can't be restarted.
	m1.record(dlengine.Progress{ID: "dl_3_c.mkv", Seq: 3, Name: "c.mkv", Status: "in_progress", Percent: 10})

	m2 := newTestManager(t)
	if n := m2.Restore(); n != 1 {
		t.Fatalf("Restore = %d requeued, want 1", n)
	}
	list := m2.List()
	if len(list) != 3 {
		t.Fatalf("restored %d entries, want 3", len(list))
	}
	// Newest first.
	if list[0].ID != "dl_3_c.mkv" || list[2].ID != "dl_1_a.mkv" {
		t.Errorf("wrong order: %q ... %q", list[0].ID, list[2].ID)
	}
	if list[0].Status != "failed" || list[0].Error == "" {
		t.Errorf("legacy interrupted job = %q (%q), want failed with error", list[0].Status, list[0].Error)
	}
	if got := m2.seq.Load(); got != 3 {
		t.Errorf("seq = %d, want 3", got)
	}
}

// TestSubmitRejectsMissingSrc checks input validation on the wire boundary.
func TestSubmitRejectsMissingSrc(t *testing.T) {
	isolate(t)
	srv := httptest.NewServer(New("box", "dev", "tok", newTestManager(t)).Handler())
	defer srv.Close()

	for _, body := range []string{`[]`, `[{"name":"x.mkv"}]`, `not json`} {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/downloads", bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", "Bearer tok")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("submit(%s) status = %d, want 400", body, resp.StatusCode)
		}
	}
}
