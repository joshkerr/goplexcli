// Package update implements a self-updater that fetches the latest release
// from GitHub and swaps the running binary in place. It uses only the standard
// library: the GitHub REST API for release discovery and an atomic
// rename-based swap that also works on Windows, where a running executable
// cannot be overwritten but can be renamed aside.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// DefaultRepo is the GitHub "owner/name" the updater pulls releases from.
const DefaultRepo = "joshkerr/goplexcli"

// httpTimeout bounds both the release lookup and the asset download.
const httpTimeout = 60 * time.Second

// Release is the subset of a GitHub release we care about.
type Release struct {
	TagName string  `json:"tag_name"`
	HTMLURL string  `json:"html_url"`
	Assets  []Asset `json:"assets"`
}

// Asset is a single downloadable file attached to a release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// AssetName returns the release asset name expected for the current platform,
// matching the names produced by `make build-all`
// (e.g. "goplexcli-darwin-arm64", "goplexcli-windows-amd64.exe").
func AssetName() string {
	name := fmt.Sprintf("goplexcli-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// FindAsset returns the asset matching the given name, if present.
func (r *Release) FindAsset(name string) (*Asset, bool) {
	for i := range r.Assets {
		if r.Assets[i].Name == name {
			return &r.Assets[i], true
		}
	}
	return nil, false
}

// LatestRelease fetches the most recent published release for repo
// ("owner/name").
func LatestRelease(ctx context.Context, repo string) (*Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "goplexcli-selfupdate")

	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query GitHub releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no releases found for %s", repo)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub returned status %s", resp.Status)
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("failed to decode release: %w", err)
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("latest release has no tag")
	}
	return &rel, nil
}

// CompareVersions compares two dotted version strings, ignoring a leading "v".
// It returns -1 if a < b, 0 if equal, and 1 if a > b. Non-numeric components
// compare as 0, so unparseable versions degrade to "equal" rather than erroring.
func CompareVersions(a, b string) int {
	aParts := splitVersion(a)
	bParts := splitVersion(b)

	n := len(aParts)
	if len(bParts) > n {
		n = len(bParts)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(aParts) {
			av = aParts[i]
		}
		if i < len(bParts) {
			bv = bParts[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

// splitVersion turns "v1.2.3" into [1, 2, 3]. A component that fails to parse
// (e.g. a pre-release suffix like "3-rc1") contributes its leading digits, or 0.
func splitVersion(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	fields := strings.Split(v, ".")
	out := make([]int, len(fields))
	for i, f := range fields {
		// Take leading digits only, so "3-rc1" -> 3.
		end := 0
		for end < len(f) && f[end] >= '0' && f[end] <= '9' {
			end++
		}
		if end > 0 {
			n, _ := strconv.Atoi(f[:end])
			out[i] = n
		}
	}
	return out
}

// Apply downloads asset and replaces the executable at exePath with it. The
// swap renames the running binary to "<exe>.old" before moving the new binary
// into place, which is the only approach that works while the binary is
// executing on Windows. On success it best-effort removes the ".old" file.
func Apply(ctx context.Context, asset *Asset, exePath string) error {
	dir := filepath.Dir(exePath)

	tmp, err := os.CreateTemp(dir, ".goplexcli-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	// Clean up the temp file unless we successfully move it into place.
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		tmp.Close()
		return err
	}
	req.Header.Set("User-Agent", "goplexcli-selfupdate")

	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Do(req)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("failed to download update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		tmp.Close()
		return fmt.Errorf("download returned status %s", resp.Status)
	}

	written, err := io.Copy(tmp, resp.Body)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("failed to write update: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to finalize download: %w", err)
	}
	if asset.Size > 0 && written != asset.Size {
		return fmt.Errorf("downloaded %d bytes, expected %d", written, asset.Size)
	}

	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	// Swap: move the running binary aside, then move the new one in.
	oldPath := exePath + ".old"
	_ = os.Remove(oldPath) // discard any leftover from a prior update
	if err := os.Rename(exePath, oldPath); err != nil {
		return fmt.Errorf("failed to move current binary aside: %w", err)
	}
	if err := os.Rename(tmpPath, exePath); err != nil {
		// Roll back so the user is left with a working binary.
		if rbErr := os.Rename(oldPath, exePath); rbErr != nil {
			return fmt.Errorf("failed to install update (%v) and rollback failed (%v); restore manually from %s", err, rbErr, oldPath)
		}
		return fmt.Errorf("failed to install update: %w", err)
	}
	tmpPath = "" // moved into place; don't delete in the deferred cleanup

	// Best-effort: remove the old binary. On Windows this typically fails while
	// the process is running, which is harmless.
	_ = os.Remove(oldPath)

	return nil
}

// Run performs the full update flow and writes human-readable progress to out.
// currentVersion is the running build's version; a value of "dev" disables
// updates. When checkOnly is true it reports availability without downloading.
func Run(ctx context.Context, repo, currentVersion string, checkOnly bool, out io.Writer) error {
	if currentVersion == "dev" || currentVersion == "" {
		fmt.Fprintln(out, "Running a development build; self-update is disabled.")
		fmt.Fprintln(out, "Build a released version or install via 'make install' to use updates.")
		return nil
	}

	fmt.Fprintln(out, "Checking for updates...")
	rel, err := LatestRelease(ctx, repo)
	if err != nil {
		return err
	}

	cmp := CompareVersions(currentVersion, rel.TagName)
	if cmp >= 0 {
		fmt.Fprintf(out, "You're up to date (v%s).\n", strings.TrimPrefix(currentVersion, "v"))
		return nil
	}

	fmt.Fprintf(out, "Update available: v%s -> %s\n", strings.TrimPrefix(currentVersion, "v"), rel.TagName)
	if rel.HTMLURL != "" {
		fmt.Fprintf(out, "Release notes: %s\n", rel.HTMLURL)
	}

	assetName := AssetName()
	asset, ok := rel.FindAsset(assetName)
	if !ok {
		return fmt.Errorf("release %s has no asset %q for this platform", rel.TagName, assetName)
	}

	if checkOnly {
		fmt.Fprintln(out, "Run 'goplexcli update' to install it.")
		return nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot locate current executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}

	fmt.Fprintf(out, "Downloading %s...\n", assetName)
	if err := Apply(ctx, asset, exePath); err != nil {
		return err
	}

	fmt.Fprintf(out, "Updated to %s. Restart goplexcli to use the new version.\n", rel.TagName)
	return nil
}
