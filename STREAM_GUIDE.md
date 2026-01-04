# Stream Feature - Quick Start Guide

## Publishing a Stream (Mac/Desktop)

```bash
# 1. Browse your library
goplexcli browse

# 2. Select a movie or TV show
# 3. Choose "Stream" option

# Output:
✓ Stream published
Stream ID: stream-1704312345678
Title: The Matrix (1999)

🌐 Stream server running on port 8765

📱 Open on your device: http://192.168.1.100:8765

Other options:
  • Web UI: http://192.168.1.100:8765
  • CLI: goplexcli stream

Press Ctrl+C to stop the server
```

## Accessing on iPad/iPhone

### Method 1: Web UI (Recommended)
1. Open Safari on your iPad
2. Navigate to the URL shown (e.g., `http://192.168.1.100:8765`)
3. You'll see a beautiful web interface with:
   - List of all published streams
   - Movie/TV metadata (title, year, duration, summary)
   - "Play in Infuse" button (opens directly in Infuse)
   - "Play in VLC" button (opens directly in VLC)
   - "Play in Plex" button (opens directly in Plex)

### Method 2: CLI (Mac/Linux only)
```bash
goplexcli stream
# Discovers servers automatically
# Select stream to play in MPV
```

## Web UI Features

- **Mobile-Responsive**: Optimized for iOS Safari and mobile browsers
- **Deep Link Support**: Tap to open in Infuse, VLC, or Plex
- **Auto-Refresh**: Page updates every 5 seconds to show new streams
- **Beautiful Design**: Gradient backgrounds, smooth animations
- **Stream Metadata**: Shows year, duration, and plot summaries
- **Multi-Stream**: Can publish multiple streams from different devices

## Supported Players

### iOS/iPadOS
- **Infuse** (Recommended) - Best quality, supports all formats
- **VLC** - Free, open-source
- **Plex** - Native Plex experience

### Desktop
- **MPV** (via CLI) - High performance
- **VLC** - Cross-platform
- **Any browser** - Web UI works everywhere

## Network Requirements

- Both devices on same WiFi network
- Port 8765 accessible (HTTP)
- Port 5353 for mDNS discovery (optional, only for CLI)

## Example Workflow

**Mac:**
```bash
goplexcli browse
# Select "Inception (2010)"
# Choose "Stream"
# Server shows: http://192.168.1.100:8765
```

**iPad:**
1. Open Safari
2. Go to `http://192.168.1.100:8765`
3. See "Inception (2010)" with play buttons
4. Tap "Play in Infuse"
5. Infuse opens and starts playing immediately

## Technical Details

- HTTP server on port 8765
- mDNS service announcement as `_goplexcli._tcp`
- RESTful JSON API at `/streams`
- Web UI served at `/`
- Embedded HTML/CSS (no external dependencies)
- Graceful shutdown with Ctrl+C

## Troubleshooting

**Can't access web UI from iPad:**
- Verify both devices on same network
- Check firewall allows port 8765
- Try IP address directly in Safari
- Disable cellular data on iPad

**Deep links not working:**
- Ensure player app is installed
- Try copying stream URL manually
- Some players require tap-and-hold → "Open in..."

**Stream won't play:**
- Check Plex server is accessible from iPad
- Verify stream URL is valid
- Try opening URL directly in Safari first
