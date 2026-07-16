# Persistent Error Toasts — Design

**Date:** 2026-07-16
**Status:** Approved

## Problem

The GUI automatically removes red error toasts after six seconds. Playback
errors commonly arrive while mpv or another window has focus, so users can
miss the message before returning to GoplexCLI.

## Goals

- Keep every red error toast visible until the user dismisses it.
- Preserve the current short auto-dismiss behavior for informational toasts.
- Keep the existing bottom-right presentation and click-to-dismiss interaction.

Persisting errors across application restarts and adding a notification history
are out of scope.

## Design

`App.tsx` remains responsible for toast state and lifetime. Its `toast()`
callback will continue adding both toast kinds to state, but it will schedule an
auto-dismiss timer only for informational toasts. Error toasts will have no
timer and will leave state only through the existing `onDismiss` callback when
the user clicks them.

No changes are needed to the `Toast` type or `Toasts` component. Multiple errors
may stack, and each can be dismissed independently.

This lifetime rule applies to all error toasts, including playback, setup,
download, indexing, settings, and startup failures.

## Testing

- Add a focused frontend test proving informational toasts receive an
  auto-dismiss delay and error toasts do not.
- Run the frontend production build.
- Run the existing Go suite to ensure the surrounding playback work remains
  green.
