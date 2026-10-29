import { useCallback, useEffect, useRef, useState } from "react";
import { api, isMac, onEvent } from "./lib/api";
import type {
  Category,
  DownloadProgress,
  Media,
  MediaCard,
  Person,
  PlaybackStatus,
  ReindexProgress,
  RemoteServer,
  SortField,
  Status,
} from "./lib/types";
import { Sidebar, type NavKey } from "./components/Sidebar";
import { PosterGrid } from "./components/PosterGrid";
import { DetailDrawer } from "./components/DetailDrawer";
import { DRAWER_WIDTH } from "./components/DetailPanel";
import { DownloadsPanel, isActive } from "./components/DownloadsPanel";
import { Splash } from "./components/Splash";
import { Toasts, type Toast } from "./components/Toasts";
import { Setup } from "./views/Setup";
import { Settings } from "./views/Settings";
import Select from "./components/Select";
import { FilterMenu } from "./components/FilterMenu";

// The content header's title for each grid.
const TITLES: Record<Category, string> = {
  movies: "Movies",
  "tv-shows": "TV Shows",
  "continue-watching": "Continue Watching",
  "favorites-movies": "Favorite Movies",
  "favorites-tv": "Favorite TV Shows",
  "recently-added-movies": "New Movies",
  "recently-added-tv": "New Episodes",
};

const EMPTY_MESSAGES: Partial<Record<NavKey, string>> = {
  movies: "No movies in your library yet.",
  "tv-shows": "No TV shows in your library yet.",
  "continue-watching": "Nothing in progress — start watching something!",
  "favorites-movies": "No favorite movies yet — click the star on a movie to add it.",
  "favorites-tv": "No favorite shows yet — click the star on a show to add it.",
  "recently-added-movies": "No movies indexed yet.",
  "recently-added-tv": "No episodes indexed yet.",
};

// Category nav keys (everything except the Downloads/Settings panels).
function isCategory(k: NavKey): k is Category {
  return k !== "downloads" && k !== "settings";
}

// Input types that don't take typing: Escape on one of these (an episode's
// checkbox, the Hide foreign toggle) still closes the detail drawer.
const NON_TEXT_INPUTS = new Set(["checkbox", "radio", "button", "submit", "reset", "range", "color", "file"]);

