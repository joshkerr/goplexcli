import { useEffect, useRef, useState } from "react";
import { api } from "../lib/api";
import type {
  DownloadConflict,
  MatchCandidate,
  MatchResult,
  Media,
  MediaCard,
  OnExisting,
  Season,
} from "../lib/types";
import { formatDuration, formatRating } from "../lib/format";
import { ConflictModal } from "./ConflictModal";
import {
  Action,
  ActionGroup,
  DetailPanel,
  Facts,
  Overview,
  PrimaryAction,
  Section,
  dateLabel,
} from "./DetailPanel";
import {
  DownloadIcon,
  FilmIcon,
  PlayIcon,
  ResumeIcon,
  StarIcon,
  StarOutlineIcon,
  TvIcon,
} from "./icons";
import Select from "./Select";

interface Props {
  media: Media;
  // Slide the drawer in: it's opening, not taking a title swapped in.
  animateIn?: boolean;
  rcloneAvailable: boolean;
  mpvAvailable: boolean;
  // Names of enabled remote download servers. When non-empty, a picker next
  // to the Download button chooses where the transfer runs ("" = locally).
  remoteServers: string[];
  isFavorite: boolean;
  onToggleFavorite: (key: string) => void;
  onClose: () => void;
  onToast: (msg: string, kind?: "info" | "error") => void;
  // Run a field-scoped search (e.g. all movies by a director). Called when a
  // director/cast/genre tag is clicked; the results fill the grid beside
  // this panel, which stays open.
  onSearch: (query: string) => void;
  // Open another item in the panel (a "More like this" card, an episode's
  // show); the caller swaps the panel's content to the new item.
  onSelectSimilar: (card: MediaCard) => void;
  // Reload the panel with the item at `key` after its metadata changed (a
  // TMDB match was fixed). The key differs from media.key when a show was
  // renamed by the new match.
  onReopen: (key: string) => void;
}

// canFixMatch reports whether the "Fix match…" control applies: TMDB matches
// can only be corrected for locally indexed movies and shows. A show's key
// ("show:<title>") doesn't say which library it came from, so the control is
// offered for every show and the backend rejects Plex ones.
function canFixMatch(media: Media): boolean {
  if (media.type === "movie") return media.key.startsWith("/local/metadata/");
  if (media.type === "show") return media.key.startsWith("show:");
  return false;
}

// FixMatchButton is the small text trigger that opens the FixMatchPanel.
function FixMatchButton({
  open,
  onClick,
}: {
  open: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`rounded text-xs font-medium transition-colors hover:underline focus:outline-none ${
        open ? "text-accent-soft" : "text-accent/80 hover:text-accent"
      }`}
    >
      Fix match…
    </button>
  );
}

const PANEL_INPUT =
  "rounded-lg border border-white/10 bg-ink-800 px-3 py-2 text-sm text-white placeholder-white/30 outline-none focus:border-accent/60 disabled:opacity-50";

const KIND_OPTIONS = [
  { value: "movie" as const, label: "Movie" },
  { value: "tv" as const, label: "TV Show" },
];

// stripYear drops a trailing " (YYYY)" from a title so the TMDB query starts
// from the bare name ("Heat (1995)" → "Heat").
function stripYear(title: string): string {
  return title.replace(/\s*\((?:19|20)\d{2}\)\s*$/, "").trim();
}

