# Playback Progress Tracking Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Track video playback progress via MPV IPC and sync with Plex server for cross-device resume.

**Architecture:** New `internal/progress` package handles MPV IPC communication and Plex timeline API updates. The player package gains IPC socket support and resume flags. Main.go adds resume prompts before playback.

**Tech Stack:** Go stdlib (net for Unix sockets), existing Plex HTTP client patterns, MPV JSON IPC protocol.

---

## Task 1: Add ViewOffset to MediaItem

**Files:**
- Modify: `internal/plex/client.go:42-59`
- Modify: `internal/plex/client.go:322-344` (mediaResp struct)

**Step 1: Add ViewOffset and ViewCount fields to MediaItem**

```go
type MediaItem struct {
	Key         string
	Title       string
	Year        int
	Type        string // movie, show, season, episode
	Summary     string
	Rating      float64
	Duration    int
	FilePath    string
	RclonePath  string
	ParentTitle string // For episodes: show name
	GrandTitle  string // For episodes: season name
	Index       int64  // Episode or season number
	ParentIndex int64  // Season number for episodes
	Thumb       string // Poster/thumbnail URL path
	ServerName  string // Name of the Plex server this item belongs to
	ServerURL   string // URL of the Plex server this item belongs to
	ViewOffset  int    // Playback position in milliseconds (0 if not started)
	ViewCount   int    // Number of times fully watched
}
```

**Step 2: Update mediaResp struct to include viewOffset and viewCount**

In `GetMediaFromSection`, update the Metadata struct:

```go
var mediaResp struct {
	MediaContainer struct {
		Metadata []struct {
			Key              string   `json:"key"`
			Title            string   `json:"title"`
			Year             *int     `json:"year"`
			Summary          *string  `json:"summary"`
			Rating           *float32 `json:"rating"`
			Duration         *int     `json:"duration"`
			Thumb            *string  `json:"thumb"`
			GrandparentTitle *string  `json:"grandparentTitle"`
			ParentTitle      *string  `json:"parentTitle"`
			Index            *int     `json:"index"`
			ParentIndex      *int     `json:"parentIndex"`
			ViewOffset       *int     `json:"viewOffset"`
			ViewCount        *int     `json:"viewCount"`
			Media            []struct {
				Part []struct {
					File *string `json:"file"`
				} `json:"Part"`
			} `json:"Media"`
		} `json:"Metadata"`
	} `json:"MediaContainer"`
}
```

**Step 3: Map ViewOffset and ViewCount when building MediaItem**

Add to both movie and episode processing blocks:

```go
ViewOffset: valueOrZeroInt(metadata.ViewOffset),
ViewCount:  valueOrZeroInt(metadata.ViewCount),
```

**Step 4: Verify build compiles**

Run: `go build ./...`
Expected: Build succeeds

**Step 5: Commit**

```bash
git add internal/plex/client.go
git commit -m "feat(plex): add ViewOffset and ViewCount to MediaItem"
```

---

## Task 2: Add Plex Timeline API Method

**Files:**
- Modify: `internal/plex/client.go`
- Create: `internal/plex/client_test.go` (if not exists)

**Step 1: Write test for UpdateTimeline**

Create or add to `internal/plex/client_test.go`:

```go
package plex

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpdateTimeline(t *testing.T) {
	// Create a mock Plex server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and path
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/:/timeline" {
			t.Errorf("expected /:/timeline, got %s", r.URL.Path)
		}

		// Verify query parameters
		q := r.URL.Query()
		if q.Get("ratingKey") != "12345" {
			t.Errorf("expected ratingKey=12345, got %s", q.Get("ratingKey"))
		}
		if q.Get("state") != "playing" {
			t.Errorf("expected state=playing, got %s", q.Get("state"))
		}
		if q.Get("time") != "60000" {
			t.Errorf("expected time=60000, got %s", q.Get("time"))
		}
		if q.Get("duration") != "7200000" {
			t.Errorf("expected duration=7200000, got %s", q.Get("duration"))
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := New(server.URL, "test-token")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	err = client.UpdateTimeline("12345", "playing", 60000, 7200000)
	if err != nil {
		t.Fatalf("UpdateTimeline failed: %v", err)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/plex/... -run TestUpdateTimeline -v`
Expected: FAIL with "UpdateTimeline not defined"

**Step 3: Implement UpdateTimeline method**

Add to `internal/plex/client.go`:

