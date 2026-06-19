import { useEffect, useState } from "react";
import { api } from "../lib/api";
import type { Media, Season } from "../lib/types";
import { formatDuration, formatRating } from "../lib/format";
import {
  CloseIcon,
  DownloadIcon,
  FilmIcon,
  PlayIcon,
  ResumeIcon,
  StarIcon,
  TvIcon,
} from "./icons";

interface Props {
  media: Media;
  rcloneAvailable: boolean;
  mpvAvailable: boolean;
  onClose: () => void;
  onToast: (msg: string, kind?: "info" | "error") => void;
}

export function DetailModal(props: Props) {
  const { media } = props;
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-6 animate-fade-in"
      onClick={props.onClose}
    >
      <div className="absolute inset-0 bg-black/70 backdrop-blur-sm" />
      <div
        className="relative z-10 flex max-h-[86vh] w-full max-w-3xl overflow-hidden rounded-2xl border border-white/10 bg-ink-700 shadow-card"
        onClick={(e) => e.stopPropagation()}
      >
        <button
          onClick={props.onClose}
          className="absolute right-3 top-3 z-20 flex h-8 w-8 items-center justify-center rounded-full bg-black/40 text-white/70 transition-colors hover:bg-black/70 hover:text-white"
        >
          <CloseIcon width={18} height={18} />
        </button>
        {media.type === "show" ? (
          <ShowDetail {...props} />
        ) : (
          <ItemDetail {...props} />
        )}
      </div>
    </div>
  );
}

function Poster({ media }: { media: Media }) {
  const Placeholder = media.type === "show" ? TvIcon : FilmIcon;
  return (
    <div className="aspect-[2/3] w-48 shrink-0 overflow-hidden rounded-xl bg-ink-600 ring-1 ring-white/10">
      {media.thumbURL ? (
        <img src={media.thumbURL} alt={media.title} className="h-full w-full object-cover" />
      ) : (
        <div className="flex h-full w-full items-center justify-center text-white/20">
          <Placeholder width={48} height={48} />
        </div>
      )}
    </div>
  );
}

function MetaRow({ media }: { media: Media }) {
  const bits: string[] = [];
  if (media.year > 0) bits.push(String(media.year));
  if (media.contentRating) bits.push(media.contentRating);
  const dur = formatDuration(media.duration);
  if (dur) bits.push(dur);
  const rating = formatRating(media.rating);
  return (
    <div className="flex flex-wrap items-center gap-3 text-sm text-white/50">
      {bits.map((b, i) => (
        <span key={i}>{b}</span>
      ))}
      {rating && (
        <span className="flex items-center gap-1 text-accent-soft">
          <StarIcon width={14} height={14} /> {rating}
        </span>
      )}
    </div>
  );
}

