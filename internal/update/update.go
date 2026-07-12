// Package update implements a self-updater that fetches the latest release
// from GitHub and swaps the running binary in place. It uses only the standard
// library: the GitHub REST API for release discovery and an atomic
// rename-based swap that also works on Windows, where a running executable
// cannot be overwritten but can be renamed aside.
//
// Private repositories are supported via a GitHub token, discovered from
// GH_TOKEN / GITHUB_TOKEN or, failing that, the `gh` CLI (`gh auth token`).
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// DefaultRepo is the GitHub "owner/name" the updater pulls releases from.
const DefaultRepo = "joshkerr/goplexcli"

// binaryName is the base name of the released binaries (asset names look like
// "<binaryName>-<os>-<arch>" with a .exe suffix on Windows).
const binaryName = "goplexcli"

// httpTimeout bounds both the release lookup and the asset download.
const httpTimeout = 60 * time.Second

// Release is the subset of a GitHub release we care about.
type Release struct {
	TagName string  `json:"tag_name"`
	HTMLURL string  `json:"html_url"`
	Assets  []Asset `json:"assets"`
}

// Asset is a single downloadable file attached to a release. URL is the API
// asset endpoint (used for authenticated downloads on private repos);
// BrowserDownloadURL is the public direct link.
type Asset struct {
	Name               string `json:"name"`
	URL                string `json:"url"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// AssetName returns the release asset name expected for the current platform,
// matching the names produced by `make build-all`
// (e.g. "goplexcli-darwin-arm64", "goplexcli-windows-amd64.exe").
func AssetName() string {
	name := fmt.Sprintf("%s-%s-%s", binaryName, runtime.GOOS, runtime.GOARCH)
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

// tokenCandidates returns GitHub tokens to try, in order: $GH_TOKEN,
// $GITHUB_TOKEN, the `gh` CLI's token, and finally "" (unauthenticated, which
// works for public repos). Trying several makes the updater resilient to a
// stale or invalid env token shadowing a working `gh` login.
func tokenCandidates() []string {
	seen := map[string]bool{}
	var cands []string
	add := func(t string) {
		t = strings.TrimSpace(t)
		if t != "" && !seen[t] {
			seen[t] = true
			cands = append(cands, t)
		}
	}
	add(os.Getenv("GH_TOKEN"))
	add(os.Getenv("GITHUB_TOKEN"))
	if path, err := exec.LookPath("gh"); err == nil {
		// Strip GH_TOKEN/GITHUB_TOKEN from gh's environment so it returns its
		// stored (keyring) login instead of echoing back a possibly-invalid env
		// token — which is already a candidate above.
		cmd := exec.Command(path, "auth", "token")
		cmd.Env = envWithout(os.Environ(), "GH_TOKEN", "GITHUB_TOKEN")
		// Suppress the console window Windows would otherwise flash when the GUI
		// (a windowed process) spawns the console-mode `gh` here.
		hideConsoleWindow(cmd)
		if out, oerr := cmd.Output(); oerr == nil {
			add(string(out))
		}
	}
	return append(cands, "") // final unauthenticated attempt (public repos)
}

// envWithout returns env with any KEY=... entries for the given keys removed.
func envWithout(env []string, keys ...string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		drop := false
		for _, k := range keys {
			if strings.HasPrefix(kv, k+"=") {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}

// ResolveLatest fetches the latest release for repo, trying each GitHub token
// candidate in turn (env vars, the gh CLI, then unauthenticated) until one
// succeeds. It returns the release and the token that worked (empty when the
// unauthenticated attempt succeeded), so the caller can reuse that same token to
// download the release's assets. This tolerates a stale env token by falling
// back to the gh CLI and finally to a public request.
func ResolveLatest(ctx context.Context, repo string) (*Release, string, error) {
	var lastErr error
	for _, cand := range tokenCandidates() {
		r, err := LatestRelease(ctx, repo, cand)
		if err == nil {
			return r, cand, nil
		}
		lastErr = err
	}
	return nil, "", lastErr
}

// LatestRelease fetches the most recent published release for repo
// ("owner/name"). If token is non-empty it is sent as a bearer credential,
// which is required for private repositories.
func LatestRelease(ctx context.Context, repo, token string) (*Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", binaryName+"-selfupdate")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query GitHub releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		if token == "" {
			return nil, fmt.Errorf("no releases found for %s (if it is a private repo, set GH_TOKEN/GITHUB_TOKEN or run `gh auth login`)", repo)
		}
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

// DownloadAsset downloads asset to destPath, authenticating with token when set
// (required for private repos). It uses the API asset endpoint with
// Accept: application/octet-stream so GitHub redirects to a signed URL. The
// caller controls timeouts via ctx (pass a generous deadline — GUI bundles are
// larger than the CLI binary). It verifies the byte count against asset.Size
// when known. Used by the GUI self-updater, which downloads a zip bundle rather
// than swapping a bare binary.
func DownloadAsset(ctx context.Context, asset *Asset, token, destPath string) error {
	downloadURL := asset.URL
	if downloadURL == "" {
		downloadURL = asset.BrowserDownloadURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", binaryName+"-selfupdate")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := (&http.Client{}).Do(req) // ctx bounds the transfer
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %s", resp.Status)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		return fmt.Errorf("failed to write download: %w", copyErr)
	}
	if closeErr != nil {
		return closeErr
	}
	if asset.Size > 0 && written != asset.Size {
		return fmt.Errorf("downloaded %d bytes, expected %d", written, asset.Size)
	}
	return nil
}

// Apply downloads asset and replaces the executable at exePath with it. The
// swap renames the running binary to "<exe>.old" before moving the new binary
// into place, which is the only approach that works while the binary is
// executing on Windows. On success it best-effort removes the ".old" file.
//
// The download uses the API asset endpoint with Accept: application/octet-stream
// so it works for private repos when token is set; GitHub redirects to a signed
// URL and Go drops the Authorization header on the cross-host redirect.
func Apply(ctx context.Context, asset *Asset, token, exePath string) error {
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

	downloadURL := asset.URL
	if downloadURL == "" {
		downloadURL = asset.BrowserDownloadURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		tmp.Close()
		return err
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", binaryName+"-selfupdate")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

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
	rel, token, err := ResolveLatest(ctx, repo)
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
		fmt.Fprintf(out, "Run '%s update' to install it.\n", binaryName)
		return nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot locate current executable: %w", err)
	}
	if resolved, rerr := filepath.EvalSymlinks(exePath); rerr == nil {
		exePath = resolved
	}

	fmt.Fprintf(out, "Downloading %s...\n", assetName)
	if err := Apply(ctx, asset, token, exePath); err != nil {
		return err
	}

	fmt.Fprintf(out, "Updated to %s. Restart %s to use the new version.\n", rel.TagName, binaryName)
	return nil
}
