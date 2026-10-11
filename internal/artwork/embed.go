package artwork

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // artwork dimensions for ComposeLogo
	_ "image/png"  // title-logo dimensions for ComposeLogo
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ErrUnsupported is returned by Embed for containers it cannot write cover
// art into. SupportedContainer tells callers ahead of time.
var ErrUnsupported = errors.New("artwork: unsupported container")

// Cover-image limits. Covers over coverMaxSize are shrunk before embedding —
// larger images fail to display in some players — to at most coverMaxWidth
// pixels wide.
const (
	coverMaxSize  = 100 * 1024
	coverMaxWidth = 1000
)

// embedMaxDownload caps what DownloadImage will accept from the network.
const embedMaxDownload = 50 * 1024 * 1024

// embedUserAgent is the browser User-Agent the artwork CDNs (TMDB, Apple,
// Netflix) expect; some refuse the Go default.
const embedUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36"

// Per-tool deadlines. Every external command is capped so one wedged process
// (e.g. reading a file another program is still growing) can never hold its
// caller forever. Each is layered on top of the caller's context.
const (
	embedProbeTimeout    = 30 * time.Second // ffprobe / mkvmerge inspection
	embedCompressTimeout = 60 * time.Second // resizing one cover image
	embedComposeTimeout  = 60 * time.Second // overlaying a title logo
	embedPropeditTimeout = 5 * time.Minute  // mkvpropedit attachment edit (an append, not a remux)
	embedRemuxTimeout    = 10 * time.Minute // full ffmpeg remux of a large file
)

// Title-logo placement, as a fraction of the artwork (gowebdav's numbers):
// the title treatment sits lower-left over the story art.
const (
	embedLogoLeft      = 0.05 // logo's left edge
	embedLogoBottom    = 0.88 // logo's bottom edge
	embedLogoWidth     = 0.32 // logo width
	embedLogoMaxHeight = 0.30 // tall logos shrink to fit this
)

// embedHTTPClient performs DownloadImage requests. Tests swap it for one
// that trusts an httptest TLS server.
var embedHTTPClient = &http.Client{Timeout: 2 * time.Minute}

// Tools locates the external programs; empty fields mean "look on PATH".
type Tools struct {
	FFmpeg      string
	FFprobe     string
	MkvPropEdit string
	MkvMerge    string
}

// Available reports whether ffmpeg and ffprobe can be found (mkvpropedit is
// optional: without it MKV covers go through an ffmpeg remux).
func (t Tools) Available() bool {
	return len(t.Missing()) == 0
}

// Missing lists which required tools are absent, for user-facing messages.
func (t Tools) Missing() []string {
	var missing []string
	if _, err := exec.LookPath(t.ffmpeg()); err != nil {
		missing = append(missing, "ffmpeg")
	}
	if _, err := exec.LookPath(t.ffprobe()); err != nil {
		missing = append(missing, "ffprobe")
	}
	return missing
}

func (t Tools) ffmpeg() string      { return embedToolName(t.FFmpeg, "ffmpeg") }
func (t Tools) ffprobe() string     { return embedToolName(t.FFprobe, "ffprobe") }
func (t Tools) mkvpropedit() string { return embedToolName(t.MkvPropEdit, "mkvpropedit") }
func (t Tools) mkvmerge() string    { return embedToolName(t.MkvMerge, "mkvmerge") }

// embedToolName resolves a configured tool path, falling back to the bare
// program name so exec looks it up on PATH.
func embedToolName(configured, fallback string) string {
	if strings.TrimSpace(configured) != "" {
		return configured
	}
	return fallback
}

// embedToolExists reports whether a tool (bare name or path) can be run.
func embedToolExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// SupportedContainer reports whether Embed can handle the file's extension
// (.mkv .mp4 .m4v).
func SupportedContainer(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mkv", ".mp4", ".m4v":
		return true
	}
	return false
}

// embedTempName is where a remux writes before replacing the original:
// "<name>.tmp<ext>" next to the file, so the final rename stays on one
// filesystem and the extension still tells ffmpeg which muxer to use.
func embedTempName(videoFile string) string {
	return videoFile + ".tmp" + strings.ToLower(filepath.Ext(videoFile))
}