// FixMatchPanel lets the user correct a wrong TMDB match for a locally
// indexed movie or show: search TMDB by kind/title/year, pick a candidate
// (or fall back to automatic matching), then reopen the rebuilt item.
function FixMatchPanel({
  media,
  onToast,
  onReopen,
  onClose,
}: {
  media: Media;
  onToast: (msg: string, kind?: "info" | "error") => void;
  onReopen: (key: string) => void;
  onClose: () => void;
}) {
  const [kind, setKind] = useState<MatchCandidate["kind"]>(
    media.type === "show" ? "tv" : "movie"
  );
  const [query, setQuery] = useState(() => stripYear(media.title));
  const [year, setYear] = useState(media.year > 0 ? String(media.year) : "");
  const [searching, setSearching] = useState(false);
  const [searched, setSearched] = useState(false);
  const [results, setResults] = useState<MatchCandidate[]>([]);
  const [applying, setApplying] = useState(false);
  const busy = searching || applying;

  const search = async () => {
    const q = query.trim();
    if (!q || busy) return;
    setSearching(true);
    try {
      const y = parseInt(year, 10);
      const found = await api.searchMatches(media.key, kind, q, isNaN(y) ? 0 : y);
      setResults(found);
      setSearched(true);
    } catch (e: any) {
      onToast(String(e?.message ?? e), "error");
    } finally {
      setSearching(false);
    }
  };

  // apply sends the chosen TMDB id (0 = automatic) and reopens the item at
  // whatever key the backend reports, since a show can be renamed.
  const apply = async (tmdbID: number) => {
    if (busy) return;
    setApplying(true);
    try {
      const res: MatchResult = await api.applyMatch(media.key, tmdbID);
      onToast(`Matched to "${res.title}"`);
      onClose();
      onReopen(res.key || media.key);
    } catch (e: any) {
      onToast(String(e?.message ?? e), "error");
      setApplying(false);
    }
  };

  return (
    <div
      className="rounded-xl border border-white/10 bg-ink-800/60 p-4"
      // Escape from anywhere in here (its inputs included) cancels the fix —
      // not the drawer, whose handler skips an Escape marked handled. While
      // a match applies it's swallowed: the drawer reopens on the result.
      // The kind picker's own list, when open, takes Escape before this.
      onKeyDown={(e) => {
        if (e.key !== "Escape" || e.defaultPrevented) return;
        e.preventDefault();
        if (!applying) onClose();
      }}
    >
      <div className="flex items-center justify-between gap-3">
        <div className="text-xs font-semibold uppercase tracking-widest text-white/40">
          Fix match
        </div>
        <button
          type="button"
          onClick={onClose}
          disabled={applying}
          className="rounded text-xs font-medium text-white/50 transition-colors hover:text-white disabled:opacity-50"
        >
          Cancel
        </button>
      </div>

      <div className="mt-3 flex flex-wrap gap-2">
        <Select<MatchCandidate["kind"]>
          value={kind}
          onChange={setKind}
          options={KIND_OPTIONS}
          disabled={busy}
          className="bg-ink-800 px-3 py-2 text-sm text-white"
          aria-label="Match kind"
        />
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              void search();
            }
          }}
          disabled={busy}
          placeholder="Title"
          aria-label="Title to search for"
          className={`min-w-0 flex-1 ${PANEL_INPUT}`}
        />
        <input
          value={year}
          onChange={(e) => setYear(e.target.value.replace(/\D/g, "").slice(0, 4))}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              void search();
            }
          }}
          disabled={busy}
          inputMode="numeric"
          placeholder="Year"
          aria-label="Release year (optional)"
          className={`w-20 ${PANEL_INPUT}`}
        />
        <button
          type="button"
          onClick={search}
          disabled={busy || !query.trim()}
          className="rounded-lg bg-accent px-4 py-2 text-sm font-semibold text-ink-900 transition-colors hover:bg-accent-soft disabled:opacity-50"
        >
          {searching ? "Searching…" : "Search"}
        </button>
      </div>

      {applying && (
        <div className="mt-3 flex items-center gap-2 text-sm text-white/60">
          <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-white/20 border-t-accent" />
          Updating…
        </div>
      )}

      {searching && (
        <div className="mt-3 flex items-center gap-2 text-sm text-white/60">
          <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-white/20 border-t-accent" />
          Searching TMDB…
        </div>
      )}

      {!searching && searched && results.length === 0 && (
        <div className="mt-3 text-sm text-white/40">
          No matches found — try a different title or clear the year.
        </div>
      )}

      {!searching && results.length > 0 && (
        <div
          className={`mt-3 max-h-72 space-y-1 overflow-y-auto pr-1 ${
            applying ? "pointer-events-none opacity-50" : ""
          }`}
        >
          {results.map((c) => (
            <MatchRow key={`${c.kind}:${c.id}`} candidate={c} onPick={() => apply(c.id)} />
          ))}
        </div>
      )}

      <div className="mt-3 text-xs text-white/40">
        Wrong title picked automatically?{" "}
        <button
          type="button"
          onClick={() => apply(0)}
          disabled={busy}
          className="rounded font-medium text-accent/80 transition-colors hover:text-accent hover:underline disabled:opacity-50"
        >
          Use automatic match
        </button>
      </div>
    </div>
  );
}

