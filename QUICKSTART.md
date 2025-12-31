# GoplexCLI Quick Start Guide

This guide will help you get GoplexCLI up and running in minutes.

## Prerequisites Check

Before starting, verify you have the required tools installed:

```bash
# Check fzf
fzf --version

# Check mpv
mpv --version

# Check rclone
rclone version

# Check Go (if building from source)
go version
```

If any are missing, install them:

### macOS
```bash
brew install fzf mpv rclone
```

### Linux (Debian/Ubuntu)
```bash
sudo apt install fzf mpv rclone
```

### Linux (Arch)
```bash
sudo pacman -S fzf mpv rclone
```

## Installation

### Option 1: Build from Source

```bash
# Clone the repository
git clone https://github.com/joshkerr/goplexcli.git
cd goplexcli

# Build
make build

# Install (optional)
make install
```

### Option 2: Download Pre-built Binary

Download the latest release for your platform from the [releases page](https://github.com/joshkerr/goplexcli/releases).

Extract and install both binaries:

```bash
# macOS/Linux
tar xzf goplexcli-*.tar.gz
sudo mv goplexcli goplexcli-preview /usr/local/bin/
```

## First Time Setup

### Step 1: Login to Plex

```bash
goplexcli login
```

Enter your Plex username and password when prompted. Your credentials are only used for authentication, and the resulting token is saved securely.

### Step 2: Build Media Cache

```bash
goplexcli cache reindex
```

This fetches your entire media library from Plex and caches it locally. This may take a minute depending on your library size.

### Step 3: Browse Media

```bash
goplexcli browse
```

This opens fzf with your media library. You can:
- Select media type (Movies, TV Shows, or All)
- Type to search
- Use arrow keys to navigate
- Press **i** to toggle preview window (shows posters and metadata)
- Press Enter to select
- Press Esc to cancel

After selecting media, choose:
- **Watch**: Stream with MPV
- **Download**: Download with rclone
- **Cancel**: Go back

## Common Workflows

### Watch a Movie

```bash
goplexcli browse
# Type movie name
# Press Enter
# Select "Watch"
```

### Download a TV Episode

```bash
goplexcli browse
# Type show name
# Select episode
# Select "Download"
```

### Update Cache with New Media

```bash
goplexcli cache update
```

Run this after adding new media to your Plex server.

### View Configuration

```bash
goplexcli config
```

Shows your current configuration and file locations.

### Check Cache Status

```bash
goplexcli cache info
```

Shows cache statistics (number of movies, episodes, last update time).

## Troubleshooting

### "fzf not found"

Install fzf:
```bash
# macOS
brew install fzf

# Linux
sudo apt install fzf
```

### "mpv not found"

Install mpv:
```bash
# macOS
brew install mpv

# Linux
sudo apt install mpv
```

### "rclone not found"

Install and configure rclone:
```bash
# macOS
brew install rclone

# Linux
sudo apt install rclone

# Configure remotes
rclone config
```

### "Cache is empty"

Build the cache:
```bash
goplexcli cache reindex
```

### "Preview binary not found"

Ensure both binaries are installed:
```bash
# If built from source
cd goplexcli
make install

# Verify both are in PATH
which goplexcli goplexcli-preview
```

### Authentication Failed

1. Verify your Plex credentials
2. Check your Plex server is running
3. Ensure your Plex server is accessible

### Can't Download Files

Make sure rclone is configured with the correct remotes. The remote names should match the directory names in your Plex file paths.

For example, if Plex shows files at:
```
/home/joshkerr/plexcloudservers2/Media/...
```

You need an rclone remote named `plexcloudservers2`:
```bash
rclone config
# Add remote named "plexcloudservers2"
```

## Advanced Usage

### Custom Binary Paths

Edit `~/.config/goplexcli/config.json`:

```json
{
  "plex_url": "http://your-server:32400",
  "plex_token": "your-token",
  "mpv_path": "/custom/path/to/mpv",
  "rclone_path": "/custom/path/to/rclone",
  "fzf_path": "/custom/path/to/fzf"
}
```

### Manual Configuration

If automatic login fails, manually edit the config:

```json
{
  "plex_url": "http://192.168.1.100:32400",
  "plex_token": "YOUR_PLEX_TOKEN"
}
```

To get your Plex token, see: https://support.plex.tv/articles/204059436-finding-an-authentication-token-x-plex-token/

## Next Steps

- Read the full [README](README.md) for detailed information
- Check out [CONTRIBUTING](CONTRIBUTING.md) if you want to contribute
- Report bugs or request features on [GitHub Issues](https://github.com/joshkerr/goplexcli/issues)

## Tips & Tricks

### Keyboard Shortcuts in fzf

- `i`: Toggle preview window
- `Ctrl-J/Ctrl-K`: Move down/up
- `Ctrl-D/Ctrl-U`: Scroll down/up half page
- `Tab`: Mark multiple items (if enabled)
- `Esc`: Cancel

### Quick Search Examples

In the browse interface:
- `inception` - Find movies/shows with "inception" in the title
- `s01e01` - Find first episodes of shows
- `2023` - Find media from 2023

### Regular Cache Updates

Add to crontab for daily updates:
```bash
crontab -e
# Add:
0 2 * * * /usr/local/bin/goplexcli cache update
```

Enjoy using GoplexCLI! 🎬
