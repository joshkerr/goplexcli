package dlserver

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/joshkerr/goplexcli/internal/dlengine"
)

// TestPostProcessRunsOnCompletion fakes rclone with a script that copies the
// source to the destination, and checks the completed record reaches the
// PostProcess hook with its metadata.
func TestPostProcessRunsOnCompletion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake rclone")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "rclone")
	// rclone copyto <flags…> src dest: only the last two arguments matter.
	if err := os.WriteFile(fake, []byte("#!/bin/bash\nsrc=\"${@: -2:1}\"; dest=\"${@: -1}\"; cp \"$src\" \"$dest\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "src.mkv")
	if err := os.WriteFile(src, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	destDir := filepath.Join(dir, "dl")
	done := make(chan dlengine.Progress, 1)
	t.Setenv("HOME", dir) // keep the manager's history file out of the real config dir
	t.Setenv("GOPLEXCLI_CONFIG_DIR", filepath.Join(dir, "cfg"))
	mgr := NewManager(Options{
		RcloneBin:   fake,
		DownloadDir: destDir,
		Sort:        true,
		PostProcess: func(p dlengine.Progress) { done <- p },
	})
	if _, err := mgr.Submit([]DownloadRequest{{Src: src, Type: "episode", Show: "Severance", Title: "Severance", Year: 2022}}); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-done:
		want := filepath.Join(destDir, "TV Shows", "Severance", "src.mkv")
		if p.Status != "completed" || p.Dest != want || p.Title != "Severance" || p.Year != 2022 || p.MediaType != "episode" {
			t.Errorf("post-process record = %+v", p)
		}
		if b, err := os.ReadFile(want); err != nil || string(b) != "video" {
			t.Errorf("destination not written: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("PostProcess never ran")
	}
	mgr.Shutdown()
}