```go
// UpdateTimeline reports playback progress to the Plex server.
// This updates the resume position and shows "Now Playing" on the Plex dashboard.
// state should be "playing", "paused", or "stopped".
// timeMs is the current position in milliseconds.
// durationMs is the total duration in milliseconds.
func (c *Client) UpdateTimeline(ratingKey string, state string, timeMs int, durationMs int) error {
	url := fmt.Sprintf("%s/:/timeline?ratingKey=%s&key=/library/metadata/%s&state=%s&time=%d&duration=%d&X-Plex-Token=%s",
		c.serverURL, ratingKey, ratingKey, state, timeMs, durationMs, c.token)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create timeline request: %w", err)
	}

	req.Header.Set("X-Plex-Client-Identifier", "goplexcli")
	req.Header.Set("X-Plex-Product", "GoplexCLI")
	req.Header.Set("X-Plex-Version", "1.0")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to update timeline: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("timeline update failed with status %d", resp.StatusCode)
	}

	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/plex/... -run TestUpdateTimeline -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/plex/client.go internal/plex/client_test.go
git commit -m "feat(plex): add UpdateTimeline method for progress reporting"
```

---

## Task 3: Create MPV IPC Client

**Files:**
- Create: `internal/progress/mpv.go`
- Create: `internal/progress/mpv_test.go`

**Step 1: Write test for MPV IPC message parsing**

Create `internal/progress/mpv_test.go`:

```go
package progress

import (
	"encoding/json"
	"testing"
)

func TestParseMPVResponse(t *testing.T) {
	tests := []struct {
		name     string
		response string
		wantData interface{}
		wantErr  bool
	}{
		{
			name:     "time-pos response",
			response: `{"data":125.432,"error":"success"}`,
			wantData: 125.432,
			wantErr:  false,
		},
		{
			name:     "pause response false",
			response: `{"data":false,"error":"success"}`,
			wantData: false,
			wantErr:  false,
		},
		{
			name:     "pause response true",
			response: `{"data":true,"error":"success"}`,
			wantData: true,
			wantErr:  false,
		},
		{
			name:     "error response",
			response: `{"data":null,"error":"property not found"}`,
			wantData: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp mpvResponse
			if err := json.Unmarshal([]byte(tt.response), &resp); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if tt.wantErr {
				if resp.Error == "success" {
					t.Error("expected error, got success")
				}
			} else {
				if resp.Error != "success" {
					t.Errorf("expected success, got %s", resp.Error)
				}
			}
		})
	}
}

func TestBuildMPVCommand(t *testing.T) {
	cmd := buildMPVCommand("get_property", "time-pos")
	expected := `{"command":["get_property","time-pos"]}`

	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	if string(data) != expected {
		t.Errorf("expected %s, got %s", expected, string(data))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/progress/... -v`
Expected: FAIL with "package not found" or "undefined"

**Step 3: Implement MPV IPC types and helpers**

Create `internal/progress/mpv.go`:

```go
// Package progress handles playback progress tracking via MPV IPC and Plex API.
package progress

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// mpvCommand represents a command to send to MPV via IPC.
type mpvCommand struct {
	Command []interface{} `json:"command"`
}

// mpvResponse represents a response from MPV IPC.
type mpvResponse struct {
	Data  interface{} `json:"data"`
	Error string      `json:"error"`
}

// buildMPVCommand creates an MPV IPC command.
func buildMPVCommand(args ...interface{}) mpvCommand {
	return mpvCommand{Command: args}
}

// MPVClient communicates with MPV via IPC socket.
type MPVClient struct {
	socketPath string
	conn       net.Conn
	mu         sync.Mutex
}

// NewMPVClient creates a new MPV IPC client.
func NewMPVClient(socketPath string) *MPVClient {
	return &MPVClient{socketPath: socketPath}
}

// GenerateSocketPath returns a unique socket path for this process.
func GenerateSocketPath() string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf(`\\.\pipe\goplexcli-mpv-%d`, os.Getpid())
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("goplexcli-mpv-%d.sock", os.Getpid()))
}

// Connect establishes a connection to the MPV IPC socket.
func (c *MPVClient) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return nil // Already connected
	}

	// Retry connection a few times (MPV may take a moment to create socket)
	var conn net.Conn
	var err error
	for i := 0; i < 10; i++ {
		conn, err = net.Dial("unix", c.socketPath)
		if err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		return fmt.Errorf("failed to connect to MPV socket: %w", err)
	}

	c.conn = conn
	return nil
}

// Close closes the connection to the MPV IPC socket.
func (c *MPVClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil
	}

	err := c.conn.Close()
	c.conn = nil
	return err
}

// sendCommand sends a command and returns the response.
func (c *MPVClient) sendCommand(cmd mpvCommand) (*mpvResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil, fmt.Errorf("not connected to MPV")
	}

	// Set deadline for read/write
	c.conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Send command
	data, err := json.Marshal(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal command: %w", err)
	}
	data = append(data, '\n')

	if _, err := c.conn.Write(data); err != nil {
		return nil, fmt.Errorf("failed to send command: %w", err)
	}

	// Read response
	reader := bufio.NewReader(c.conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var resp mpvResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &resp, nil
}

// GetTimePos returns the current playback position in seconds.
func (c *MPVClient) GetTimePos() (float64, error) {
	resp, err := c.sendCommand(buildMPVCommand("get_property", "time-pos"))
	if err != nil {
		return 0, err
	}

	if resp.Error != "success" {
		return 0, fmt.Errorf("MPV error: %s", resp.Error)
	}

	pos, ok := resp.Data.(float64)
	if !ok {
		return 0, fmt.Errorf("unexpected time-pos type: %T", resp.Data)
	}

	return pos, nil
}

// GetPaused returns whether playback is paused.
func (c *MPVClient) GetPaused() (bool, error) {
	resp, err := c.sendCommand(buildMPVCommand("get_property", "pause"))
	if err != nil {
		return false, err
	}

	if resp.Error != "success" {
		return false, fmt.Errorf("MPV error: %s", resp.Error)
	}

	paused, ok := resp.Data.(bool)
	if !ok {
		return false, fmt.Errorf("unexpected pause type: %T", resp.Data)
	}

	return paused, nil
}

// GetPlaylistPos returns the current playlist position (0-indexed).
func (c *MPVClient) GetPlaylistPos() (int, error) {
	resp, err := c.sendCommand(buildMPVCommand("get_property", "playlist-pos"))
	if err != nil {
		return 0, err
	}

	if resp.Error != "success" {
		return 0, fmt.Errorf("MPV error: %s", resp.Error)
	}

	pos, ok := resp.Data.(float64)
	if !ok {
		return 0, fmt.Errorf("unexpected playlist-pos type: %T", resp.Data)
	}

	return int(pos), nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/progress/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/progress/mpv.go internal/progress/mpv_test.go
git commit -m "feat(progress): add MPV IPC client for playback tracking"
```

