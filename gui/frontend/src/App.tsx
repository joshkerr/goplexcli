import { useCallback, useEffect, useRef, useState } from "react";
import { api, onEvent } from "./lib/api";
import type {
  Category,
  DownloadProgress,
  Media,
  MediaCard,
  Person,
  PlaybackStatus,
  SortField,
  Status,
} from "./lib/types";
import { Sidebar, type NavKey } from "./components/Sidebar";
import { PosterGrid } from "./components/PosterGrid";
import { DetailModal } from "./components/DetailModal";
import { DownloadsPanel } from "./components/DownloadsPanel";
import { Toasts, type Toast } from "./components/Toasts";
import { Setup } from "./views/Setup";
import { Settings } from "./views/Settings";
import { SearchIcon } from "./components/icons";

const CATEGORY_TITLES: Record<NavKey, string> = {
  movies: "Movies",
  "tv-shows": "TV Shows",
  "continue-watching": "Continue Watching",
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
  "favorites-movies": "No favorite movies yet — click the star on a movie to add it.",
  "favorites-tv": "No favorite shows yet — click the star on a show to add it.",
  "recently-added-movies": "No movies indexed yet.",
  "recently-added-tv": "No episodes indexed yet.",
};

// Category nav keys (everything except the Downloads/Settings panels).
function isCategory(k: NavKey): k is Category {
  return k !== "downloads" && k !== "settings";
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

  // Movies-grid controls (genre filter + sort). Honored only for the Movies
  // category; other grids ignore them.
  const [genre, setGenre] = useState("");
  const [sortField, setSortField] = useState<SortField>("title");
  const [desc, setDesc] = useState(false);
  const [movieGenres, setMovieGenres] = useState<string[]>([]);

  const [query, setQuery] = useState("");
  const [searchResults, setSearchResults] = useState<MediaCard[] | null>(null);
  const [people, setPeople] = useState<Person[]>([]);
  const searchTimer = useRef<number | null>(null);

  const [downloads, setDownloads] = useState<Record<string, DownloadProgress>>({});
  const [toasts, setToasts] = useState<Toast[]>([]);

  // Favorited card keys (movie keys and synthetic "show:<title>" keys), loaded
  // once and kept in sync locally on toggle so stars update instantly.
  const [favorites, setFavorites] = useState<Set<string>>(new Set());

  const toast = useCallback((message: string, kind: "info" | "error" = "info") => {
    const id = Date.now() + Math.random();
    setToasts((t) => [...t, { id, message, kind }]);
    window.setTimeout(() => {
      setToasts((t) => t.filter((x) => x.id !== id));
    }, kind === "error" ? 6000 : 3500);
  }, []);

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
        const data = await api.listCategory(cat, { genre, sortField, desc });
        setItems(data);
      } catch (e: any) {
        toast(String(e?.message ?? e), "error");
        setItems([]);
      } finally {
        setLoadingGrid(false);
      }
    },
    [toast, genre, sortField, desc]
  );

  useEffect(() => {
    if (needsSetup) return;
    if (searchResults !== null) return;
    loadCategory(browseCategory);
  }, [browseCategory, needsSetup, loadCategory, searchResults]);

  // Populate the movie genre filter once the library is ready.
  useEffect(() => {
    if (needsSetup) return;
    api.movieGenres().then(setMovieGenres).catch(() => {});
  }, [needsSetup]);

  // Load the persisted favorites once the library is ready.
  useEffect(() => {
    if (needsSetup) return;
    api
      .listFavoriteKeys()
      .then((keys) => setFavorites(new Set(keys)))
      .catch(() => {});
  }, [needsSetup]);

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

  // Shows only carry title/year/added-order, so the other sort fields don't
  // apply; snap back to title when landing on the Favorites TV grid with one.
  useEffect(() => {
    if (active === "favorites-tv" && sortField !== "title" && sortField !== "year" && sortField !== "added") {
      setSortField("title");
    }
  }, [active, sortField]);

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
    await refreshStatus();
    loadCategory(browseCategory);
  }, [refreshStatus, loadCategory, browseCategory]);

  if (!status) {
    return (
      <div className="flex h-full items-center justify-center bg-ink-900 px-8 text-center">
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
          <div className="text-white/40">Loading…</div>
        )}
      </div>
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

          {(active === "movies" || active === "favorites-movies" || active === "favorites-tv") && !showSearch && (
            <div
              className="flex items-center gap-2"
              style={{ ["--wails-draggable" as any]: "no-drag" }}
            >
              {active !== "favorites-tv" && (
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
              )}
              <select
                value={sortField}
                onChange={(e) => setSortField(e.target.value as SortField)}
                className="rounded-lg border border-white/10 bg-ink-700 px-2.5 py-2 text-sm text-white outline-none focus:border-accent/60"
                title="Sort by"
              >
                <option value="title">Title</option>
                <option value="year">Year</option>
                <option value="added">Date Added</option>
                {active !== "favorites-tv" && (
                  <>
                    <option value="rating">Rating</option>
                    <option value="duration">Duration</option>
                  </>
                )}
              </select>
              <button
                onClick={() => setDesc((d) => !d)}
                className="rounded-lg border border-white/10 bg-ink-700 px-3 py-2 text-sm text-white outline-none hover:border-accent/60"
                title={desc ? "Descending" : "Ascending"}
              >
                {desc ? "↓" : "↑"}
              </button>
            </div>
          )}

          <div className="flex-1" />
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
                onReindexed={onLibraryChanged}
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
        />
      )}

      <Toasts
        toasts={toasts}
        onDismiss={(id) => setToasts((t) => t.filter((x) => x.id !== id))}
      />
    </div>
  );
}
