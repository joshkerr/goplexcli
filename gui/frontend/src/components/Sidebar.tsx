import type { ReactNode } from "react";
import type { Category, Person } from "../lib/types";
import { isMac } from "../lib/api";
import {
  BrandMark,
  CloseIcon,
  DownloadIcon,
  FilmIcon,
  ResumeIcon,
  SearchIcon,
  SettingsIcon,
  SparkIcon,
  StackIcon,
  SyncIcon,
  TvIcon,
} from "./icons";

export type NavKey = Category | "downloads" | "settings";

interface NavItem {
  key: NavKey;
  label: string;
  icon: (p: any) => JSX.Element;
  group?: string;
}

const ITEMS: NavItem[] = [
  { key: "movies", label: "Movies", icon: FilmIcon, group: "Library" },
  { key: "tv-shows", label: "TV Shows", icon: TvIcon, group: "Library" },
  { key: "continue-watching", label: "Continue Watching", icon: ResumeIcon, group: "Library" },
  { key: "favorites-movies", label: "Movies", icon: FilmIcon, group: "Favorites" },
  { key: "favorites-tv", label: "TV Shows", icon: TvIcon, group: "Favorites" },
  { key: "recently-added-movies", label: "New Movies", icon: SparkIcon, group: "Recently Added" },
  { key: "recently-added-tv", label: "New Episodes", icon: StackIcon, group: "Recently Added" },
  { key: "downloads", label: "Downloads", icon: DownloadIcon, group: "Activity" },
  { key: "settings", label: "Settings", icon: SettingsIcon, group: "Activity" },
];

const GROUP_LABEL =
  "px-3 pb-1.5 pt-4 text-[10px] font-semibold uppercase tracking-widest text-white/30";

interface Props {
  active: NavKey;
  onSelect: (key: NavKey) => void;
  downloadCount: number;

  query: string;
  onQueryChange: (q: string) => void;
  // Shown under the search box while results are up (e.g. "Directed by …").
  searchSummary?: string;
  // Actor/director suggestions for the current query; picking one runs the
  // exact cast:/director: filter.
  people: Person[];
  onPickPerson: (p: Person) => void;

  syncing: boolean;
  onSync: () => void;

  // Genre/sort controls for the current grid, pinned to the sidebar's foot.
  // Null when the current view has nothing to filter or sort.
  controls?: ReactNode;
}

export function Sidebar({
  active,
  onSelect,
  downloadCount,
  query,
  onQueryChange,
  searchSummary,
  people,
  onPickPerson,
  syncing,
  onSync,
  controls,
}: Props) {
  let lastGroup = "";
  return (
    <aside className="flex h-full w-60 shrink-0 flex-col border-r border-white/5 bg-ink-800/80">
      {/* The brand row doubles as the window drag handle now that there's no
          top bar. */}
      <div
        className={`flex items-center gap-2.5 px-5 pb-4 ${isMac ? "pt-9" : "pt-5"}`}
        style={{ ["--wails-draggable" as any]: "drag" }}
      >
        <BrandMark width={36} height={36} />
        <div className="flex-1 leading-tight">
          <div className="text-[15px] font-semibold tracking-tight text-white">
            Goplex
          </div>
          <div className="text-[11px] font-medium uppercase tracking-widest text-white/40">
            Media
          </div>
        </div>
        <button
          onClick={onSync}
          disabled={syncing}
          title={
            syncing
              ? "Library sync in progress…"
              : "Sync library — copy the latest index from your sync computer (set in Settings)"
          }
          style={{ ["--wails-draggable" as any]: "no-drag" }}
          className="rounded-lg border border-white/10 bg-ink-700 p-2 text-white/80 outline-none transition-colors hover:border-accent/60 hover:text-white disabled:opacity-60"
        >
          <SyncIcon width={14} height={14} className={syncing ? "animate-spin" : ""} />
        </button>
      </div>

      <div className="px-3 pb-1">
        <div className="relative">
          <SearchIcon
            width={15}
            height={15}
            className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-white/40"
          />
          <input
            value={query}
            onChange={(e) => onQueryChange(e.target.value)}
            placeholder="Search library…"
            className="w-full rounded-lg border border-white/10 bg-ink-700 py-2 pl-9 pr-8 text-sm text-white placeholder-white/30 outline-none focus:border-accent/60"
          />
          {query !== "" && (
            <button
              onClick={() => onQueryChange("")}
              title="Clear search"
              className="absolute right-1.5 top-1/2 -translate-y-1/2 rounded-md p-1 text-white/40 hover:bg-white/10 hover:text-white"
            >
              <CloseIcon width={13} height={13} />
            </button>
          )}
        </div>
        {searchSummary && (
          <div className="truncate px-1 pt-2 text-xs text-white/50" title={searchSummary}>
            {searchSummary}
          </div>
        )}
      </div>

      <nav className="flex-1 space-y-0.5 overflow-y-auto px-3 pb-4">
        {people.length > 0 && (
          <div>
            <div className={GROUP_LABEL}>People</div>
            {people.map((p) => (
              <button
                key={`${p.role}:${p.name}`}
                onClick={() => onPickPerson(p)}
                className="flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-sm text-white/70 transition-colors hover:bg-white/5 hover:text-white"
              >
                <span className="min-w-0 flex-1 truncate text-left">{p.name}</span>
                <span className="text-[10px] font-medium uppercase tracking-wider text-accent/70">
                  {p.role}
                </span>
              </button>
            ))}
          </div>
        )}
        {ITEMS.map((item) => {
          const showGroup = item.group && item.group !== lastGroup;
          lastGroup = item.group ?? lastGroup;
          const isActive = active === item.key;
          const Icon = item.icon;
          return (
            <div key={item.key}>
              {showGroup && <div className={GROUP_LABEL}>{item.group}</div>}
              <button
                onClick={() => onSelect(item.key)}
                className={`group flex w-full items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors ${
                  isActive
                    ? "bg-accent/15 text-accent-soft"
                    : "text-white/60 hover:bg-white/5 hover:text-white"
                }`}
              >
                <Icon
                  width={18}
                  height={18}
                  className={isActive ? "text-accent" : "text-white/50 group-hover:text-white"}
                />
                <span className="flex-1 text-left">{item.label}</span>
                {item.key === "downloads" && downloadCount > 0 && (
                  <span className="rounded-full bg-accent px-1.5 py-0.5 text-[10px] font-bold text-ink-900">
                    {downloadCount}
                  </span>
                )}
              </button>
            </div>
          );
        })}
      </nav>

      {controls && (
        <div className="shrink-0 border-t border-white/5 px-3 pb-4">
          <div className={GROUP_LABEL}>View</div>
          {controls}
        </div>
      )}
    </aside>
  );
}
