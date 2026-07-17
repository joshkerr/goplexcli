# Sidebar BrandMark Logo — Design

**Date:** 2026-07-17
**Status:** Approved

## Problem

The sidebar header still shows the pre-rebrand logo: a gold gradient square
(`from-accent to-accent-dark`) containing `FilmIcon`. The desktop app icon and
the Setup screen were rebranded (dark squircle tile, cyan→violet→pink→red
triangle-and-bar glyph, subtle glow) but the sidebar was left behind.

## Design

Replace the sidebar badge with the existing `BrandMark` component
(`gui/frontend/src/components/icons.tsx`) rendered at 36×36, next to the
unchanged "Goplex / Media" wordmark.

- Remove the badge wrapper div (its `shadow-glow` is redundant; `BrandMark`
  carries its own glow).
- Keep the `FilmIcon` import — the Movies nav item still uses it.
- No new artwork, colors, components, or backend changes. One brand mark
  everywhere (dock icon, Setup, sidebar).

## Verification

- `npm run build` (tsc + vite) passes.
- Visual check of the sidebar header.
