# GoplexCLI

A powerful, fast, and elegant command-line interface for browsing and streaming media — from your Plex server, or from media folders GoplexCLI indexes itself with no Plex Media Server at all.

## Features

- **Browse Media** — Quickly browse your entire library using fzf's fuzzy finder
- **Plex-free** — Index your own media folders (or rclone mounts of cloud storage) with TMDB metadata and serve them to every GoplexCLI client; no Plex Media Server needed
- **Search** — Pass a search term directly to find movies and TV shows instantly
- **Multi-Select** — Select multiple items with TAB for batch downloads or sequential playback
- **Download Queue** — Add items to a persistent queue for batch downloads later
- **Continue Watching** — Resume playback from where you left off, with progress tracked via MPV IPC
- **Recently Added** — Jump straight to the newest items in your library
- **Rich Previews** — View detailed metadata (rating, duration, cast, summary) in fzf's preview pane
- **Stream with MPV** — Watch movies and TV shows directly with MPV player
- **Download with Rclone** — Download media files with a real-time progress bar UI
- **Remote Streaming** — Publish streams for playback on other devices via mDNS discovery and a web UI
- **LAN Cache Sync** — Copy the media cache between your computers over the local network instead of reindexing each one from Plex
- **Transfer to WebDAV** — Push media to gowebdav servers discovered on your LAN via mDNS
- **Transfer to Outplayer** — Upload media to the Outplayer iOS app over Wi-Fi to multiple configurable targets
- **SenPlayer Integration** — Play or download media in SenPlayer via deep links (macOS)
- **Sort & Filter** — Sort your library by name, date added, year, rating, or duration
- **Smart Caching** — Cache your media library locally for instant offline browsing
- **Multi-Server Support** — Connect to and manage multiple Plex servers (and local libraries, side by side)
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
- **ffprobe** (optional) — Only used when indexing your own folders with `library_probe` enabled, to record exact durations and audio languages. Ships with ffmpeg (`brew install ffmpeg` / `sudo apt install ffmpeg`).

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
make gui-dev         # build the frontend and run with Wails development tools
make gui-build       # build a native binary into gui/build/bin/
make gui-install     # Windows: install per-user and create a Start Menu shortcut
```

On Windows, `make gui-install DESKTOP=1` also creates a desktop shortcut. The
icon-enabled executable is installed under
`%LOCALAPPDATA%\Programs\GoplexCLI`; administrator privileges are not required.

(Equivalently, `cd gui && wails dev` / `wails build`.)

**Platform notes**

| Platform | Webview / extra deps |
|----------|----------------------|
| Windows  | WebView2 runtime (preinstalled on Windows 11) |
| macOS    | WKWebView (built in) |
| Linux    | `libgtk-3` and `libwebkit2gtk-4.0` (`sudo apt install libgtk-3-dev libwebkit2gtk-4.0-dev`) |

### Using the GUI

1. **Sign in** with your Plex account and pick the servers to index — or
   choose **Index local folders instead** to point the app at your own media
   folders with no Plex server (see [Indexing Your Own Media](#indexing-your-own-media-no-plex)).
2. **Build library** to populate the local cache (shows live progress).
3. **Browse** Movies, TV Shows, Recently Added, and Continue Watching from the
   sidebar; **search** filters the grid instantly. On the Movies grid, filter by
   **genre** and **sort** by title, year, date added, rating, or duration
   (ascending or descending).
4. Open any title for details, then **Play**/**Resume** (MPV) or **Download**
   (rclone, with live progress in the Downloads panel). TV shows drill into
   Season → Episode with multi-select for playlist playback or batch downloads.
   Opening or closing the details pane keeps the poster grid at the same titles.
   Selecting **Downloads** or **Settings** under **Activity** closes the detail panel.
   The download destination is remembered across titles and app launches for
   movies and TV episodes. If that server is removed or disabled, the picker
   falls back to **this computer**.
5. Keep the library fresh from **Settings → Library**: **Update** (fetch only
   newly-added titles), **Reindex** (rebuild from scratch), or **Sync from LAN**
   (pull the cache from another computer running GoplexCLI — see below).

Poster grids request compact Plex-generated renditions and keep them in a
bounded local cache under the GoplexCLI cache directory. Visible posters are
prioritized and the on-screen window is warmed via the backend so jumping
around a large library fills in quickly without re-downloading artwork.

## Quick Start

**With a Plex server:**

```bash
# 1. Authenticate with Plex
goplexcli login