---

## Task 4: Create Progress Tracker

**Files:**
- Create: `internal/progress/tracker.go`
- Create: `internal/progress/tracker_test.go`

**Step 1: Write test for Tracker**

Create `internal/progress/tracker_test.go`:

```go
package progress

import (
	"testing"

	"github.com/joshkerr/goplexcli/internal/plex"
)

func TestTrackerState(t *testing.T) {
	items := []*plex.MediaItem{
		{Key: "/library/metadata/1", Title: "Movie 1", Duration: 7200000},
		{Key: "/library/metadata/2", Title: "Movie 2", Duration: 5400000},
	}

	tracker := NewTracker(items, nil, nil)

	// Initially at position 0
	if tracker.CurrentIndex() != 0 {
		t.Errorf("expected index 0, got %d", tracker.CurrentIndex())
	}

	// Get current media
	current := tracker.CurrentMedia()
	if current.Key != "/library/metadata/1" {
		t.Errorf("expected key /library/metadata/1, got %s", current.Key)
	}

	// Advance to next
	tracker.SetIndex(1)
	if tracker.CurrentIndex() != 1 {
		t.Errorf("expected index 1, got %d", tracker.CurrentIndex())
	}

	current = tracker.CurrentMedia()
	if current.Key != "/library/metadata/2" {
		t.Errorf("expected key /library/metadata/2, got %s", current.Key)
	}
}

func TestExtractRatingKey(t *testing.T) {
	tests := []struct {
		key      string
		expected string
	}{
		{"/library/metadata/12345", "12345"},
		{"/library/metadata/1", "1"},
		{"/library/metadata/999999", "999999"},
	}

	for _, tt := range tests {
		result := extractRatingKey(tt.key)
		if result != tt.expected {
			t.Errorf("extractRatingKey(%s) = %s, want %s", tt.key, result, tt.expected)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/progress/... -run TestTracker -v`
Expected: FAIL with "undefined"

**Step 3: Implement Tracker**

Create `internal/progress/tracker.go`:

