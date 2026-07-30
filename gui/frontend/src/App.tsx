import { useCallback, useEffect, useRef, useState } from "react";
import { api, onEvent } from "./lib/api";
import type {
  Category,
  DownloadProgress,
  Media,
  MediaCard,
  Person,
  PlaybackStatus,
  ReindexProgress,
  SortField,
  Status,
} from "./lib/types";
import { Sidebar, type NavKey } from "./components/Sidebar";
import { PosterGrid } from "./components/PosterGrid";
import { DetailModal } from "./components/DetailModal";
import { DownloadsPanel } from "./components/DownloadsPanel";
import { Splash } from "./components/Splash";
import { Toasts, type Toast } from "./components/Toasts";
import { Setup } from "./views/Setup";
import { Settings } from "./views/Settings";
import { SearchIcon, SyncIcon } from "./components/icons";

const CATEGORY_TITLES: Record<NavKey, string> = {
  movies: "Movies",
  "tv-shows": "TV Shows",
  "continue-watching": "Continue Watching",
  "watch-again": "Watch Again",
  "favorites-movies": "Favorite Movies",
  "favorites-tv": "Favorite TV Shows",
  "recently-added-movies": "Recently Added Movies",
  "recently-added-tv": "Recently Added Episodes",
  downloads: "Downloads",
  settings: "Settings",
};

const EMPTY_MESSAGES: Partial<Record<NavKey, string>> = {
  movies: "No movies in your library yet.",
  "tv-shows": "No TV shows in your library yet.",
  "continue-watching": "Nothing in progress — start watching something!",
  "watch-again": "Nothing watched yet.",
  "favorites-movies": "No favorite movies yet — click the star on a movie to add it.",
  "favorites-tv": "No favorite shows yet — click the star on a show to add it.",
  "recently-added-movies": "No movies indexed yet.",
  "recently-added-tv": "No episodes indexed yet.",
};

// Category nav keys (everything except the Downloads/Settings panels).
function isCategory(k: NavKey): k is Category {
  return k !== "downloads" && k !== "settings";
}

// Per-category sort preferences, persisted to localStorage so each grid
// remembers its order across launches.
interface SortPref {
  sortField: SortField;
  desc: boolean;
}

const SORT_STORAGE_KEY = "goplex:sortPrefs";
// Unlike the session-scoped genre filter, hiding foreign-language films is a
// standing preference, so it persists across launches.
const HIDE_FOREIGN_KEY = "goplex:hideForeign";

function loadHideForeign(): boolean {
  try {
    return localStorage.getItem(HIDE_FOREIGN_KEY) === "1";
  } catch {
    return false;
  }
}
const SORTABLE_CATEGORIES: Category[] = ["movies", "favorites-movies", "tv-shows", "favorites-tv"];
// Show cards only carry title/year/added-order; the other fields are movie-only.
const TV_CATEGORIES: Category[] = ["tv-shows", "favorites-tv"];
const TV_SORT_FIELDS: SortField[] = ["title", "year", "added"];

// TV Shows historically lists shows with the newest episodes first; keep that
// as its default. Everything else defaults to title A-Z.
const SORT_DEFAULTS: Partial<Record<Category, SortPref>> = {
  "tv-shows": { sortField: "added", desc: true },
};
const FALLBACK_SORT: SortPref = { sortField: "title", desc: false };

function loadSortPrefs(): Partial<Record<Category, SortPref>> {
  try {
    return JSON.parse(localStorage.getItem(SORT_STORAGE_KEY) ?? "{}");
  } catch {
    return {};
  }
}

// searchHeading turns a query into the header shown above the results. A
// field-scoped query (director:"…" / cast:"…" / genre:"…", produced by clicking
// a name in the detail modal) gets a friendly label; anything else falls back to
// the raw search string.
function searchHeading(query: string): string {
  const m = /^(director|cast|genre):"?(.+?)"?$/i.exec(query.trim());
  if (m) {
    const field = m[1].toLowerCase();
    const value = m[2];
    if (field === "director") return `Directed by ${value}`;
    if (field === "cast") return `Starring ${value}`;
    return `${value} movies`; // genre
  }
  return `Search: “${query}”`;
}

