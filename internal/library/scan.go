package library

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshkerr/goplexcli/internal/config"
)

// ScannedFile is one video file found under a library folder.
type ScannedFile struct {
	// Path is the absolute local path (library Path joined with Rel).
	Path string
	// Rel is the slash-separated path relative to the library root.
	Rel     string
	Size    int64
	ModTime time.Time
}

// ScanOptions tunes how a library folder is listed.
type ScanOptions struct {
	// RcloneBin is the rclone executable used to list a library's Remote
	// directly. Empty means "rclone" from PATH.
	RcloneBin string
	// NoRclone forces a filesystem walk even when the library has a Remote.
	NoRclone bool
	// Progress, if set, is called with the running count of files found.
	Progress func(found int)
}

// ScanFolder lists every video file under a library folder, skipping extras,
// samples and hidden files. When the library names an rclone Remote and
// rclone is available, the remote is listed directly with `rclone lsjson -R
// --fast-list`, which for cloud storage is far faster than walking a FUSE
// mount directory by directory; the results are mapped back onto the local
// Path. Otherwise the folder is walked on disk.
func ScanFolder(ctx context.Context, lib config.LibraryFolder, opts ScanOptions) ([]ScannedFile, error) {
	if lib.Remote != "" && !opts.NoRclone {
		files, err := scanRemote(ctx, lib, opts)
		if err == nil {
			return files, nil
		}
		// A listing failure (rclone missing, remote misconfigured, network)
		// falls back to the mount when it exists; otherwise surface the
		// error so the caller keeps its previous index.
		if st, serr := os.Stat(lib.Path); serr != nil || !st.IsDir() {
			return nil, fmt.Errorf("list %s: %w", lib.Remote, err)
		}
	}
	return scanLocal(ctx, lib, opts)
}

func scanLocal(ctx context.Context, lib config.LibraryFolder, opts ScanOptions) ([]ScannedFile, error) {
	root := filepath.Clean(lib.Path)
	st, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}

	var files []ScannedFile
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			// Unreadable subtrees are skipped rather than aborting the
			// whole scan (cloud mounts hiccup).
			if p == root {
				return walkErr
			}
			return nil
		}
		name := d.Name()
		if p != root && IsIgnoredName(name) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !IsVideoFile(name) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		files = append(files, ScannedFile{
			Path:    p,
			Rel:     filepath.ToSlash(rel),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
		if opts.Progress != nil {
			opts.Progress(len(files))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// rcloneEntry is one object in `rclone lsjson` output.
type rcloneEntry struct {
	Path    string `json:"Path"`
	Name    string `json:"Name"`
	Size    int64  `json:"Size"`
	ModTime string `json:"ModTime"`
	IsDir   bool   `json:"IsDir"`
}

func scanRemote(ctx context.Context, lib config.LibraryFolder, opts ScanOptions) ([]ScannedFile, error) {
	bin := opts.RcloneBin
	if bin == "" {
		bin = "rclone"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, bin, "lsjson", "-R", "--files-only", "--fast-list", "--no-mimetype", lib.Remote)
	cmd.Stderr = io.Discard
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	files, parseErr := parseLsjson(bufio.NewReaderSize(stdout, 1<<20), lib, opts)
	waitErr := cmd.Wait()
	if parseErr != nil {
		return nil, parseErr
	}
	if waitErr != nil {
		return nil, fmt.Errorf("rclone lsjson %s: %w", lib.Remote, waitErr)
	}
	return files, nil
}

// parseLsjson streams a JSON array of rclone entries without buffering the
// whole document (large remotes produce hundreds of megabytes).
func parseLsjson(r io.Reader, lib config.LibraryFolder, opts ScanOptions) ([]ScannedFile, error) {
	dec := json.NewDecoder(r)
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("rclone output: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		return nil, fmt.Errorf("rclone output: expected array")
	}
	root := filepath.Clean(lib.Path)
	var files []ScannedFile
	for dec.More() {
		var e rcloneEntry
		if err := dec.Decode(&e); err != nil {
			return nil, fmt.Errorf("rclone output: %w", err)
		}
		if e.IsDir || !IsVideoFile(e.Name) || pathIgnored(e.Path) {
			continue
		}
		mod, _ := time.Parse(time.RFC3339Nano, e.ModTime)
		files = append(files, ScannedFile{
			Path:    filepath.Join(root, filepath.FromSlash(e.Path)),
			Rel:     path.Clean(e.Path),
			Size:    e.Size,
			ModTime: mod,
		})
		if opts.Progress != nil {
			opts.Progress(len(files))
		}
	}
	return files, nil
}

// pathIgnored reports whether any segment of a slash-separated relative path
// is an ignored name.
func pathIgnored(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if seg != "" && IsIgnoredName(seg) {
			return true
		}
	}
	return false
}
