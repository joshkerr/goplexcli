import type { Category } from "../lib/types";
import { isMac } from "../lib/api";
import {
  DownloadIcon,
  FilmIcon,
  ResumeIcon,
  SettingsIcon,
  SparkIcon,
  StackIcon,
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
  { key: "recently-added-movies", label: "New Movies", icon: SparkIcon, group: "Recently Added" },
  { key: "recently-added-tv", label: "New Episodes", icon: StackIcon, group: "Recently Added" },
  { key: "downloads", label: "Downloads", icon: DownloadIcon, group: "Activity" },
  { key: "settings", label: "Settings", icon: SettingsIcon, group: "Activity" },
];

interface Props {
  active: NavKey;
  onSelect: (key: NavKey) => void;
  downloadCount: number;
}

export function Sidebar({ active, onSelect, downloadCount }: Props) {
  let lastGroup = "";
  return (
    <aside className="flex h-full w-60 shrink-0 flex-col border-r border-white/5 bg-ink-800/80">
      <div
        className={`flex items-center gap-2.5 px-5 pb-5 ${isMac ? "pt-9" : "pt-5"}`}
      >
        <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-gradient-to-br from-accent to-accent-dark text-ink-900 shadow-glow">
          <FilmIcon width={20} height={20} />
        </div>
        <div className="leading-tight">
          <div className="text-[15px] font-semibold tracking-tight text-white">
            Goplex
          </div>
          <div className="text-[11px] font-medium uppercase tracking-widest text-white/40">
            Media
          </div>
        </div>
      </div>

      <nav className="flex-1 space-y-0.5 overflow-y-auto px-3 pb-4">
        {ITEMS.map((item) => {
          const showGroup = item.group && item.group !== lastGroup;
          lastGroup = item.group ?? lastGroup;
          const isActive = active === item.key;
          const Icon = item.icon;
          return (
            <div key={item.key}>
              {showGroup && (
                <div className="px-3 pb-1.5 pt-4 text-[10px] font-semibold uppercase tracking-widest text-white/30">
                  {item.group}
                </div>
              )}
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
    </aside>
  );
}
