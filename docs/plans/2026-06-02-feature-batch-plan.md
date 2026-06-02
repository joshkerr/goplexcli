# Feature Batch Plan — 2026-06-02

Implementation plan for five features. Build order: **#2 → #4 → #1 → #3 → #5**
(trivial/independent first, then config-touching, then cache/UI, then the one
needing a release-process change).

A key finding shaping #3: the cache `MediaItem` already stores `ViewOffset`,
`ViewCount`, and `AddedAt` (`internal/plex/client.go:60-67`), so the hubs can be
built almost entirely from the local cache with no new Plex calls.

---

## Feature 1 — Configurable rclone path mapping

**Why:** `convertToRclonePath` (`internal/plex/client.go:745`) hardcodes
`strings.TrimPrefix(filePath, "/home/joshkerr/")`, making the tool author-only.

**Approach**
- `internal/config/config.go`: add
  ```go
  type PathMapping struct {
      Prefix string `json:"prefix"` // e.g. "/home/joshkerr/plexcloudservers2/"
      Remote string `json:"remote"` // e.g. "plexcloudservers2:"
  }
  ```
  and `PathMappings []PathMapping` on `Config`.
- `internal/plex/client.go`: store mappings on the `Client`; rewrite
  `convertToRclonePath` to try each mapping longest-prefix-first
  (`m.Remote + filePath[len(m.Prefix):]`), falling back to the legacy
  `/home/joshkerr/` heuristic when nothing matches (so existing installs keep
  working).
- Thread mappings into the client used by `cache reindex`/`update`.

**Edge cases:** empty mappings → legacy fallback; trailing-slash normalization;
first/longest match wins. **Tests:** table-driven `convertToRclonePath`.
**Effort:** Medium.

---

## Feature 2 — Shell completions

**Approach**
- Explicit `completion` command (cobra provides the generator).
- Dynamic `ValidArgsFunction` on `server enable/disable/remove` returning
  configured server names; static `ValidArgs` on `sort [field]`.
- `cmd/goplexcli/main.go` only. README: install instructions incl. PowerShell.

**Effort:** Small. Most independent — good warm-up.

---

## Feature 3 — "Continue Watching" & "Recently Added" hubs

**Approach (MVP, cache-only)**
- `internal/ui/fzf.go` `SelectMediaTypeWithQueue`: prepend
  `Continue Watching (N)` (only if N>0) and `Recently Added`.
- `cmd/goplexcli/main.go` `runBrowse` switch: build `filteredMedia` from cache:
  - **Continue Watching:** `ui.HasResumableProgress(item)`, sorted most-recent.
  - **Recently Added:** sort by `AddedAt` desc, top ~50.
- Add `LastViewedAt int64` to `MediaItem` + fetch mapping for correct ordering.

**Caveat:** incremental `cache update` only fetches `addedAt >= since`
(`client.go:422`), so progress on older items won't refresh — Continue Watching
from cache can be stale. MVP documents `cache reindex` refreshes progress.
Follow-up enhancement: `client.GetOnDeck()` against `/library/onDeck` for a live
hub.

**Effort:** Medium (MVP); +Medium for live on-deck.

---

## Feature 4 — Configurable download destination

**Why:** `handleDownloadMultiple` (`main.go:1296`) hardcodes `os.Getwd()`.

**Approach**
- `config.go`: add `DownloadDir string json:"download_dir,omitempty"`.
- `main.go`: resolve dest = `cfg.DownloadDir` (expand `~`/env, `os.MkdirAll`) else
  cwd; add `--dest` flag to override per-run. Queue downloads inherit it.
- Update dry-run message to show resolved destination.

**Effort:** Small.

---

## Feature 5 — Self-update command

**Approach**
- New `goplexcli update` command + `internal/update` package, implemented with
  the stdlib (no new dependency): query
  `https://api.github.com/repos/joshkerr/goplexcli/releases/latest`, compare
  `tag_name` vs `version`, match an asset by `GOOS/GOARCH`
  (`goplexcli-<os>-<arch>[.exe]`), download to temp, swap with the running
  binary via the rename dance (rename current → `.old`, move new into place;
  best-effort remove `.old` for Windows).
- Flags: `--check` (report only). Skip when `version == "dev"`.

**Prerequisite:** releases must publish `build-all` artifacts with consistent
asset names + semver tags (GitHub Action or documented `gh release upload`).

**Effort:** Medium (command small; release pipeline is the real work).

---

### Cross-cutting
- #1 and #4 both extend `Config` (backward-compatible, `omitempty`).
- Each feature lands as its own logical change with table-driven tests beside the
  existing `_test.go` files; `make test`, `make vet`, `make lint` already exist.
