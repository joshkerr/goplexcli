# GoplexCLI

A powerful, fast, and elegant command-line interface for browsing and streaming media from your Plex server.

## Features

- **Browse Media** — Quickly browse your entire Plex library using fzf's fuzzy finder
- **Search** — Pass a search term directly to find movies and TV shows instantly
- **Multi-Select** — Select multiple items with TAB for batch downloads or sequential playback
- **Download Queue** — Add items to a persistent queue for batch downloads later
- **Continue Watching** — Resume playback from where you left off, with progress tracked via MPV IPC
- **Recently Added** — Jump straight to the newest items in your library
- **Rich Previews** — View detailed metadata (rating, duration, cast, summary) in fzf's preview pane
- **Stream with MPV** — Watch movies and TV shows directly with MPV player
- **Download with Rclone** — Download media files with a real-time progress bar UI
- **Remote Streaming** — Publish streams for playback on other devices via mDNS discovery and a web UI
- **Transfer to WebDAV** — Push media to gowebdav servers discovered on your LAN via mDNS
- **SenPlayer Integration** — Play or download media in SenPlayer via deep links (macOS)
- **Sort & Filter** — Sort your library by name, date added, year, rating, or duration
- **Smart Caching** — Cache your media library locally for instant offline browsing
- **Multi-Server Support** — Connect to and manage multiple Plex servers
- **Hierarchical TV Browsing** — Drill down through Show → Season → Episode
- **Self-Updating** — Update to the latest release with a single command
- **Shell Completions** — Tab completions for Bash, Zsh, Fish, and PowerShell
- **Cross-Platform** — Works on macOS, Linux, and Windows (AMD64 and ARM64)
- **Beautiful UI** — Built with Charm libraries for a polished terminal experience

## Prerequisites

Before using GoplexCLI, ensure you have the following installed:

- **fzf** — Fuzzy finder for browsing media (required)
  - macOS: `brew install fzf`
  - Linux: `sudo apt install fzf` or `sudo pacman -S fzf`
  - Windows: `choco install fzf` or `winget install junegunn.fzf`
