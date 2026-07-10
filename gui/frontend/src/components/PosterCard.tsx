import type { MediaCard } from "../lib/types";
import { FilmIcon, TvIcon } from "./icons";

interface Props {
  media: MediaCard;
  onClick: () => void;
  // posterHeight fixes the poster box height in px so every card is identical
  // regardless of the source image's aspect ratio. When omitted, the card falls
  // back to a 2:3 aspect ratio.
  posterHeight?: number;
  priority?: boolean;
}

export function PosterCard({ media, onClick, posterHeight, priority = false }: Props) {
  const isShow = media.type === "show";
  const Placeholder = isShow ? TvIcon : FilmIcon;
  const subtitle =
    media.type === "show"
      ? `${media.episodeCount} episode${media.episodeCount === 1 ? "" : "s"}`
      : media.type === "episode"
      ? media.displayTitle
      : media.year > 0
      ? String(media.year)
      : "";

  return (
    <button
      onClick={onClick}
      className="group flex flex-col text-left focus:outline-none"
    >
      <div
        className={`relative w-full overflow-hidden rounded-xl bg-ink-600 shadow-card ring-1 ring-white/5 transition-transform duration-200 group-hover:-translate-y-1 group-hover:ring-accent/40 ${
          posterHeight ? "" : "aspect-[2/3]"
        }`}
        style={posterHeight ? { height: posterHeight } : undefined}
      >
        {media.thumbURL ? (
          <img
            src={media.thumbURL}
            alt={media.title}
            loading={priority ? "eager" : "lazy"}
            fetchPriority={priority ? "high" : "auto"}
            decoding="async"
            className="h-full w-full object-cover transition-transform duration-300 group-hover:scale-105"
            onError={(e) => {
              (e.currentTarget as HTMLImageElement).style.display = "none";
            }}
          />
        ) : null}
        {!media.thumbURL && (
          <div className="flex h-full w-full items-center justify-center text-white/20">
            <Placeholder width={40} height={40} />
          </div>
        )}

        {/* Hover overlay */}
        <div className="absolute inset-0 bg-gradient-to-t from-black/70 via-transparent to-transparent opacity-0 transition-opacity duration-200 group-hover:opacity-100" />

        {/* Watched check */}
        {media.viewCount > 0 && media.progressPct >= 95 && (
          <div className="absolute right-2 top-2 flex h-6 w-6 items-center justify-center rounded-full bg-accent text-ink-900 shadow">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round">
              <path d="M5 12l5 5L20 7" />
            </svg>
          </div>
        )}

        {/* Progress sliver */}
        {media.progressPct > 0 && media.progressPct < 95 && (
          <div className="absolute inset-x-0 bottom-0 h-1 bg-black/40">
            <div
              className="h-full bg-accent"
              style={{ width: `${media.progressPct}%` }}
            />
          </div>
        )}
      </div>

      <div className="mt-2 px-0.5">
        <div className="truncate text-sm font-medium text-white/90 group-hover:text-white">
          {media.title}
        </div>
        {subtitle && (
          <div className="truncate text-xs text-white/40">{subtitle}</div>
        )}
      </div>
    </button>
  );
}
