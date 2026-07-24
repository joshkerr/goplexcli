package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshkerr/goplexcli/internal/config"
)

func TestFindRclonecpConfiguredPath(t *testing.T) {
	useTempConfigDir(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "rclonecp.exe")
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.cfg = &config.Config{RclonecpPath: bin}
	got, err := app.findRclonecp()
	if err != nil || got != bin {
		t.Fatalf("findRclonecp = %q, %v; want %q", got, err, bin)
	}

	// A bare name (what the Settings placeholder suggests) must resolve
	// against PATH, exactly like the mpv/rclone overrides do.
	t.Setenv("PATH", dir)
	app.cfg = &config.Config{RclonecpPath: "rclonecp"}
	got, err = app.findRclonecp()
	if err != nil || got == "" {
		t.Fatalf("bare-name findRclonecp = %q, %v; want %q", got, err, bin)
	}

	// A configured-but-missing path is an explicit error, not a PATH fallback:
	// the user pointed at a location, so a stale setting should be surfaced.
	app.cfg = &config.Config{RclonecpPath: filepath.Join(dir, "gone.exe")}
	if _, err := app.findRclonecp(); err == nil {
		t.Fatal("expected error for missing configured path")
	}
}

func TestSendToRclonecpGuards(t *testing.T) {
	useTempConfigDir(t)
	app := NewApp()
	app.cfg = &config.Config{}

	if err := app.SendToRclonecp("nope"); err == nil || !strings.Contains(err.Error(), "unknown download") {
		t.Errorf("unknown id error = %v", err)
	}

	app.dlHist["dl_1"] = &DownloadProgress{ID: "dl_1", Name: "a.mkv", Status: "in_progress"}
	if err := app.SendToRclonecp("dl_1"); err == nil || !strings.Contains(err.Error(), "hasn't finished") {
		t.Errorf("in-progress error = %v", err)
	}

	app.dlHist["dl_2"] = &DownloadProgress{
		ID: "dl_2", Name: "b.mkv", Status: "completed",
		Dest: filepath.Join(t.TempDir(), "b.mkv"),
	}
	if err := app.SendToRclonecp("dl_2"); err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Errorf("missing file error = %v", err)
	}
}

func TestRecordDownloadCarriesTitleAndYear(t *testing.T) {
	useTempConfigDir(t)
	app := NewApp()

	app.recordDownload(DownloadProgress{
		ID: "dl_9", Seq: 9, Name: "das.boot.mkv", Status: "pending",
		Src: "remote:das.boot.mkv", Dest: `C:\dl\das.boot.mkv`,
		Title: "Das Boot", Year: 1981,
	})
	// Progress and completion records omit Title/Year (like Src/Dest); the
	// carry-over must preserve them so the rclonecp handoff still has the
	// metadata at completion time.
	app.recordDownload(DownloadProgress{ID: "dl_9", Seq: 9, Name: "das.boot.mkv", Status: "completed"})

	app.dlStateMu.Lock()
	e := app.dlHist["dl_9"]
	app.dlStateMu.Unlock()
	if e == nil || e.Title != "Das Boot" || e.Year != 1981 || e.Dest == "" {
		t.Fatalf("completed entry lost metadata: %+v", e)
	}
}
