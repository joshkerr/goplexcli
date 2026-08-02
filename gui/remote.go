package main

// Remote download servers: the GUI-side client for `goplexcli serve` daemons
// running on other machines. Downloads sent to a remote server run there —
// with that machine's rclone, download directory, and sorting settings — and
// keep going after this GUI closes. Their progress is fetched by polling
// (ListRemoteDownloads, driven by a frontend interval) rather than events,
// and merged into the same Downloads panel with an origin badge.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/joshkerr/goplexcli/internal/config"
	"github.com/joshkerr/goplexcli/internal/dlengine"
	"github.com/joshkerr/goplexcli/internal/dlserver"
)

// remoteIDSep joins a server name and its job ID into one globally unique ID
// ("media-server!dl_3_x.mkv"), so a single ID string routes cancel requests
// back to the right daemon. Server names are validated to never contain it.
const remoteIDSep = "!"

// remoteRequestTimeout bounds every call to a remote daemon. The API answers
// from memory, so anything slower than this is effectively offline.
const remoteRequestTimeout = 5 * time.Second

// remoteClient is shared by all remote-daemon requests; per-request deadlines
// come from contexts so a stuck server can't wedge a poll cycle.
var remoteClient = &http.Client{}

// RemoteServerDTO is a registered remote download server, as edited in the
// Settings view.
type RemoteServerDTO struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Token   string `json:"token"`
	Enabled bool   `json:"enabled"`
}