```go
package progress

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/joshkerr/goplexcli/internal/plex"
)

// Tracker monitors MPV playback and reports progress to Plex.
type Tracker struct {
	items      []*plex.MediaItem
	mpv        *MPVClient
	plexClient *plex.Client
	index      int
	mu         sync.RWMutex
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// NewTracker creates a new progress tracker.
func NewTracker(items []*plex.MediaItem, mpv *MPVClient, plexClient *plex.Client) *Tracker {
	return &Tracker{
		items:      items,
		mpv:        mpv,
		plexClient: plexClient,
		stopCh:     make(chan struct{}),
	}
}

// CurrentIndex returns the current playlist index.
func (t *Tracker) CurrentIndex() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.index
}

// SetIndex sets the current playlist index.
func (t *Tracker) SetIndex(idx int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if idx >= 0 && idx < len(t.items) {
		t.index = idx
	}
}

// CurrentMedia returns the currently playing media item.
func (t *Tracker) CurrentMedia() *plex.MediaItem {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.index >= 0 && t.index < len(t.items) {
		return t.items[t.index]
	}
	return nil
}

// extractRatingKey extracts the numeric rating key from a Plex media key.
// e.g., "/library/metadata/12345" -> "12345"
func extractRatingKey(key string) string {
	parts := strings.Split(key, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return key
}

// Start begins tracking playback progress.
// It polls MPV every interval and reports to Plex.
func (t *Tracker) Start(ctx context.Context, interval time.Duration) {
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		t.trackLoop(ctx, interval)
	}()
}

// Stop stops the progress tracker.
func (t *Tracker) Stop() {
	close(t.stopCh)
	t.wg.Wait()
}

// trackLoop is the main tracking loop.
func (t *Tracker) trackLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastPos float64
	var lastIndex int = -1

	for {
		select {
		case <-ctx.Done():
			t.reportFinalPosition()
			return
		case <-t.stopCh:
			t.reportFinalPosition()
			return
		case <-ticker.C:
			t.tick(&lastPos, &lastIndex)
		}
	}
}

// tick performs one tracking iteration.
func (t *Tracker) tick(lastPos *float64, lastIndex *int) {
	if t.mpv == nil {
		return
	}

	// Get current playlist position
	playlistPos, err := t.mpv.GetPlaylistPos()
	if err != nil {
		// MPV may have exited
		return
	}

	// Check if playlist position changed
	if playlistPos != *lastIndex {
		// Report final position for previous item
		if *lastIndex >= 0 && *lastIndex < len(t.items) {
			t.reportPosition(*lastIndex, *lastPos, "stopped")
		}
		*lastIndex = playlistPos
		t.SetIndex(playlistPos)
		*lastPos = 0
	}

	// Get current time position
	pos, err := t.mpv.GetTimePos()
	if err != nil {
		return
	}

	// Only report if position changed significantly (>5 seconds)
	if pos-*lastPos > 5 || *lastPos-pos > 5 {
		// Get pause state
		paused, err := t.mpv.GetPaused()
		if err != nil {
			paused = false
		}

		state := "playing"
		if paused {
			state = "paused"
		}

		t.reportPosition(playlistPos, pos, state)
		*lastPos = pos
	}
}

// reportPosition reports the current playback position to Plex.
func (t *Tracker) reportPosition(index int, posSeconds float64, state string) {
	if t.plexClient == nil {
		return
	}

	if index < 0 || index >= len(t.items) {
		return
	}

	media := t.items[index]
	ratingKey := extractRatingKey(media.Key)
	timeMs := int(posSeconds * 1000)

	err := t.plexClient.UpdateTimeline(ratingKey, state, timeMs, media.Duration)
	if err != nil {
		log.Printf("Failed to update timeline: %v", err)
	}
}

// reportFinalPosition reports the final position when playback ends.
func (t *Tracker) reportFinalPosition() {
	if t.mpv == nil || t.plexClient == nil {
		return
	}

	pos, err := t.mpv.GetTimePos()
	if err != nil {
		return
	}

	index := t.CurrentIndex()
	t.reportPosition(index, pos, "stopped")
}

// FormatDuration formats milliseconds as HH:MM:SS or MM:SS.
func FormatDuration(ms int) string {
	totalSecs := ms / 1000
	hours := totalSecs / 3600
	mins := (totalSecs % 3600) / 60
	secs := totalSecs % 60

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, mins, secs)
	}
	return fmt.Sprintf("%d:%02d", mins, secs)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/progress/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/progress/tracker.go internal/progress/tracker_test.go
git commit -m "feat(progress): add Tracker for coordinating MPV and Plex updates"
```

---

## Task 5: Update Player Package for IPC Support

**Files:**
- Modify: `internal/player/player.go`
- Create: `internal/player/player_test.go`

**Step 1: Write test for building MPV args with IPC socket**

Create `internal/player/player_test.go`:

```go
package player

import (
	"testing"
)

func TestBuildMPVArgs(t *testing.T) {
	tests := []struct {
		name       string
		urls       []string
		socketPath string
		startPos   int
		wantSocket bool
		wantStart  bool
	}{
		{
			name:       "basic playback",
			urls:       []string{"http://example.com/video.mp4"},
			socketPath: "",
			startPos:   0,
			wantSocket: false,
			wantStart:  false,
		},
		{
			name:       "with IPC socket",
			urls:       []string{"http://example.com/video.mp4"},
			socketPath: "/tmp/mpv.sock",
			startPos:   0,
			wantSocket: true,
			wantStart:  false,
		},
		{
			name:       "with resume position",
			urls:       []string{"http://example.com/video.mp4"},
			socketPath: "/tmp/mpv.sock",
			startPos:   125,
			wantSocket: true,
			wantStart:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := buildMPVArgs(tt.urls, tt.socketPath, tt.startPos)

			hasSocket := false
			hasStart := false
			for _, arg := range args {
				if len(arg) > 18 && arg[:18] == "--input-ipc-server" {
					hasSocket = true
				}
				if len(arg) > 8 && arg[:8] == "--start=" {
					hasStart = true
				}
			}

			if hasSocket != tt.wantSocket {
				t.Errorf("socket flag: got %v, want %v", hasSocket, tt.wantSocket)
			}
			if hasStart != tt.wantStart {
				t.Errorf("start flag: got %v, want %v", hasStart, tt.wantStart)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/player/... -run TestBuildMPVArgs -v`
Expected: FAIL with "buildMPVArgs undefined"

