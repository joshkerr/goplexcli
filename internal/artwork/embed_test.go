package artwork

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSupportedContainer(t *testing.T) {
	cases := map[string]bool{
		"movie.mkv":            true,
		"movie.MKV":            true,
		"movie.mp4":            true,
		"movie.m4v":            true,
		"/a/b/Show.S01E01.Mp4": true,
		"movie.avi":            false,
		"movie.mov":            false,
		"movie.webm":           false,
		"movie":                false,
		"":                     false,
		"movie.mkv.part":       false,
	}
	for path, want := range cases {
		if got := SupportedContainer(path); got != want {
			t.Errorf("SupportedContainer(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestEmbedTempName(t *testing.T) {
	cases := map[string]string{
		"/lib/Movie (2020).mkv": "/lib/Movie (2020).mkv.tmp.mkv",
		"/lib/clip.MP4":         "/lib/clip.MP4.tmp.mp4",
		"ep.m4v":                "ep.m4v.tmp.m4v",
	}
	for in, want := range cases {
		got := embedTempName(in)
		if got != want {
			t.Errorf("embedTempName(%q) = %q, want %q", in, got, want)
		}
		if filepath.Dir(got) != filepath.Dir(in) {
			t.Errorf("embedTempName(%q) left the file's directory: %q", in, got)
		}
	}
}

func TestEmbedUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v.mkv")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	same, _ := os.Stat(path)
	if !embedUnchanged(before, same) {
		t.Error("identical snapshots reported as changed")
	}

	// Same size, different mtime (a writer rewrote the same bytes).
	later := before.ModTime().Add(2 * time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	touched, _ := os.Stat(path)
	if embedUnchanged(before, touched) {
		t.Error("mtime change not detected")
	}

	// Size grew (a download still in progress).
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	grown, _ := os.Stat(path)
	if embedUnchanged(before, grown) {
		t.Error("size change not detected")
	}

	if embedUnchanged(nil, before) || embedUnchanged(before, nil) {
		t.Error("nil snapshot must count as changed")
	}
}

func TestCoverNeedsCompress(t *testing.T) {
	cases := map[int64]bool{
		0:                 false,
		1:                 false,
		coverMaxSize - 1:  false,
		coverMaxSize:      false,
		coverMaxSize + 1:  true,
		10 * coverMaxSize: true,
	}
	for size, want := range cases {
		if got := coverNeedsCompress(size); got != want {
			t.Errorf("coverNeedsCompress(%d) = %v, want %v", size, got, want)
		}
	}
	if coverMaxSize != 100*1024 {
		t.Errorf("coverMaxSize = %d, want 100 KB", coverMaxSize)
	}
}

func TestImgLogoPlacement(t *testing.T) {
	// Wide logo on 16:9 art: width-bound, bottom edge at 88%.
	w, x, y := imgLogoPlacement(1920, 1080, 400, 100)
	if w != 614 { // 1920 * 0.32 = 614.4
		t.Errorf("wide logo width = %d, want 614", w)
	}
	if x != 96 { // 1920 * 0.05
		t.Errorf("x = %d, want 96", x)
	}
	// h = 614.4 * 100/400 = 153.6; y = 1080*0.88 - 153.6 = 796.8
	if y != 796 {
		t.Errorf("y = %d, want 796", y)
	}

	// Tall logo: capped at 30% of the height, width shrinks to keep the ratio.
	w, x, y = imgLogoPlacement(1000, 1000, 100, 400)
	// width 320 -> height 1280 > 300, so height = 300, width = 320*300/1280 = 75
	if w != 75 {
		t.Errorf("tall logo width = %d, want 75", w)
	}
	if x != 50 {
		t.Errorf("x = %d, want 50", x)
	}
	if y != 580 { // 880 - 300
		t.Errorf("y = %d, want 580", y)
	}
}

func TestToolsMissing(t *testing.T) {
	missing := Tools{FFmpeg: "/nonexistent/ffmpeg", FFprobe: "/nonexistent/ffprobe"}
	if missing.Available() {
		t.Error("Available() true with bogus tool paths")
	}
	got := missing.Missing()
	if strings.Join(got, ",") != "ffmpeg,ffprobe" {
		t.Errorf("Missing() = %v, want [ffmpeg ffprobe]", got)
	}

	// Empty fields fall back to PATH lookup by bare name.
	if (Tools{}).ffmpeg() != "ffmpeg" || (Tools{}).mkvpropedit() != "mkvpropedit" {
		t.Error("empty Tools fields should resolve to bare program names")
	}
	if (Tools{FFmpeg: "/opt/ffmpeg"}).ffmpeg() != "/opt/ffmpeg" {
		t.Error("configured tool path not honoured")
	}
}

func TestEmbedUnsupported(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "clip.avi")
	cover := filepath.Join(dir, "cover.jpg")
	for _, p := range []string{video, cover} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tool, err := Embed(context.Background(), Tools{}, video, cover)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Embed(.avi) err = %v, want ErrUnsupported", err)
	}
	if tool != "" {
		t.Errorf("tool = %q, want empty", tool)
	}
	if HasCoverArt(context.Background(), Tools{}, video) {
		t.Error("HasCoverArt true for an unsupported container")
	}
}

// swapHTTPClient points DownloadImage at a test server for the test's lifetime.
func swapHTTPClient(t *testing.T, c *http.Client) {
	t.Helper()
	old := embedHTTPClient
	embedHTTPClient = c
	t.Cleanup(func() { embedHTTPClient = old })
}

func TestDownloadImage(t *testing.T) {
	payload := []byte("\xff\xd8\xff\xe0 not really a jpeg but close enough")
	var gotUA string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok.jpg":
			gotUA = r.Header.Get("User-Agent")
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write(payload)
		case "/missing.jpg":
			http.NotFound(w, r)
		case "/empty.jpg":
			w.WriteHeader(http.StatusOK)
		case "/huge-declared.jpg":
			// Declares an oversized body up front; must be rejected before
			// anything is read.
			w.Header().Set("Content-Length", "99999999999")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("tiny"))
		case "/huge-streamed.jpg":
			// No Content-Length: the cap must be enforced while streaming.
			chunk := make([]byte, 1<<20)
			for sent := 0; sent <= embedMaxDownload; sent += len(chunk) {
				if _, err := w.Write(chunk); err != nil {
					return
				}
			}
		default:
			http.Error(w, "unexpected path", http.StatusTeapot)
		}
	}))
	defer srv.Close()
	swapHTTPClient(t, srv.Client())

	dir := t.TempDir()
	ctx := context.Background()

	t.Run("ok", func(t *testing.T) {
		dest := filepath.Join(dir, "sub", "ok.jpg")
		if err := DownloadImage(ctx, srv.URL+"/ok.jpg", dest); err != nil {
			t.Fatalf("DownloadImage: %v", err)
		}
		got, err := os.ReadFile(dest)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(payload) {
			t.Errorf("downloaded bytes differ: %q", got)
		}
		if !strings.HasPrefix(gotUA, "Mozilla/5.0") {
			t.Errorf("User-Agent = %q, want a browser UA", gotUA)
		}
		entries, _ := os.ReadDir(filepath.Dir(dest))
		if len(entries) != 1 {
			t.Errorf("temp file left behind: %v", entries)
		}
	})

	t.Run("rejects plain http", func(t *testing.T) {
		dest := filepath.Join(dir, "http.jpg")
		httpURL := "http://" + strings.TrimPrefix(srv.URL, "https://") + "/ok.jpg"
		if err := DownloadImage(ctx, httpURL, dest); err == nil {
			t.Fatal("plain http URL accepted")
		}
		if _, err := os.Stat(dest); err == nil {
			t.Error("dest created for rejected URL")
		}
	})

	t.Run("rejects bad url", func(t *testing.T) {
		for _, u := range []string{"", "not a url", "ftp://x/y.jpg", "https:///nohost.jpg", "file:///etc/passwd"} {
			if err := DownloadImage(ctx, u, filepath.Join(dir, "bad.jpg")); err == nil {
				t.Errorf("URL %q accepted", u)
			}
		}
	})

	t.Run("http error status", func(t *testing.T) {
		dest := filepath.Join(dir, "missing.jpg")
		err := DownloadImage(ctx, srv.URL+"/missing.jpg", dest)
		if err == nil || !strings.Contains(err.Error(), "404") {
			t.Fatalf("err = %v, want HTTP 404", err)
		}
		if _, err := os.Stat(dest); err == nil {
			t.Error("dest created for 404")
		}
	})

	t.Run("empty body", func(t *testing.T) {
		dest := filepath.Join(dir, "empty.jpg")
		if err := DownloadImage(ctx, srv.URL+"/empty.jpg", dest); err == nil {
			t.Fatal("empty response accepted")
		}
		if _, err := os.Stat(dest); err == nil {
			t.Error("dest created for empty body")
		}
	})

	t.Run("cap declared", func(t *testing.T) {
		dest := filepath.Join(dir, "huge1.jpg")
		err := DownloadImage(ctx, srv.URL+"/huge-declared.jpg", dest)
		if err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("err = %v, want size cap error", err)
		}
		if _, err := os.Stat(dest); err == nil {
			t.Error("dest created for oversized declared body")
		}
	})

	t.Run("cap streamed", func(t *testing.T) {
		dest := filepath.Join(dir, "huge2.jpg")
		err := DownloadImage(ctx, srv.URL+"/huge-streamed.jpg", dest)
		if err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("err = %v, want size cap error", err)
		}
		if _, err := os.Stat(dest); err == nil {
			t.Error("dest created for oversized streamed body")
		}
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if strings.Contains(e.Name(), ".part") {
				t.Errorf("temp file left behind: %s", e.Name())
			}
		}
	})

	t.Run("context cancelled", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		if err := DownloadImage(cctx, srv.URL+"/ok.jpg", filepath.Join(dir, "cancelled.jpg")); err == nil {
			t.Fatal("cancelled context accepted")
		}
	})
}