function ItemDetail(props: Props) {
  const { media, mpvAvailable, rcloneAvailable, onToast } = props;
  const [busy, setBusy] = useState(false);
  const canResume = media.viewOffset > 0 && media.progressPct < 95;

  const play = async (resume: boolean) => {
    if (!mpvAvailable) {
      onToast("mpv is not installed", "error");
      return;
    }
    setBusy(true);
    try {
      await api.play([media.key], resume);
    } catch (e: any) {
      onToast(String(e?.message ?? e), "error");
    } finally {
      setBusy(false);
    }
  };

  const download = async () => {
    if (!rcloneAvailable) {
      onToast("rclone is not installed", "error");
      return;
    }
    try {
      onToast(`Downloading ${media.title}…`);
      await api.download([media.key], "");
    } catch (e: any) {
      onToast(String(e?.message ?? e), "error");
    }
  };

  return (
    <div className="flex gap-6 overflow-y-auto p-6">
      <Poster media={media} />
      <div className="flex min-w-0 flex-1 flex-col">
        {media.type === "episode" && media.parentTitle && (
          <div className="text-xs font-semibold uppercase tracking-widest text-accent/80">
            {media.parentTitle}
          </div>
        )}
        <h2 className="mt-1 text-2xl font-semibold leading-tight text-white">
          {media.type === "episode"
            ? `S${pad(media.parentIndex)}E${pad(media.index)} · ${media.title}`
            : media.title}
        </h2>
        <div className="mt-2">
          <MetaRow media={media} />
        </div>

        {media.genre && (
          <div className="mt-3 text-xs text-white/40">{media.genre}</div>
        )}

        {media.summary && (
          <p className="mt-4 max-h-40 overflow-y-auto text-sm leading-relaxed text-white/70">
            {media.summary}
          </p>
        )}

        {media.cast && (
          <div className="mt-4 text-xs text-white/40">
            <span className="font-semibold text-white/55">Cast: </span>
            {media.cast}
          </div>
        )}
        {media.director && (
          <div className="mt-1 text-xs text-white/40">
            <span className="font-semibold text-white/55">Director: </span>
            {media.director}
          </div>
        )}

        <div className="mt-auto flex flex-wrap gap-3 pt-6">
          {canResume && (
            <button
              disabled={busy}
              onClick={() => play(true)}
              className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2.5 text-sm font-semibold text-ink-900 transition-colors hover:bg-accent-soft disabled:opacity-50"
            >
              <ResumeIcon width={18} height={18} /> Resume {media.progressPct}%
            </button>
          )}
          <button
            disabled={busy}
            onClick={() => play(false)}
            className={`flex items-center gap-2 rounded-lg px-4 py-2.5 text-sm font-semibold transition-colors disabled:opacity-50 ${
              canResume
                ? "bg-white/10 text-white hover:bg-white/20"
                : "bg-accent text-ink-900 hover:bg-accent-soft"
            }`}
          >
            <PlayIcon width={18} height={18} /> {canResume ? "Play from start" : "Play"}
          </button>
          <button
            onClick={download}
            className="flex items-center gap-2 rounded-lg bg-white/10 px-4 py-2.5 text-sm font-semibold text-white transition-colors hover:bg-white/20"
          >
            <DownloadIcon width={18} height={18} /> Download
          </button>
        </div>
      </div>
    </div>
  );
}