// embedUnchanged reports whether a file's size and mtime are the same in two
// snapshots. A remux ends with the temp file replacing the original — safe
// only if nothing else wrote to the original meanwhile. A concurrent writer
// (a download still in progress) would keep writing to the replaced,
// now-orphaned inode, silently destroying its own transfer.
func embedUnchanged(before, after os.FileInfo) bool {
	if before == nil || after == nil {
		return false
	}
	return before.Size() == after.Size() && before.ModTime().Equal(after.ModTime())
}

// coverNeedsCompress reports whether a cover of the given byte size is over
// the embedding limit and should be shrunk first.
func coverNeedsCompress(size int64) bool {
	return size > coverMaxSize
}

// embedRun runs a tool under a deadline layered on ctx, returning its
// combined output. A deadline hit is reported as such rather than as the
// tool's "killed" exit status.
func embedRun(ctx context.Context, d time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return out, fmt.Errorf("%s timed out after %s", filepath.Base(name), d)
	}
	return out, err
}

// embedOutputErr wraps a tool failure with the tail of its output, which is
// where ffmpeg and mkvpropedit put the useful line.
func embedOutputErr(what string, err error, out []byte) error {
	tail := bytes.TrimSpace(out)
	if n := len(tail); n > 2048 {
		tail = tail[n-2048:]
	}
	if len(tail) == 0 {
		return fmt.Errorf("%s: %w", what, err)
	}
	return fmt.Errorf("%s: %w\n%s", what, err, tail)
}

