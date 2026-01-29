# CLI Command Simplification Design

## Overview

Replace the `browse` command with direct `movie`, `tv`, and `queue` commands to streamline the user experience.

## Problem

Current flow requires an extra step:
```
goplexcli browse → Select "Movies" or "TV Shows" → Pick item → Choose action
```

## Solution

Direct commands that skip media type selection:
```
goplexcli movie   # Shows movie list directly
goplexcli tv      # Shows TV show list directly
goplexcli queue   # View and manage queued items
```

## Command Structure

### Removed
- `browse` command (deleted entirely)

### Added
| Command | Description |
|---------|-------------|
| `movie` | Filter to movies, show selection, prompt action |
| `tv` | Filter to TV episodes, show selection, prompt action |
| `queue` | View/manage download queue |

### Unchanged
- `login`, `cache`, `config`, `stream`, `server`, `version`

## Command Behavior

### `goplexcli movie`
1. Load media cache (error if empty → suggest `cache update`)
2. Filter to `item.Type == "movie"`
3. If queue has items, show "View Queue (N items)" at top of list
4. Show movie list with preview via fzf
5. On selection → action prompt (Watch/Download/Add to Queue/Stream/Cancel)

### `goplexcli tv`
1. Load media cache
2. Filter to `item.Type == "episode"`
3. If queue has items, show "View Queue (N items)" at top
4. Show episode list with preview
5. On selection → action prompt

### `goplexcli queue`
1. Load queue
2. If empty → "Queue is empty" and exit
3. Show queued items in fzf list
4. Actions: Watch/Download/Remove/Clear Queue/Cancel

### Queue Actions
- **Watch** - Play selected items
- **Download** - Download selected items
- **Remove** - Remove selected from queue
- **Clear Queue** - Empty entire queue (with confirmation)
- **Cancel** - Exit

After Watch/Download/Remove, return to queue view if items remain.

## Implementation Changes

### `cmd/goplexcli/main.go`
1. Remove `browseCmd` and `runBrowse()` function
2. Add `movieCmd` with `runMovie()` function
3. Add `tvCmd` with `runTV()` function
4. Add `queueCmd` with `runQueue()` function
5. Update `init()` to register new commands

### Shared Logic
Extract common flow into helper:
```go
func runMediaBrowser(mediaType string) error
```
- `runMovie()` calls `runMediaBrowser("movie")`
- `runTV()` calls `runMediaBrowser("episode")`

### `internal/ui/fzf.go`
1. Remove `SelectMediaTypeWithQueue()` - no longer needed
2. Add `SelectMediaWithQueueOption()` - selection with queue item at top
3. Add `SelectQueueItems()` - for queue command UI

### No Changes Needed
- `internal/cache/cache.go`
- `internal/queue/queue.go`
- `internal/plex/client.go`

## Error Handling
- No cache → "Run `goplexcli cache update` first"
- No movies/episodes → "No [movies|TV shows] in cache"
- No fzf → Fall back to manual numbered selection

## User Flow Comparison

| Before | After |
|--------|-------|
| `goplexcli browse` → Select type → Pick | `goplexcli movie` → Pick |
| `goplexcli browse` → Select type → Pick | `goplexcli tv` → Pick |
| `goplexcli browse` → View Queue | `goplexcli queue` |