# 2. Index your media library
goplexcli cache reindex

# 3. Browse and play
goplexcli browse
```

**Without Plex** (index your own folders — see [Indexing Your Own Media](#indexing-your-own-media-no-plex)):

```bash
# 1. Point GoplexCLI at your media folders
goplexcli library add /mnt/media/Movies --type movie
goplexcli library add /mnt/media/TV --type show

# 2. Add a TMDB API key to config.json for metadata and posters (optional)
#    { "tmdb_api_key": "..." }

# 3. Index them, then keep them served (playback streams from this server;
#    the desktop GUI serves them too while it is open)
goplexcli library scan
goplexcli daemon

# 4. Browse and play (from another terminal or another computer)
goplexcli browse
```

## Indexing Your Own Media (No Plex)

GoplexCLI can index media folders itself and act as the server for every
GoplexCLI client (CLI, desktop GUI, other computers that sync from it), so a
Plex Media Server is no longer required. Each folder is scanned for video
files, titles are read from the folder and file names, metadata and artwork
come from [TMDB](https://www.themoviedb.org), and the results land in the same
cache Plex items use — so `browse`, the GUI, LAN sync, playback progress and
rclone downloads all work unchanged. Plex servers and local libraries can be
mixed in one config.

### Adding libraries

```bash
goplexcli library add <path> --type movie|show [--name "Label"] [--remote remote:folder]

goplexcli library add /mnt/media/Movies --type movie
goplexcli library add /mnt/media/TV --type show --name "TV"
goplexcli library add /mnt/gdrive/Media/Movies --type movie --remote gdrive:Media/Movies

goplexcli library list                # folders, item counts, server name and URL
goplexcli library enable "TV"         # include in scans
goplexcli library disable "TV"        # exclude from scans
goplexcli library remove "TV"         # items drop from the cache on the next scan
```

- `--type` is `movie` (default) or `show` and decides how names are
  interpreted (`movies`, `tv`, `series`, … are accepted as aliases).
- `--name` defaults to the folder name. The name is part of each item's
  identity, so renaming a library resets its playback positions and favorites.
- `--remote` is the rclone path that mirrors the folder (e.g. the remote an
  rclone mount is backed by). It does two things: downloads and transfers can
  fetch the files via rclone, and the scan lists the remote directly with
  `rclone lsjson -R --fast-list` instead of walking the mount — far faster for
  cloud storage (it falls back to walking the folder if rclone fails). Without
  it, files are only reachable through this machine's library server, unless a
  `path_mappings` entry covers the path.

### Folder layout

Plex-style naming works best. For **movies**, a folder per title (with an
optional IMDB/TMDB id for an exact match) or plain release-style file names:

```
Movies/
├── Blade Runner 2049 (2017) [imdb-tt1856101]/Blade Runner 2049 (2017).mkv
├── Dune (2021) {tmdb-438631}/Dune.2021.2160p.WEB-DL.mkv
├── The.Lincoln.Lawyer.2011.1080p.BluRay.x264-GROUP.mkv
└── Heat (1995).mp4
```

A folder name with a `(Year)` or an id wins; otherwise the file name is parsed
as a scene release (the title ends at the first resolution/source/codec token,
and the last plausible year before it is the release year); a folder holding
a single file lends it its name.

For **shows**, a show folder (optionally with `(Year)` and ids), season
folders, and `SxxEyy`-tagged files:

```
TV/
├── The Expanse (2015) [imdb-tt3230854]/
│   ├── Season 01/The.Expanse.S01E01.Dulcinea.mkv
│   ├── Season 01/The.Expanse.S01E02.mkv
│   └── Specials/The.Expanse.S00E01.mkv
└── Severance/
    └── Season 1/Severance - 1x01 - Good News About Hell.mkv