function ShowDetail(props: Props) {
  const { media, mpvAvailable, rcloneAvailable, onToast } = props;
  const [seasons, setSeasons] = useState<Season[]>([]);
  const [season, setSeason] = useState<number | null>(null);
  const [episodes, setEpisodes] = useState<Media[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api.getSeasons(media.title).then((s) => {
      setSeasons(s);
      setLoading(false);
      if (s.length > 0) loadSeason(s[0].season);
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [media.title]);

  const loadSeason = (s: number) => {
    setSeason(s);
    setSelected(new Set());
    api.getEpisodes(media.title, s).then(setEpisodes);
  };

  const toggle = (key: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      next.has(key) ? next.delete(key) : next.add(key);
      return next;
    });
  };

  const orderedSelection = () =>
    episodes.filter((e) => selected.has(e.key)).map((e) => e.key);

  const playEpisode = async (key: string) => {
    if (!mpvAvailable) return onToast("mpv is not installed", "error");
    try {
      await api.play([key], false);
    } catch (e: any) {
      onToast(String(e?.message ?? e), "error");
    }
  };

  const playSelected = async () => {
    const keys = orderedSelection();
    if (keys.length === 0) return;
    if (!mpvAvailable) return onToast("mpv is not installed", "error");
    try {
      await api.play(keys, false);
    } catch (e: any) {
      onToast(String(e?.message ?? e), "error");
    }
  };

  const downloadSelected = async () => {
    const keys = orderedSelection();
    if (keys.length === 0) return;
    if (!rcloneAvailable) return onToast("rclone is not installed", "error");
    try {
      onToast(`Downloading ${keys.length} episode(s)…`);
      await api.download(keys, "");
    } catch (e: any) {
      onToast(String(e?.message ?? e), "error");
    }
  };

  return (
    <div className="flex max-h-[86vh] w-full flex-col">
      <div className="flex gap-5 border-b border-white/5 p-6">
        <div className="aspect-[2/3] w-32 shrink-0 overflow-hidden rounded-lg bg-ink-600 ring-1 ring-white/10">
          {media.thumbURL ? (
            <img src={media.thumbURL} alt={media.title} className="h-full w-full object-cover" />
          ) : (
            <div className="flex h-full w-full items-center justify-center text-white/20">
              <TvIcon width={40} height={40} />
            </div>
          )}
        </div>
        <div className="min-w-0 flex-1">
          <div className="text-xs font-semibold uppercase tracking-widest text-accent/80">
            TV Show
          </div>
          <h2 className="mt-1 text-2xl font-semibold text-white">{media.title}</h2>
          {media.genre && <div className="mt-1 text-xs text-white/40">{media.genre}</div>}
          {media.summary && (
            <p className="mt-3 max-h-20 overflow-y-auto text-sm leading-relaxed text-white/60">
              {media.summary}
            </p>
          )}
        </div>
      </div>

      {/* Season tabs */}
      <div className="flex shrink-0 gap-2 overflow-x-auto border-b border-white/5 px-6 py-3">
        {loading && <span className="text-sm text-white/40">Loading seasons…</span>}
        {seasons.map((s) => (
          <button
            key={s.season}
            onClick={() => loadSeason(s.season)}
            className={`whitespace-nowrap rounded-full px-3.5 py-1.5 text-sm font-medium transition-colors ${
              season === s.season
                ? "bg-accent text-ink-900"
                : "bg-white/5 text-white/60 hover:bg-white/10 hover:text-white"
            }`}
          >
            {s.season === 0 ? "Specials" : `Season ${s.season}`}
          </button>
        ))}
      </div>

      {/* Episodes */}
      <div className="flex-1 overflow-y-auto px-3 py-2">
        {episodes.map((ep) => {
          const isSel = selected.has(ep.key);
          return (
            <div
              key={ep.key}
              className={`group flex items-center gap-3 rounded-lg px-3 py-2.5 transition-colors ${
                isSel ? "bg-accent/10" : "hover:bg-white/5"
              }`}
            >
              <input
                type="checkbox"
                checked={isSel}
                onChange={() => toggle(ep.key)}
                className="h-4 w-4 shrink-0 accent-accent"
              />
              <button
                onClick={() => playEpisode(ep.key)}
                className="flex min-w-0 flex-1 items-center gap-3 text-left"
              >
                <span className="w-8 shrink-0 text-center text-sm font-semibold text-white/40">
                  {ep.index}
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-medium text-white/90">
                    {ep.title}
                  </span>
                  {ep.progressPct > 0 && ep.progressPct < 95 && (
                    <span className="text-xs text-accent/80">Watched {ep.progressPct}%</span>
                  )}
                </span>
                {formatDuration(ep.duration) && (
                  <span className="shrink-0 text-xs text-white/30">
                    {formatDuration(ep.duration)}
                  </span>
                )}
                <PlayIcon
                  width={16}
                  height={16}
                  className="shrink-0 text-white/0 transition-colors group-hover:text-white/70"
                />
              </button>
            </div>
          );
        })}
        {!loading && episodes.length === 0 && (
          <div className="py-8 text-center text-sm text-white/40">No episodes</div>
        )}
      </div>

      {/* Selection action bar */}
      {selected.size > 0 && (
        <div className="flex shrink-0 items-center justify-between border-t border-white/5 bg-ink-800/60 px-6 py-3">
          <span className="text-sm text-white/60">{selected.size} selected</span>
          <div className="flex gap-2">
            <button
              onClick={playSelected}
              className="flex items-center gap-2 rounded-lg bg-accent px-3.5 py-2 text-sm font-semibold text-ink-900 hover:bg-accent-soft"
            >
              <PlayIcon width={16} height={16} /> Play
            </button>
            <button
              onClick={downloadSelected}
              className="flex items-center gap-2 rounded-lg bg-white/10 px-3.5 py-2 text-sm font-semibold text-white hover:bg-white/20"
            >
              <DownloadIcon width={16} height={16} /> Download
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

function pad(n: number): string {
  return String(n).padStart(2, "0");
}
