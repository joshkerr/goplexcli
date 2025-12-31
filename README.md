# GoplexCLI

A powerful, fast, and elegant command-line interface for browsing and streaming media from your Plex server.

## Features

- **Browse Media**: Quickly browse your entire Plex library using fzf's fuzzy finder
- **Rich Previews**: View movie posters and detailed metadata in the preview window
- **Stream with MPV**: Watch movies and TV shows directly with MPV player
- **Download with Rclone**: Download media files to your local system with beautiful progress bars
- **Smart Caching**: Cache your media library locally for instant browsing
- **Media Type Filtering**: Filter by Movies, TV Shows, or browse all media
- **Cross-Platform**: Works on macOS, Linux, and Windows
- **Beautiful UI**: Built with Charm libraries for a polished terminal experience

## Prerequisites

Before using GoplexCLI, ensure you have the following installed:

- **Go** 1.20+ (for building from source)
- **fzf** - Fuzzy finder for browsing media
  - macOS: `brew install fzf`
  - Linux: `sudo apt install fzf` or `sudo pacman -S fzf`
  - Windows: `choco install fzf`
- **mpv** - Media player for streaming
  - macOS: `brew install mpv`
  - Linux: `sudo apt install mpv` or `sudo pacman -S mpv`
  - Windows: Download from [mpv.io](https://mpv.io)
- **rclone** - For downloading media files
  - macOS: `brew install rclone`
  - Linux: `sudo apt install rclone` or download from [rclone.org](https://rclone.org)
  - Windows: Download from [rclone.org](https://rclone.org)

## Installation

### From Source

```bash
git clone https://github.com/joshkerr/goplexcli.git
cd goplexcli
make build
```

This builds both `goplexcli` (main application) and `goplexcli-preview` (preview helper).

Then install to your PATH:

```bash
# Using make (installs to /usr/local/bin)
make install

# Or manually
sudo cp goplexcli goplexcli-preview /usr/local/bin/

# Or add project directory to PATH
export PATH="$PATH:/path/to/goplexcli"
```

## Quick Start

### 1. Login to Plex

First, authenticate with your Plex account:

```bash
goplexcli login
```

You'll be prompted for your Plex username and password. Your credentials are used only for authentication and the resulting token is saved securely in your config directory.

### 2. Build Media Cache

Index your media library:

```bash
goplexcli cache reindex
```

This will fetch all your movies and TV shows from your Plex server and cache them locally for fast browsing.

### 3. Browse and Play

Launch the media browser:

```bash
goplexcli browse
```

This will open fzf with your entire media library. Use the arrow keys or type to search, then:

- Press **Enter** to select a media item
- Choose **Watch** to stream with MPV
- Choose **Download** to download with rclone

## Commands

### `goplexcli login`

Authenticate with your Plex account and save credentials.

```bash
goplexcli login
```

### `goplexcli browse`

Browse and play media from your Plex server.

```bash
goplexcli browse
```

**Features:**
- Select media type (Movies, TV Shows, or All)
- Fuzzy search across your entire library
- Press **i** to toggle preview window with:
  - Year, rating, duration
  - Plot summary
  - File path
- Press **Enter** to select media
- Choose **Watch** to stream or **Download** to save locally

### `goplexcli cache`

Manage your local media cache.

#### Update Cache

Update the cache with new media (incremental):

```bash
goplexcli cache update
```

#### Rebuild Cache

Rebuild the entire cache from scratch:

```bash
goplexcli cache reindex
```

#### Cache Info

View cache statistics:

```bash
goplexcli cache info
```

### `goplexcli config`

Display current configuration:

```bash
goplexcli config
```

## Configuration

Configuration files are stored in platform-specific directories:

- **macOS**: `~/.config/goplexcli/`
- **Linux**: `~/.config/goplexcli/` or `$XDG_CONFIG_HOME/goplexcli/`
- **Windows**: `%APPDATA%\goplexcli\`

### Config File Structure

The `config.json` file contains:

```json
{
  "plex_url": "http://your-plex-server:32400",
  "plex_token": "your-auth-token",
  "plex_username": "your-username",
  "mpv_path": "mpv",
  "rclone_path": "rclone",
  "fzf_path": "fzf"
}
```

You can manually edit this file to set custom paths for mpv, rclone, or fzf if they're not in your PATH.

## How It Works

### Media Caching

GoplexCLI caches your media library locally to enable fast, offline browsing with fzf. The cache stores:

- Movie titles, years, and metadata
- TV show names, season and episode numbers
- File paths for streaming and downloading
- Rclone remote paths (automatically converted from Plex paths)
- Poster/thumbnail URLs for preview display

**Cache Location:**
- macOS/Linux: `~/.config/goplexcli/cache/media.json`
- Windows: `%APPDATA%\goplexcli\cache\media.json`

**Poster Cache:**
- Posters are cached in `/tmp/goplexcli-posters/` to avoid re-downloading
- Cache persists until system reboot

### Rclone Path Conversion

GoplexCLI automatically converts Plex file paths to rclone remote paths. For example:

**Plex Path:**
```
/home/joshkerr/plexcloudservers2/Media/TV/ShowName/Season 01/Episode.mkv
```

**Rclone Path:**
```
plexcloudservers2:/Media/TV/ShowName/Season 01/Episode.mkv
```

The conversion:
1. Removes the `/home/joshkerr/` prefix
2. Adds a `:` after the remote name (e.g., `plexcloudservers2`)

### Streaming

When you choose to watch a media item, GoplexCLI:

1. Requests a direct stream URL from your Plex server
2. Launches MPV with the stream URL
3. MPV handles the playback with seeking and buffering

### Downloading

When you choose to download a media item, GoplexCLI:

1. Extracts the rclone remote path from the cached media
2. Uses rclone to copy the file to your current directory
3. Displays a progress bar during download (via rclone-golib)

## Troubleshooting

### "fzf not found"

Install fzf using your package manager (see Prerequisites).

### "mpv not found"

Install mpv using your package manager (see Prerequisites).

### "rclone not found"

Install rclone and ensure it's configured with your remotes:

```bash
rclone config
```

### "Cache is empty"

Run `goplexcli cache reindex` to build your media cache.

### "Preview binary not found"

Ensure both binaries are installed:

```bash
make build
make install
# Or add the project directory to your PATH
```

### Authentication Issues

If you're having trouble logging in:

1. Verify your Plex username and password
2. Check that your Plex server is accessible
3. Try manually editing `~/.config/goplexcli/config.json` with your server URL and token

## Project Structure

```
goplexcli/
├── cmd/
│   ├── goplexcli/
│   │   └── main.go          # Main CLI application
│   └── preview/
│       └── main.go          # Preview helper for fzf
├── internal/
│   ├── cache/
│   │   └── cache.go         # Media caching logic
│   ├── config/
│   │   └── config.go        # Configuration management
│   ├── download/
│   │   └── download.go      # Rclone download integration
│   ├── player/
│   │   └── player.go        # MPV player integration
│   ├── plex/
│   │   └── client.go        # Plex API client
│   └── ui/
│       └── fzf.go           # fzf integration
├── Makefile                 # Build automation
├── go.mod
├── go.sum
├── .gitignore
└── README.md
```

## Dependencies

- [LukeHagar/plexgo](https://github.com/LukeHagar/plexgo) - Plex API SDK
- [charmbracelet/lipgloss](https://github.com/charmbracelet/lipgloss) - Terminal styling
- [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [spf13/cobra](https://github.com/spf13/cobra) - CLI framework
- [joshkerr/rclone-golib](https://github.com/joshkerr/rclone-golib) - Rclone integration with progress bars
- [golang.org/x/term](https://golang.org/x/term) - Secure terminal input

**External Tools:**
- [fzf](https://github.com/junegunn/fzf) - Fuzzy finder
- [mpv](https://mpv.io) - Media player
- [rclone](https://rclone.org) - Cloud storage sync

## Contributing

Contributions are welcome! Please feel free to submit issues or pull requests.

## License

MIT License - See LICENSE file for details

## Acknowledgments

- Built with [Charm](https://charm.sh/) libraries for beautiful terminal UIs
- Plex API integration via [plexgo](https://github.com/LukeHagar/plexgo)
- File downloads via [rclone-golib](https://github.com/joshkerr/rclone-golib)