```

`S01E02` (including multi-episode `S01E01E02` / `S01E01-E02`), `1x02`,
`Season 1 Episode 2` and the anime-style `Show - 01` are all recognized; a
`Season NN`, `S01` or `Specials` folder supplies the season when the file name
has none. Files at the library root are grouped by the show name in the file
name.

Hidden files, `sample`/`samples`, Plex extras folders (`Trailers`, `Featurettes`,
`Behind The Scenes`, `Deleted Scenes`, …) and files ending in `-trailer`,
`-sample`, `-featurette`, etc. are skipped. Recognized video extensions: `.mkv
.mp4 .m4v .avi .mov .wmv .webm .ts .m2ts .mpg .mpeg .flv`.

### TMDB metadata

Get a free API key from [themoviedb.org](https://www.themoviedb.org/settings/api)
(a v3 key or a v4 read token both work) and add it to your config:

```json
{ "tmdb_api_key": "your-tmdb-key" }
```

With a key, every title gets its summary, rating, genres, cast, runtime,
content rating and poster. TMDB responses are cached on disk for 30 days, so
rescans are cheap. Without a key, titles and years come from the names only
and there is no artwork. Set `library_probe: true` to additionally run
`ffprobe` on each new file for exact durations and audio languages (off by
default — it opens every file, which is slow on cloud mounts).

### Scanning

```bash
goplexcli library scan          # index now; unchanged files keep their metadata
goplexcli library scan --full   # re-enrich every title from TMDB (progress is kept)
```

`goplexcli cache update` and `goplexcli cache reindex` — and the GUI's
**Update** / **Reindex** buttons — now also scan local libraries, so one
command refreshes Plex servers and local folders together. Titles TMDB
couldn't identify are listed as warnings and retried on the next scan.

### Fixing a wrong match

When TMDB picked the wrong title (a remake, a same-named show from another
country), pin the movie or the whole show to the right one. Fixes are stored
in `matches.json` under the cache's `library` folder and survive every later
scan; the show fix applies to episodes added later too.

```bash
goplexcli library search "Accused" --tv            # list TMDB candidates with ids
goplexcli library match "Accused" --tv --tmdb 193744   # pin the show by name…
goplexcli library match /local/metadata/1a2b… --tmdb 603   # …or a movie by key
goplexcli library match "Accused" --tv --auto      # back to automatic matching
goplexcli library matches                          # list manual fixes on this machine
```

The rebuild happens on the library server that owns the item (the daemon
on your NAS, say), so the command works from any computer that synced the
cache, and the fix shows up on every device at the next sync. Without a
local `tmdb_api_key`, `library search` asks that server too.

In the GUI, open a movie or show and click **Fix match…** next to its
source line: search by title (Movie / TV Show, optional year), pick the
right result, and the card updates in place.

### Cover art for downloads

Every download goplexcli makes — in the GUI, with the CLI, or on a remote
`goplexcli daemon` — gets cover art embedded once the transfer finishes.
Art is looked up on Apple TV's catalog first (the 16:9 art with the title
treatment), then TMDB (backdrop, or poster), then Netflix (with its title
logo composited on). MKV files are updated in place with `mkvpropedit`;
MP4/M4V files are remuxed with ffmpeg. Episodes get their show's art, and a
`poster.jpg` is dropped into a `TV Shows/<show>/` folder the first time.
Files that already carry art are left alone.

Needs `ffmpeg` and `ffprobe` (and `mkvpropedit` for in-place MKV updates).
Apple art needs no key; TMDB and Netflix need `tmdb_api_key`.

```bash
goplexcli artwork ~/Downloads/Media            # add art to files already on disk (walks folders)
goplexcli artwork "Some.Movie.2019.mkv" --title "Some Movie" --year 2019
goplexcli artwork "TV Shows/Severance" --tv --force   # replace existing art
goplexcli artwork ~/Downloads/Media --dry-run  # show the lookups only
```

Config: `artwork` (`""`/`"auto"` on, `"off"` to disable), `artwork_orientation`
(`horizontal` default, or `vertical` for 2:3 posters), `apple_storefront`
(default `143441`, US), and `ffmpeg_path`/`ffprobe_path`/`mkvpropedit_path`
overrides. In the GUI: Settings → Preferences → "Embed cover art into
downloads".

### Browse API (for thin clients)

The library server also answers a small JSON API under `/api/v1/` so
clients like the Android app can browse a large library page by page
instead of downloading the whole cache. It uses the same `library_token`
(as `X-Plex-Token`) as the rest of the library server.

| Route | Returns |
|---|---|
| `GET /api/v1/status` | server name, version, counts, last indexed |
| `GET /api/v1/browse?category=…&sort=…&desc=1&genre=…&offset=…&limit=…` | a page of cards; categories `movies`, `shows`, `recent-movies`, `recent-shows`, `continue`, `favorite-movies`, `favorite-shows` |
| `GET /api/v1/search?q=…` | movies and shows by title (fuzzy), or `director:` / `cast:` / `genre:` queries |
| `GET /api/v1/item?key=…` | a movie or episode (with `streamPath` and `rclonePath`), or a show for `show:<title>` |
| `GET /api/v1/episodes?show=…&season=N` | one season in order |
| `GET /api/v1/genres` | the most common movie genres |
| `POST /api/v1/favorite` `{key, favorite}` | sets a favorite (shared with LAN sync peers) |
| `POST /api/v1/watched` `{key, watched}` | marks watched or unwatched |

Playback progress uses the Plex-style `/:/timeline` endpoint, posters
`/photo/:/transcode?url=<thumb>&width=…`, and streams `<streamPath>` with
Range support.

### Running the daemon

`goplexcli daemon` turns a machine into an always-on server. In one process it:

- serves locally indexed media to players and other GoplexCLI instances
  (streaming, posters, playback progress) on port **47822** — the library
  server. A file that isn't on disk (no mount, or the mount is down) is
  streamed straight from its `--remote` path with `rclone cat`, so a FUSE
  mount is optional — though a mount with a VFS cache seeks faster;
- advertises and serves the media cache on the LAN on port **47820** (like
  `sync serve`);
- accepts download requests from remote GUIs on port **47821** (like `serve`);
- rescans the library folders on an interval (default 1h, or
  `library_scan_interval` in config; send `SIGHUP` to rescan right away,
  e.g. `systemctl --user kill -s HUP goplexcli-daemon`, or `SIGUSR1` for a
  full re-enrichment like `library scan --full`), so new files show up on every device
  that syncs from this machine.

```bash
goplexcli daemon
#   --scan-interval 30m   how often to rescan (default library_scan_interval, else 1h; 0 disables)
#   --scan-on-start=false skip the scan that normally runs at startup
#   --no-library          don't serve local media or scan libraries
#   --no-sync             don't advertise the cache on the LAN
#   --no-downloads        don't run the remote download server
```

The library server speaks just enough of the Plex wire protocol (identity,
item metadata, part streaming with range requests, poster images, timeline
progress reports) that existing clients stream, fetch posters and report
progress unchanged. Posters are proxied from TMDB and kept in a bounded cache
under the GoplexCLI cache directory. The server is open to the LAN (like the
cache sync server) unless `library_token` is set, in which case every request
except `/identity` must carry it as an `X-Plex-Token` header or query
parameter.

### Other devices

Other computers don't index anything themselves — they pull the cache from the
machine running the daemon via [LAN Cache Sync](#lan-cache-sync)
(`goplexcli sync pull`, or **Sync from LAN** in the GUI), and playback streams
from the daemon's library server. Each local item carries the server address
as its `library_url`, so that URL must be reachable from wherever the cache is
used: it defaults to the machine's first LAN IPv4 address plus the library
port, and can be pinned with `library_url` (e.g. `http://nas.local:47822`) if
the machine has several interfaces or a stable hostname. Downloads on other
devices use the library's `--remote` rclone path when set.