// One toast tag shared by every message of a library sync/update/reindex run,
// so progress updates replace the toast in place instead of stacking.
const SYNC_TOAST = "library-sync";

// syncDoneMessage builds the final toast for a finished update/reindex,
// describing what changed by diffing the library counts from before the run.
function syncDoneMessage(
  d: { mode?: "reindex" | "update"; count: number; added?: number },
  before: Status | null,
  after: Status | null
): string {
  const total = (after?.cacheCount ?? d.count).toLocaleString();
  if (d.mode === "reindex") {
    return `Reindex complete — ${d.count.toLocaleString()} items in library`;
  }
  if (!d.added) return `Library up to date — no new items (${total} total)`;
  if (before && after) {
    const parts: string[] = [];
    const movies = after.movieCount - before.movieCount;
    const shows = after.showCount - before.showCount;
    const episodes = after.episodeCount - before.episodeCount;
    if (movies > 0) parts.push(`${movies} movie${movies === 1 ? "" : "s"}`);
    if (shows > 0) parts.push(`${shows} show${shows === 1 ? "" : "s"}`);
    if (episodes > 0) parts.push(`${episodes} episode${episodes === 1 ? "" : "s"}`);
    if (parts.length) {
      return `Sync complete — added ${parts.join(", ")} (${total} total)`;
    }
  }
  return `Sync complete — ${d.added} new item${d.added === 1 ? "" : "s"} (${total} total)`;
}

// lanSyncDoneMessage builds the final toast for a LAN sync, which replaces the
// whole cache with the peer's — so counts can move in either direction.
function lanSyncDoneMessage(
  d: { count?: number; source?: string },
  before: Status | null,
  after: Status | null
): string {
  const total = (after?.cacheCount ?? d.count ?? 0).toLocaleString();
  const from = d.source ? ` from ${d.source}` : "";
  if (before && after) {
    const parts: string[] = [];
    const diff = (n: number, label: string) => {
      if (n === 0) return;
      parts.push(`${n > 0 ? "+" : "−"}${Math.abs(n)} ${label}${Math.abs(n) === 1 ? "" : "s"}`);
    };
    diff(after.movieCount - before.movieCount, "movie");
    diff(after.showCount - before.showCount, "show");
    diff(after.episodeCount - before.episodeCount, "episode");
    if (parts.length) return `Synced${from} — ${parts.join(", ")} (${total} total)`;
    return `Synced${from} — no new items (${total} total)`;
  }
  return `Synced ${d.count?.toLocaleString() ?? ""} items${from}`;
}

