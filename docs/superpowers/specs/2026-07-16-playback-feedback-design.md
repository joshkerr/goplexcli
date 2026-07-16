# GUI Playback Feedback — Design

**Date:** 2026-07-16
**Status:** Approved

## Problem

Clicking a play button in the GUI sometimes results in nothing happening, with no
feedback. Two root causes:

1. `internal/player/player.go` discards mpv's stderr and deliberately returns
   `nil` on any non-zero exit (`cmd.Wait()` error is swallowed to avoid treating
   "user quit" as a failure). When mpv launches and immediately dies — bad
   stream URL, expired token, codec failure — `App.Play` returns success and the
   UI shows nothing.
2. There is no "what is it doing" feedback between the click and the mpv window
   appearing. The backend emits `playback:started`/`playback:stopped` Wails
   events, but the frontend never subscribes to them.

Two smaller silent paths: `resolveItems` silently drops requested keys missing
from the cache (errors only when *all* are missing), and the multi-server
playlist path falls back to the first item's Plex client when creating a
per-item client fails, producing a stream URL on the wrong server.

## Goals

- Show what playback is doing (progress stages) via the existing toast system.
- When playback fails, show an error toast containing the real cause, including
  mpv's own error output.
- User quitting mpv is not an error and produces no error feedback.

Non-goals: a persistent Now Playing UI, playback state polling API (YAGNI —
the stage events leave room for these later).

## Design

### 1. Player package: stop swallowing mpv failures

In `internal/player/player.go`:

- Capture mpv's stderr into a bounded buffer (last ~40 lines) while it runs.
- Interpret mpv's documented exit codes:
  - `0` — success, includes user quit. Not an error (unchanged behavior).
  - `1` — fatal error (bad options, initialization failure).
  - `2` — file could not be played.
  - `3` — some files played, some failed.
- Codes 1–3 return a new typed error:

  ```go
  type PlaybackError struct {
      ExitCode int
      Detail   string // last stderr line matching an error pattern,
                      // falling back to the last non-empty line
  }
  ```

- `Error()` reads like: `mpv exited 2: Failed to open https://… (HTTP 401)`.
- The CLI shares this path, so command-line playback failures start being
  reported too — intentional side benefit.

### 2. Backend: stage events from `App.Play` (gui/playback.go)

Replace the two unused ad-hoc events with one `playback:status` event:

```json
{ "stage": "preparing" | "starting" | "playing" | "warning" | "stopped",
  "title": "…", "count": 2, "detail": "…" }
```

- `preparing` — emitted on entry, after arg validation (items being resolved,
  Plex client being created).
- `starting` — stream URLs fetched, mpv is being launched.
- `playing` — mpv's IPC socket connected (playback actually underway).
- `warning` — something non-fatal, `detail` holds the message (see below).
- `stopped` — mpv exited normally.

**Errors do not get an event.** Failures keep flowing through `Play`'s returned
error → Wails rejected promise → the existing `catch` + toast in each play
handler. One channel per concern; no double-toasting.

Silent-path fixes:

- `resolveItems`: when some (not all) requested keys are missing from the
  cache, emit a `warning` stage event ("2 of 5 items not in cache — playing the
  rest") and continue with the found items. All missing stays a hard error.
- Multi-server fallback (`playback.go:55`): failing to create the per-item
  client becomes a hard error naming the item, instead of silently using the
  first item's client.

### 3. Frontend: one subscription, existing toasts

- A single `useEffect` in `App.tsx` subscribes with the existing-but-unused
  `onEvent` helper (`gui/frontend/src/lib/api.ts:120`), unsubscribing on
  cleanup.
- Stage → toast mapping, via the existing `toast()` (auto-dismissing):
  - `preparing` → info toast "Preparing *Title*…"
  - `playing` → info toast "Playing *Title*"
  - `warning` → error-styled toast with `detail`
  - `starting`, `stopped` → no toast (avoid noise; `starting` is subsumed by
    `preparing` unless the gap proves long in practice)
- Error toasts stay in the play handlers' existing catch blocks — they now
  carry the real cause from `PlaybackError`.
- No new components; `Toasts.tsx` unchanged.

### 4. Error handling and edge cases

- `cmd.Start()` failure is already an error today; unchanged.
- Stderr buffer is bounded, so a chatty mpv cannot grow memory unbounded.
- mpv absent / cache empty / stream-URL failures already reject the promise and
  toast today; unchanged.
- User quit → exit code 0 → no error, no error toast.

### 5. Testing

- Unit tests in `internal/player`: exit-code → error mapping and
  stderr-tail/error-line extraction, driven by a stub executable (shell script)
  that exits with a chosen code and stderr — no real mpv required.
- Existing `buildMPVArgs` tests unchanged.
- Manual verification: normal play; user quit (no toast); forced failure (bad
  stream URL / revoked token) shows the detailed error toast; multi-episode
  selection shows preparing/playing toasts.