### NAS / home-server setup (systemd)

A typical setup is a NAS or small server with the media (or an rclone mount of
cloud storage) attached and `goplexcli daemon` running as a user service:

```ini
# ~/.config/systemd/user/goplexcli.service
[Unit]
Description=GoplexCLI daemon (library server, LAN sync, downloads)
After=network-online.target rclone-gdrive.service
Wants=network-online.target

[Service]
ExecStart=%h/bin/goplexcli daemon
Restart=on-failure
RestartSec=10

[Install]
WantedBy=default.target
```

If the media lives in cloud storage, mount it with rclone first. Use
`--vfs-cache-mode full` so players can seek:

```ini
# ~/.config/systemd/user/rclone-gdrive.service
[Unit]
Description=rclone mount of gdrive:Media
After=network-online.target

[Service]
ExecStart=/usr/bin/rclone mount gdrive:Media %h/media/gdrive \
    --vfs-cache-mode full --vfs-cache-max-size 20G \
    --vfs-read-chunk-size 32M --dir-cache-time 1h --umask 002
ExecStop=/bin/fusermount -uz %h/media/gdrive
Restart=on-failure
RestartSec=10

[Install]
WantedBy=default.target
```

```bash
mkdir -p ~/media/gdrive
systemctl --user daemon-reload
systemctl --user enable --now rclone-gdrive.service goplexcli.service
loginctl enable-linger "$USER"      # keep user services running without a login session

goplexcli library add ~/media/gdrive/Movies --type movie --remote gdrive:Media/Movies
goplexcli library add ~/media/gdrive/TV --type show --remote gdrive:Media/TV
```