**Step 3: Refactor player.go to support IPC and resume**

Update `internal/player/player.go`:

```go
// Package player provides media playback functionality using external players.
// It supports playing single files or multiple files as a playlist using mpv.
package player

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

// PlaybackOptions configures MPV playback behavior.
type PlaybackOptions struct {
	SocketPath string // IPC socket path for progress tracking (empty to disable)
	StartPos   int    // Start position in seconds (0 to start from beginning)
}

// MPVPlayer implements the Player interface using mpv media player.
// It provides high-quality media playback with seeking support.
type MPVPlayer struct {
	// Path is the path to the mpv executable. If empty, "mpv" is used.
	Path string
}

// NewMPVPlayer creates a new MPVPlayer with the specified path.
// If path is empty, the system PATH will be searched for mpv.
func NewMPVPlayer(path string) *MPVPlayer {
	return &MPVPlayer{Path: path}
}

// Play plays a single media URL.
func (p *MPVPlayer) Play(ctx context.Context, url string) error {
	return p.PlayMultiple(ctx, []string{url})
}

// PlayMultiple plays multiple URLs as a playlist.
// Users can navigate between items using 'n' (next) in mpv.
func (p *MPVPlayer) PlayMultiple(ctx context.Context, urls []string) error {
	if len(urls) == 0 {
		return fmt.Errorf("no stream URLs provided")
	}
	return playWithMPV(p.getPath(), urls, PlaybackOptions{})
}

// PlayWithOptions plays multiple URLs with custom options.
func (p *MPVPlayer) PlayWithOptions(ctx context.Context, urls []string, opts PlaybackOptions) error {
	if len(urls) == 0 {
		return fmt.Errorf("no stream URLs provided")
	}
	return playWithMPV(p.getPath(), urls, opts)
}

// IsAvailable checks if mpv is available on the system.
func (p *MPVPlayer) IsAvailable() bool {
	_, err := exec.LookPath(p.getPath())
	return err == nil
}

// getPath returns the mpv path, defaulting to "mpv" if not set.
func (p *MPVPlayer) getPath() string {
	if p.Path == "" {
		return "mpv"
	}
	return p.Path
}

// buildMPVArgs constructs the argument list for MPV.
func buildMPVArgs(urls []string, socketPath string, startPos int) []string {
	args := []string{
		"--force-seekable=yes",
		"--hr-seek=yes",
	}

	// Add IPC socket if specified
	if socketPath != "" {
		args = append(args, fmt.Sprintf("--input-ipc-server=%s", socketPath))
	} else {
		// Only disable resume playback if we're not tracking
		args = append(args, "--no-resume-playback")
	}

	// Add start position if specified
	if startPos > 0 {
		args = append(args, fmt.Sprintf("--start=%d", startPos))
	}

	args = append(args, urls...)
	return args
}

// playWithMPV is a helper function that executes mpv with the given arguments
func playWithMPV(mpvPath string, streamURLs []string, opts PlaybackOptions) error {
	if mpvPath == "" {
		mpvPath = "mpv"
	}

	// Check if mpv is available
	if _, err := exec.LookPath(mpvPath); err != nil {
		return fmt.Errorf("mpv not found in PATH. Please install mpv or specify the path in config")
	}

	// Build mpv command
	args := buildMPVArgs(streamURLs, opts.SocketPath, opts.StartPos)

	cmd := exec.Command(mpvPath, args...)

	// Inherit stdin, stdout, stderr for interactive playback
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	// Start mpv
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start mpv: %w", err)
	}

	// Wait for mpv to finish
	if err := cmd.Wait(); err != nil {
		// mpv returns non-zero exit codes for various reasons (user quit, etc.)
		// Don't treat this as an error
		return nil
	}

	return nil
}

// Play launches MPV to play the given URL.
// This is a convenience function that uses the default player.
func Play(streamURL, mpvPath string) error {
	return playWithMPV(mpvPath, []string{streamURL}, PlaybackOptions{})
}

// PlayMultiple launches MPV to play multiple URLs sequentially.
// This is a convenience function that uses the default player.
func PlayMultiple(streamURLs []string, mpvPath string) error {
	if len(streamURLs) == 0 {
		return fmt.Errorf("no stream URLs provided")
	}

	return playWithMPV(mpvPath, streamURLs, PlaybackOptions{})
}

// PlayMultipleWithOptions launches MPV with custom options.
func PlayMultipleWithOptions(streamURLs []string, mpvPath string, opts PlaybackOptions) error {
	if len(streamURLs) == 0 {
		return fmt.Errorf("no stream URLs provided")
	}

	return playWithMPV(mpvPath, streamURLs, opts)
}

// IsAvailable checks if MPV is available on the system.
// This is a convenience function for checking availability.
func IsAvailable(mpvPath string) bool {
	if mpvPath == "" {
		mpvPath = "mpv"
	}

	_, err := exec.LookPath(mpvPath)
	return err == nil
}

// GetDefaultPath returns the default MPV path for the current platform.
func GetDefaultPath() string {
	switch runtime.GOOS {
	case "windows":
		return "mpv.exe"
	default:
		return "mpv"
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/player/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/player/player.go internal/player/player_test.go
git commit -m "feat(player): add IPC socket and resume position support"
```

