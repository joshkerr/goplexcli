// Package outplayer uploads media files to Outplayer's "Wi-Fi transfer" feature.
//
// Outplayer (an iOS media player) exposes a GCDWebUploader HTTP server when
// Wi-Fi transfer is enabled. It accepts multipart form uploads at POST /upload
// (form fields: "path" for the destination directory and "files[]" for the
// file) and lists directories at GET /list?path=/.
//
// Unlike the gowebdav transfer path, the source media lives on an rclone remote
// rather than local disk, so uploads are streamed: `rclone cat <remote>` is
// piped straight into a chunked multipart POST without staging the file on
// local disk. GCDWebUploader accepts a chunked (unknown Content-Length) body,
// so the exact file size is never required.
package outplayer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"os/exec"
	"path"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	rclone "github.com/joshkerr/rclone-golib"
)

// reachableTimeout bounds the pre-flight connectivity check.
const reachableTimeout = 10 * time.Second

// teaOptions lets tests override Bubble Tea program options (e.g. disable TTY
// input in headless runs, where the blocking console read cannot be canceled).
var teaOptions []tea.ProgramOption

// Reachable verifies that an Outplayer target is responding by requesting a
// directory listing. It returns a descriptive error when the target cannot be
// reached, so callers can fail fast before starting a large upload.
func Reachable(ctx context.Context, baseURL string) error {
	if baseURL == "" {
		return fmt.Errorf("target URL is empty")
	}
	listURL := strings.TrimRight(baseURL, "/") + "/list?path=/"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: reachableTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected response: HTTP %d", resp.StatusCode)
	}
	return nil
}

// NormalizeDir converts a user-supplied folder into the "path" form Outplayer
// expects: a leading and trailing slash, with the root represented as "/".
func NormalizeDir(dir string) string {
	dir = strings.Trim(strings.TrimSpace(dir), "/")
	if dir == "" {
		return "/"
	}
	return "/" + dir + "/"
}

// Upload streams each rclone remote file to the Outplayer target sequentially,
// displaying the same Bubble Tea progress UI used by downloads and WebDAV
// uploads. baseURL is the target root (e.g. "http://192.168.0.34"), dir is the
// destination folder ("" = root), and rcloneBinary defaults to "rclone". A
// failed file does not stop the remaining uploads; the first error encountered
// is returned.
func Upload(ctx context.Context, rclonePaths []string, baseURL, dir, rcloneBinary string) error {
	if len(rclonePaths) == 0 {
		return fmt.Errorf("no files to upload")
	}
	if baseURL == "" {
		return fmt.Errorf("target URL is empty")
	}
	if rcloneBinary == "" {
		rcloneBinary = "rclone"
	}
	if _, err := exec.LookPath(rcloneBinary); err != nil {
		return fmt.Errorf("rclone not found in PATH. Please install rclone or set its path in config")
	}

	uploadURL := strings.TrimRight(baseURL, "/") + "/upload"
	uploadPath := NormalizeDir(dir)

	// The transfer manager is fed manually from the byte counter (the rclone
	// executor is not used here since the source is piped through an HTTP POST).
	manager := rclone.NewManager()
	type job struct {
		id   string
		src  string
		name string
	}
	jobs := make([]job, 0, len(rclonePaths))
	for i, src := range rclonePaths {
		name := path.Base(src)
		id := fmt.Sprintf("outplayer_%d_%s", i, name)
		jobs = append(jobs, job{id: id, src: src, name: name})
		manager.Add(id, src, uploadPath+name)
	}

	// Start the Bubble Tea progress UI in a goroutine (same pattern as
	// download.DownloadMultiple).
	var wg sync.WaitGroup
	var uiErr error
	uiReady := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		p := tea.NewProgram(rclone.NewModel(manager), teaOptions...)
		close(uiReady)
		if _, err := p.Run(); err != nil {
			uiErr = err
		}
	}()
	<-uiReady

	// The UI only exits once no transfer is pending or in progress, so every
	// job must reach Complete or Fail even after an earlier error.
	var firstErr error
	for _, j := range jobs {
		total := remoteSize(ctx, rcloneBinary, j.src) // best-effort; 0 = unknown
		manager.Start(j.id)
		if err := uploadOne(ctx, uploadURL, uploadPath, j.src, j.name, rcloneBinary, manager, j.id, total); err != nil {
			manager.Fail(j.id, err)
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", j.name, err)
			}
		} else {
			manager.Complete(j.id)
		}
	}

	wg.Wait()

	if uiErr != nil {
		return fmt.Errorf("UI error: %w", uiErr)
	}
	return firstErr
}