The daemon picks up libraries added while it is running on its next scan.
Set `GOPLEXCLI_CONFIG_DIR` in the unit's `Environment=` to keep the daemon's
config and cache separate from an interactive user's.

### In the GUI

The desktop app exposes the same setup: **Settings → Local libraries** adds,
enables/disables and removes folders, sets the TMDB key and library server
options, and shows the server name and URL. On first launch, **Setup →
"Index local folders instead"** skips the Plex sign-in and walks through
adding folders. **Update** and **Reindex** on the Library tab scan local
libraries along with any Plex servers, and the GUI serves its local items and
cache while it is open, so it can be the source for other machines without
running the daemon.

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
3. **Pick an action** — Watch, Download, Transfer to WebDAV, Transfer to Outplayer, SenPlayer Play, SenPlayer Download, Add to Queue, or Stream

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

`reindex` and `update` cover both Plex servers and any local libraries you
have added with `goplexcli library add`.

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

### Outplayer Transfer

[Outplayer](https://outplayer.app) is an iOS media player with a "Wi-Fi transfer"
feature that runs a small HTTP upload server on your device. Configure one or
more Outplayer targets and push media to them straight from your rclone remotes
(the file is streamed through, never staged on local disk):

```bash
goplexcli outplayer add              # Add a target (name + URL, e.g. http://192.168.0.34)
goplexcli outplayer list             # List targets and their enabled/disabled status
goplexcli outplayer disable iPhone   # Hide a target from the transfer menu
goplexcli outplayer enable iPhone    # Show it again
goplexcli outplayer remove iPhone    # Delete a target
```

Enable Wi-Fi transfer in Outplayer (which shows the address to use), add it as a
target, then during browse select media and choose **Transfer to Outplayer**.
You can also transfer an entire queue: from **View Queue** choose **Transfer to
Outplayer** (or **Transfer to WebDAV**) to send every queued item — the queue is
left intact afterwards. The **Transfer to Outplayer** action only appears when at
least one target is enabled, so disabling all targets hides it. Uploads default
to the device root; Outplayer's built-in folders (e.g. `Inbox`) are not writable.

### LAN Cache Sync

If you run GoplexCLI on more than one computer, you can copy an already-built
media cache between them over your local network instead of reindexing each one
from Plex — for a large library this takes seconds rather than minutes.

One machine **serves** its cache; the other **pulls** it (only if the served
cache is newer):

```bash
# On the source machine — advertise and serve its cache (runs until Ctrl-C):
goplexcli sync serve
#   --port N               serve on a specific port (default 47820)
#   --update-interval 30m  refresh this cache from Plex on an interval so peers
#                          stay current (default 1h; 0 disables)

# On another machine — pull the newest cache found on the LAN:
goplexcli sync pull
#   --peer ghost-2.local        pull directly from a host, bypassing discovery
#   --peer 192.168.1.20:47820   ...or an explicit host:port
```

Machines are found automatically via mDNS. Some networks block mDNS (Windows
firewall, VPNs, Wi-Fi client isolation); if auto-discovery doesn't find the
other computer, set the peer directly — either with `--peer`, or persistently in
config so the GUI's **Sync from LAN** button and `sync pull` both use it:

```json
{ "sync_peer": "ghost-2.local" }
```

In the desktop GUI this is **Settings → Preferences → Sync from computer (LAN)**,
and the **Sync from LAN** button on the Library tab performs the pull. The GUI
also serves its cache automatically while it is open, so it can be the source for
other machines without running `sync serve`.

The transferred cache carries the source machine's Plex server URLs and rclone
path mappings, so it works as-is only when those match on both machines. It never
contains Plex tokens (those live in config, not the cache).

### Other Commands

```bash
goplexcli login       # Authenticate with Plex (supports multi-server)
goplexcli library     # Index local media folders without a Plex server (see above)
goplexcli daemon      # Library server + LAN sync + download server + scheduled scans
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

Setting the `GOPLEXCLI_CONFIG_DIR` environment variable overrides the
directory on every platform (the cache lives under it too), which lets a
second instance — a daemon under another account, or tests — keep its own
config and cache.

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
  "sync_peer": "ghost-2.local",
  "path_mappings": [
    { "prefix": "/mnt/media/tv/", "remote": "gdrive:Media/TV/" },
    { "prefix": "/mnt/media/", "remote": "gdrive:Media/" }
  ],
  "webdav_user": "user",
  "webdav_pass": "password",
  "webdav_dir": "",
  "outplayer_targets": [
    { "name": "iPhone", "url": "http://192.168.0.34", "dir": "", "enabled": true }
  ],
  "libraries": [
    { "name": "Movies", "path": "/mnt/media/Movies", "type": "movie", "remote": "gdrive:Media/Movies", "enabled": true },
    { "name": "TV", "path": "/mnt/media/TV", "type": "show", "enabled": true }
  ],
  "tmdb_api_key": "your-tmdb-key",
  "library_listen": ":47822",
  "library_url": "http://nas.local:47822",
  "library_name": "nas",
  "library_token": "",
  "library_probe": false,
  "library_scan_interval": "1h"
}
```

- **servers** — One or more Plex servers, individually enabled/disabled
- **libraries** — Folders GoplexCLI indexes itself (see [Indexing Your Own Media](#indexing-your-own-media-no-plex)). Each has a `name`, an absolute `path`, a `type` of `movie` or `show`, an optional rclone `remote` mirroring the folder, and an `enabled` flag (managed via `goplexcli library add/list/enable/disable/remove`)
- **tmdb_api_key** — TMDB API key (v3 key or v4 read token) for metadata and artwork when indexing local libraries. Without it, titles come from file names only.
- **library_listen** — Address the built-in library server binds (default `:47822`)
- **library_url** — Base URL other devices use to reach this machine's library server; stamped on every local item, so it must be reachable from wherever the cache is used. Defaults to the first LAN IPv4 address plus the library port.
- **library_name** — Label for locally indexed items (their server name). Defaults to the hostname.
- **library_token** — When set, required as `X-Plex-Token` on every library server request. Empty leaves the server open to the LAN.
- **library_probe** — Run `ffprobe` on each new file during indexing for exact durations and audio languages. Off by default (slow on cloud mounts; TMDB runtimes are used instead).
- **library_scan_interval** — How often `goplexcli daemon` rescans libraries, as a Go duration (`"1h"`, `"30m"`). Empty means 1h; `"0"` disables periodic rescans.
- **mpv_path**, **rclone_path**, **fzf_path** — Override tool paths if not in PATH
- **download_dir** — Default download destination (`~` is expanded). Override per-run with `--dest`.
- **sync_peer** — Hostname/IP (optionally `host:port`) of another computer to pull the cache from with LAN Cache Sync. Blank falls back to mDNS auto-discovery.
- **path_mappings** — Translate Plex file paths to rclone remotes. Longest matching prefix wins. Run `cache reindex` after changing.
- **webdav_user**, **webdav_pass**, **webdav_dir** — Shared credentials and optional subdirectory for gowebdav transfers (set via `goplexcli webdav set-creds`)
- **outplayer_targets** — Outplayer Wi-Fi transfer destinations, each with a `name`, `url`, optional `dir`, and `enabled` flag (managed via `goplexcli outplayer add/list/enable/disable/remove`)

### Setting Up rclone

Downloads pull the **original media file** directly from wherever your Plex
library is stored (a cloud drive, a remote server, a NAS, …) using
[rclone](https://rclone.org) — not by re-streaming from Plex. Plex reports each
item's on-disk file path; GoplexCLI rewrites that path into an rclone remote
path and runs `rclone copyto`. So there are two things to configure: an rclone
**remote** that can reach your media, and a **path mapping** that points Plex's
file paths at that remote.

**1. Install rclone** (see [Prerequisites](#prerequisites)) and confirm it's on
your PATH:

```bash
rclone version
```

**2. Create a remote** for the storage your Plex library lives on:

```bash
rclone config
```

Follow the prompts to add a remote (Google Drive, S3, SFTP, WebDAV, …). Give it
a memorable name — that name is what you reference in the mapping below. rclone
saves this in its own config (`~/.config/rclone/rclone.conf`); GoplexCLI only
references the remote by name, never its credentials. Verify it can see your
media:

```bash
rclone lsd myremote:
```

**3. Find your Plex file paths.** Check what path Plex reports for an item — via
`goplexcli cache search "title"`, Plex's own **Get Info → file path**, or by
looking on the Plex server. You'll see something like:

```
/mnt/media/tv/The Show/Season 01/episode.mkv
```

**4. Map the Plex path prefix to the rclone remote** with `path_mappings` in
your [config file](#config-file). Each mapping replaces a leading `prefix` with
`remote`:

```json
"path_mappings": [
  { "prefix": "/mnt/media/tv/", "remote": "myremote:Media/TV/" },
  { "prefix": "/mnt/media/",    "remote": "myremote:Media/" }
]
```

With the mapping above, the example path becomes
`myremote:Media/TV/The Show/Season 01/episode.mkv`. The **longest matching
prefix wins**, so list more specific prefixes first. The text after the colon is
the location *inside* the remote — adjust it so it matches where those files
actually live (the prefix and remote roots often line up, but they don't have
to).

**5. Re-index and test.** Path mappings are applied when the cache is built, so
re-index after changing them, then download something:

```bash
goplexcli cache reindex
goplexcli browse            # pick an item → Download
```

If a download fails with an "object/directory not found" error, the mapped path
doesn't match the remote's layout — check it with `rclone ls "myremote:Media/TV/…"`
and adjust the mapping.

**Tips**

- Set **`download_dir`** to control where files land (GUI: **Settings → Download
  directory**; CLI: `--dest`). It defaults to the current directory.
- If rclone isn't on your PATH, set **`rclone_path`** to its full path (GUI:
  **Settings → rclone path**).
- With **no** `path_mappings`, a legacy fallback strips a `/home/joshkerr/`
  prefix and infers the remote from the first path segment. That only fits the
  original author's setup, so configure your own `path_mappings`.

## How It Works

### Playback Progress

When you watch media through GoplexCLI, progress is tracked via MPV's IPC socket and reported back to your Plex server in real time. After playback ends, progress is also written to the local cache so items appear in **Continue Watching** immediately — no reindex needed.

Progress made on *other* Plex clients requires a `cache reindex` to refresh.

### Resume Playback

If a media item has saved progress, you'll be prompted to resume from your last position or start from the beginning.

### Download Queue

The queue is persistent between sessions and concurrent-safe (uses file locking). Multiple instances can add items while another downloads. Duplicate items are automatically deduplicated by key.

### Rclone Path Conversion

GoplexCLI translates Plex on-disk file paths to rclone remote paths for downloads, then runs `rclone copyto` to fetch the original file. See [Setting Up rclone](#setting-up-rclone) for the full walkthrough; in short, `path_mappings` rewrites a Plex path prefix into a `remote:path` (longest prefix wins), with a legacy `/home/joshkerr/` fallback when none is configured.

## Troubleshooting

| Problem | Solution |
|---------|----------|
| "fzf not found" | Install fzf (see Prerequisites) |
| "mpv not found" | Install mpv (see Prerequisites) |
| "rclone not found" | Install rclone and run `rclone config` to set up remotes |
| "Cache is empty" | Run `goplexcli cache reindex` (or `goplexcli library scan` for local folders) |
| Locally indexed items won't play or show posters | The library server must be running (`goplexcli daemon`, or the desktop GUI) and `library_url` must be reachable from the playing device. Check with `goplexcli library list`. |
| Local titles have no summaries or artwork | Set `tmdb_api_key` in config and run `goplexcli library scan --full` |
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
│   ├── library/         # Local folder indexer: name parsing, scanning, TMDB enrichment
│   ├── libserver/       # Plex-compatible HTTP server for locally indexed media
│   ├── logging/         # Logging utilities
│   ├── outplayer/       # Outplayer Wi-Fi transfer uploads (streamed via rclone)
│   ├── player/          # MPV player wrapper
│   ├── plex/            # Plex API client (SDK + direct HTTP)
│   ├── preview/         # fzf preview pane renderer
│   ├── progress/        # MPV IPC progress tracker
│   ├── queue/           # Persistent download queue with file locking
│   ├── stream/          # Stream server, mDNS, and web UI
│   ├── termuxfix/       # Termux/Android compatibility
│   ├── tmdb/            # TMDB API client with on-disk response cache
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