// ---- integration: real ffmpeg / ffprobe / mkvpropedit ---------------------

func requireTools(t *testing.T) Tools {
	t.Helper()
	for _, name := range []string{"ffmpeg", "ffprobe", "mkvpropedit"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("%s not on PATH; skipping integration test", name)
		}
	}
	return Tools{}
}

// pickVideoCodec returns an encoder the local ffmpeg can use for test clips.
func pickVideoCodec(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
	if err != nil {
		t.Skipf("ffmpeg -encoders failed: %v", err)
	}
	for _, c := range []string{"libx264", "mpeg4"} {
		if strings.Contains(string(out), " "+c+" ") {
			return c
		}
	}
	t.Skip("neither libx264 nor mpeg4 encoder available")
	return ""
}

func ffmpegMust(t *testing.T, args ...string) {
	t.Helper()
	full := append([]string{"-y", "-v", "error", "-nostdin"}, args...)
	out, err := exec.Command("ffmpeg", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg %v: %v\n%s", args, err, out)
	}
}

func makeClip(t *testing.T, path, codec string) {
	t.Helper()
	ffmpegMust(t, "-f", "lavfi", "-i", "testsrc=duration=1:size=64x64:rate=5",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-c:v", codec, "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", path)
}

// ffprobeStreams returns ffprobe's -show_streams output for the file,
// failing the test when the file does not parse at all.
func ffprobeStreams(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-show_streams", path).CombinedOutput()
	if err != nil {
		t.Fatalf("ffprobe %s: %v\n%s", path, err, out)
	}
	return string(out)
}

func TestEmbedIntegration(t *testing.T) {
	tools := requireTools(t)
	codec := pickVideoCodec(t)
	dir := t.TempDir()
	ctx := context.Background()

	cover := filepath.Join(dir, "cover.jpg")
	ffmpegMust(t, "-f", "lavfi", "-i", "color=c=red:size=64x96", "-frames:v", "1", cover)
	if st, err := os.Stat(cover); err != nil || st.Size() == 0 {
		t.Fatalf("cover not generated: %v", err)
	}

	for _, ext := range []string{".mkv", ".mp4", ".m4v"} {
		t.Run(ext, func(t *testing.T) {
			video := filepath.Join(dir, "clip"+ext)
			makeClip(t, video, codec)

			if HasCoverArt(ctx, tools, video) {
				t.Fatal("fresh clip reports cover art")
			}

			tool, err := Embed(ctx, tools, video, cover)
			if err != nil {
				t.Fatalf("Embed: %v", err)
			}
			wantTool := "ffmpeg"
			if ext == ".mkv" {
				wantTool = "mkvpropedit"
			}
			if tool != wantTool {
				t.Errorf("tool = %q, want %q", tool, wantTool)
			}
			if _, err := os.Stat(embedTempName(video)); err == nil {
				t.Error("temp remux output left behind")
			}

			if !HasCoverArt(ctx, tools, video) {
				t.Error("HasCoverArt false after Embed")
			}
			streams := ffprobeStreams(t, video)
			if !strings.Contains(streams, "codec_type=video") {
				t.Error("video stream missing after embed")
			}
			if !strings.Contains(streams, "codec_type=audio") {
				t.Error("audio stream missing after embed")
			}
			switch ext {
			case ".mkv":
				// ffprobe < 7 lists the attachment as codec_type=attachment;
				// newer builds surface it as an attached_pic video stream.
				if !strings.Contains(streams, "mimetype=image/jpeg") &&
					!strings.Contains(streams, "codec_type=attachment") {
					t.Error("image attachment missing from MKV")
				}
			default:
				if !strings.Contains(streams, "DISPOSITION:attached_pic=1") {
					t.Error("attached_pic disposition missing from MP4")
				}
			}

			// Embedding twice replaces rather than stacks the cover.
			if _, err := Embed(ctx, tools, video, cover); err != nil {
				t.Fatalf("second Embed: %v", err)
			}
			if ext == ".mkv" {
				out, _ := exec.Command("mkvmerge", "-i", video).CombinedOutput()
				if n := strings.Count(string(out), "image/jpeg"); n != 1 {
					t.Errorf("MKV has %d image attachments after re-embed, want 1\n%s", n, out)
				}
			} else {
				if n := strings.Count(ffprobeStreams(t, video), "DISPOSITION:attached_pic=1"); n != 1 {
					t.Errorf("MP4 has %d attached_pic streams after re-embed, want 1", n)
				}
			}
		})
	}
}

func TestEmbedIntegration_MKVFfmpegFallback(t *testing.T) {
	tools := requireTools(t)
	codec := pickVideoCodec(t)
	dir := t.TempDir()
	ctx := context.Background()

	cover := filepath.Join(dir, "cover.jpg")
	ffmpegMust(t, "-f", "lavfi", "-i", "color=c=blue:size=64x96", "-frames:v", "1", cover)
	video := filepath.Join(dir, "clip.mkv")
	makeClip(t, video, codec)

	// Point mkvpropedit at nothing so the ffmpeg -attach remux path runs.
	tools.MkvPropEdit = filepath.Join(dir, "no-such-mkvpropedit")
	tool, err := Embed(ctx, tools, video, cover)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if tool != "ffmpeg" {
		t.Errorf("tool = %q, want ffmpeg", tool)
	}
	if !HasCoverArt(ctx, tools, video) {
		t.Error("HasCoverArt false after ffmpeg MKV remux")
	}
	// ffprobe fallback for detection too (mkvmerge pointed at nothing).
	tools.MkvMerge = filepath.Join(dir, "no-such-mkvmerge")
	if !HasCoverArt(ctx, tools, video) {
		t.Error("HasCoverArt (ffprobe fallback) false after ffmpeg MKV remux")
	}
	if !strings.Contains(ffprobeStreams(t, video), "codec_type=video") {
		t.Error("video stream missing after remux")
	}
}

func TestEmbedIntegration_GuardAbortsOnChange(t *testing.T) {
	tools := requireTools(t)
	codec := pickVideoCodec(t)
	dir := t.TempDir()
	ctx := context.Background()

	cover := filepath.Join(dir, "cover.jpg")
	ffmpegMust(t, "-f", "lavfi", "-i", "color=c=green:size=64x96", "-frames:v", "1", cover)
	video := filepath.Join(dir, "clip.mp4")
	makeClip(t, video, codec)
	original, _ := os.ReadFile(video)

	// Simulate a concurrent writer: ffmpeg is wrapped by a script that grows
	// the source file while "remuxing" (it still runs the real remux so the
	// output is valid — only the guard should stop the swap).
	real, _ := exec.LookPath("ffmpeg")
	wrapper := filepath.Join(dir, "ffmpeg")
	script := "#!/bin/sh\nprintf extra >> \"" + video + "\"\nexec \"" + real + "\" \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	tools.FFmpeg = wrapper

	_, err := Embed(ctx, tools, video, cover)
	if err == nil || !strings.Contains(err.Error(), "changed while embedding") {
		t.Fatalf("err = %v, want size/mtime guard failure", err)
	}
	if _, err := os.Stat(embedTempName(video)); err == nil {
		t.Error("temp remux output left behind after guard failure")
	}
	now, _ := os.ReadFile(video)
	if string(now) != string(original)+"extra" {
		t.Error("original file was replaced despite the guard")
	}
}

func TestCoverCompressIntegration(t *testing.T) {
	tools := requireTools(t)
	dir := t.TempDir()
	ctx := context.Background()

	// Small cover: used as-is.
	small := filepath.Join(dir, "small.jpg")
	ffmpegMust(t, "-f", "lavfi", "-i", "color=c=red:size=64x96", "-frames:v", "1", small)
	path, cleanup, err := coverCompress(ctx, tools, small)
	cleanup()
	if err != nil || path != small {
		t.Errorf("small cover: path=%q err=%v, want original", path, err)
	}

	// Noisy, large cover: must come back smaller and narrower than 1000px.
	big := filepath.Join(dir, "big.jpg")
	ffmpegMust(t, "-f", "lavfi", "-i", "nullsrc=size=2000x3000",
		"-vf", "geq=random(1)*255:random(2)*255:random(3)*255",
		"-frames:v", "1", "-q:v", "1", big)
	bigInfo, err := os.Stat(big)
	if err != nil {
		t.Fatal(err)
	}
	if !coverNeedsCompress(bigInfo.Size()) {
		t.Skipf("generated cover is only %d bytes; cannot exercise compression", bigInfo.Size())
	}
	path, cleanup, err = coverCompress(ctx, tools, big)
	defer cleanup()
	if err != nil {
		t.Fatalf("coverCompress: %v", err)
	}
	if path == big {
		t.Fatal("large cover was not compressed")
	}
	cInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if cInfo.Size() >= bigInfo.Size() {
		t.Errorf("compressed %d bytes >= original %d", cInfo.Size(), bigInfo.Size())
	}
	if w, _, err := imgSize(path); err != nil || w != coverMaxWidth {
		t.Errorf("compressed width = %d (err %v), want %d", w, err, coverMaxWidth)
	}
	cleanup()
	if _, err := os.Stat(path); err == nil {
		t.Error("cleanup left the compressed file behind")
	}
}

func TestComposeLogoIntegration(t *testing.T) {
	tools := requireTools(t)
	dir := t.TempDir()
	ctx := context.Background()

	art := filepath.Join(dir, "art.jpg")
	ffmpegMust(t, "-f", "lavfi", "-i", "color=c=black:size=320x180", "-frames:v", "1", art)
	logo := filepath.Join(dir, "logo.png")
	ffmpegMust(t, "-f", "lavfi", "-i", "color=c=white@0.5:size=200x50", "-frames:v", "1",
		"-pix_fmt", "rgba", "-c:v", "png", logo)

	if err := ComposeLogo(ctx, tools, art, logo); err != nil {
		t.Fatalf("ComposeLogo: %v", err)
	}
	w, h, err := imgSize(art)
	if err != nil {
		t.Fatalf("composed artwork does not decode: %v", err)
	}
	if w != 320 || h != 180 {
		t.Errorf("composed artwork is %dx%d, want 320x180", w, h)
	}
	if _, err := os.Stat(filepath.Join(dir, "titled-art.jpg")); err == nil {
		t.Error("intermediate titled-art.jpg left behind")
	}
	// The overlay region must no longer be pure black: sample via ffmpeg's
	// signalstats on the crop where the logo was drawn (x=16, w=102, bottom at y=158).
	out, err := exec.Command("ffprobe", "-v", "error", "-f", "lavfi",
		"-i", "movie="+art+",crop=102:25:16:133,signalstats",
		"-show_entries", "frame_tags=lavfi.signalstats.YAVG", "-of", "csv=p=0").Output()
	if err != nil {
		t.Fatalf("ffprobe signalstats: %v", err)
	}
	if avg := strings.TrimSpace(string(out)); avg == "" || strings.HasPrefix(avg, "0.0") || avg == "0" || avg == "16.000000" {
		t.Errorf("logo region luma average %q looks unchanged (logo not drawn?)", avg)
	}

	// Garbage inputs are rejected before ffmpeg runs.
	bogus := filepath.Join(dir, "bogus.png")
	os.WriteFile(bogus, []byte("nope"), 0o644)
	if err := ComposeLogo(ctx, tools, art, bogus); err == nil {
		t.Error("ComposeLogo accepted an undecodable logo")
	}
}
