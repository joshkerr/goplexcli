package main

import (
	"runtime"
	"testing"
)

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

// isolateHistory points the config/cache dir at a temp dir so history tests
// don't touch the real ~/.config/goplexcli.
func isolateHistory(t *testing.T) {
	t.Helper()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("APPDATA", t.TempDir())
	default:
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", "")
	}
}

// TestDownloadHistoryPersistence checks that terminal downloads round-trip
// through downloads.json, that interrupted jobs are requeued (or failed when
// they can't be), and that the sequence counter resumes past restored entries.
func TestDownloadHistoryPersistence(t *testing.T) {
	isolateHistory(t)

	a := NewApp()
	a.recordDownload(DownloadProgress{ID: "dl_1_a.mkv", Seq: 1, Name: "a.mkv", Status: "completed", Percent: 100})
	a.recordDownload(DownloadProgress{ID: "dl_2_b.mkv", Seq: 2, Name: "b.mkv", Status: "failed", Error: "boom"})
	// A job mid-transfer when the app "quits". It has src/dest (recorded with
	// the initial pending event and carried over by recordDownload), so the
	// next launch requeues it.
	a.recordDownload(DownloadProgress{
		ID: "dl_3_c.mkv", Seq: 3, Name: "c.mkv", Status: "pending",
		Src: "remote:media/c.mkv", Dest: "/tmp/dl/c.mkv",
	})
	a.recordDownload(DownloadProgress{ID: "dl_3_c.mkv", Seq: 3, Name: "c.mkv", Status: "in_progress", Percent: 40})
	// A legacy interrupted entry with no src/dest can't be restarted.
	a.recordDownload(DownloadProgress{ID: "dl_4_d.mkv", Seq: 4, Name: "d.mkv", Status: "in_progress", Percent: 10})

	b := NewApp()
	requeue := b.loadDownloadHistory()
	list := b.ListDownloads()
	if len(list) != 4 {
		t.Fatalf("restored %d entries, want 4", len(list))
	}
	// Newest first.
	if list[0].ID != "dl_4_d.mkv" || list[3].ID != "dl_1_a.mkv" {
		t.Errorf("wrong order: %q ... %q", list[0].ID, list[3].ID)
	}
	// The restartable job is queued again, progress reset, src/dest intact.
	if len(requeue) != 1 || requeue[0].id != "dl_3_c.mkv" || requeue[0].src != "remote:media/c.mkv" {
		t.Fatalf("requeue = %+v, want the dl_3 job with its src", requeue)
	}
	if list[1].Status != "pending" || list[1].Percent != 0 {
		t.Errorf("restartable job = %q at %v%%, want pending at 0%%", list[1].Status, list[1].Percent)
	}
	// The legacy entry without src/dest is failed, not stuck in_progress.
	if list[0].Status != "failed" || list[0].Error == "" {
		t.Errorf("legacy interrupted job = %q (%q), want failed with error", list[0].Status, list[0].Error)
	}
	if got := b.dlSeq.Load(); got != 4 {
		t.Errorf("dlSeq = %d, want 4", got)
	}
}

// TestQuitPreservesInProgress checks that a shutdown-triggered cancel does not
// overwrite the persisted in_progress state (which is what allows the restart
// on relaunch), while a user cancel does.
func TestQuitPreservesInProgress(t *testing.T) {
	isolateHistory(t)

	a := NewApp()
	j := downloadJob{id: "dl_1_x.mkv", seq: 1, name: "x.mkv", src: "r:x.mkv", dest: "/tmp/x.mkv"}
	a.recordDownload(DownloadProgress{ID: j.id, Seq: j.seq, Name: j.name, Status: "in_progress", Src: j.src, Dest: j.dest})

	a.quitting.Store(true)
	// runRclone must refuse to start new work during shutdown.
	if err := a.runRclone("false", j); err != nil {
		t.Fatalf("runRclone during shutdown: %v", err)
	}
	if got := a.ListDownloads()[0].Status; got != "in_progress" {
		t.Errorf("status after shutdown-skip = %q, want in_progress preserved", got)
	}
}

// TestClearDownloadHistory checks that clearing removes finished entries but
// keeps active ones.
func TestClearDownloadHistory(t *testing.T) {
	isolateHistory(t)

	a := NewApp()
	a.recordDownload(DownloadProgress{ID: "done", Seq: 1, Status: "completed"})
	a.recordDownload(DownloadProgress{ID: "dead", Seq: 2, Status: "failed"})
	a.recordDownload(DownloadProgress{ID: "gone", Seq: 3, Status: "cancelled"})
	a.recordDownload(DownloadProgress{ID: "live", Seq: 4, Status: "in_progress"})
	if err := a.ClearDownloadHistory(); err != nil {
		t.Fatalf("ClearDownloadHistory: %v", err)
	}
	list := a.ListDownloads()
	if len(list) != 1 || list[0].ID != "live" {
		t.Fatalf("after clear = %+v, want just the live entry", list)
	}
}

// TestCancelPendingDownload checks that cancelling a queued job marks it
// cancelled and that runRclone then skips it.
func TestCancelPendingDownload(t *testing.T) {
	isolateHistory(t)

	a := NewApp()
	j := downloadJob{id: "dl_1_x.mkv", seq: 1, name: "x.mkv"}
	a.recordDownload(DownloadProgress{ID: j.id, Seq: j.seq, Name: j.name, Status: "pending"})
	if err := a.CancelDownload(j.id); err != nil {
		t.Fatalf("CancelDownload: %v", err)
	}
	if got := a.ListDownloads()[0].Status; got != "cancelled" {
		t.Fatalf("status = %q, want cancelled", got)
	}
	// A cancelled queued job must not launch rclone: "false" would exit
	// nonzero and flip the status to failed if it ran.
	if err := a.runRclone("false", j); err != nil {
		t.Fatalf("runRclone on cancelled job: %v", err)
	}
	if got := a.ListDownloads()[0].Status; got != "cancelled" {
		t.Errorf("status after runRclone = %q, want cancelled (job should be skipped)", got)
	}

	if err := a.CancelDownload("nope"); err == nil {
		t.Errorf("CancelDownload(unknown) = nil, want error")
	}
}
