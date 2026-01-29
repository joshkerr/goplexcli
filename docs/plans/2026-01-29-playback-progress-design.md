# Playback Progress Tracking Design

Track video playback progress and sync with Plex server for cross-device resume.

## Overview

Enable users to start watching on one device and continue on another by syncing playback progress directly with the Plex server.

## Architecture

```
┌─────────────┐     IPC Socket      ┌──────────────┐
│     MPV     │◄───────────────────►│   progress   │
│  (player)   │   position updates  │   package    │
└─────────────┘                     └──────┬───────┘
                                           │
                                           │ HTTP API
                                           ▼
                                    ┌──────────────┐
                                    │ Plex Server  │
                                    │  (timeline)  │
                                    └──────────────┘
```

### Flow

1. Before playback: Query Plex for existing progress on the media item
2. If progress exists: Prompt user "Resume from X?" or "Start from beginning"
3. Start MPV with IPC socket enabled, optionally with `--start=` flag for resume
4. During playback: Poll MPV every ~10 seconds, send position to Plex timeline API
5. On playback end: Send final position update

## MPV IPC Integration

### Socket Setup

MPV supports a JSON-based IPC protocol via Unix sockets (macOS/Linux) or named pipes (Windows). Launch MPV with:

```
--input-ipc-server=/tmp/goplexcli-mpv-<pid>.sock
```

### IPC Commands

```json
// Get current playback position (seconds)
{"command": ["get_property", "time-pos"]}
// Response: {"data": 125.432, "error": "success"}

// Get pause state
{"command": ["get_property", "pause"]}
// Response: {"data": false, "error": "success"}
```

### Resume

Use `--start=+<seconds>` flag when launching MPV for resume functionality.

### Polling Strategy

- Start a goroutine that polls every 10 seconds
- Only send to Plex if position changed significantly (>5 seconds) or state changed (play/pause)
- Stop polling when MPV process exits
- Clean up socket file on exit

### Multi-Video Handling

When playing multiple videos (playlist), MPV fires `end-file` events. Listen for these to know when one video ends and the next begins, updating the correct media item.

## Plex API Integration

### Fetching Progress

Add `ViewOffset` field to `MediaItem`:

```go
type MediaItem struct {
    // ... existing fields ...
    ViewOffset  int  // milliseconds into playback (0 if not started)
    ViewCount   int  // number of times fully watched
}
```

### Reporting Progress

Use Plex timeline endpoint:

```
POST /:/timeline
?ratingKey=12345           // media item ID
&key=/library/metadata/12345
&state=playing             // or "paused", "stopped"
&time=125000               // position in milliseconds
&duration=7200000          // total duration in milliseconds
&X-Plex-Token=xxx
```

This endpoint:
- Updates the resume position
- Marks as "watched" when you hit ~90% completion
- Shows "Now Playing" on the Plex dashboard

### New Methods

```go
func (c *Client) GetViewOffset(mediaKey string) (int, error)
func (c *Client) UpdateTimeline(mediaKey string, state string, timeMs int, durationMs int) error
```

## User Experience

### Resume Prompt

When selecting a video with existing progress:

```
"The Matrix" has saved progress at 1:23:45 / 2:16:00 (61%)

  ► Resume from 1:23:45
    Start from beginning
```

Use fzf for consistency. Skip prompt if no progress exists.

### Progress Display in Browse List

```
  The Matrix (2:16:00)                    # no progress
  Inception (45:32 / 2:28:00) ▶           # in progress
  Interstellar ✓                          # watched
```

- `▶` indicates resumable
- `✓` indicates completed (watched)

### Multi-Select Behavior

When multiple videos are selected with progress:
- Show summary: "2 of 5 videos have saved progress. Resume all?"
- Options: "Resume all", "Start all from beginning", "Choose individually"

## Error Handling

### Network Failures

- If Plex API call fails during playback, log error but don't interrupt viewing
- Queue failed updates and retry on next successful connection
- If Plex unreachable at start, skip resume prompt and play without tracking

### MPV IPC Failures

- If socket connection fails, fall back to "no tracking" mode
- Log warning: "Could not connect to MPV IPC, progress won't be saved"
- Playback still works, just no progress sync

### Process Crashes

- MPV crash: Last successful update (within 10 seconds) preserved on Plex
- goplexcli crash: MPV keeps playing, no more updates sent

### Stale Progress

- If progress >95% complete, treat as "watched" and don't offer resume
- Plex handles this automatically (marks watched at ~90%)

### Multiple Plex Servers

- Progress stored per-server (Plex handles this)
- No special handling needed since `MediaItem` tracks `ServerURL`

## Implementation

### Files to Create/Modify

| File | Change |
|------|--------|
| `internal/progress/progress.go` | New - MPV IPC client + Plex timeline sync |
| `internal/plex/client.go` | Add `ViewOffset` field, `UpdateTimeline()` method |
| `internal/player/player.go` | Add IPC socket flag, resume start position |
| `cmd/goplexcli/main.go` | Add resume prompt, wire up progress tracking |

### Dependencies

No new external dependencies needed (Go stdlib has Unix socket support).

## Decisions

| Aspect | Decision | Rationale |
|--------|----------|-----------|
| Storage | Plex server | Native cross-device sync, no custom infrastructure |
| Position capture | MPV IPC socket | Most accurate, survives crashes |
| Update frequency | Every ~10 seconds | Balance between accuracy and API load |
| Resume behavior | Prompt user | Gives control without being automatic |
| Fallback | Graceful degradation | Playback works even if tracking fails |