// MatchRow is one clickable TMDB candidate: poster thumbnail, title, year,
// rating and a two-line overview; the whole row applies the match.
function MatchRow({
  candidate,
  onPick,
}: {
  candidate: MatchCandidate;
  onPick: () => void;
}) {
  const Placeholder = candidate.kind === "tv" ? TvIcon : FilmIcon;
  return (
    <button
      type="button"
      onClick={onPick}
      className="group flex w-full items-start gap-3 rounded-lg px-2 py-2 text-left transition-colors hover:bg-white/5 focus:bg-white/5 focus:outline-none"
    >
      <div className="aspect-[2/3] w-12 shrink-0 overflow-hidden rounded bg-ink-600 ring-1 ring-white/10">
        {candidate.posterURL ? (
          <img
            src={candidate.posterURL}
            alt=""
            loading="lazy"
            decoding="async"
            className="h-full w-full object-cover"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center text-white/20">
            <Placeholder width={18} height={18} />
          </div>
        )}
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-2">
          <span className="truncate text-sm font-medium text-white/90 group-hover:text-white">
            {candidate.title}
          </span>
          {candidate.year > 0 && (
            <span className="shrink-0 text-xs text-white/40">{candidate.year}</span>
          )}
          {candidate.rating > 0 && (
            <span className="flex shrink-0 items-center gap-1 text-xs text-accent-soft">
              <StarIcon width={11} height={11} /> {candidate.rating.toFixed(1)}
            </span>
          )}
        </div>
        {candidate.overview && (
          <p className="mt-0.5 line-clamp-2 text-xs leading-relaxed text-white/50">
            {candidate.overview}
          </p>
        )}
      </div>
      <span className="shrink-0 self-center rounded-md bg-white/10 px-2.5 py-1 text-xs font-semibold text-white/0 transition-colors group-hover:text-white group-focus:text-white">
        Use this
      </span>
    </button>
  );
}

// TagLinks splits a comma-separated tag field ("Tom Hardy, Cillian Murphy")
// into individually clickable names. Clicking one runs a field-scoped search
// (`field:"Name"`) so the grid shows every movie with that director/actor/genre.
function TagLinks({
  field,
  value,
  onSearch,
}: {
  field: "director" | "cast" | "genre";
  value: string;
  onSearch: (query: string) => void;
}) {
  const names = value
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
  return (
    <>
      {names.map((name, i) => (
        <span key={i}>
          <button
            onClick={() => onSearch(`${field}:"${name}"`)}
            className="rounded transition-colors hover:text-accent hover:underline focus:text-accent focus:outline-none"
          >
            {name}
          </button>
          {i < names.length - 1 ? ", " : ""}
        </span>
      ))}
    </>
  );
}


/** The drawer behind a poster click: the title's art, facts and synopsis,
 * its actions grouped by what they do, and for a show its seasons and
 * episodes. */
export function DetailDrawer(props: Props) {
  return props.media.type === "show" ? <ShowDrawer {...props} /> : <ItemDrawer {...props} />;
}

/** The facts line under the title: year · runtime · rating · ★ score. */
function metaParts(media: Media): string[] {
  const bits: string[] = [];
  if (media.type === "episode") bits.push(`S${pad(media.parentIndex)}E${pad(media.index)}`);
  if (media.year > 0 && media.type !== "episode") bits.push(String(media.year));
  if (media.type === "episode" && media.aired) bits.push(dateLabel(media.aired));
  const dur = formatDuration(media.duration);
  if (dur) bits.push(dur);
  if (media.contentRating) bits.push(media.contentRating);
  const rating = formatRating(media.rating);
  if (rating) bits.push(`★ ${rating}`);
  return bits;
}

/** Where the item comes from, for the facts: "Thevault · Plex". */
function sourceLabel(media: Media): string {
  if (!media.serverName) return "";
  return `${media.serverName} · ${media.key.startsWith("/local/metadata/") ? "local library" : "Plex"}`;
}

