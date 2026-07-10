# Project Improvement Roadmap

This document records potential improvements and features for future work.
The current baseline already includes multi-server support, configurable path
mappings and download destinations, Continue Watching and Recently Added hubs,
self-update, streaming and transfer targets, tests, and a Wails desktop GUI.

## Recommended roadmap

1. Add a `doctor` command and matching GUI diagnostics.
2. Add background progress and on-deck synchronization.
3. Move shared workflows out of CLI and GUI adapters into application services.
4. Replace the JSON cache with SQLite, including automatic migration.
5. Add rich filtering and saved smart views.
6. Unify playback, download, and transfer queue management.
7. Add multi-server deduplication and media-version selection.
8. Expand the remote web interface into a secure companion interface.

## Reliability and architecture

### SQLite media cache

- Indexed search, sorting, and filtering for large libraries.
- Atomic incremental updates and safer concurrent CLI/GUI access.
- Per-server refresh state and schema migrations.
- Full-text search over titles, summaries, cast, directors, and genres.
- Automatic migration from the existing JSON cache.

### Background synchronization

- Fast on-deck and playback-progress refreshes.
- Incremental updates at startup and optional periodic GUI refreshes.
- Per-server and per-library last-synchronized state.
- Cancellation, retries, and exponential backoff.
- Make full reindexing a repair operation rather than routine maintenance.

### Shared application services

Move workflow orchestration into packages such as `internal/app`, with Cobra
and Wails acting as adapters. Candidate services include browsing, indexing,
playback, transfer, queue, and server management. This reduces behavioral
drift and allows workflows to be tested without invoking UI code.

### Testing priorities

Expand coverage around integration boundaries that currently have limited or
no direct tests, especially download, preview, stream, and WebDAV behavior.
Important cases include interrupted downloads, HTTP range requests, expired
tokens, duplicate multi-server media, corrupt cache recovery, concurrent CLI
and GUI access, and Windows-specific paths and processes.

## Usability improvements

### Setup and diagnostics

Add `goplexcli doctor` and an equivalent GUI screen to verify:

- Plex reachability and authentication.
- Config and cache permissions.
- Availability and versions of mpv, rclone, fzf, and chafa.
- Configured rclone remotes and path translations.
- Download destination access and free space.

Add a path-mapping wizard that samples Plex file paths, lists rclone remotes,
proposes mappings, tests translated files, and previews the conversion.

### Rich filtering and smart views

Support filtering by server, library, media type, watched state, genre, year,
rating, runtime, resolution, codec, HDR, audio format, actor, and director.
Allow filters and sorts to be saved as named views shared by CLI and GUI.

Examples:

- Unwatched movies rated above 7.
- Episodes added this week.
- Continue Watching sorted shortest-first.
- Movies under two hours.
- Items available on a preferred server.

### Unified queue and playlists

- Reorder and remove entries.
- Named persistent playlists.
- Shuffle, repeat, and Play Next.
- Pause, resume, retry, and transfer history.
- Apply one queue to playback, downloads, WebDAV, Outplayer, or streaming.

## Feature ideas

### Playback controls

- Audio and subtitle track selection.
- Preferred languages and forced-subtitle rules.
- Quality and direct-play/transcode selection.
- MPV profiles and custom arguments.
- GUI now-playing controls through MPV IPC.

### Remote companion interface

- Browse and search the cached library remotely.
- Continue Watching and remote queue management.
- QR-code and short-code pairing.
- Remember paired devices.
- Authentication and short-lived signed stream URLs.
- Localhost-only binding unless LAN access is explicitly enabled.

### Download intelligence

- Disk-space checks and configurable concurrency.
- Bandwidth limits and schedules.
- Retry and resume.
- Filename templates and sidecar subtitle/metadata downloads.
- Transfer verification and per-target presets.
- Skip-existing and overwrite policies.

### Multi-server deduplication

Group copies using Plex GUIDs where possible and expose available versions
under one title. Select a preferred source using server priority, locality,
latency, resolution, bitrate, and current availability.

### Configuration profiles

Allow named profiles such as `home` and `travel` to select servers, download
destinations, path mappings, preferred player, quality limits, and transfer
targets.

## Maintenance and release work

- Keep `AGENTS.md` synchronized with current tests and implemented features.
- Build and test the Wails/React application in CI.
- Pin the golangci-lint version for reproducible builds.
- Add explicit config and cache migration frameworks.
- Generate CLI/config reference documentation from source definitions.
- Add a token-redacting support-bundle command.
- Publish checksums and optionally signed releases.
- Improve GUI keyboard navigation, focus handling, screen-reader labels,
  scalable text, and reduced-motion behavior.
