# New Episodes Show Cards & People Search — Design

**Date:** 2026-07-17
**Status:** Approved

## Problem

1. The New Episodes grid (`recently-added-tv`) shows one card per episode.
   Users want one card per show (the parent show's poster), like the TV Shows
   grid.
2. Searching by actor or director requires knowing the `cast:"…"` /
   `director:"…"` query syntax; nothing in the UI reveals it exists outside
   clicking a name in an open detail modal.

## Design

### 1. Backend: recentShowCards (gui/media.go)

- New helper `recentShowCards(a, c, limit)`: take the `limit` (60) newest
  episodes by AddedAt (today's pool), group by show (ParentTitle), preserving
  newest-first order. One card per show:
  - `Key: "show:<title>"` (opens the existing show detail modal),
  - `Type: "show"`, show poster art via `showThumbURL`,
  - `NewCount`: how many of the recent episodes belong to this show.
- `NewCount` is a new `MediaCardDTO` field (json `newCount`), set only here.
- `ListCategory("recently-added-tv")` returns `recentShowCards`; the doc
  comment is updated. Nothing else changes.

### 2. Frontend: "N new" badge (PosterCard.tsx)

- When `card.newCount > 0`, render a small accent badge ("3 new") on the
  poster. Only New Episodes cards carry the field, so no category-aware
  props; TV Shows cards (total `episodeCount`) are unaffected.

### 3. Backend: SearchPeople (gui/media.go)

- New bound method `SearchPeople(query string) []PersonDTO` with
  `PersonDTO{Name, Role, Count}` — role `"director"` or `"actor"`, count =
  number of movies carrying the tag.
- Scans cached movies' `Director`/`Cast` comma-split tags, dedupes
  case-insensitively (first spelling wins), matches case-insensitive
  substring for queries of 2+ characters, ranks prefix matches first then by
  count desc, caps at 8. Movies only — the same scope `cast:`/`director:`
  filtering already has. Index built per call (linear scan behind a 220 ms
  debounce); memoize later only if it proves slow.

### 4. Frontend: people chips (App.tsx, api.ts, types.ts)

- The debounced search effect also calls `api.searchPeople(query)` for plain
  queries (not `field:"…"` ones). Matches render as a "People" chip row above
  the results grid: name + role label. Clicking a chip calls the existing
  `runFieldSearch` with `director:"Name"` / `cast:"Name"`. Chips never show
  during an active field query.

## Testing

- TDD (Go): `recentShowCards` grouping, ordering, NewCount; `SearchPeople`
  matching, dedup, ranking, cap, 2-character minimum.
- Frontend: production build (tsc + vite); manual visual check.