function isFinished(d: DownloadProgress) {
  return (
    d.status === "completed" ||
    d.status === "failed" ||
    d.status === "cancelled"
  );
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

function defaultSortFor(cat: Category): SortPref {
  return SORT_DEFAULTS[cat] ?? FALLBACK_SORT;
}

function sameSort(a: SortPref, b: SortPref): boolean {
  return a.sortField === b.sortField && a.desc === b.desc;
}

function loadSortPrefs(): Partial<Record<Category, SortPref>> {
  try {
    return JSON.parse(localStorage.getItem(SORT_STORAGE_KEY) ?? "{}");
  } catch {
    return {};
  }
}

// searchSummary is the line shown under the sidebar search box while results
// are up. A field-scoped query (director:"…" / cast:"…" / genre:"…", produced by
// clicking a name in the detail modal) gets a friendly label; a plain query is
// already visible in the box, so it just gets the count.
function searchSummary(query: string, count: number): string {
  const results = `${count.toLocaleString()} result${count === 1 ? "" : "s"}`;
  const m = /^(director|cast|genre):"?(.+?)"?$/i.exec(query.trim());
  if (!m) return results;
  const field = m[1].toLowerCase();
  const value = m[2];
  const label =
    field === "director"
      ? `Directed by ${value}`
      : field === "cast"
      ? `Starring ${value}`
      : `${value} movies`; // genre
  return `${label} · ${results}`;
}

// Shared look for the filter menu's genre/sort controls.
const CONTROL =
  "rounded-lg border border-white/10 bg-ink-700 px-2.5 py-1.5 text-sm text-white outline-none focus:border-accent/60";
const MENU_LABEL = "text-[10px] font-semibold uppercase tracking-widest text-white/40";

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
  // The latest grid load; an answer for an older one (a category switched
  // away from, a refresh overtaken by another) is dropped.
  const gridLoad = useRef(0);

  // The title in the detail drawer. It's fetched by key, never taken from
  // the grid, so a refresh that replaces the grid's cards leaves it alone;
  // refreshDrawer reloads it by key instead.
  const [selected, setSelected] = useState<Media | null>(null);
  // The key of the title the drawer shows, and of the one it was last asked
  // to show (the same once that fetch lands). A fetch that lands for any
  // other key is stale — another poster was clicked meanwhile, or the drawer
  // was closed — and is dropped.
  const shownKey = useRef<string | null>(null);
  const wantedKey = useRef<string | null>(null);
  const showInDrawer = useCallback((media: Media | null) => {
    shownKey.current = media?.key ?? null;
    wantedKey.current = media?.key ?? null;
    setSelected(media);
  }, []);
  const closeDrawer = useCallback(() => showInDrawer(null), [showInDrawer]);

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
      const p = sortPrefs[cat] ?? defaultSortFor(cat);
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

  // Registered remote download servers (Settings → Remote download servers).
  // Loaded once at startup and refreshed after Settings saves; drives the
  // remote-jobs poll and the download-target picker in the detail modal.
  const [remoteServers, setRemoteServers] = useState<RemoteServer[]>([]);
  const refreshRemoteServers = useCallback(() => {
    api.listRemoteServers().then(setRemoteServers).catch(() => {});
  }, []);
  useEffect(() => {
    refreshRemoteServers();
  }, [refreshRemoteServers]);
  const enabledRemotes = remoteServers.filter((r) => r.enabled);

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

  // Remote clients' jobs arrive by polling (they run on other machines, so
  // there are no Wails events for them): replace the remote slice of the list
  // wholesale each tick and keep the local, event-driven jobs untouched.
  // Skipped while the window is hidden and when no remote server is enabled.
  useEffect(() => {
    if (enabledRemotes.length === 0) {
      // Drop stale remote entries when the last server is removed/disabled.
      setDownloads((prev) => {
        const entries = Object.entries(prev).filter(([, d]) => !d.origin);
        return entries.length === Object.keys(prev).length
          ? prev
          : Object.fromEntries(entries);
      });
      return;
    }
    let stop = false;
    const poll = async () => {
      if (stop || document.hidden) return;
      try {
        const remote = await api.listRemoteDownloads();
        if (stop) return;
        setDownloads((prev) => {
          const next: Record<string, DownloadProgress> = {};
          for (const [id, d] of Object.entries(prev)) {
            if (!d.origin) next[id] = d;
          }
          for (const d of remote) next[d.id] = d;
          return next;
        });
      } catch {
        // Servers unreachable — try again next tick.
      }
    };
    poll();
    const timer = window.setInterval(poll, 1500);
    return () => {
      stop = true;
      window.clearInterval(timer);
    };
  }, [enabledRemotes.length]);

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
        // Remote job IDs are namespaced "<server>!<id>" — route the cancel to
        // the server the job runs on.
        if (id.includes("!")) await api.cancelRemoteDownload(id);
        else await api.cancelDownload(id);
      } catch (e: any) {
        toast(String(e?.message ?? e), "error");
      }
    },
    [toast]
  );

  const cancelAllDownloads = useCallback(
    async (ids: string[]) => {
      const results = await Promise.allSettled(
        ids.map((id) =>
          id.includes("!") ? api.cancelRemoteDownload(id) : api.cancelDownload(id)
        )
      );
      const failed = results.filter((r) => r.status === "rejected").length;
      if (failed > 0) {
        toast(`Failed to cancel ${failed} download${failed === 1 ? "" : "s"}`, "error");
      } else {
        toast(`Cancelled ${ids.length} download${ids.length === 1 ? "" : "s"}`);
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
      // Also clear finished jobs on remote servers; their lists refresh on
      // the next poll tick, so only the local slice is rebuilt here.
      await api.clearRemoteDownloadHistory().catch(() => {});
      const list = await api.listDownloads();
      setDownloads((prev) => {
        const next: Record<string, DownloadProgress> = {};
        for (const d of list) next[d.id] = d;
        for (const [id, d] of Object.entries(prev)) {
          if (d.origin && !isFinished(d)) next[id] = d;
        }
        return next;
      });
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
  // A quiet load refreshes the grid in place (a sync finishing, a favorite
  // dropping out of Favorites): no skeleton, which would collapse the grid
  // and lose its scroll position.
  const loadCategory = useCallback(
    async (cat: Category, opts?: { quiet?: boolean }) => {
      const load = ++gridLoad.current;
      if (!opts?.quiet) setLoadingGrid(true);
      try {
        const { sortField, desc } = sortPrefFor(cat);
        const data = await api.listCategory(cat, { genre, sortField, desc, hideForeign });
        if (load === gridLoad.current) setItems(data);
      } catch (e: any) {
        if (load !== gridLoad.current) return;
        toast(String(e?.message ?? e), "error");
        setItems([]);
      } finally {
        if (load === gridLoad.current) setLoadingGrid(false);
      }
    },
    [toast, genre, hideForeign, sortPrefFor]
  );

  // Gated on status having loaded, not on the status object: every refresh
  // (after a sync, say) replaces that, and reloading here on each one would
  // flash the skeleton and lose the grid's scroll position. A library change
  // reloads the grid itself, quietly (onLibraryChanged).
  const statusLoaded = status !== null;
  useEffect(() => {
    if (!statusLoaded || needsSetup) return;
    if (searchResults !== null) return;
    loadCategory(browseCategory);
  }, [browseCategory, statusLoaded, needsSetup, loadCategory, searchResults]);

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
        loadCategory(browseCategory, { quiet: true });
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
        // card appears/disappears in place (the grid stays mounted and the
        // load is quiet, so the scroll position is preserved). A title
        // unfavorited from its open drawer drops out of the grid but keeps
        // the drawer — it's still a title, and its Favorite button undoes
        // the click.
        if (browseCategory === "favorites-movies" || browseCategory === "favorites-tv") {
          loadCategory(browseCategory, { quiet: true });
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

  // Open the drawer on the title at `key` (cards carry only enough for the
  // grid, so the full details are fetched). Only the latest request lands.
  // A failed fetch leaves the drawer on what it shows, or — closeOnError —
  // closes it.
  const openItem = useCallback(
    async (key: string, closeOnError = false) => {
      wantedKey.current = key;
      try {
        const full = await api.getItem(key);
        if (wantedKey.current === key) showInDrawer(full);
      } catch (e: any) {
        if (wantedKey.current !== key) return;
        if (closeOnError) closeDrawer();
        else wantedKey.current = shownKey.current;
        toast(String(e?.message ?? e), "error");
      }
    },
    [toast, showInDrawer, closeDrawer]
  );

  // A poster (or a "More like this" card, or an episode's show link) swaps
  // its title into the drawer. The open title's own poster again is a no-op,
  // so it can't flicker the drawer.
  const handleSelect = useCallback(
    (card: MediaCard) => {
      if (card.key === wantedKey.current) return;
      void openItem(card.key);
    },
    [openItem]
  );

  // Reload the open detail drawer with the item at `key` after its metadata
  // changed (a TMDB match was fixed). The key may differ from the one that
  // was open when the fix renamed a show.
  const reopenItem = useCallback((key: string) => void openItem(key, true), [openItem]);

  // After the library changed (a sync, update or reindex; a fixed match),
  // reload the drawer's title by key: it stays on that title with the fresh
  // data, or closes when the title is gone from the library.
  const refreshDrawer = useCallback(async () => {
    const key = wantedKey.current;
    if (!key) return;
    try {
      const full = await api.getItem(key);
      if (wantedKey.current === key) showInDrawer(full);
    } catch {
      if (wantedKey.current === key) closeDrawer();
    }
  }, [showInDrawer, closeDrawer]);

  // Escape closes the detail drawer. Whatever is open inside it goes first:
  // a Select's list and the conflict prompt catch Escape on window in the
  // capture phase, and the fix-match panel marks it handled (preventDefault).
  // Escape typed into a text field (the sidebar search) stays with the field.
  useEffect(() => {
    if (!selected) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.defaultPrevented) return;
      const el = e.target as HTMLElement | null;
      if (el instanceof HTMLTextAreaElement) return;
      if (el instanceof HTMLInputElement && !NON_TEXT_INPUTS.has(el.type)) return;
      closeDrawer();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [selected, closeDrawer]);

  // Run a field-scoped search (director/cast/genre click in the detail panel).
  // Setting the query drives the existing debounced search effect; the
  // results fill the grid beside the drawer, which stays on its title (the
  // grid is live, so a result swaps in with a click). The query string
  // doubles as the search-box contents, so it's visible and clearable like
  // any other search.
  const runFieldSearch = useCallback((q: string) => {
    setQuery(q);
  }, []);

  // Downloads and Settings take the grid's place (they overlay it, inside
  // the drawer's padding), so they put the drawer away — also when clearing
  // a search uncovers one, since search results show over them.
  const overlayShown = (active === "downloads" || active === "settings") && searchResults === null;
  useEffect(() => {
    if (overlayShown) closeDrawer();
  }, [overlayShown, closeDrawer]);
  const drawerMedia = overlayShown ? null : selected;

  // Whether the drawer was open at the last commit. Only opening it slides
  // it in; a title swapped into an open drawer (which remounts it, keyed by
  // title) must not replay the slide and the scrim's fade.
  const drawerWasOpen = useRef(false);
  useEffect(() => {
    drawerWasOpen.current = drawerMedia !== null;
  });

  const onSetupReady = useCallback(async () => {
    setSetupDone(true);
    const s = await refreshStatus();
    if (s) loadCategory("movies");
  }, [refreshStatus, loadCategory]);

  // After a reindex/update/sync the cache changed, so refresh the status
  // counts, the grid data (the grid stays mounted now, so it won't reload on
  // its own when returning from the Settings overlay; the load is quiet, so
  // the grid keeps its place) and the drawer's title.
  const onLibraryChanged = useCallback(async () => {
    const s = await refreshStatus();
    loadCategory(browseCategory, { quiet: true });
    void refreshDrawer();
    return s;
  }, [refreshStatus, loadCategory, browseCategory, refreshDrawer]);

  // A single item's metadata changed (its TMDB match was fixed), possibly
  // under a new card key when a show was renamed. Refresh the grid and counts
  // the same way a reindex does so the card updates in place. A drawer open
  // on the old key follows the rename before the refresh reloads it, so it
  // lands on the renamed title instead of closing on the vanished one (the
  // fix-match panel's onReopen asks for the same key).
  useEffect(() => {
    const off = onEvent<{ key: string; previous: string }>("media:changed", (d) => {
      if (needsSetup) return;
      if (d.key && d.previous && wantedKey.current === d.previous) wantedKey.current = d.key;
      void onLibraryChanged();
    });
    return off;
  }, [needsSetup, onLibraryChanged]);

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

  // Newest downloads first. seq only orders jobs from one machine, so the
  // wall-clock queue time is compared first to interleave local and remote
  // jobs correctly (entries predating queuedAt sort last — they're oldest).
  const downloadList = Object.values(downloads).sort(
    (a, b) => (b.queuedAt ?? 0) - (a.queuedAt ?? 0) || b.seq - a.seq
  );
  const activeDownloadList = downloadList.filter(isActive);
  const activeDownloads = activeDownloadList.filter(
    (d) => d.status !== "paused"
  ).length;

  const showSearch = searchResults !== null;
  const gridItems = showSearch ? searchResults! : items;

  // Sort controls target the current sortable grid (hidden during search).
  const sortCategory =
    !showSearch && isCategory(active) && SORTABLE_CATEGORIES.includes(active) ? active : null;
  const sortPref = sortCategory ? sortPrefFor(sortCategory) : null;
  const sortIsTv = sortCategory !== null && TV_CATEGORIES.includes(sortCategory);
  // Filters hiding titles from the current grid; the genre and hide-foreign
  // filters only apply to movie grids.
  const activeFilters = sortCategory && !sortIsTv ? (genre ? 1 : 0) + (hideForeign ? 1 : 0) : 0;
  // Grids get a header (title, count, filter menu); the Downloads and
  // Settings panels have their own headings.
  const showHeader = showSearch || isCategory(active);

  return (
    <div className="flex h-full overflow-hidden bg-ink-900 text-white">
      <Sidebar
        active={active}
        downloadCount={activeDownloads}
        activeDownloads={activeDownloadList}
        onSelect={(key) => {
          // Another category keeps the drawer on its title beside the new
          // grid (it's still a title, and any poster swaps in); Downloads
          // and Settings put it away (see overlayShown).
          setActive(key);
          if (!isCategory(key)) closeDrawer();
          setQuery("");
          setSearchResults(null);
        }}
        query={query}
        onQueryChange={setQuery}
        // The drawer isn't modal: a search fills the grid beside it.
        searchHotkey
        searchSummary={showSearch ? searchSummary(query, gridItems.length) : undefined}
        people={showSearch ? people : []}
        onPickPerson={(p) =>
          runFieldSearch(`${p.role === "director" ? "director" : "cast"}:"${p.name}"`)
        }
        syncing={!!indexing}
        onSync={() => startLibraryOp("sync")}
      />

      <main className="relative flex min-w-0 flex-1 flex-col">
        {/* With the title bar hidden on macOS, the grids' header doubles as
            the window's drag handle. Downloads and Settings have no header,
            so keep a thin invisible strip along the top edge there instead;
            it sits inside the panel's top padding. */}
        {isMac && !showHeader && (
          <div
            className="absolute inset-x-0 top-0 z-10 h-5"
            style={{ ["--wails-draggable" as any]: "drag" }}
          />
        )}

        {/* The grid's header: its title and count, and the filter/sort menu
            for the grids that have one. It keeps clear of the detail drawer
            like the grid below it. */}
        {showHeader && (
          <header
            className="flex h-12 shrink-0 items-end gap-3 bg-ink-750 px-8"
            style={{
              ["--wails-draggable" as any]: "drag",
              ...(drawerMedia ? { paddingRight: `calc(${DRAWER_WIDTH} + 2rem)` } : {}),
            }}
          >
            <div className="flex min-w-0 flex-1 items-baseline gap-2.5">
              <h1 className="truncate text-[15px] font-semibold tracking-tight text-white">
                {showSearch || !isCategory(active) ? "Search Results" : TITLES[active]}
              </h1>
              {(showSearch || !loadingGrid) && (
                <span className="shrink-0 text-xs tabular-nums text-white/40">
                  {gridItems.length.toLocaleString()} {gridItems.length === 1 ? "title" : "titles"}
                </span>
              )}
            </div>
            {sortCategory && sortPref && (
              <FilterMenu
                label={sortIsTv ? "Sort" : "Filters"}
                active={activeFilters}
                canReset={activeFilters > 0 || !sameSort(sortPref, defaultSortFor(sortCategory))}
                onReset={() => {
                  setGenre("");
                  updateHideForeign(false);
                  updateSortPref(sortCategory, defaultSortFor(sortCategory));
                }}
              >
                <div className="space-y-3">
                  {!sortIsTv && (
                    <div className="space-y-2">
                      <div className={MENU_LABEL}>Filter</div>
                      <Select
                        value={genre}
                        onChange={setGenre}
                        options={[
                          { value: "", label: "All Genres" },
                          ...movieGenres.map((g) => ({ value: g, label: g })),
                        ]}
                        className={`w-full ${CONTROL}`}
                        title="Filter by genre"
                        aria-label="Genre"
                      />
                      <label
                        className="flex cursor-pointer select-none items-center gap-2 px-1 py-1 text-sm text-white/70 hover:text-white"
                        title="Hide foreign-language films"
                      >
                        <input
                          type="checkbox"
                          checked={hideForeign}
                          onChange={(e) => updateHideForeign(e.target.checked)}
                          className="h-4 w-4 accent-accent"
                        />
                        Hide foreign
                      </label>
                    </div>
                  )}
                  <div className="space-y-2">
                    <div className={MENU_LABEL}>Sort</div>
                    <div className="flex gap-2">
                      <Select<SortField>
                        value={sortPref.sortField}
                        onChange={(sortField) =>
                          updateSortPref(sortCategory, { ...sortPref, sortField })
                        }
                        options={[
                          { value: "title", label: "Title" },
                          { value: "year", label: "Year" },
                          { value: "added", label: "Date Added" },
                          ...(sortIsTv
                            ? []
                            : [
                                { value: "rating" as const, label: "Rating" },
                                { value: "duration" as const, label: "Duration" },
                              ]),
                        ]}
                        className={`min-w-0 flex-1 ${CONTROL}`}
                        title="Sort by"
                        aria-label="Sort by"
                      />
                      <button
                        onClick={() =>
                          updateSortPref(sortCategory, { ...sortPref, desc: !sortPref.desc })
                        }
                        className={`touch-target relative px-3 hover:border-accent/60 ${CONTROL}`}
                        title={sortPref.desc ? "Descending" : "Ascending"}
                        aria-label={sortPref.desc ? "Descending" : "Ascending"}
                      >
                        {sortPref.desc ? "↓" : "↑"}
                      </button>
                    </div>
                  </div>
                </div>
              </FilterMenu>
            )}
          </header>
        )}

        {/* Content. The poster grid owns its own scroll (it's virtualized) and
            stays mounted underneath the Downloads/Settings panels, which overlay
            it — so returning to the library preserves the scroll position. */}
        {/* The grid keeps clear of the detail drawer (and reflows) while
            one is open; it stays live, so another poster swaps the drawer's
            title. The padding is the drawer's own width, which narrows in a
            small window so the grid keeps a column. */}
        <div
          className="relative min-h-0 flex-1 bg-ink-750"
          style={drawerMedia ? { paddingRight: DRAWER_WIDTH } : undefined}
        >
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
                onRemoteServersChanged={refreshRemoteServers}
              />
            </div>
          )}
          {active === "downloads" && !showSearch && (
            <div className="absolute inset-0 overflow-y-auto bg-ink-750 px-8 py-6">
              <DownloadsPanel
                downloads={downloadList}
                onCancel={cancelDownload}
                onCancelAll={cancelAllDownloads}
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
        {drawerMedia && (
          <DetailDrawer
            key={drawerMedia.key}
            media={drawerMedia}
            animateIn={!drawerWasOpen.current}
            mpvAvailable={status.mpvAvailable}
            rcloneAvailable={status.rcloneAvailable}
            remoteServers={enabledRemotes.map((r) => r.name)}
            isFavorite={favorites.has(drawerMedia.key)}
            onToggleFavorite={toggleFavorite}
            onClose={closeDrawer}
            onToast={toast}
            onSearch={runFieldSearch}
            onSelectSimilar={handleSelect}
            onReopen={reopenItem}
          />
        )}
      </main>

      <Toasts
        toasts={toasts}
        onDismiss={(id) => setToasts((t) => t.filter((x) => x.id !== id))}
      />
    </div>
  );
}