// RemoteTestDTO is the result of probing a remote server from the Settings
// view, so misconfiguration (wrong URL vs. wrong token) is diagnosed before
// any download is sent.
type RemoteTestDTO struct {
	Online   bool   `json:"online"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	Platform string `json:"platform"`
	Error    string `json:"error,omitempty"`
}

// ---- Bound methods: server registry ----

// ListRemoteServers returns the registered remote download servers.
func (a *App) ListRemoteServers() []RemoteServerDTO {
	cfg := a.config()
	out := make([]RemoteServerDTO, 0, len(cfg.RemoteServers))
	for _, r := range cfg.RemoteServers {
		out = append(out, RemoteServerDTO{Name: r.Name, URL: r.URL, Token: r.Token, Enabled: r.Enabled})
	}
	return out
}

// SaveRemoteServers replaces the registered remote download servers. Names are
// cleaned of the ID separator, default to the URL's host when blank, and are
// de-duplicated so each server can be addressed unambiguously.
func (a *App) SaveRemoteServers(servers []RemoteServerDTO) error {
	cfg := a.config()
	seen := map[string]bool{}
	list := make([]config.RemoteServer, 0, len(servers))
	for _, s := range servers {
		r := config.RemoteServer{
			Name:    strings.ReplaceAll(strings.TrimSpace(s.Name), remoteIDSep, ""),
			URL:     strings.TrimSpace(s.URL),
			Token:   strings.TrimSpace(s.Token),
			Enabled: s.Enabled,
		}
		if r.Name == "" {
			r.Name = hostOfURL(r.URL)
		}
		base := r.Name
		for n := 2; seen[r.Name]; n++ {
			r.Name = fmt.Sprintf("%s-%d", base, n)
		}
		seen[r.Name] = true
		if err := r.Validate(); err != nil {
			return fmt.Errorf("server %q: %w", r.Name, err)
		}
		list = append(list, r)
	}
	cfg.RemoteServers = list
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	a.reloadConfig()
	return nil
}

// hostOfURL extracts a display name from a URL ("http://nas:47821" → "nas").
func hostOfURL(rawURL string) string {
	rest, ok := strings.CutPrefix(rawURL, "http://")
	if !ok {
		rest, _ = strings.CutPrefix(rawURL, "https://")
	}
	if i := strings.IndexAny(rest, ":/"); i >= 0 {
		rest = rest[:i]
	}
	if rest == "" {
		return "server"
	}
	return rest
}

// TestRemoteServer probes a daemon: ping (no auth) to check reachability, then
// an authenticated list to check the token. A reachable daemon with a bad
// token reports the auth error rather than "offline" — more actionable.
func (a *App) TestRemoteServer(url, token string) RemoteTestDTO {
	ctx, cancel := context.WithTimeout(context.Background(), remoteRequestTimeout)
	defer cancel()

	var ping dlserver.PingResponse
	if err := remoteGet(ctx, url, "/api/v1/ping", "", &ping); err != nil {
		return RemoteTestDTO{Error: err.Error()}
	}
	out := RemoteTestDTO{Online: true, Name: ping.Name, Version: ping.Version, Platform: ping.Platform}
	var jobs []dlengine.Progress
	if err := remoteGet(ctx, url, "/api/v1/downloads", token, &jobs); err != nil {
		out.Error = err.Error()
	}
	return out
}

// ---- Bound methods: sending downloads ----

// DownloadRemote sends the given cached items (by Plex key) to a registered
// remote server, which downloads them on its own machine. The call returns as
// soon as the server accepts the batch; progress arrives via polling.
//
// onExisting matches Download's policy for files already present on the
// server: "skip" drops those transfers, "replace" deletes first, "" overwrites.
func (a *App) DownloadRemote(keys []string, serverName string, onExisting string) error {
	if len(keys) == 0 {
		return fmt.Errorf("no items to download")
	}
	rs, err := a.remoteServerByName(serverName)
	if err != nil {
		return err
	}
	c := a.media()
	if c == nil {
		return fmt.Errorf("media cache is empty")
	}
	items, missing, err := resolveItems(c, keys)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return fmt.Errorf("%d of %d items not found in cache", len(missing), len(keys))
	}

	var reqs []dlserver.DownloadRequest
	for _, it := range items {
		if it.RclonePath == "" {
			continue // no rclone path; skip silently, matching Download
		}
		// Episodes carry the show title: that's the name rclonecp's poster
		// search needs, and it routes the file into TV Shows/<show>/.
		title := it.Title
		if it.Type == "episode" && it.ParentTitle != "" {
			title = it.ParentTitle
		}
		reqs = append(reqs, dlserver.DownloadRequest{
			Src:        it.RclonePath,
			Type:       it.Type,
			Show:       it.ParentTitle,
			Title:      title,
			Year:       it.Year,
			OnExisting: onExisting,
		})
	}
	if len(reqs) == 0 {
		return fmt.Errorf("none of the selected items have a downloadable path")
	}

	ctx, cancel := context.WithTimeout(context.Background(), remoteRequestTimeout)
	defer cancel()
	var res dlserver.SubmitResult
	if err := remotePost(ctx, rs.URL, "/api/v1/downloads", rs.Token, reqs, &res); err != nil {
		return fmt.Errorf("failed to send to %s: %w", rs.Name, err)
	}
	return nil
}

func (a *App) remoteServerByName(name string) (config.RemoteServer, error) {
	for _, r := range a.config().GetEnabledRemoteServers() {
		if r.Name == name {
			return r, nil
		}
	}
	return config.RemoteServer{}, fmt.Errorf("unknown remote server %q", name)
}

// ---- Bound methods: tracking remote jobs ----

// ListRemoteDownloads fetches the job lists of every enabled remote server
// concurrently and merges them, newest first. Job IDs are namespaced with the
// server name and Origin is set so the Downloads panel can badge them.
// Unreachable servers simply contribute nothing — the poll cycle must degrade
// silently when a machine is off.
func (a *App) ListRemoteDownloads() []DownloadProgress {
	servers := a.config().GetEnabledRemoteServers()
	if len(servers) == 0 {
		return []DownloadProgress{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), remoteRequestTimeout)
	defer cancel()

	var mu sync.Mutex
	var out []DownloadProgress
	var wg sync.WaitGroup
	for _, rs := range servers {
		wg.Add(1)
		go func(rs config.RemoteServer) {
			defer wg.Done()
			var jobs []dlengine.Progress
			if err := remoteGet(ctx, rs.URL, "/api/v1/downloads", rs.Token, &jobs); err != nil {
				return
			}
			for i := range jobs {
				jobs[i].ID = rs.Name + remoteIDSep + jobs[i].ID
				jobs[i].Origin = rs.Name
			}
			mu.Lock()
			out = append(out, jobs...)
			mu.Unlock()
		}(rs)
	}
	wg.Wait()

	sort.Slice(out, func(i, j int) bool {
		if out[i].QueuedAt != out[j].QueuedAt {
			return out[i].QueuedAt > out[j].QueuedAt
		}
		return out[i].Seq > out[j].Seq
	})
	if out == nil {
		out = []DownloadProgress{}
	}
	return out
}

// CancelRemoteDownload aborts a job on whichever server its namespaced ID
// names.
func (a *App) CancelRemoteDownload(id string) error {
	serverName, jobID, ok := strings.Cut(id, remoteIDSep)
	if !ok {
		return fmt.Errorf("malformed remote download id %q", id)
	}
	rs, err := a.remoteServerByName(serverName)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteRequestTimeout)
	defer cancel()
	return remotePost(ctx, rs.URL, "/api/v1/downloads/"+jobID+"/cancel", rs.Token, nil, nil)
}

// ClearRemoteDownloadHistory removes finished entries on every enabled remote
// server. Unreachable servers are skipped; their history clears next time.
func (a *App) ClearRemoteDownloadHistory() error {
	ctx, cancel := context.WithTimeout(context.Background(), remoteRequestTimeout)
	defer cancel()
	var wg sync.WaitGroup
	for _, rs := range a.config().GetEnabledRemoteServers() {
		wg.Add(1)
		go func(rs config.RemoteServer) {
			defer wg.Done()
			_ = remotePost(ctx, rs.URL, "/api/v1/downloads/clear-finished", rs.Token, nil, nil)
		}(rs)
	}
	wg.Wait()
	return nil
}

// ---- HTTP helpers ----

func remoteGet(ctx context.Context, baseURL, path, token string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+path, nil)
	if err != nil {
		return err
	}
	return remoteDo(req, token, into)
}

func remotePost(ctx context.Context, baseURL, path, token string, body, into any) error {
	var rdr io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return remoteDo(req, token, into)
}

func remoteDo(req *http.Request, token string, into any) error {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := remoteClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		// The daemon's errors are {"error": "..."} — surface the message.
		var e struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&e) == nil && e.Error != "" {
			return fmt.Errorf("%s", e.Error)
		}
		return fmt.Errorf("server returned %s", resp.Status)
	}
	if into == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(into)
}