function ItemDrawer(props: Props) {
  const { media, mpvAvailable, rcloneAvailable, remoteServers, onToast, onSearch } = props;
  const [busy, setBusy] = useState(false);
  // Download button feedback: "queued" flips instantly on click (highlight +
  // disable, so the press visibly registered), "done" when the transfer
  // finishes. Reset when the panel swaps to another item.
  const [dlState, setDlState] = useState<"idle" | "queued" | "done">("idle");
  // Where the download runs: "" = this computer, else a remote server name.
  const [target, setTarget] = useDownloadTarget(remoteServers);
  // Destination files that already exist; non-null shows the conflict prompt.
  const [conflicts, setConflicts] = useState<DownloadConflict[] | null>(null);
  // Whether the inline "Fix match" TMDB search panel is open.
  const [fixing, setFixing] = useState(false);
  useEffect(() => {
    setDlState("idle");
    setConflicts(null);
    setFixing(false);
  }, [media.key]);
  const canResume = media.viewOffset > 0 && media.progressPct < 95;
  const isEpisode = media.type === "episode";

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
    // A remote target downloads with its own rclone; only local downloads
    // need rclone on this machine.
    if (!target && !rcloneAvailable) {
      onToast("rclone is not installed", "error");
      return;
    }
    if (target) {
      // The files land on the remote machine, so existing-file conflicts
      // can't be checked from here; the server overwrites in place.
      setDlState("queued");
      try {
        await api.downloadRemote([media.key], target, "");
        onToast(`Sent ${media.title} to ${target}`);
        setDlState("done");
      } catch (e: any) {
        onToast(String(e?.message ?? e), "error");
        setDlState("idle"); // allow a retry after a failure
      }
      return;
    }
    // Ask how to handle destination files that already exist before starting.
    try {
      const found = await api.checkDownloadConflicts([media.key], "");
      if (found.length > 0) {
        setConflicts(found);
        return;
      }
    } catch (e: any) {
      onToast(String(e?.message ?? e), "error");
      return;
    }
    await startDownload("");
  };

  // startDownload runs the transfer with the chosen existing-file policy
  // (picked in ConflictModal when the destination file already exists).
  const startDownload = async (onExisting: OnExisting) => {
    setDlState("queued");
    try {
      onToast(`Downloading ${media.title}…`);
      await api.download([media.key], "", onExisting);
      setDlState("done");
    } catch (e: any) {
      onToast(String(e?.message ?? e), "error");
      setDlState("idle"); // allow a retry after a failure
    }
  };

  const downloadLabel =
    dlState === "idle"
      ? "Download"
      : dlState === "queued"
        ? target
          ? "Sending…"
          : "Queued"
        : target
          ? "Sent"
          : "Downloaded";

  return (
    <DetailPanel
      label={media.title}
      animateIn={props.animateIn}
      onClose={props.onClose}
      art={media.artURL}
      poster={media.thumbURL}
      posterPlaceholder={<FilmIcon width={28} height={28} />}
      wideThumb={isEpisode}
      progress={media.progressPct}
      kicker={
        isEpisode && media.parentTitle ? (
          <button
            type="button"
            onClick={() =>
              props.onSelectSimilar({
                key: `show:${media.parentTitle}`,
                type: "show",
                title: media.parentTitle,
                year: 0,
                displayTitle: media.parentTitle,
                thumbURL: "",
                progressPct: 0,
                viewCount: 0,
                episodeCount: 0,
                newCount: 0,
              })
            }
            className="rounded text-accent/90 transition-colors hover:text-accent hover:underline focus:outline-none"
            title="Open the show"
          >
            {media.parentTitle} ›
          </button>
        ) : undefined
      }
      title={media.title}
      meta={metaParts(media)}
      actions={
        <>
          <PrimaryAction
            disabled={busy}
            onClick={() => play(canResume)}
            icon={canResume ? <ResumeIcon width={18} height={18} /> : <PlayIcon width={18} height={18} />}
            title={mpvAvailable ? "Play in mpv" : "mpv is not installed"}
          >
            {canResume ? `Resume · ${media.progressPct}%` : "Play"}
          </PrimaryAction>

          {(canResume || media.type === "movie") && (
            <ActionGroup label="Watch">
              {canResume && (
                <Action disabled={busy} onClick={() => play(false)} icon={<PlayIcon width={16} height={16} />}>
                  Play from start
                </Action>
              )}
              {media.type === "movie" && (
                <Action
                  onClick={() => props.onToggleFavorite(media.key)}
                  active={props.isFavorite}
                  icon={
                    props.isFavorite ? (
                      <StarIcon width={16} height={16} />
                    ) : (
                      <StarOutlineIcon width={16} height={16} />
                    )
                  }
                >
                  {props.isFavorite ? "Favorited" : "Favorite"}
                </Action>
              )}
            </ActionGroup>
          )}

          <ActionGroup label="Download">
            <Action
              onClick={download}
              disabled={dlState !== "idle"}
              active={dlState !== "idle"}
              icon={<DownloadIcon width={16} height={16} />}
              title={target ? `Download on ${target}` : "Download with rclone to this computer"}
            >
              {downloadLabel}
            </Action>
            {remoteServers.length > 0 && (
              <TargetPicker target={target} servers={remoteServers} onChange={setTarget} />
            )}
          </ActionGroup>

          {canFixMatch(media) && (
            <ActionGroup label="Library">
              <Action onClick={() => setFixing((f) => !f)} active={fixing}>
                {fixing ? "Cancel fix match" : "Fix match…"}
              </Action>
            </ActionGroup>
          )}
        </>
      }
    >
      {fixing && (
        <FixMatchPanel
          media={media}
          onToast={onToast}
          onReopen={props.onReopen}
          onClose={() => setFixing(false)}
        />
      )}

      {media.summary && (
        <Section title={isEpisode ? "This episode" : "Synopsis"}>
          <div className="flex gap-4">
            {/* The episode's own still, when the band shows the show's art. */}
            {isEpisode && media.artURL && media.thumbURL && (
              <img
                src={media.thumbURL}
                alt=""
                draggable={false}
                className="w-36 shrink-0 self-start rounded-md shadow-card ring-1 ring-white/10"
              />
            )}
            <Overview text={media.summary} />
          </div>
        </Section>
      )}

      <Section title="Details">
        <Facts
          items={[
            [isEpisode ? "Show" : "Directed by", isEpisode ? media.parentTitle : media.director && <TagLinks field="director" value={media.director} onSearch={onSearch} />],
            ["Cast", media.cast && <TagLinks field="cast" value={media.cast} onSearch={onSearch} />],
            ["Genres", media.genre && <TagLinks field="genre" value={media.genre} onSearch={onSearch} />],
            ["Studio", media.studio],
            [isEpisode ? "Aired" : "Released", media.aired && dateLabel(media.aired)],
            ["Source", sourceLabel(media)],
          ]}
        />
      </Section>

      <MoreLikeThis media={media} onSelect={props.onSelectSimilar} />

      {conflicts && (
        <ConflictModal
          conflicts={conflicts}
          onChoose={(choice) => {
            setConflicts(null);
            if (choice === "cancel") return;
            void startDownload(choice);
          }}
        />
      )}
    </DetailPanel>
  );
}