// DownloadImage fetches an https image URL to dest (temp file + rename),
// with a browser User-Agent and a 50 MB cap.
func DownloadImage(ctx context.Context, imageURL, dest string) error {
	u, err := url.Parse(imageURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("artwork: image URL must be https: %q", imageURL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return fmt.Errorf("artwork: build request: %w", err)
	}
	req.Header.Set("User-Agent", embedUserAgent)
	req.Header.Set("Accept", "image/*,*/*;q=0.8")

	resp, err := embedHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("artwork: download image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("artwork: download image: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > embedMaxDownload {
		return fmt.Errorf("artwork: image exceeds %d bytes", embedMaxDownload)
	}

	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("artwork: create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(dest)+".*.part")
	if err != nil {
		return fmt.Errorf("artwork: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	// Read one byte past the cap so an oversized body is detected rather
	// than silently truncated into a corrupt image.
	written, err := io.Copy(tmp, io.LimitReader(resp.Body, embedMaxDownload+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		cleanup()
		return fmt.Errorf("artwork: write image: %w", err)
	}
	if written > embedMaxDownload {
		cleanup()
		return fmt.Errorf("artwork: image exceeds %d bytes", embedMaxDownload)
	}
	if written == 0 {
		cleanup()
		return errors.New("artwork: image download was empty")
	}
	if err := os.Rename(tmpName, dest); err != nil {
		cleanup()
		return fmt.Errorf("artwork: move image into place: %w", err)
	}
	return nil
}

// imgSize reads a JPEG or PNG header for its pixel dimensions.
func imgSize(path string) (w, h int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

// imgLogoPlacement works out the logo's scaled width and its top-left
// corner on artwork of artW x artH: left edge at 5% of the width, 32% of
// the width wide, height capped at 30%, bottom edge at 88% of the height.
func imgLogoPlacement(artW, artH, logoW, logoH int) (w, x, y int) {
	fw := float64(artW) * embedLogoWidth
	fh := fw * float64(logoH) / float64(logoW)
	if maxH := float64(artH) * embedLogoMaxHeight; fh > maxH {
		fw, fh = fw*maxH/fh, maxH
	}
	x = int(float64(artW) * embedLogoLeft)
	y = int(float64(artH)*embedLogoBottom - fh)
	return int(fw), x, y
}

// ComposeLogo draws a title logo (PNG, possibly with alpha) onto artwork in
// place, bottom-left like gowebdav: x = 5% of width, logo width 32% of art
// width, logo height capped at 30%, bottom edge at 88% of height. ffmpeg
// does the compositing; it is already required for embedding.
func ComposeLogo(ctx context.Context, tools Tools, art, logo string) error {
	artW, artH, err := imgSize(art)
	if err != nil {
		return fmt.Errorf("artwork: reading artwork: %w", err)
	}
	logoW, logoH, err := imgSize(logo)
	if err != nil {
		return fmt.Errorf("artwork: reading title logo: %w", err)
	}
	if artW == 0 || artH == 0 || logoW == 0 || logoH == 0 {
		return errors.New("artwork: empty image")
	}

	w, x, y := imgLogoPlacement(artW, artH, logoW, logoH)
	out := filepath.Join(filepath.Dir(art), "titled-"+filepath.Base(art))
	filter := fmt.Sprintf("[1]scale=w=%d:h=-1:flags=lanczos[l];[0][l]overlay=x=%d:y=%d:format=auto", w, x, y)
	output, err := embedRun(ctx, embedComposeTimeout, tools.ffmpeg(),
		"-y", "-v", "error", "-nostdin",
		"-i", art, "-i", logo,
		"-filter_complex", filter,
		"-frames:v", "1", "-q:v", "2", out)
	if err != nil {
		_ = os.Remove(out)
		return embedOutputErr("artwork: compositing title logo", err, output)
	}
	if info, err := os.Stat(out); err != nil || info.Size() == 0 {
		_ = os.Remove(out)
		return errors.New("artwork: compositing title logo produced no image")
	}
	if err := os.Rename(out, art); err != nil {
		_ = os.Remove(out)
		return fmt.Errorf("artwork: replace artwork: %w", err)
	}
	return nil
}

// coverCompress shrinks a cover over coverMaxSize with ffmpeg
// (scale=1000:-1, -q:v 8). It returns the path to embed — the compressed
// copy only when that is actually smaller, otherwise the original — and a
// cleanup the caller must always run.
func coverCompress(ctx context.Context, tools Tools, coverFile string) (string, func(), error) {
	noop := func() {}
	info, err := os.Stat(coverFile)
	if err != nil {
		return coverFile, noop, err
	}
	if !coverNeedsCompress(info.Size()) {
		return coverFile, noop, nil
	}

	tempFile := coverFile + ".compressed.jpg"
	cleanup := func() { _ = os.Remove(tempFile) }
	output, err := embedRun(ctx, embedCompressTimeout, tools.ffmpeg(),
		"-y", "-v", "error", "-nostdin",
		"-i", coverFile,
		"-vf", fmt.Sprintf("scale=%d:-1", coverMaxWidth),
		"-q:v", "8",
		tempFile)
	if err != nil {
		cleanup()
		return coverFile, noop, embedOutputErr("compressing cover", err, output)
	}

	compressed, err := os.Stat(tempFile)
	if err != nil || compressed.Size() == 0 || compressed.Size() >= info.Size() {
		cleanup()
		return coverFile, noop, nil // compression didn't help; use the original
	}
	return tempFile, cleanup, nil
}

// Embed writes coverFile into videoFile as cover art.
//
// MKV: mkvpropedit in place (deleting existing image/jpeg and image/png
// attachments and adding cover.jpg in one invocation, so existing art is never
// stripped if the add fails), falling back to an ffmpeg remux when mkvpropedit
// is unavailable or fails. MP4/M4V: ffmpeg remux with the cover as an
// attached_pic video stream (-c copy, -strict -2).
//
// Images over 100 KB are first compressed with ffmpeg scale=1000:-1 -q:v 8
// (used only if smaller). A remux writes next to the file as "<name>.tmp<ext>",
// verifies the original's size and mtime did not change meanwhile and the
// output is non-empty, then renames over the original.
//
// Returns the tool used ("mkvpropedit" | "ffmpeg") for logging. Unsupported
// extensions return ErrUnsupported.
func Embed(ctx context.Context, tools Tools, videoFile, coverFile string) (string, error) {
	ext := strings.ToLower(filepath.Ext(videoFile))
	if !SupportedContainer(videoFile) {
		return "", fmt.Errorf("%w: %s", ErrUnsupported, ext)
	}

	cover, cleanup, err := coverCompress(ctx, tools, coverFile)
	if err != nil {
		cleanup()
		return "", fmt.Errorf("artwork: prepare cover %s: %w", filepath.Base(coverFile), err)
	}
	defer cleanup()

	tempOutput := embedTempName(videoFile)
	var remuxArgs []string
	var propeditErr error

	switch ext {
	case ".mp4", ".m4v":
		remuxArgs = []string{"-y", "-nostdin", "-i", videoFile, "-i", cover,
			"-map", "0:v:0", "-map", "0:a?", "-map", "0:s?", "-map", "1",
			"-c", "copy",
			// Streams are copied verbatim, yet ffmpeg still refuses to mux
			// codecs like Opus or FLAC back into MP4 without opting in
			// ("Opus in MP4 support is experimental, add '-strict -2'").
			// The source already held these streams, so allowing them is safe.
			"-strict", "-2",
			"-disposition:v:1", "attached_pic",
			"-metadata:s:v:1", "title=Cover",
			"-metadata:s:v:1", "comment=Cover (front)",
			tempOutput}

	case ".mkv":
		if embedToolExists(tools.mkvpropedit()) {
			output, err := embedRun(ctx, embedPropeditTimeout, tools.mkvpropedit(), videoFile,
				"--delete-attachment", "mime-type:image/jpeg",
				"--delete-attachment", "mime-type:image/png",
				"--attachment-name", "cover.jpg",
				"--attachment-mime-type", "image/jpeg",
				"--add-attachment", cover)
			if err == nil {
				return "mkvpropedit", nil
			}
			// Exit code 1 means warnings only (e.g. no existing attachment
			// matched the delete specs); processing still completed.
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
				return "mkvpropedit", nil
			}
			if ctx.Err() != nil {
				return "mkvpropedit", embedOutputErr("artwork: mkvpropedit", err, output)
			}
			propeditErr = embedOutputErr("mkvpropedit", err, output)
		}
		// mkvpropedit absent or failed: full ffmpeg remux with the cover as
		// a Matroska attachment.
		remuxArgs = []string{"-y", "-nostdin", "-i", videoFile, "-attach", cover,
			"-map", "0:v", "-map", "0:a?", "-map", "0:s?",
			"-metadata:s:t:0", "mimetype=image/jpeg",
			"-metadata:s:t:0", "filename=cover.jpg",
			"-c", "copy",
			tempOutput}
	}

	before, err := os.Stat(videoFile)
	if err != nil {
		return "ffmpeg", err
	}

	output, err := embedRun(ctx, embedRemuxTimeout, tools.ffmpeg(), remuxArgs...)
	if err != nil {
		_ = os.Remove(tempOutput)
		if propeditErr != nil {
			return "ffmpeg", fmt.Errorf("artwork: %v; ffmpeg fallback also failed: %w", propeditErr, embedOutputErr("remux", err, output))
		}
		return "ffmpeg", embedOutputErr("artwork: ffmpeg remux", err, output)
	}

	if info, err := os.Stat(tempOutput); err != nil || info.Size() == 0 {
		_ = os.Remove(tempOutput)
		return "ffmpeg", errors.New("artwork: remux output is empty or missing")
	}

	after, err := os.Stat(videoFile)
	if err != nil || !embedUnchanged(before, after) {
		_ = os.Remove(tempOutput)
		return "ffmpeg", fmt.Errorf("artwork: %s changed while embedding (still being written?); not replacing it", filepath.Base(videoFile))
	}

	if err := os.Rename(tempOutput, videoFile); err != nil {
		_ = os.Remove(tempOutput)
		return "ffmpeg", fmt.Errorf("artwork: replace original: %w", err)
	}
	return "ffmpeg", nil
}

// HasCoverArt reports whether the file already carries embedded cover art:
// an attached_pic video stream for MP4/M4V (via ffprobe); an image
// attachment for MKV (via mkvmerge -i, falling back to ffprobe stream
// inspection).
func HasCoverArt(ctx context.Context, tools Tools, videoFile string) bool {
	switch strings.ToLower(filepath.Ext(videoFile)) {
	case ".mp4", ".m4v":
		output, err := embedRun(ctx, embedProbeTimeout, tools.ffprobe(),
			"-v", "quiet", "-show_streams", "-select_streams", "v", videoFile)
		if err != nil {
			return false
		}
		return strings.Contains(string(output), "DISPOSITION:attached_pic=1")

	case ".mkv":
		if embedToolExists(tools.mkvmerge()) {
			output, _ := embedRun(ctx, embedProbeTimeout, tools.mkvmerge(), "-i", videoFile)
			s := string(output)
			return strings.Contains(s, "image/jpeg") || strings.Contains(s, "image/png")
		}
		// Older ffprobe lists Matroska attachments as codec_type=attachment
		// streams tagged mimetype=image/...; ffmpeg 7+ surfaces image
		// attachments as attached_pic video streams instead. Probe every
		// stream and accept either marker.
		output, _ := embedRun(ctx, embedProbeTimeout, tools.ffprobe(),
			"-v", "quiet", "-show_streams", videoFile)
		s := string(output)
		return strings.Contains(s, "mimetype=image") || strings.Contains(s, "DISPOSITION:attached_pic=1")
	}
	return false
}
