package libserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

// rcloneStreamer serves a file straight from an rclone remote when it isn't
// reachable on the local filesystem (no FUSE mount, or the mount is down).
// Each request runs `rclone cat --offset --count`, so seeks cost a process
// spawn plus the remote's first-byte latency; a mount with a VFS cache is
// still the better path when it is available.
type rcloneStreamer struct {
	bin   string
	sizes sync.Map // rclone path -> int64
}

func newRcloneStreamer(bin string) *rcloneStreamer {
	if bin == "" {
		bin = "rclone"
	}
	return &rcloneStreamer{bin: bin}
}

// available reports whether the rclone binary can be found.
func (rs *rcloneStreamer) available() bool {
	_, err := exec.LookPath(rs.bin)
	return err == nil
}

// size returns the object's size, asking rclone once per path.
func (rs *rcloneStreamer) size(ctx context.Context, remote string) (int64, error) {
	if v, ok := rs.sizes.Load(remote); ok {
		return v.(int64), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, rs.bin, "lsjson", "--files-only", "--no-mimetype", "--no-modtime", remote).Output()
	if err != nil {
		return 0, fmt.Errorf("rclone lsjson: %w", err)
	}
	var entries []struct {
		Size int64 `json:"Size"`
	}
	if err := json.Unmarshal(out, &entries); err != nil || len(entries) != 1 {
		return 0, fmt.Errorf("rclone lsjson: unexpected output for %s", remote)
	}
	rs.sizes.Store(remote, entries[0].Size)
	return entries[0].Size, nil
}

// serve answers a GET/HEAD for the remote object with Range support.
func (rs *rcloneStreamer) serve(w http.ResponseWriter, r *http.Request, remote string, size int64) {
	if size <= 0 {
		var err error
		size, err = rs.size(r.Context(), remote)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
	}
	base := path.Base(strings.ReplaceAll(remote, "\\", "/"))
	w.Header().Set("Content-Type", videoContentType(base))
	w.Header().Set("Content-Disposition", contentDisposition("inline", base))
	w.Header().Set("Accept-Ranges", "bytes")

	start, end, partial, ok := parseRange(r.Header.Get("Range"), size)
	if !ok {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		writeJSON(w, http.StatusRequestedRangeNotSatisfiable, map[string]string{"error": "range not satisfiable"})
		return
	}
	length := end - start + 1
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	status := http.StatusOK
	if partial {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
		status = http.StatusPartialContent
	}
	if r.Method == http.MethodHead || length == 0 {
		w.WriteHeader(status)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	cmd := exec.CommandContext(ctx, rs.bin, "cat", "--offset", strconv.FormatInt(start, 10), "--count", strconv.FormatInt(length, 10), remote)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if err := cmd.Start(); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "rclone: " + err.Error()})
		return
	}
	w.WriteHeader(status)
	// Copy in large chunks and flush as we go so the player starts quickly.
	buf := bufio.NewReaderSize(stdout, 1<<20)
	_, _ = io.CopyN(flushWriter{w}, buf, length)
	cancel()
	_ = cmd.Wait()
}

// parseRange handles the single-range forms players use: "bytes=a-b",
// "bytes=a-" and "bytes=-n". It returns the inclusive byte range to send,
// whether the response is partial, and false when the range is unsatisfiable.
func parseRange(header string, size int64) (start, end int64, partial, ok bool) {
	if header == "" || !strings.HasPrefix(header, "bytes=") {
		return 0, size - 1, false, true
	}
	spec := strings.TrimPrefix(header, "bytes=")
	if i := strings.IndexByte(spec, ','); i >= 0 {
		spec = spec[:i] // only the first range is honored
	}
	a, b, found := strings.Cut(strings.TrimSpace(spec), "-")
	if !found {
		return 0, size - 1, false, true
	}
	switch {
	case a == "" && b != "":
		n, err := strconv.ParseInt(b, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false, false
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, true, true
	case a != "":
		s, err := strconv.ParseInt(a, 10, 64)
		if err != nil || s < 0 || s >= size {
			return 0, 0, false, false
		}
		e := size - 1
		if b != "" {
			if v, err := strconv.ParseInt(b, 10, 64); err == nil && v < e {
				e = v
			}
		}
		if e < s {
			return 0, 0, false, false
		}
		return s, e, true, true
	}
	return 0, size - 1, false, true
}

// flushWriter flushes after every write so streamed bytes reach the client
// without waiting for the response buffer to fill.
type flushWriter struct{ w http.ResponseWriter }

func (f flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if fl, ok := f.w.(http.Flusher); ok {
		fl.Flush()
	}
	return n, err
}