const DOWNLOAD_TARGET_KEY = "goplex:downloadTarget";

// Share the last destination across movie/episode/show panels and app launches.
// Only user selections write it: the server list is initially empty while the
// app loads, so falling back to this computer must not erase the preference.
function useDownloadTarget(servers: string[]): [string, (target: string) => void] {
  const [savedTarget, setSavedTarget] = useState(() => {
    try {
      return localStorage.getItem(DOWNLOAD_TARGET_KEY) ?? "";
    } catch {
      return "";
    }
  });
  const selectTarget = (target: string) => {
    setSavedTarget(target);
    try {
      localStorage.setItem(DOWNLOAD_TARGET_KEY, target);
    } catch {
      // Storage may be unavailable; the current selection still works.
    }
  };
  return [servers.includes(savedTarget) ? savedTarget : "", selectTarget];
}

// TargetPicker chooses where a download runs: this computer or one of the
// registered remote download servers. Only rendered when at least one remote
// server is enabled; "" = this computer.
function TargetPicker({
  target,
  servers,
  onChange,
  compact = false,
}: {
  target: string;
  servers: string[];
  onChange: (t: string) => void;
  compact?: boolean;
}) {
  return (
    <Select
      value={target}
      onChange={onChange}
      options={[
        { value: "", label: "To: this computer" },
        ...servers.map((s) => ({ value: s, label: `To: ⇄ ${s}` })),
      ]}
      title="Where the download runs"
      className={`bg-ink-700 text-white ${compact ? "px-2 py-2 text-sm" : "h-9 w-full px-3 text-sm"}`}
    />
  );
}

// MoreLikeThis fetches and renders a horizontal strip of the items most
// similar to the open one (summary TF-IDF + genre/director/cast/year overlap,
// computed backend-side). Renders nothing while loading or when the library
// has no meaningful neighbors. For an episode the backend returns similar
// shows, so the strip works in every ItemDetail context.
function MoreLikeThis({
  media,
  onSelect,
}: {
  media: Media;
  onSelect: (card: MediaCard) => void;
}) {
  const [cards, setCards] = useState<MediaCard[]>([]);
  useEffect(() => {
    let stale = false;
    setCards([]);
    api
      .similarItems(media.key)
      .then((c) => {
        if (!stale) setCards(c);
      })
      .catch(() => {});
    return () => {
      stale = true;
    };
  }, [media.key]);

  if (cards.length === 0) return null;
  return (
    <Section title="More like this">
      <div className="flex gap-3 overflow-x-auto pb-1">
        {cards.map((card) => {
          const Placeholder = card.type === "show" ? TvIcon : FilmIcon;
          return (
            <button
              key={card.key}
              onClick={() => onSelect(card)}
              title={card.title}
              className="group w-24 shrink-0 text-left focus:outline-none"
            >
              <div className="aspect-[2/3] w-full overflow-hidden rounded-lg bg-ink-600 ring-1 ring-white/5 transition group-hover:ring-accent/40">
                {card.thumbURL ? (
                  <img
                    src={card.thumbURL}
                    alt={card.title}
                    loading="lazy"
                    decoding="async"
                    className="h-full w-full object-cover transition-transform duration-200 group-hover:scale-105"
                  />
                ) : (
                  <div className="flex h-full w-full items-center justify-center text-white/20">
                    <Placeholder width={24} height={24} />
                  </div>
                )}
              </div>
              <div className="mt-1.5 truncate text-xs font-medium text-white/70 group-hover:text-white">
                {card.title}
              </div>
            </button>
          );
        })}
      </div>
    </Section>
  );
}


