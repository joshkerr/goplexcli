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
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
	"time"
)

// reachableTimeout bounds the pre-flight connectivity check.
const reachableTimeout = 10 * time.Second

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
// printing progress as it goes. baseURL is the target root (e.g.
// "http://192.168.0.34"), dir is the destination folder ("" = root), and
// rcloneBinary defaults to "rclone". It returns the first error encountered.
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

	for i, src := range rclonePaths {
		name := path.Base(src)
		total := remoteSize(ctx, rcloneBinary, src) // best-effort; 0 = unknown
		label := fmt.Sprintf("(%d/%d) %s", i+1, len(rclonePaths), name)
		if err := uploadOne(ctx, uploadURL, uploadPath, src, name, rcloneBinary, label, total); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

// uploadOne streams a single rclone remote file into a multipart POST, driving
// a progress line from the bytes read out of rclone.
func uploadOne(ctx context.Context, uploadURL, uploadPath, src, filename, rcloneBinary, label string, total int64) error {
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
		reportProgress(label, counter, total, stop)
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

// reportProgress renders an in-place progress line on stderr until stopped. When
// total is known it shows a percentage bar; otherwise it shows a spinner with
// the running byte count. The final state is rendered once more on stop.
func reportProgress(label string, c *countingWriter, total int64, stop <-chan struct{}) {
	start := time.Now()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	spinner := []rune{'|', '/', '-', '\\'}
	tick := 0

	render := func() {
		sent := c.count()
		elapsed := time.Since(start).Seconds()
		var rate int64
		if elapsed > 0 {
			rate = int64(float64(sent) / elapsed)
		}
		if total > 0 {
			pct := float64(sent) / float64(total)
			if pct > 1 {
				pct = 1
			}
			fmt.Fprintf(os.Stderr, "\r  %s %s %3.0f%%  %s / %s  %s/s   ",
				label, bar(pct, 24), pct*100, formatBytes(sent), formatBytes(total), formatBytes(rate))
		} else {
			fmt.Fprintf(os.Stderr, "\r  %s %c  %s  %s/s   ",
				label, spinner[tick%len(spinner)], formatBytes(sent), formatBytes(rate))
			tick++
		}
	}

	for {
		select {
		case <-stop:
			render()
			fmt.Fprintln(os.Stderr)
			return
		case <-ticker.C:
			render()
		}
	}
}

// bar renders a fixed-width [====    ] progress bar for the given fraction.
func bar(pct float64, width int) string {
	if pct < 0 {
		pct = 0
	}
	filled := int(pct * float64(width))
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("=", filled) + strings.Repeat(" ", width-filled) + "]"
}

// formatBytes renders a byte count in human-readable units.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
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