---

## Task 6: Add Resume Prompt UI

**Files:**
- Create: `internal/ui/resume.go`
- Create: `internal/ui/resume_test.go`

**Step 1: Write test for resume prompt options**

Create `internal/ui/resume_test.go`:

```go
package ui

import (
	"testing"

	"github.com/joshkerr/goplexcli/internal/plex"
	"github.com/joshkerr/goplexcli/internal/progress"
)

func TestFormatResumeOption(t *testing.T) {
	media := &plex.MediaItem{
		Title:    "The Matrix",
		Duration: 8160000, // 2:16:00 in ms
	}

	// Test resume option formatting
	resumeText := formatResumeOption(media, 5025000) // 1:23:45 in ms
	if resumeText == "" {
		t.Error("expected non-empty resume text")
	}

	// Should contain the time
	expected := "Resume from 1:23:45"
	if resumeText != expected {
		t.Errorf("expected %q, got %q", expected, resumeText)
	}
}

func TestHasProgress(t *testing.T) {
	tests := []struct {
		name       string
		viewOffset int
		duration   int
		want       bool
	}{
		{"no progress", 0, 7200000, false},
		{"has progress", 3600000, 7200000, true},
		{"almost complete (95%)", 6840000, 7200000, false}, // Treat as watched
		{"90% progress", 6480000, 7200000, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			media := &plex.MediaItem{
				ViewOffset: tt.viewOffset,
				Duration:   tt.duration,
			}
			got := HasResumableProgress(media)
			if got != tt.want {
				t.Errorf("HasResumableProgress() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		ms   int
		want string
	}{
		{0, "0:00"},
		{60000, "1:00"},
		{3661000, "1:01:01"},
		{7200000, "2:00:00"},
	}

	for _, tt := range tests {
		got := progress.FormatDuration(tt.ms)
		if got != tt.want {
			t.Errorf("FormatDuration(%d) = %q, want %q", tt.ms, got, tt.want)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/... -run TestFormat -v`
Expected: FAIL with "undefined"

**Step 3: Implement resume UI helpers**

Create `internal/ui/resume.go`:

```go
package ui

import (
	"fmt"

	"github.com/joshkerr/goplexcli/internal/plex"
	"github.com/joshkerr/goplexcli/internal/progress"
)

// ResumeChoice represents the user's choice for resuming playback.
type ResumeChoice int

const (
	ResumeFromPosition ResumeChoice = iota
	StartFromBeginning
)

// HasResumableProgress returns true if the media has progress that can be resumed.
// Returns false if no progress or if >95% complete (treat as watched).
func HasResumableProgress(media *plex.MediaItem) bool {
	if media.ViewOffset <= 0 || media.Duration <= 0 {
		return false
	}

	// If >95% complete, treat as watched
	percentComplete := float64(media.ViewOffset) / float64(media.Duration)
	if percentComplete > 0.95 {
		return false
	}

	return true
}

// formatResumeOption formats the resume option text.
func formatResumeOption(media *plex.MediaItem, viewOffset int) string {
	return fmt.Sprintf("Resume from %s", progress.FormatDuration(viewOffset))
}

// ResumePromptOptions contains the options for the resume prompt.
type ResumePromptOptions struct {
	Title       string
	ViewOffset  int // milliseconds
	Duration    int // milliseconds
	FzfPath     string
}

// PromptResume displays a resume prompt using fzf and returns the user's choice.
// Returns ResumeFromPosition if user wants to resume, StartFromBeginning otherwise.
func PromptResume(opts ResumePromptOptions) (ResumeChoice, error) {
	resumeText := fmt.Sprintf("► Resume from %s", progress.FormatDuration(opts.ViewOffset))
	beginningText := "  Start from beginning"

	options := []string{resumeText, beginningText}

	header := fmt.Sprintf("%q has saved progress at %s / %s (%d%%)",
		opts.Title,
		progress.FormatDuration(opts.ViewOffset),
		progress.FormatDuration(opts.Duration),
		opts.ViewOffset*100/opts.Duration,
	)

	selected, err := SelectWithFzf(options, opts.FzfPath, header)
	if err != nil {
		return StartFromBeginning, err
	}

	if selected == resumeText {
		return ResumeFromPosition, nil
	}
	return StartFromBeginning, nil
}

// MultiResumeChoice represents the user's choice when multiple items have progress.
type MultiResumeChoice int

const (
	ResumeAll MultiResumeChoice = iota
	StartAllFromBeginning
	ChooseIndividually
)

// PromptMultiResume displays a prompt when multiple items have progress.
func PromptMultiResume(itemsWithProgress int, totalItems int, fzfPath string) (MultiResumeChoice, error) {
	options := []string{
		"► Resume all from saved positions",
		"  Start all from beginning",
		"  Choose individually for each",
	}

	header := fmt.Sprintf("%d of %d videos have saved progress", itemsWithProgress, totalItems)

	selected, err := SelectWithFzf(options, fzfPath, header)
	if err != nil {
		return StartAllFromBeginning, err
	}

	switch selected {
	case options[0]:
		return ResumeAll, nil
	case options[1]:
		return StartAllFromBeginning, nil
	case options[2]:
		return ChooseIndividually, nil
	default:
		return StartAllFromBeginning, nil
	}
}

// SelectWithFzf runs fzf with the given options and returns the selected item.
func SelectWithFzf(options []string, fzfPath string, header string) (string, error) {
	args := []string{
		"--height=10",
		"--layout=reverse",
		"--no-multi",
		"--ansi",
	}

	if header != "" {
		args = append(args, "--header", header)
	}

	return RunFzfWithArgs(options, fzfPath, args)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/ui/resume.go internal/ui/resume_test.go
git commit -m "feat(ui): add resume prompt for playback with progress"
```