function ShowDrawer(props: Props) {
  const { media, mpvAvailable, rcloneAvailable, remoteServers, onToast, onSearch } = props;
  const [seasons, setSeasons] = useState<Season[]>([]);
  const [season, setSeason] = useState<number | null>(null);
  const [episodes, setEpisodes] = useState<Media[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(true);
  // Brief "Queued" confirmation on the Download buttons, mirroring the
  // movie download button's press feedback.
  const [queued, setQueued] = useState(false);
  // Where the download runs: "" = this computer, else a remote server name.
  const [target, setTarget] = useDownloadTarget(remoteServers);
  // Destination files that already exist for the pending keys; non-null
  // shows the conflict prompt. keys captures the set at check time so the
  // choice applies to exactly the files the user was warned about.
  const [conflicts, setConflicts] = useState<{ keys: string[]; files: DownloadConflict[] } | null>(null);
  // Whether the inline "Fix match" TMDB search panel is open.
  const [fixing, setFixing] = useState(false);
  // The season whose episodes were last asked for: a slower answer for
  // another (a tab clicked before the last one loaded) is dropped.
  const wantedSeason = useRef<number | null>(null);

  // Load the seasons on open — and again when the drawer is handed fresh
  // data for this show (a library sync), staying on the season being looked
  // at, with its ticks, while it still exists. (The drawer is keyed by
  // title, so another show remounts this instead.)
  useEffect(() => {
    let stale = false;
    api
      .getSeasons(media.title)
      .then((list) => {
        if (stale) return;
        const s = list ?? [];
        setSeasons(s);
        setLoading(false);
        const keep = wantedSeason.current;
        const next = keep !== null && s.some((x) => x.season === keep) ? keep : s[0]?.season ?? null;
        if (next === null) {
          wantedSeason.current = null;
          setSeason(null);
          setEpisodes([]);
          return;
        }
        loadSeason(next, next === keep);
      })
      .catch(() => {
        if (!stale) setLoading(false);
      });
    return () => {
      stale = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [media]);

  const loadSeason = (s: number, keepTicks = false) => {
    wantedSeason.current = s;
    setSeason(s);
    if (!keepTicks) setSelected(new Set());
    api
      .getEpisodes(media.title, s)
      .then((list) => {
        if (wantedSeason.current !== s) return;
        const eps = list ?? [];
        setEpisodes(eps);
        if (!keepTicks) return;
        // Drop ticks on episodes the refresh took away.
        const present = new Set(eps.map((e) => e.key));
        setSelected((prev) => {
          const next = new Set([...prev].filter((k) => present.has(k)));
          return next.size === prev.size ? prev : next;
        });
      })
      .catch(() => {});
  };

  const toggle = (key: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      next.has(key) ? next.delete(key) : next.add(key);
      return next;
    });
  };

  const orderedSelection = () => episodes.filter((e) => selected.has(e.key)).map((e) => e.key);

  const playKeys = async (keys: string[]) => {
    if (keys.length === 0) return;
    if (!mpvAvailable) return onToast("mpv is not installed", "error");
    try {
      await api.play(keys, false);
    } catch (e: any) {
      onToast(String(e?.message ?? e), "error");
    }
  };

  const downloadKeys = async (keys: string[]) => {
    if (keys.length === 0) return;
    if (target) {
      // The files land on the remote machine, so existing-file conflicts
      // can't be checked from here; the server overwrites in place.
      try {
        await api.downloadRemote(keys, target, "");
        onToast(`Sent ${keys.length} episode(s) to ${target}`);
        flashQueued();
      } catch (e: any) {
        onToast(String(e?.message ?? e), "error");
      }
      return;
    }
    if (!rcloneAvailable) return onToast("rclone is not installed", "error");
    // Ask how to handle destination files that already exist before starting.
    try {
      const found = await api.checkDownloadConflicts(keys, "");
      if (found.length > 0) {
        setConflicts({ keys, files: found });
        return;
      }
    } catch (e: any) {
      return onToast(String(e?.message ?? e), "error");
    }
    await startDownload(keys, "");
  };

  const flashQueued = () => {
    setQueued(true);
    setTimeout(() => setQueued(false), 2000);
  };

  // startDownload runs the transfer with the chosen existing-file policy
  // (picked in ConflictModal when some destination files already exist).
  const startDownload = async (keys: string[], onExisting: OnExisting) => {
    // Confirm the press now: a local download resolves only once every
    // file has transferred, which can be long after the drawer moved on.
    flashQueued();
    try {
      onToast(`Downloading ${keys.length} episode(s)…`);
      await api.download(keys, "", onExisting);
    } catch (e: any) {
      onToast(String(e?.message ?? e), "error");
    }
  };

  const seasonName = (n: number) => (n === 0 ? "Specials" : `Season ${n}`);
  const meta: string[] = [];
  if (media.year > 0) meta.push(String(media.year));
  if (media.contentRating) meta.push(media.contentRating);
  if (seasons.length > 0) meta.push(`${seasons.length} season${seasons.length === 1 ? "" : "s"}`);
  if (media.episodeCount > 0) meta.push(`${media.episodeCount} episode${media.episodeCount === 1 ? "" : "s"}`);

  return (
    <DetailPanel
      label={media.title}
      animateIn={props.animateIn}
      onClose={props.onClose}
      art={media.artURL}
      poster={media.thumbURL}
      posterPlaceholder={<TvIcon width={28} height={28} />}
      title={media.title}
      meta={meta}
      actions={
        <>
          <PrimaryAction
            disabled={episodes.length === 0}
            onClick={() => playKeys(episodes.map((e) => e.key))}
            icon={<PlayIcon width={18} height={18} />}
            title={
              season === null
                ? undefined
                : `Play every episode of ${seasonName(season)} in order${mpvAvailable ? "" : " — mpv is not installed"}`
            }
          >
            {season === null ? "Play" : `Play ${seasonName(season)}`}
          </PrimaryAction>

          <ActionGroup label="Watch">
            <Action
              onClick={() => props.onToggleFavorite(media.key)}
              active={props.isFavorite}
              icon={
                props.isFavorite ? <StarIcon width={16} height={16} /> : <StarOutlineIcon width={16} height={16} />
              }
            >
              {props.isFavorite ? "Favorited" : "Favorite"}
            </Action>
          </ActionGroup>

          <ActionGroup label="Download">
            <Action
              onClick={() => downloadKeys(episodes.map((e) => e.key))}
              disabled={queued || episodes.length === 0}
              active={queued}
              icon={<DownloadIcon width={16} height={16} />}
              title={season === null ? undefined : `Download every episode of ${seasonName(season)}`}
            >
              {queued ? "Queued" : season === null ? "Download" : `Download ${seasonName(season)}`}
            </Action>
            {remoteServers.length > 0 && (
              <TargetPicker target={target} servers={remoteServers} onChange={setTarget} />
            )}
          </ActionGroup>

          {canFixMatch(media) && (
            <ActionGroup label="Library">
              <Action onClick={() => setFixing((f) => !f)} active={fixing}>
                {fixing ? "Cancel fix match" : "Fix match…"}
              </Action>
            </ActionGroup>
          )}
        </>
      }
      footer={
        selected.size > 0 ? (
          <div className="flex shrink-0 items-center justify-between gap-3 border-t border-white/10 bg-ink-800/95 px-5 py-3">
            <span className="min-w-0 truncate text-sm text-white/60">{selected.size} selected</span>
            <div className="flex shrink-0 gap-2">
              <button
                type="button"
                onClick={() => playKeys(orderedSelection())}
                className="flex items-center gap-2 rounded-lg bg-accent px-3.5 py-2 text-sm font-semibold text-ink-900 hover:bg-accent-soft"
              >
                <PlayIcon width={16} height={16} /> Play
              </button>
              <button
                type="button"
                onClick={() => downloadKeys(orderedSelection())}
                disabled={queued}
                className={`flex items-center gap-2 rounded-lg px-3.5 py-2 text-sm font-semibold transition-all ${
                  queued ? "cursor-default bg-accent/20 text-accent-soft" : "bg-white/10 text-white hover:bg-white/20"
                }`}
              >
                <DownloadIcon width={16} height={16} /> {queued ? "Queued" : "Download"}
              </button>
            </div>
          </div>
        ) : undefined
      }
    >
      {fixing && (
        <FixMatchPanel
          media={media}
          onToast={onToast}
          onReopen={props.onReopen}
          onClose={() => setFixing(false)}
        />
      )}

      {media.summary && (
        <Section title="Synopsis">
          <Overview text={media.summary} />
        </Section>
      )}

      <Section title="Details">
        <Facts
          items={[
            ["Created by", media.director && <TagLinks field="director" value={media.director} onSearch={onSearch} />],
            ["Cast", media.cast && <TagLinks field="cast" value={media.cast} onSearch={onSearch} />],
            ["Genres", media.genre && <TagLinks field="genre" value={media.genre} onSearch={onSearch} />],
            ["Network", media.studio],
            ["Source", sourceLabel(media)],
          ]}
        />
      </Section>

      <section>
        {seasons.length > 1 && (
          <div className="mb-3 flex flex-wrap gap-1.5" role="tablist" aria-label="Seasons">
            {seasons.map((s) => (
              <button
                key={s.season}
                type="button"
                role="tab"
                aria-selected={season === s.season}
                onClick={() => loadSeason(s.season)}
                className={`rounded-full px-3 py-1 text-xs font-medium transition-colors ${
                  season === s.season
                    ? "bg-accent text-ink-900"
                    : "bg-white/5 text-white/60 hover:bg-white/10 hover:text-white"
                }`}
              >
                {seasonName(s.season)}
              </button>
            ))}
          </div>
        )}
        <div className="mb-2 flex items-baseline gap-3">
          <h2 className="text-base font-semibold text-white">
            {loading ? "Seasons" : season === null ? "Episodes" : seasonName(season)}
          </h2>
          {loading ? (
            <span className="text-xs text-white/45">Loading…</span>
          ) : (
            <span className="text-xs text-white/45">
              {episodes.length} episode{episodes.length === 1 ? "" : "s"} · tick episodes to play or download a set
            </span>
          )}
        </div>
        <ul className="divide-y divide-white/5 overflow-hidden rounded-xl border border-white/10 bg-ink-750/60">
          {episodes.map((ep) => {
            const isSel = selected.has(ep.key);
            const watched = ep.viewCount > 0 && (ep.progressPct >= 95 || ep.progressPct === 0);
            const line: string[] = [];
            if (ep.aired) line.push(dateLabel(ep.aired));
            if (formatDuration(ep.duration)) line.push(formatDuration(ep.duration));
            if (formatRating(ep.rating)) line.push(`★ ${formatRating(ep.rating)}`);
            return (
              <li
                key={ep.key}
                className={`group flex items-start gap-3 p-2 transition-colors ${isSel ? "bg-accent/10" : "hover:bg-white/[0.03]"}`}
              >
                <input
                  type="checkbox"
                  checked={isSel}
                  onChange={() => toggle(ep.key)}
                  aria-label={`Select episode ${ep.index}`}
                  className="mt-7 h-4 w-4 shrink-0 accent-accent"
                />
                <button
                  type="button"
                  onClick={() => playKeys([ep.key])}
                  className="flex min-w-0 flex-1 items-start gap-3 text-left focus:outline-none"
                  title={mpvAvailable ? "Play" : "mpv is not installed"}
                >
                  <span className="relative block aspect-video w-28 shrink-0 overflow-hidden rounded-md bg-ink-600 ring-1 ring-white/10">
                    {ep.thumbURL ? (
                      <img
                        src={ep.thumbURL}
                        alt=""
                        loading="lazy"
                        decoding="async"
                        draggable={false}
                        className={`h-full w-full object-cover ${watched ? "opacity-70" : ""}`}
                      />
                    ) : (
                      <span className="flex h-full w-full items-center justify-center text-white/20">
                        <TvIcon width={18} height={18} />
                      </span>
                    )}
                    {ep.progressPct > 0 && ep.progressPct < 95 && (
                      <span className="absolute inset-x-0 bottom-0 h-1 bg-white/25">
                        <span className="block h-full bg-accent" style={{ width: `${ep.progressPct}%` }} />
                      </span>
                    )}
                    <PlayIcon
                      width={22}
                      height={22}
                      className="absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 text-white/0 drop-shadow transition-colors group-hover:text-white"
                    />
                  </span>
                  <span className="min-w-0 flex-1 py-0.5">
                    <span className="flex items-baseline gap-2">
                      <span className="shrink-0 font-mono text-[11px] text-white/45">E{pad(ep.index)}</span>
                      <span className={`truncate text-sm font-medium ${watched ? "text-white/60" : "text-white/90"}`}>
                        {ep.title}
                      </span>
                      {watched ? (
                        <span className="ml-auto shrink-0 text-[11px] text-accent-soft">Watched</span>
                      ) : ep.progressPct > 0 ? (
                        <span className="ml-auto shrink-0 text-[11px] text-accent-soft">{ep.progressPct}%</span>
                      ) : null}
                    </span>
                    {line.length > 0 && (
                      <span className="mt-0.5 block truncate text-[11px] text-white/45">{line.join(" · ")}</span>
                    )}
                    {ep.summary && (
                      <span className="mt-1 line-clamp-2 block text-xs leading-relaxed text-white/60">{ep.summary}</span>
                    )}
                  </span>
                </button>
              </li>
            );
          })}
        </ul>
        {!loading && episodes.length === 0 && (
          <div className="py-8 text-center text-sm text-white/40">No episodes</div>
        )}
      </section>

      {conflicts && (
        <ConflictModal
          conflicts={conflicts.files}
          onChoose={(choice) => {
            const keys = conflicts.keys;
            setConflicts(null);
            if (choice === "cancel") return;
            void startDownload(keys, choice);
          }}
        />
      )}
    </DetailPanel>
  );
}

function pad(n: number): string {
  return String(n).padStart(2, "0");
}
