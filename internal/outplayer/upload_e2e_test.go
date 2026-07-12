package outplayer

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestUploadEndToEnd exercises the full Upload pipeline (rclone cat -> counting
// -> multipart POST -> manager/UI) against a local server that mimics
// Outplayer's GCDWebUploader. It needs a real rclone binary and spawns the
// Bubble Tea UI, so it is opt-in via OUTPLAYER_E2E=1.
func TestUploadEndToEnd(t *testing.T) {
	if os.Getenv("OUTPLAYER_E2E") == "" {
		t.Skip("set OUTPLAYER_E2E=1 to run")
	}
	if _, err := exec.LookPath("rclone"); err != nil {
		t.Skip("rclone not installed")
	}

	// Under `go test` on Windows, Bubble Tea's console input read cannot be
	// canceled and would hang Program.Run's shutdown forever. Disable input.
	teaOptions = []tea.ProgramOption{tea.WithInput(nil)}
	t.Cleanup(func() { teaOptions = nil })

	var gotPath, gotName string
	var gotBytes int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/list":
			_, _ = w.Write([]byte("[]"))
		case r.Method == http.MethodPost && r.URL.Path == "/upload":
			mr, err := r.MultipartReader()
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			for {
				part, err := mr.NextPart()
				if err != nil {
					break
				}
				switch part.FormName() {
				case "path":
					b, _ := io.ReadAll(part)
					gotPath = string(b)
				case "files[]":
					gotName = part.FileName()
					n, _ := io.Copy(io.Discard, part)
					gotBytes = n
				}
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	src := filepath.Join(dir, "movie.mkv")
	data := bytes.Repeat([]byte("goplexcli"), 1<<20) // ~9 MB
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}

	err := Upload(context.Background(), []string{src}, srv.URL, "Inbox", "")
	// In a TTY-less test run the Bubble Tea program may fail to start; the
	// transfer itself must still have gone through.
	if err != nil && !strings.Contains(err.Error(), "UI error") {
		t.Fatalf("Upload returned error: %v", err)
	}
	if err != nil {
		t.Logf("UI unavailable in test environment (expected): %v", err)
	}

	if gotBytes != int64(len(data)) {
		t.Errorf("server received %d bytes, want %d", gotBytes, len(data))
	}
	if gotName != "movie.mkv" {
		t.Errorf("filename = %q, want %q", gotName, "movie.mkv")
	}
	if gotPath != "/Inbox/" {
		t.Errorf("path field = %q, want %q", gotPath, "/Inbox/")
	}
}