- **mpv** — Media player for streaming (required for Watch)
  - macOS: `brew install mpv`
  - Linux: `sudo apt install mpv` or `sudo pacman -S mpv`
  - Windows: Download from [mpv.io](https://mpv.io)
- **rclone** — For downloading media files (required for Download)
  - macOS: `brew install rclone`
  - Linux: `sudo apt install rclone` or download from [rclone.org](https://rclone.org)
  - Windows: Download from [rclone.org](https://rclone.org)
- **chafa** (optional) — Terminal image viewer for poster art in the TUI browser
  - macOS: `brew install chafa`
  - Linux: `sudo apt install chafa`

## Installation

### From Releases

Download the latest binary for your platform from the [Releases](https://github.com/joshkerr/goplexcli/releases) page and place it somewhere in your PATH.

### From Source

Requires Go 1.24+.

```bash
git clone https://github.com/joshkerr/goplexcli.git
cd goplexcli
make build
```

This builds the `goplexcli` binary (or `goplexcli.exe` on Windows). The fzf preview pane is rendered by a hidden `__preview` subcommand of the same binary, so there's nothing else to install.

Then install to your PATH:

```bash
# macOS/Linux
sudo cp goplexcli /usr/local/bin/

# Or use make
make install
```

## Desktop GUI

GoplexCLI also ships a cross-platform desktop app (macOS, Windows, Linux) with a
modern poster-grid interface. It reuses the same engine as the CLI — Plex
access, the local cache, MPV playback with progress tracking, and rclone
downloads — so logging in or indexing from either side is shared.

The GUI lives in [`gui/`](gui/) and is built with [Wails v2](https://wails.io)
(Go backend + a React/TypeScript frontend). Playback still launches **MPV**
externally and downloads still use **rclone**, so those prerequisites apply just
as they do for the CLI.

### Building the GUI

Requires Go 1.24+, [Node.js](https://nodejs.org) 18+, and the Wails CLI:

```bash
make gui-deps        # one-time: installs the Wails CLI to GOPATH/bin
make gui-dev         # run with hot reload
make gui-build       # build a native binary into gui/build/bin/
```

(Equivalently, `cd gui && wails dev` / `wails build`.)

**Platform notes**

| Platform | Webview / extra deps |
|----------|----------------------|
| Windows  | WebView2 runtime (preinstalled on Windows 11) |
| macOS    | WKWebView (built in) |
| Linux    | `libgtk-3` and `libwebkit2gtk-4.0` (`sudo apt install libgtk-3-dev libwebkit2gtk-4.0-dev`) |

### Using the GUI

1. **Sign in** with your Plex account and pick the servers to index.
2. **Build library** to populate the local cache (shows live progress).
3. **Browse** Movies, TV Shows, Recently Added, and Continue Watching from the
   sidebar; **search** filters the grid instantly.
4. Open any title for details, then **Play**/**Resume** (MPV) or **Download**
   (rclone, with live progress in the Downloads panel). TV shows drill into
   Season → Episode with multi-select for playlist playback or batch downloads.

## Quick Start

```bash
# 1. Authenticate with Plex
goplexcli login

# 2. Index your media library
goplexcli cache reindex

# 3. Browse and play
goplexcli browse
```

## Usage

### Quick Search

Pass a search term directly to find matching media:

```bash
goplexcli "The Lincoln Lawyer"
goplexcli -d "time travel"    # Also search descriptions/summaries
```

Movies can be played immediately. TV shows drill into Season → Episode selection.

### Browse

```bash
goplexcli browse
goplexcli browse --dry-run          # Show what would download without downloading
goplexcli browse --dest ~/Movies    # Override download directory
```

The browse flow:

1. **Pick a category** — Movies, TV Shows, All, Recently Added, Continue Watching, or View Queue
2. **Select media** — Fuzzy search with preview pane (Ctrl+P to toggle). TAB for multi-select.
3. **Pick an action** — Watch, Download, Transfer to WebDAV, SenPlayer Play, SenPlayer Download, Add to Queue, or Stream

For TV Shows, the picker drills hierarchically: Show → Season → Episode(s).

### Sort

Sort and display media from your cache:

```bash
goplexcli sort added --desc --limit 20    # Last 20 added items
goplexcli sort name --asc                 # A-Z by title
goplexcli sort rating --desc --limit 10   # Top 10 rated
goplexcli sort year --desc --type movies  # Newest movie releases
goplexcli sort duration --desc -i         # Longest items, open in picker
```

Available fields: `name`, `added`, `year`, `rating`, `duration`

Flags:
- `--desc` / `--asc` — Sort direction (defaults: descending for numeric fields, ascending for name)
- `--limit N` — Max items to display (default 20)
- `--type` — Filter: `movies`, `shows`, or `all`
- `-i` / `--interactive` — Open results in the interactive browser for playback/download

### Cache Management

```bash
goplexcli cache reindex         # Rebuild entire cache from scratch
goplexcli cache update          # Incremental update with new media
goplexcli cache info            # Show cache statistics
goplexcli cache search "title"  # Search in both cache and Plex server
```

### Server Management

```bash
goplexcli server list                  # List configured servers
goplexcli server enable "Server Name"  # Enable a server for indexing
goplexcli server disable "Server Name" # Disable a server
goplexcli server remove "Server Name"  # Remove a server entirely
```

### Stream Discovery

Publish a stream from one device and play it on another over the local network:

```bash
# On the publishing device: browse → select → choose "Stream"
goplexcli browse

# On the consuming device: discover and play
goplexcli stream
```

The stream server also exposes a web UI at `http://<ip>:8765` with deep links to Infuse, VLC, OutPlayer, SenPlayer, IINA, and VidHub — play directly on an iPad, iPhone, or Apple TV from your browser.

### Self-Update

```bash
goplexcli update          # Download and install the latest release
goplexcli update --check  # Check for updates without installing
```

### Shell Completions

```bash
# Bash
source <(goplexcli completion bash)

# Zsh
goplexcli completion zsh > "${fpath[1]}/_goplexcli"

# Fish
goplexcli completion fish | source

# PowerShell
goplexcli completion powershell | Out-String | Invoke-Expression
```

### WebDAV Transfer

Discover [gowebdav](https://github.com/joshkerr/gowebdav) servers on your LAN and push media to them:

```bash
goplexcli webdav discover      # Scan for gowebdav servers
goplexcli webdav set-creds     # Set shared username/password for transfers
```

Then during browse, select media and choose **Transfer to WebDAV** to push files to the discovered server.

### Other Commands

```bash
goplexcli login       # Authenticate with Plex (supports multi-server)
goplexcli config      # Show current configuration
goplexcli version     # Show version
```

## Configuration

Configuration is stored in a platform-specific directory:

| Platform | Path |
|----------|------|
| macOS | `~/.config/goplexcli/config.json` |
| Linux | `~/.config/goplexcli/config.json` (or `$XDG_CONFIG_HOME`) |
| Windows | `%APPDATA%\goplexcli\config.json` |

### Config File

```json
{
  "plex_token": "your-auth-token",
  "servers": [
    {
      "name": "My Plex Server",
      "url": "http://192.168.1.100:32400",
      "enabled": true
    }
  ],
  "plex_username": "your-username",
  "mpv_path": "mpv",
  "rclone_path": "rclone",
  "fzf_path": "fzf",
  "download_dir": "~/Downloads/Plex",
  "path_mappings": [
    { "prefix": "/mnt/media/tv/", "remote": "gdrive:Media/TV/" },
    { "prefix": "/mnt/media/", "remote": "gdrive:Media/" }
  ],
  "webdav_user": "user",
  "webdav_pass": "password",
  "webdav_dir": ""
}
```

- **servers** — One or more Plex servers, individually enabled/disabled
- **mpv_path**, **rclone_path**, **fzf_path** — Override tool paths if not in PATH
- **download_dir** — Default download destination (`~` is expanded). Override per-run with `--dest`.
- **path_mappings** — Translate Plex file paths to rclone remotes. Longest matching prefix wins. Run `cache reindex` after changing.
- **webdav_user**, **webdav_pass**, **webdav_dir** — Shared credentials and optional subdirectory for gowebdav transfers (set via `goplexcli webdav set-creds`)

## How It Works

### Playback Progress

When you watch media through GoplexCLI, progress is tracked via MPV's IPC socket and reported back to your Plex server in real time. After playback ends, progress is also written to the local cache so items appear in **Continue Watching** immediately — no reindex needed.

Progress made on *other* Plex clients requires a `cache reindex` to refresh.

### Resume Playback

If a media item has saved progress, you'll be prompted to resume from your last position or start from the beginning.

### Download Queue

The queue is persistent between sessions and concurrent-safe (uses file locking). Multiple instances can add items while another downloads. Duplicate items are automatically deduplicated by key.

### Rclone Path Conversion

GoplexCLI translates Plex on-disk file paths to rclone remote paths for downloads. Configure `path_mappings` in your config for explicit control, or the legacy heuristic strips a prefix and infers the remote name from the first path component.

## Troubleshooting

| Problem | Solution |
|---------|----------|
| "fzf not found" | Install fzf (see Prerequisites) |
| "mpv not found" | Install mpv (see Prerequisites) |
| "rclone not found" | Install rclone and run `rclone config` to set up remotes |
| "Cache is empty" | Run `goplexcli cache reindex` |
| Stream discovery not working | Ensure both devices are on the same network. Check firewall allows mDNS (port 5353 UDP) and HTTP (port 8765 TCP). |
| Web UI not accessible | Verify the URL shown during stream publishing. Ensure port 8765 is not blocked. |
| Deep links not opening on iOS | Ensure the target app (Infuse, VLC, etc.) is installed. Try copy/paste of the stream URL. |

## Project Structure

```
goplexcli/
├── cmd/goplexcli/       # CLI entry point and all commands
├── gui/                 # Cross-platform desktop app (Wails v2 + React)
│   ├── *.go             # Backend bindings reusing the internal/ packages
│   └── frontend/        # React + TypeScript + Tailwind UI
├── internal/
│   ├── cache/           # JSON-based media cache
│   ├── config/          # Configuration loading/saving/validation
│   ├── download/        # Rclone download with progress UI
│   ├── errors/          # Shared error types
│   ├── interfaces/      # Shared interfaces
│   ├── logging/         # Logging utilities
│   ├── player/          # MPV player wrapper
│   ├── plex/            # Plex API client (SDK + direct HTTP)
│   ├── preview/         # fzf preview pane renderer
│   ├── progress/        # MPV IPC progress tracker
│   ├── queue/           # Persistent download queue with file locking
│   ├── stream/          # Stream server, mDNS, and web UI
│   ├── termuxfix/       # Termux/Android compatibility
│   ├── ui/              # fzf integration, TUI browser, resume prompts
│   ├── update/          # Self-update from GitHub releases
│   └── webdav/          # gowebdav server discovery via mDNS
├── Makefile
├── go.mod
└── go.sum
```

## Dependencies

**Go Libraries:**
- [plexgo](https://github.com/LukeHagar/plexgo) — Plex API SDK
- [bubbletea](https://github.com/charmbracelet/bubbletea) — TUI framework
- [lipgloss](https://github.com/charmbracelet/lipgloss) — Terminal styling
- [cobra](https://github.com/spf13/cobra) — CLI framework
- [rclone-golib](https://github.com/joshkerr/rclone-golib) — Rclone integration with progress bars
- [flock](https://github.com/gofrs/flock) — Cross-platform file locking
- [zeroconf](https://github.com/grandcat/zeroconf) — mDNS/DNS-SD discovery
- [fuzzy](https://github.com/sahilm/fuzzy) — Fuzzy matching
- [term](https://golang.org/x/term) — Secure terminal input

**External Tools:**
- [fzf](https://github.com/junegunn/fzf) — Fuzzy finder
- [mpv](https://mpv.io) — Media player
- [rclone](https://rclone.org) — Cloud storage transfers
- [chafa](https://hpjansson.org/chafa/) (optional) — Terminal image rendering

## Contributing

Contributions are welcome! Please feel free to submit issues or pull requests.

## License

MIT License — See [LICENSE](LICENSE) for details.