// uploadOne streams a single rclone remote file into a multipart POST, feeding
// the transfer manager (and thus the Bubble Tea UI) from the bytes read out of
// rclone.
func uploadOne(ctx context.Context, uploadURL, uploadPath, src, filename, rcloneBinary string, manager *rclone.Manager, transferID string, total int64) error {
	cmd := exec.CommandContext(ctx, rcloneBinary, "cat", src)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start rclone: %w", err)
	}

	counter := &countingWriter{}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		reportProgress(manager, transferID, counter, total, stop)
	}()

	status, respBody, postErr := postFile(ctx, uploadURL, uploadPath, filename, io.TeeReader(stdout, counter))
	// Always reap rclone so we surface a source-side failure (e.g. a bad remote
	// path) even when the POST itself "succeeded" with a truncated body.
	waitErr := cmd.Wait()

	close(stop)
	wg.Wait()

	if postErr != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%w (rclone: %s)", postErr, msg)
		}
		return postErr
	}
	if waitErr != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("reading source failed: %s", msg)
		}
		return fmt.Errorf("reading source failed: %w", waitErr)
	}
	if status != http.StatusOK {
		return fmt.Errorf("upload rejected (HTTP %d): %s", status, extractError(respBody))
	}
	return nil
}

// postFile builds a multipart body (the "path" field plus a "files[]" file part
// fed from src) and POSTs it to uploadURL. The body length is unknown, so
// net/http sends it with chunked transfer encoding. It returns the response
// status code and a truncated copy of the response body.
func postFile(ctx context.Context, uploadURL, uploadPath, filename string, src io.Reader) (int, string, error) {
	pr, pw := io.Pipe()
	mpw := multipart.NewWriter(pw)
	contentType := mpw.FormDataContentType()

	go func() {
		if err := mpw.WriteField("path", uploadPath); err != nil {
			pw.CloseWithError(err)
			return
		}
		part, err := mpw.CreateFormFile("files[]", filename)
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, src); err != nil {
			pw.CloseWithError(err)
			return
		}
		if err := mpw.Close(); err != nil {
			pw.CloseWithError(err)
			return
		}
		pw.Close()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, pr)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	return resp.StatusCode, string(body), nil
}

// remoteSize best-effort resolves the byte size of an rclone remote file via
// `rclone lsjson`, used only to render a percentage. It returns 0 when the size
// cannot be determined, in which case progress falls back to a byte counter.
func remoteSize(ctx context.Context, rcloneBinary, src string) int64 {
	out, err := exec.CommandContext(ctx, rcloneBinary, "lsjson", src).Output()
	if err != nil {
		return 0
	}
	var items []struct {
		Size  int64 `json:"Size"`
		IsDir bool  `json:"IsDir"`
	}
	if err := json.Unmarshal(out, &items); err != nil {
		return 0
	}
	var total int64
	for _, it := range items {
		if it.IsDir || it.Size < 0 {
			continue
		}
		total += it.Size
	}
	return total
}

// countingWriter counts the bytes written through it, safely for concurrent
// reads from the progress goroutine.
type countingWriter struct {
	mu sync.Mutex
	n  int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.n += int64(len(p))
	w.mu.Unlock()
	return len(p), nil
}

func (w *countingWriter) count() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.n
}

// reportProgress feeds the transfer manager from the byte counter until
// stopped, pushing a final update on stop. When total is unknown (0) the
// percentage stays at 0 and the UI shows its "initializing" state, but the
// byte count is still recorded.
func reportProgress(manager *rclone.Manager, transferID string, c *countingWriter, total int64, stop <-chan struct{}) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	update := func() {
		sent := c.count()
		var pct float64
		if total > 0 {
			pct = float64(sent) / float64(total) * 100
			if pct > 100 {
				pct = 100
			}
		}
		manager.UpdateProgress(transferID, pct, sent, total)
	}

	for {
		select {
		case <-stop:
			update()
			return
		case <-ticker.C:
			update()
		}
	}
}

// extractError pulls a human-readable message out of a GCDWebUploader HTML error
// page, falling back to the raw (trimmed) body when no <h1> is present.
func extractError(body string) string {
	if i := strings.Index(body, "<h1>"); i >= 0 {
		rest := body[i+len("<h1>"):]
		if j := strings.Index(rest, "</h1>"); j >= 0 {
			return html.UnescapeString(strings.TrimSpace(rest[:j]))
		}
	}
	msg := strings.TrimSpace(body)
	if msg == "" {
		return "no response body"
	}
	return msg
}
