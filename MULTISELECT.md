# Multi-Select Feature

This document describes the multi-select functionality added to GoplexCLI, allowing users to select and process multiple media items at once.

## Overview

The multi-select feature enables users to:
- **Select multiple items** in the fzf interface using TAB
- **Download multiple files** sequentially with progress tracking
- **Watch multiple items** in sequence using MPV's playlist feature
- **Stream only one item** (multi-select uses first item for streaming)

## Usage

### Selecting Multiple Items

When browsing media with `goplexcli browse`:

1. Use **TAB** to select/deselect individual items
2. Use **Shift+TAB** to deselect items
3. Press **Enter** to confirm your selection
4. Choose an action (Watch, Download, or Stream)

### Watch Multiple Items

When you select multiple items and choose "Watch":
- All items are queued in MPV as a playlist
- MPV will play each item sequentially
- Use **'n'** in MPV to skip to the next item
- Use **'p'** in MPV to go back to the previous item
- Use **'q'** to quit playback

**Example output:**
```
Preparing to play 3 items...
Getting stream URLs [3/3] Show Name - S01E03 - Episode Title
✓ Starting playback of 3 items...
Use 'n' in MPV to skip to next item
```

### Download Multiple Items

When you select multiple items and choose "Download":
- All items are downloaded sequentially
- Progress is shown for each download using the Bubble Tea UI
- If an item has no rclone path, it's skipped with a warning
- Downloads continue even if one fails

**Example output:**
```
Preparing to download 3 items...
  - Movie Title (2023)
  - Another Movie (2024)
  - Third Movie (2022)

✓ Starting download of 3 items...
[Progress UI shows each download]
✓ All downloads complete
```

### Stream with Multiple Selection

Streaming only supports a single item. If you select multiple items:
```
Note: Stream only supports single selection, using first item
```

## Technical Implementation

### Modified Files

#### 1. `internal/ui/fzf.go`
**Function:** `SelectMediaWithPreview()`
- **Changed return type:** `int` → `[]int` to support multiple indices
- **Added `--multi` flag** to fzf command
- **Parses multiple lines** from fzf output (one per selected item)
- **Returns slice of indices** for selected items

#### 2. `internal/player/player.go`
**New function:** `PlayMultiple(streamURLs []string, mpvPath string)`
- Accepts multiple stream URLs
- Passes all URLs to MPV, which creates a playlist
- MPV handles sequential playback automatically

#### 3. `internal/download/download.go`
**New function:** `DownloadMultiple(rclonePaths []string, destinationDir, rcloneBinary string)`
- Accepts multiple rclone paths
- Creates transfer manager with all transfers
- Executes downloads sequentially
- Shows progress for all downloads in Bubble Tea UI

#### 4. `cmd/goplexcli/main.go`
**Modified:** `runBrowse()`
- Now handles multiple selections from fzf
- Builds list of selected media items
- Routes to new multi-item handlers

**New functions:**
- `handleWatchMultiple()` - Handles watching multiple items
- `handleDownloadMultiple()` - Handles downloading multiple items

**Modified functions:**
- `handleWatch()` - Kept for single-item backward compatibility
- `handleDownload()` - Kept for single-item backward compatibility

## Backward Compatibility

The changes are fully backward compatible:
- Single selection still works as before
- Manual selection (non-fzf fallback) defaults to single item
- All existing commands and flags remain unchanged
- Single-item functions are preserved

## User Experience

### Prompts
The fzf prompt now indicates multi-select capability:
```
Select media (TAB for multi-select):
```

### Visual Feedback
- Selected items are highlighted in fzf with `>` marker
- Progress messages show current item count (e.g., `[2/5]`)
- Status messages clearly indicate multiple items

### Error Handling
- Invalid selections are skipped gracefully
- Missing rclone paths show warnings but don't stop downloads
- Failed downloads report errors but don't block subsequent downloads

## Keyboard Shortcuts in fzf

- **TAB** - Select/deselect current item
- **Shift+TAB** - Deselect current item
- **Ctrl+P** or **Ctrl+/** - Toggle preview window
- **Enter** - Confirm selection
- **Ctrl+C** - Cancel selection

## Keyboard Shortcuts in MPV (Watch Mode)

- **n** - Next item in playlist
- **p** - Previous item in playlist
- **q** - Quit playback
- **Space** - Pause/resume
- **Arrow keys** - Seek forward/backward
- **f** - Toggle fullscreen

## Limitations

1. **Stream action:** Only supports single selection (uses first item)
2. **Downloads:** Sequential, not parallel (by design for better progress tracking)
3. **Manual selection fallback:** Only supports single item selection

## Future Enhancements

Potential improvements for future versions:
- Parallel downloads with concurrent progress bars
- Resume failed downloads
- Playlist management (save/load playlists)
- Custom ordering of selected items
- Batch streaming (multiple devices)

## Testing

To test multi-select:

1. **Login and cache:**
   ```bash
   goplexcli login
   goplexcli cache reindex
   ```

2. **Browse with multi-select:**
   ```bash
   goplexcli browse
   # Select "Movies" or "TV Shows"
   # Use TAB to select multiple items
   # Press Enter
   # Choose "Watch" or "Download"
   ```

3. **Verify:**
   - Multiple items are processed
   - Progress is shown correctly
   - Playback/download completes successfully

## Troubleshooting

### fzf doesn't show multi-select
**Problem:** TAB doesn't select items
**Solution:** Make sure fzf is up to date (`fzf --version` should be 0.30+)

### MPV doesn't play next item
**Problem:** MPV exits after first item
**Solution:** Ensure MPV is not configured with `--playlist-start=0` in your config

### Downloads fail silently
**Problem:** Download appears to succeed but files missing
**Solution:** Check console output for warnings about missing rclone paths

---

*Last updated: January 2026*