export default function App() {
  const [status, setStatus] = useState<Status | null>(null);
  const [startupError, setStartupError] = useState("");
  const [setupDone, setSetupDone] = useState(false);

  const [active, setActive] = useState<NavKey>("movies");
  // browseCategory tracks the last real content category, so opening the
  // Downloads/Settings panels (which overlay the grid rather than replace it)
  // doesn't unmount the grid or reload it — the scroll position is preserved.
  const [browseCategory, setBrowseCategory] = useState<Category>("movies");
  const [items, setItems] = useState<MediaCard[]>([]);
  const [loadingGrid, setLoadingGrid] = useState(false);
  const [selected, setSelected] = useState<Media | null>(null);

  // Grid controls: the genre filter (movie grids only, session-scoped), the
  // hide-foreign toggle (movie grids only, persisted), and the per-category
  // sort preferences (persisted across launches).
  const [genre, setGenre] = useState("");
  const [hideForeign, setHideForeign] = useState(loadHideForeign);
  const [sortPrefs, setSortPrefs] = useState<Partial<Record<Category, SortPref>>>(loadSortPrefs);
  const [movieGenres, setMovieGenres] = useState<string[]>([]);

  const updateHideForeign = useCallback((on: boolean) => {
    setHideForeign(on);
    try {
      localStorage.setItem(HIDE_FOREIGN_KEY, on ? "1" : "0");
    } catch {
      // Storage unavailable — the preference still applies for this session.
    }
  }, []);

  const sortPrefFor = useCallback(
    (cat: Category): SortPref => {
      const p = sortPrefs[cat] ?? SORT_DEFAULTS[cat] ?? FALLBACK_SORT;
      // Clamp a stale stored value (e.g. "rating" on a TV grid) so the select
      // and the backend stay in agreement.
      if (TV_CATEGORIES.includes(cat) && !TV_SORT_FIELDS.includes(p.sortField)) {
        return { ...p, sortField: "title" };
      }
      return p;
    },
    [sortPrefs]
  );

  const updateSortPref = useCallback((cat: Category, pref: SortPref) => {
    setSortPrefs((prev) => {
      const next = { ...prev, [cat]: pref };
      try {
        localStorage.setItem(SORT_STORAGE_KEY, JSON.stringify(next));
      } catch {
        // Storage unavailable — the preference still applies for this session.
      }
      return next;
    });
  }, []);

  const [query, setQuery] = useState("");
  const [searchResults, setSearchResults] = useState<MediaCard[] | null>(null);
  const [people, setPeople] = useState<Person[]>([]);
  const searchTimer = useRef<number | null>(null);

  const [downloads, setDownloads] = useState<Record<string, DownloadProgress>>({});
  const [toasts, setToasts] = useState<Toast[]>([]);

  // Favorited card keys (movie keys and synthetic "show:<title>" keys), loaded
  // once and kept in sync locally on toggle so stars update instantly.
  const [favorites, setFavorites] = useState<Set<string>>(new Set());

  // Auto-dismiss timers for tagged toasts, so an upsert can reset the clock.
  const toastTimers = useRef<Record<string, number>>({});

  const toast = useCallback(
    (
      message: string,
      kind: "info" | "error" = "info",
      opts?: { tag?: string; sticky?: boolean }
    ) => {
      const tag = opts?.tag;
      const id = Date.now() + Math.random();
      setToasts((t) => {
        if (tag && t.some((x) => x.tag === tag)) {
          return t.map((x) => (x.tag === tag ? { ...x, message, kind } : x));
        }
        return [...t, { id, message, kind, tag }];
      });
      if (tag && toastTimers.current[tag]) {
        window.clearTimeout(toastTimers.current[tag]);
        delete toastTimers.current[tag];
      }
      if (opts?.sticky) return;
      const timer = window.setTimeout(() => {
        setToasts((t) => t.filter((x) => (tag ? x.tag !== tag : x.id !== id)));
        if (tag) delete toastTimers.current[tag];
      }, kind === "error" ? 6000 : 3500);
      if (tag) toastTimers.current[tag] = timer;
    },
    []
  );

  const refreshStatus = useCallback(async () => {
    try {
      const s = await api.getStatus();
      setStatus(s);
      setStartupError("");
      return s;
    } catch (e: any) {
      const message = String(e?.message ?? e);
      setStartupError(message);
      toast(message, "error");
      return null;
    }
  }, [toast]);

  // Initial status load.
  useEffect(() => {
    refreshStatus();
  }, [refreshStatus]);

  // Live download progress.
  useEffect(() => {
    const off = onEvent<DownloadProgress>("download:progress", (d) => {
      setDownloads((prev) => ({ ...prev, [d.id]: d }));
    });
    return off;
  }, []);

  // Backend-initiated toasts: background flows (e.g. auto-send to rclonecp)
  // have no bound-call return path, so their outcomes arrive as events.
  useEffect(() => {
    const off = onEvent<{ kind?: string; message: string }>("toast", (d) =>
      toast(d.message, d.kind === "error" ? "error" : "info")
    );
    return off;
  }, [toast]);

  // Playback stage feedback. Errors are not events — they arrive as rejected
  // Play() promises and are toasted by each play button's catch block.
  useEffect(() => {
    const off = onEvent<PlaybackStatus>("playback:status", (s) => {
      const label = s.count > 1 ? `${s.title} (+${s.count - 1} more)` : s.title;
      if (s.stage === "preparing") toast(`Preparing ${label}…`);
      else if (s.stage === "playing") toast(`Playing ${label}`);
      else if (s.stage === "warning") toast(s.detail, "error");
    });
    return off;
  }, [toast]);

  // Restore persisted download history once the backend is reachable. Merge
  // under any live events that may have already arrived.
  const downloadsLoaded = useRef(false);
  useEffect(() => {
    if (!status || downloadsLoaded.current) return;
    downloadsLoaded.current = true;
    api
      .listDownloads()
      .then((list) => {
        setDownloads((prev) => {
          const merged: Record<string, DownloadProgress> = {};
          for (const d of list) merged[d.id] = d;
          return { ...merged, ...prev };
        });
      })
      .catch(() => {});
  }, [status]);

  const cancelDownload = useCallback(
    async (id: string) => {
      try {
        await api.cancelDownload(id);
      } catch (e: any) {
        toast(String(e?.message ?? e), "error");
      }
    },
    [toast]
  );

  const pauseDownload = useCallback(
    async (id: string) => {
      try {
        await api.pauseDownload(id);
      } catch (e: any) {
        toast(String(e?.message ?? e), "error");
      }
    },
    [toast]
  );

  const resumeDownload = useCallback(
    async (id: string) => {
      try {
        await api.resumeDownload(id);
      } catch (e: any) {
        toast(String(e?.message ?? e), "error");
      }
    },
    [toast]
  );

  const clearDownloadHistory = useCallback(async () => {
    try {
      await api.clearDownloadHistory();
      const list = await api.listDownloads();
      setDownloads(Object.fromEntries(list.map((d) => [d.id, d])));
    } catch (e: any) {
      toast(String(e?.message ?? e), "error");
    }
  }, [toast]);

  const needsSetup =
    status && (!status.configured || (!status.hasCache && !setupDone));

  // Remember the last content category so the Downloads/Settings overlays don't
  // change what the grid is showing.
  useEffect(() => {
    if (isCategory(active)) setBrowseCategory(active);
  }, [active]);

  // Load the browse category whenever it (or its genre/sort options) changes.
  const loadCategory = useCallback(
    async (cat: Category) => {
      setLoadingGrid(true);
      try {
        const { sortField, desc } = sortPrefFor(cat);
        const data = await api.listCategory(cat, { genre, sortField, desc, hideForeign });
        setItems(data);
      } catch (e: any) {
        toast(String(e?.message ?? e), "error");
        setItems([]);
      } finally {
        setLoadingGrid(false);
      }
    },
    [toast, genre, hideForeign, sortPrefFor]
  );

  useEffect(() => {
    if (!status || needsSetup) return;
    if (searchResults !== null) return;
    loadCategory(browseCategory);
  }, [browseCategory, status, needsSetup, loadCategory, searchResults]);

  // Populate the movie genre filter once the library is ready. Gated on
  // status (not just needsSetup, which is null while status is loading):
  // before the bindings are up, api.movieGenres throws synchronously and
  // would crash the tree during the splash.
  useEffect(() => {
    if (!status || needsSetup) return;
    api.movieGenres().then(setMovieGenres).catch(() => {});
  }, [status, needsSetup]);

  // Load the persisted favorites once the library is ready.
  useEffect(() => {
    if (!status || needsSetup) return;
    api
      .listFavoriteKeys()
      .then((keys) => setFavorites(new Set(keys)))
      .catch(() => {});
  }, [status, needsSetup]);

  // Favorites can change out from under us — another machine pushing its set
  // to our LAN sync server, or a background/explicit sync merging a peer's.
  // Refresh the star set and, if a favorites grid is showing, its contents.
  useEffect(() => {
    const off = onEvent("favorites:changed", () => {
      api
        .listFavoriteKeys()
        .then((keys) => setFavorites(new Set(keys)))
        .catch(() => {});
      if (browseCategory === "favorites-movies" || browseCategory === "favorites-tv") {
        loadCategory(browseCategory);
      }
    });
    return off;
  }, [browseCategory, loadCategory]);

  const toggleFavorite = useCallback(
    async (key: string) => {
      try {
        const nowFav = await api.toggleFavorite(key);
        setFavorites((prev) => {
          const next = new Set(prev);
          if (nowFav) next.add(key);
          else next.delete(key);
          return next;
        });
        // A favorites grid changes membership on toggle; refresh it so the
        // card appears/disappears in place (the grid stays mounted, so the
        // scroll position is preserved).
        if (browseCategory === "favorites-movies" || browseCategory === "favorites-tv") {
          loadCategory(browseCategory);
        }
      } catch (e: any) {
        toast(String(e?.message ?? e), "error");
      }
    },
    [browseCategory, loadCategory, toast]
  );

  // Debounced search. Plain queries also fetch actor/director suggestions for
  // the People row; field-scoped queries (cast:"…" etc.) are already the result
  // of picking a person, so no suggestions there.
  useEffect(() => {
    if (searchTimer.current) window.clearTimeout(searchTimer.current);
    if (query.trim() === "") {
      setSearchResults(null);
      setPeople([]);
      return;
    }
    const isFieldQuery = /^(director|cast|genre):/i.test(query.trim());
    searchTimer.current = window.setTimeout(async () => {
      try {
        const [res, ppl] = await Promise.all([
          api.search(query),
          isFieldQuery ? Promise.resolve([]) : api.searchPeople(query),
        ]);
        setSearchResults(res);
        setPeople(ppl);
      } catch (e: any) {
        toast(String(e?.message ?? e), "error");
      }
    }, 220);
    return () => {
      if (searchTimer.current) window.clearTimeout(searchTimer.current);
    };
  }, [query, toast]);

  // Cards carry only enough for the grid; fetch full details on open.
  const handleSelect = useCallback(
    async (card: MediaCard) => {
      try {
        const full = await api.getItem(card.key);
        setSelected(full);
      } catch (e: any) {
        toast(String(e?.message ?? e), "error");
      }
    },
    [toast]
  );

  // Run a field-scoped search (director/cast/genre click in the detail modal).
  // Setting the query drives the existing debounced search effect; closing the
  // modal reveals the results grid underneath. The query string doubles as the
  // search-box contents, so it's visible and clearable like any other search.
  const runFieldSearch = useCallback((q: string) => {
    setQuery(q);
    setSelected(null);
  }, []);

  const onSetupReady = useCallback(async () => {
    setSetupDone(true);
    const s = await refreshStatus();
    if (s) loadCategory("movies");
  }, [refreshStatus, loadCategory]);

  // After a reindex/update/sync the cache changed, so refresh both the status
  // counts and the grid data (the grid stays mounted now, so it won't reload on
  // its own when returning from the Settings overlay).
  const onLibraryChanged = useCallback(async () => {
    const s = await refreshStatus();
    loadCategory(browseCategory);
    return s;
  }, [refreshStatus, loadCategory, browseCategory]);

  // Library sync/update/reindex state, shared by the header sync button and the
  // Settings panel so either entry point disables both and feeds the same
  // progress toast. The event listeners live here (not in Settings) so feedback
  // arrives no matter which view is showing.
  const [indexing, setIndexing] = useState<null | "reindex" | "update" | "sync">(null);
  const [indexProgress, setIndexProgress] = useState<ReindexProgress | null>(null);
  const [syncMsg, setSyncMsg] = useState("");
  // Library counts captured when an op starts, diffed for the final toast.
  const preOpStatus = useRef<Status | null>(null);

  useEffect(() => {
    const off = onEvent<ReindexProgress>("reindex:progress", (p) => {
      setIndexProgress(p);
      // During first-run setup the index step renders its own progress UI.
      if (needsSetup) return;
      toast(
        `Syncing library — ${p.server} · ${p.library} · ${p.items.toLocaleString()} items`,
        "info",
        { tag: SYNC_TOAST, sticky: true }
      );
    });
    const offDone = onEvent<{
      mode?: "reindex" | "update";
      count: number;
      added?: number;
      error?: string;
    }>("reindex:done", async (d) => {
      setIndexing(null);
      setIndexProgress(null);
      const before = preOpStatus.current;
      preOpStatus.current = null;
      if (needsSetup) return; // Setup toasts its own completion.
      if (d.error) {
        toast(d.error, "error", { tag: SYNC_TOAST });
        return;
      }
      const after = await onLibraryChanged();
      toast(syncDoneMessage(d, before, after), "info", { tag: SYNC_TOAST });
    });
    return () => {
      off();
      offDone();
    };
  }, [needsSetup, toast, onLibraryChanged]);

  useEffect(() => {
    const off = onEvent<{ message: string }>("sync:progress", (d) => {
      setSyncMsg(d.message);
      if (!needsSetup) toast(d.message, "info", { tag: SYNC_TOAST, sticky: true });
    });
    const offDone = onEvent<{
      updated?: boolean;
      upToDate?: boolean;
      count?: number;
      source?: string;
      error?: string;
    }>("sync:done", async (d) => {
      setIndexing(null);
      setSyncMsg("");
      const before = preOpStatus.current;
      preOpStatus.current = null;
      if (needsSetup) return;
      if (d.error) toast(d.error, "error", { tag: SYNC_TOAST });
      else if (d.upToDate) {
        toast("Already up to date — no newer index found", "info", { tag: SYNC_TOAST });
      } else {
        const after = await onLibraryChanged();
        toast(lanSyncDoneMessage(d, before, after), "info", { tag: SYNC_TOAST });
      }
    });
    return () => {
      off();
      offDone();
    };
  }, [needsSetup, toast, onLibraryChanged]);

  const startLibraryOp = useCallback(
    async (op: "update" | "sync" | "reindex") => {
      setIndexing(op);
      setIndexProgress(null);
      preOpStatus.current = status;
      if (op === "sync") setSyncMsg("Looking for other computers…");
      toast(
        op === "sync"
          ? "Looking for other computers…"
          : op === "reindex"
          ? "Reindexing library…"
          : "Syncing library — checking for new items…",
        "info",
        { tag: SYNC_TOAST, sticky: true }
      );
      try {
        if (op === "update") await api.update();
        else if (op === "sync") await api.syncFromLAN();
        else await api.reindex();
      } catch (e: any) {
        // The done-event handler may have already reported this; the shared
        // tag collapses both into one toast either way.
        setIndexing(null);
        setSyncMsg("");
        toast(String(e?.message ?? e), "error", { tag: SYNC_TOAST });
      }
    },
    [status, toast]
  );

  if (!status) {
    return (
      <Splash>
        {startupError ? (
          <div className="max-w-lg">
            <div className="text-base font-semibold text-white/80">
              GoplexCLI could not start
            </div>
            <div className="mt-2 text-sm text-red-300/80">{startupError}</div>
            <button
              onClick={refreshStatus}
              className="mt-5 rounded-lg bg-white/10 px-4 py-2 text-sm font-semibold text-white hover:bg-white/20"
            >
              Retry
            </button>
          </div>
        ) : (
          <div className="animate-pulse text-sm text-white/40">Loading…</div>
        )}
      </Splash>
    );
  }

  if (needsSetup) {
    return (
      <>
        <Setup status={status} onReady={onSetupReady} onToast={toast} />
        <Toasts toasts={toasts} onDismiss={(id) => setToasts((t) => t.filter((x) => x.id !== id))} />
      </>
    );
  }

  // Newest downloads first.
  const downloadList = Object.values(downloads).sort((a, b) => b.seq - a.seq);
  const activeDownloads = downloadList.filter(
    (d) => d.status === "in_progress" || d.status === "pending"
  ).length;

  const showSearch = searchResults !== null;
  const gridItems = showSearch ? searchResults! : items;

  // Sort controls target the current sortable grid (hidden during search).
  const sortCategory =
    !showSearch && isCategory(active) && SORTABLE_CATEGORIES.includes(active) ? active : null;
  const sortPref = sortCategory ? sortPrefFor(sortCategory) : null;
  const sortIsTv = sortCategory !== null && TV_CATEGORIES.includes(sortCategory);

  return (
    <div className="flex h-full overflow-hidden bg-ink-900 text-white">
      <Sidebar
        active={active}
        downloadCount={activeDownloads}
        onSelect={(key) => {
          setActive(key);
          setQuery("");
          setSearchResults(null);
        }}
      />

      <main className="flex min-w-0 flex-1 flex-col">
        {/* Top bar */}
        <header
          className="flex shrink-0 items-center gap-4 border-b border-white/5 px-8 py-4"
          style={{ ["--wails-draggable" as any]: "drag" }}
        >
          <h1 className="text-lg font-semibold tracking-tight text-white">
            {showSearch ? searchHeading(query) : CATEGORY_TITLES[active]}
          </h1>

          {sortCategory && sortPref && (
            <div
              className="flex items-center gap-2"
              style={{ ["--wails-draggable" as any]: "no-drag" }}
            >
              {!sortIsTv && (
                <>
                  <select
                    value={genre}
                    onChange={(e) => setGenre(e.target.value)}
                    className="rounded-lg border border-white/10 bg-ink-700 px-2.5 py-2 text-sm text-white outline-none focus:border-accent/60"
                    title="Filter by genre"
                  >
                    <option value="">All Genres</option>
                    {movieGenres.map((g) => (
                      <option key={g} value={g}>
                        {g}
                      </option>
                    ))}
                  </select>
                  <label
                    className="flex cursor-pointer select-none items-center gap-1.5 rounded-lg border border-white/10 bg-ink-700 px-2.5 py-2 text-sm text-white outline-none hover:border-accent/60"
                    title="Hide foreign-language films"
                  >
                    <input
                      type="checkbox"
                      checked={hideForeign}
                      onChange={(e) => updateHideForeign(e.target.checked)}
                      className="accent-accent"
                    />
                    Hide foreign
                  </label>
                </>
              )}
              <select
                value={sortPref.sortField}
                onChange={(e) =>
                  updateSortPref(sortCategory, {
                    ...sortPref,
                    sortField: e.target.value as SortField,
                  })
                }
                className="rounded-lg border border-white/10 bg-ink-700 px-2.5 py-2 text-sm text-white outline-none focus:border-accent/60"
                title="Sort by"
              >
                <option value="title">Title</option>
                <option value="year">Year</option>
                <option value="added">Date Added</option>
                {!sortIsTv && (
                  <>
                    <option value="rating">Rating</option>
                    <option value="duration">Duration</option>
                  </>
                )}
              </select>
              <button
                onClick={() =>
                  updateSortPref(sortCategory, { ...sortPref, desc: !sortPref.desc })
                }
                className="rounded-lg border border-white/10 bg-ink-700 px-3 py-2 text-sm text-white outline-none hover:border-accent/60"
                title={sortPref.desc ? "Descending" : "Ascending"}
              >
                {sortPref.desc ? "↓" : "↑"}
              </button>
            </div>
          )}

          <div className="flex-1" />
          <button
            onClick={() => startLibraryOp("sync")}
            disabled={!!indexing}
            title={
              indexing
                ? "Library sync in progress…"
                : "Sync library — copy the latest index from your sync computer (set in Settings)"
            }
            style={{ ["--wails-draggable" as any]: "no-drag" }}
            className="rounded-lg border border-white/10 bg-ink-700 p-2.5 text-white/80 outline-none transition-colors hover:border-accent/60 hover:text-white disabled:opacity-60"
          >
            <SyncIcon
              width={16}
              height={16}
              className={indexing ? "animate-spin" : ""}
            />
          </button>
          <div
            className="relative w-72"
            style={{ ["--wails-draggable" as any]: "no-drag" }}
          >
            <SearchIcon
              width={16}
              height={16}
              className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-white/40"
            />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search library…"
              className="w-full rounded-lg border border-white/10 bg-ink-700 py-2 pl-9 pr-3 text-sm text-white placeholder-white/30 outline-none focus:border-accent/60"
            />
          </div>
        </header>

        {/* People suggestions: click a person to run the exact cast:/director:
            filter that previously required typing the query syntax by hand. */}
        {showSearch && people.length > 0 && (
          <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-white/5 bg-ink-750 px-8 py-3">
            <span className="text-[10px] font-semibold uppercase tracking-widest text-white/30">
              People
            </span>
            {people.map((p) => (
              <button
                key={`${p.role}:${p.name}`}
                onClick={() =>
                  runFieldSearch(
                    `${p.role === "director" ? "director" : "cast"}:"${p.name}"`
                  )
                }
                className="flex items-center gap-1.5 rounded-full border border-white/10 bg-ink-700 px-3 py-1 text-sm text-white/80 transition-colors hover:border-accent/60 hover:text-white"
              >
                {p.name}
                <span className="text-[10px] font-medium uppercase tracking-wider text-accent/70">
                  {p.role}
                </span>
              </button>
            ))}
          </div>
        )}

        {/* Content. The poster grid owns its own scroll (it's virtualized) and
            stays mounted underneath the Downloads/Settings panels, which overlay
            it — so returning to the library preserves the scroll position. */}
        <div className="relative min-h-0 flex-1 bg-ink-750">
          <PosterGrid
            key={showSearch ? "search" : browseCategory}
            items={gridItems}
            loading={loadingGrid && !showSearch}
            emptyMessage={
              showSearch
                ? "No matches found."
                : EMPTY_MESSAGES[browseCategory] ?? "Nothing here yet."
            }
            onSelect={handleSelect}
            favorites={favorites}
            onToggleFavorite={toggleFavorite}
          />
          {active === "settings" && !showSearch && (
            <div className="absolute inset-0 overflow-y-auto bg-ink-750 px-8 py-6">
              <Settings
                status={status}
                indexing={indexing}
                progress={indexProgress}
                syncMsg={syncMsg}
                onUpdate={() => startLibraryOp("update")}
                onSync={() => startLibraryOp("sync")}
                onReindex={() => startLibraryOp("reindex")}
                onToast={toast}
              />
            </div>
          )}
          {active === "downloads" && !showSearch && (
            <div className="absolute inset-0 overflow-y-auto bg-ink-750 px-8 py-6">
              <DownloadsPanel
                downloads={downloadList}
                onCancel={cancelDownload}
                onPause={pauseDownload}
                onResume={resumeDownload}
                onClearHistory={clearDownloadHistory}
                onSendToRclonecp={
                  status.rclonecpAvailable
                    ? (id) =>
                        api
                          .sendToRclonecp(id)
                          .then(() => toast("Sent to rclonecp"))
                          .catch((e: any) =>
                            toast(String(e?.message ?? e), "error")
                          )
                    : undefined
                }
              />
            </div>
          )}
        </div>
      </main>

      {selected && (
        <DetailModal
          media={selected}
          mpvAvailable={status.mpvAvailable}
          rcloneAvailable={status.rcloneAvailable}
          isFavorite={favorites.has(selected.key)}
          onToggleFavorite={toggleFavorite}
          onClose={() => setSelected(null)}
          onToast={toast}
          onSearch={runFieldSearch}
          onSelectSimilar={handleSelect}
        />
      )}

      <Toasts
        toasts={toasts}
        onDismiss={(id) => setToasts((t) => t.filter((x) => x.id !== id))}
      />
    </div>
  );
}