---

## Task 7: Integrate Progress Tracking into Main

**Files:**
- Modify: `cmd/goplexcli/main.go`

**Step 1: Update handleWatchMultiple to support progress tracking**

Modify `handleWatchMultiple` in `cmd/goplexcli/main.go`:

```go
func handleWatchMultiple(cfg *config.Config, mediaItems []*plex.MediaItem) error {
	if len(mediaItems) == 0 {
		return fmt.Errorf("no media items provided")
	}

	// Check if MPV is available
	if !player.IsAvailable(cfg.MPVPath) {
		return fmt.Errorf("mpv is not installed. Please install mpv to watch media")
	}

	// Create Plex client
	client, err := plex.New(cfg.PlexURL, cfg.PlexToken)
	if err != nil {
		return fmt.Errorf("failed to create plex client: %w", err)
	}

	// Check for items with progress
	var itemsWithProgress []*plex.MediaItem
	for _, media := range mediaItems {
		if ui.HasResumableProgress(media) {
			itemsWithProgress = append(itemsWithProgress, media)
		}
	}

	// Determine start positions based on user choice
	startPositions := make([]int, len(mediaItems))

	if len(itemsWithProgress) > 0 {
		if len(mediaItems) == 1 {
			// Single item - prompt for this item
			choice, err := ui.PromptResume(ui.ResumePromptOptions{
				Title:      mediaItems[0].FormatMediaTitle(),
				ViewOffset: mediaItems[0].ViewOffset,
				Duration:   mediaItems[0].Duration,
				FzfPath:    cfg.FzfPath,
			})
			if err != nil {
				fmt.Println(warningStyle.Render("Could not show resume prompt, starting from beginning"))
			} else if choice == ui.ResumeFromPosition {
				startPositions[0] = mediaItems[0].ViewOffset / 1000 // Convert to seconds
			}
		} else {
			// Multiple items - prompt for batch behavior
			choice, err := ui.PromptMultiResume(len(itemsWithProgress), len(mediaItems), cfg.FzfPath)
			if err != nil {
				fmt.Println(warningStyle.Render("Could not show resume prompt, starting from beginning"))
			} else {
				switch choice {
				case ui.ResumeAll:
					for i, media := range mediaItems {
						if ui.HasResumableProgress(media) {
							startPositions[i] = media.ViewOffset / 1000
						}
					}
				case ui.ChooseIndividually:
					for i, media := range mediaItems {
						if ui.HasResumableProgress(media) {
							itemChoice, err := ui.PromptResume(ui.ResumePromptOptions{
								Title:      media.FormatMediaTitle(),
								ViewOffset: media.ViewOffset,
								Duration:   media.Duration,
								FzfPath:    cfg.FzfPath,
							})
							if err == nil && itemChoice == ui.ResumeFromPosition {
								startPositions[i] = media.ViewOffset / 1000
							}
						}
					}
				}
			}
		}
	}

	fmt.Println(infoStyle.Render(fmt.Sprintf("\nPreparing to play %d items...", len(mediaItems))))

	// Get stream URLs for all items
	var streamURLs []string
	for i, media := range mediaItems {
		fmt.Printf("\r\x1b[K%s [%d/%d] %s",
			infoStyle.Render("Getting stream URLs"),
			i+1,
			len(mediaItems),
			media.FormatMediaTitle(),
		)

		streamURL, err := client.GetStreamURL(media.Key)
		if err != nil {
			fmt.Println()
			return fmt.Errorf("failed to get stream URL for %s: %w", media.FormatMediaTitle(), err)
		}
		streamURLs = append(streamURLs, streamURL)
	}
	fmt.Println()

	// Set up progress tracking
	socketPath := progress.GenerateSocketPath()
	mpvClient := progress.NewMPVClient(socketPath)
	tracker := progress.NewTracker(mediaItems, mpvClient, client)

	fmt.Println(successStyle.Render(fmt.Sprintf("✓ Starting playback of %d items...", len(mediaItems))))
	fmt.Println(infoStyle.Render("Use 'n' in MPV to skip to next item"))

	// Determine initial start position (first item)
	initialStart := 0
	if len(startPositions) > 0 {
		initialStart = startPositions[0]
	}

	// Start playback with progress tracking
	opts := player.PlaybackOptions{
		SocketPath: socketPath,
		StartPos:   initialStart,
	}

	// Start MPV in a goroutine so we can connect to IPC
	errCh := make(chan error, 1)
	go func() {
		errCh <- player.PlayMultipleWithOptions(streamURLs, cfg.MPVPath, opts)
	}()

	// Connect to MPV IPC and start tracking
	if err := mpvClient.Connect(); err != nil {
		fmt.Println(warningStyle.Render(fmt.Sprintf("Progress tracking unavailable: %v", err)))
	} else {
		defer mpvClient.Close()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		tracker.Start(ctx, 10*time.Second)
		defer tracker.Stop()
	}

	// Wait for playback to finish
	if err := <-errCh; err != nil {
		return fmt.Errorf("playback failed: %w", err)
	}

	fmt.Println(successStyle.Render("✓ Playback finished"))
	return nil
}
```

