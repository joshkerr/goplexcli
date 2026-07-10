import { useCallback, useEffect, useRef, useState } from "react";
import { api, onEvent } from "./lib/api";
import type {
  Category,
  DownloadProgress,
  Media,
  MediaCard,
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
  "recently-added-movies": "Recently Added Movies",
  "recently-added-tv": "Recently Added Episodes",
  downloads: "Downloads",
  settings: "Settings",
};

const EMPTY_MESSAGES: Partial<Record<NavKey, string>> = {
  movies: "No movies in your library yet.",
  "tv-shows": "No TV shows in your library yet.",
  "continue-watching": "Nothing in progress — start watching something!",
  "recently-added-movies": "No movies indexed yet.",
  "recently-added-tv": "No episodes indexed yet.",
};

export default function App() {
  const [status, setStatus] = useState<Status | null>(null);
  const [startupError, setStartupError] = useState("");
  const [setupDone, setSetupDone] = useState(false);

  const [active, setActive] = useState<NavKey>("movies");
  const [items, setItems] = useState<MediaCard[]>([]);
  const [loadingGrid, setLoadingGrid] = useState(false);
  const [selected, setSelected] = useState<Media | null>(null);

  const [query, setQuery] = useState("");
  const [searchResults, setSearchResults] = useState<MediaCard[] | null>(null);
  const searchTimer = useRef<number | null>(null);

  const [downloads, setDownloads] = useState<Record<string, DownloadProgress>>({});
  const [toasts, setToasts] = useState<Toast[]>([]);

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

  const needsSetup =
    status && (!status.configured || (!status.hasCache && !setupDone));

  // Load the active category whenever it changes (when not searching).
  const loadCategory = useCallback(
    async (cat: NavKey) => {
      if (cat === "downloads" || cat === "settings") return;
      setLoadingGrid(true);
      try {
        const data = await api.listCategory(cat as Category);
        setItems(data);
      } catch (e: any) {
        toast(String(e?.message ?? e), "error");
        setItems([]);
      } finally {
        setLoadingGrid(false);
      }
    },
    [toast]
  );

  useEffect(() => {
    if (needsSetup) return;
    if (active === "downloads" || active === "settings") return;
    if (searchResults !== null) return;
    loadCategory(active);
  }, [active, needsSetup, loadCategory, searchResults]);

  // Debounced search.
  useEffect(() => {
    if (searchTimer.current) window.clearTimeout(searchTimer.current);
    if (query.trim() === "") {
      setSearchResults(null);
      return;
    }
    searchTimer.current = window.setTimeout(async () => {
      try {
        const res = await api.search(query);
        setSearchResults(res);
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

  const onSetupReady = useCallback(async () => {
    setSetupDone(true);
    const s = await refreshStatus();
    if (s) loadCategory("movies");
  }, [refreshStatus, loadCategory]);

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

  const downloadList = Object.values(downloads).sort((a, b) =>
    a.id.localeCompare(b.id)
  );
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
            {showSearch ? `Search: “${query}”` : CATEGORY_TITLES[active]}
          </h1>
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

        {/* Content. The poster grid owns its own scroll (it's virtualized);
            Settings and Downloads scroll in a padded wrapper. */}
        <div className="min-h-0 flex-1">
          {active === "settings" && !showSearch ? (
            <div className="h-full overflow-y-auto px-8 py-6">
              <Settings
                status={status}
                onReindexed={refreshStatus}
                onToast={toast}
              />
            </div>
          ) : active === "downloads" && !showSearch ? (
            <div className="h-full overflow-y-auto px-8 py-6">
              <DownloadsPanel downloads={downloadList} />
            </div>
          ) : (
            <PosterGrid
              key={showSearch ? "search" : active}
              items={gridItems}
              loading={loadingGrid && !showSearch}
              emptyMessage={
                showSearch
                  ? "No matches found."
                  : EMPTY_MESSAGES[active] ?? "Nothing here yet."
              }
              onSelect={handleSelect}
            />
          )}
        </div>
      </main>

      {selected && (
        <DetailModal
          media={selected}
          mpvAvailable={status.mpvAvailable}
          rcloneAvailable={status.rcloneAvailable}
          onClose={() => setSelected(null)}
          onToast={toast}
        />
      )}

      <Toasts
        toasts={toasts}
        onDismiss={(id) => setToasts((t) => t.filter((x) => x.id !== id))}
      />
    </div>
  );
}
