package libserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeRclone writes a shell script that answers `lsjson` and `cat --offset
// --count` from a local file, standing in for rclone.
func fakeRclone(t *testing.T, backing string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake rclone")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "rclone")
	script := `#!/bin/sh
case "$1" in
  lsjson) printf '[{"Path":"x.mkv","Name":"x.mkv","Size":%s,"IsDir":false}]' "$(wc -c < "` + backing + `" | tr -d ' ')" ;;
  cat) off=$3; cnt=$5; tail -c +$((off+1)) "` + backing + `" | head -c "$cnt" ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestRcloneFallbackStreaming(t *testing.T) {
	dir := t.TempDir()
	backing := filepath.Join(dir, "backing.mkv")
	data := make([]byte, 5000)
	for i := range data {
		data[i] = byte(i % 251)
	}
	if err := os.WriteFile(backing, data, 0o644); err != nil {
		t.Fatal(err)
	}
	bin := fakeRclone(t, backing)

	res := &fakeResolver{items: map[string]FileInfo{
		"abc": {Path: filepath.Join(dir, "missing", "x.mkv"), Rclone: "remote:dir/x.mkv", Type: "movie"},
	}}
	srv := httptest.NewServer(New(res, Options{ImageCacheDir: dir, RcloneBin: bin}))
	defer srv.Close()

	// Full body.
	resp, err := http.Get(srv.URL + "/local/file/abc")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || len(body) != 5000 || string(body) != string(data) {
		t.Fatalf("full: status %d len %d", resp.StatusCode, len(body))
	}
	if ct := resp.Header.Get("Content-Type"); ct != "video/x-matroska" {
		t.Errorf("content-type %q", ct)
	}

	// Range.
	req, _ := http.NewRequest("GET", srv.URL+"/local/file/abc", nil)
	req.Header.Set("Range", "bytes=1000-1999")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 206 || len(body) != 1000 || string(body) != string(data[1000:2000]) {
		t.Fatalf("range: status %d len %d", resp.StatusCode, len(body))
	}
	if cr := resp.Header.Get("Content-Range"); cr != "bytes 1000-1999/5000" {
		t.Errorf("content-range %q", cr)
	}

	// Open-ended and suffix ranges, HEAD, unsatisfiable.
	req.Header.Set("Range", "bytes=4990-")
	resp, _ = http.DefaultClient.Do(req)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 206 || len(body) != 10 {
		t.Errorf("open-ended: %d len %d", resp.StatusCode, len(body))
	}
	req.Header.Set("Range", "bytes=-7")
	resp, _ = http.DefaultClient.Do(req)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 206 || string(body) != string(data[4993:]) {
		t.Errorf("suffix: %d len %d", resp.StatusCode, len(body))
	}
	req.Header.Set("Range", "bytes=9000-")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 416 {
		t.Errorf("unsatisfiable: %d", resp.StatusCode)
	}
	hreq, _ := http.NewRequest("HEAD", srv.URL+"/local/file/abc", nil)
	resp, _ = http.DefaultClient.Do(hreq)
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Length") != "5000" {
		t.Errorf("head: %d len %s", resp.StatusCode, resp.Header.Get("Content-Length"))
	}
}

func TestParseRange(t *testing.T) {
	cases := []struct {
		h           string
		start, end  int64
		partial, ok bool
	}{
		{"", 0, 99, false, true},
		{"bytes=0-9", 0, 9, true, true},
		{"bytes=50-", 50, 99, true, true},
		{"bytes=-10", 90, 99, true, true},
		{"bytes=0-500", 0, 99, true, true},
		{"bytes=100-", 0, 0, false, false},
		{"bytes=5-2", 0, 0, false, false},
		{"bytes=0-9,20-29", 0, 9, true, true},
	}
	for _, c := range cases {
		s, e, p, ok := parseRange(c.h, 100)
		if s != c.start || e != c.end || p != c.partial || ok != c.ok {
			t.Errorf("%q: got %d-%d partial=%v ok=%v", c.h, s, e, p, ok)
		}
	}
}