**Step 2: Add necessary imports to main.go**

Add to imports:

```go
import (
	// ... existing imports ...
	"github.com/joshkerr/goplexcli/internal/progress"
)
```

**Step 3: Verify build compiles**

Run: `go build ./...`
Expected: Build succeeds

**Step 4: Commit**

```bash
git add cmd/goplexcli/main.go
git commit -m "feat: integrate progress tracking into watch command"
```

---

## Task 8: Update Browse Display to Show Progress

**Files:**
- Modify: `internal/plex/client.go` (FormatMediaTitle method)

**Step 1: Update FormatMediaTitle to show progress indicators**

Modify `FormatMediaTitle` in `internal/plex/client.go`:

```go
// FormatMediaTitle returns a formatted title for display
func (m *MediaItem) FormatMediaTitle() string {
	var title string
	switch m.Type {
	case "movie":
		if m.Year > 0 {
			title = fmt.Sprintf("%s (%d)", m.Title, m.Year)
		} else {
			title = m.Title
		}
	case "episode":
		title = fmt.Sprintf("%s - S%02dE%02d - %s", m.ParentTitle, m.ParentIndex, m.Index, m.Title)
	default:
		title = m.Title
	}

	// Add server name if present and multiple servers might be in use
	if m.ServerName != "" && m.ServerName != "Default Server" {
		title = fmt.Sprintf("[%s] %s", m.ServerName, title)
	}

	// Add progress indicator
	if m.Duration > 0 {
		if m.ViewCount > 0 {
			// Watched
			title = fmt.Sprintf("%s ✓", title)
		} else if m.ViewOffset > 0 {
			// Calculate percentage
			pct := m.ViewOffset * 100 / m.Duration
			if pct > 95 {
				// Nearly complete, show as watched
				title = fmt.Sprintf("%s ✓", title)
			} else {
				// In progress
				title = fmt.Sprintf("%s ▶ %d%%", title, pct)
			}
		}
	}

	return title
}
```

**Step 2: Verify build compiles**

Run: `go build ./...`
Expected: Build succeeds

**Step 3: Commit**

```bash
git add internal/plex/client.go
git commit -m "feat(plex): show progress indicators in media titles"
```

---

## Task 9: End-to-End Testing

**Files:**
- No new files, manual testing

**Step 1: Build the application**

Run: `go build -o goplexcli ./cmd/goplexcli`
Expected: Build succeeds

**Step 2: Test browse displays progress**

Run: `./goplexcli browse`
Expected: Media with progress shows `▶ X%`, watched shows `✓`

**Step 3: Test single video resume prompt**

1. Start watching a video, quit partway through
2. Select the same video again
Expected: Prompt appears asking "Resume from X?" or "Start from beginning"

**Step 4: Test progress tracking during playback**

1. Start watching a video
2. Watch for 30+ seconds
3. Quit MPV
4. Check Plex web UI for the item
Expected: Progress shows on Plex dashboard/item

**Step 5: Commit final verification**

```bash
git add -A
git commit -m "chore: verify playback progress tracking feature complete"
```

---

## Summary

| Task | Component | Description |
|------|-----------|-------------|
| 1 | plex/client.go | Add ViewOffset/ViewCount to MediaItem |
| 2 | plex/client.go | Add UpdateTimeline API method |
| 3 | progress/mpv.go | Create MPV IPC client |
| 4 | progress/tracker.go | Create progress Tracker |
| 5 | player/player.go | Add IPC socket and resume support |
| 6 | ui/resume.go | Add resume prompt UI |
| 7 | main.go | Integrate progress tracking |
| 8 | plex/client.go | Show progress in browse display |
| 9 | Manual | End-to-end testing |

Total: 9 tasks, approximately 8 commits
