// The Downloads view: one card per job, local and remote together, in the
// same shape as qbt-dl's download card (status badge and kind pill beside
// the name, bytes and speed on the right, a gradient progress track).
import type { DownloadProgress } from "../lib/types";
import { formatBytes, formatSpeed } from "../lib/format";
import {
  CloseIcon,
  DownloadIcon,
  FilmIcon,
  PauseIcon,
  PlayIcon,
  SendIcon,
  TvIcon,
} from "./icons";

interface Props {
  downloads: DownloadProgress[];
  onCancel: (id: string) => void;
  onCancelAll: (ids: string[]) => void;
  onPause: (id: string) => void;
  onResume: (id: string) => void;
  onClearHistory: () => void;
  // Hand a completed download to rclonecp for cover art + onward copy.
  // Undefined hides the button (rclonecp not installed).
  onSendToRclonecp?: (id: string) => void;
}

const STATUS_LABEL: Record<DownloadProgress["status"], string> = {
  pending: "Queued",
  in_progress: "Downloading",
  paused: "Paused",
  completed: "Completed",
  failed: "Failed",
  cancelled: "Cancelled",
};

const STATUS_STYLE: Record<DownloadProgress["status"], string> = {
  pending: "bg-white/10 text-white/60",
  in_progress: "bg-accent/15 text-accent-soft",
  paused: "bg-amber-500/15 text-amber-300",
  completed: "bg-emerald-500/15 text-emerald-300",
  failed: "bg-red-500/15 text-red-300",
  cancelled: "bg-white/10 text-white/45",
};

// Active entries (including paused ones) show control buttons and are kept by
// Clear finished.
export function isActive(d: DownloadProgress) {
  return (
    d.status === "pending" ||
    d.status === "in_progress" ||
    d.status === "paused"
  );
}

// Older history entries (and jobs on older serve daemons) carry no media type;
// fall back to spotting an SxxExx marker in the file name.
function isEpisode(d: DownloadProgress) {
  if (d.mediaType) return d.mediaType === "episode";
  return /\bS\d{1,3}\s?E\d{1,4}\b/i.test(d.name);
}

// The subline under the name: the library title the job came from, else
// where the file is going.
function detail(d: DownloadProgress) {
  if (d.title) return d.year ? `${d.title} (${d.year})` : d.title;
  return d.dest ?? "";
}

export function DownloadsPanel({
  downloads,
  onCancel,
  onCancelAll,
  onPause,
  onResume,
  onClearHistory,
  onSendToRclonecp,
}: Props) {
  const active = downloads.filter(isActive);
  const hasFinished = downloads.length > active.length;

  return (
    <div className="flex flex-col gap-4 pb-8">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-semibold uppercase tracking-wider text-white/40">
          Downloads
        </h2>
        <div className="flex items-center gap-2">
          {active.length > 0 && (
            <button
              onClick={() => onCancelAll(active.map((d) => d.id))}
              className="flex items-center gap-1.5 rounded-lg border border-red-400/30 bg-red-500/10 px-3 py-1.5 text-xs font-semibold text-red-300 hover:border-red-400/60 hover:bg-red-500/20"
            >
              <CloseIcon width={14} height={14} />
              Cancel all
            </button>
          )}
          {hasFinished && (
            <button
              onClick={onClearHistory}
              className="rounded-lg border border-white/10 bg-ink-700 px-3 py-1.5 text-xs font-semibold text-white/70 hover:border-accent/60 hover:text-white"
            >
              Clear finished
            </button>
          )}
        </div>
      </div>

      {downloads.length === 0 ? (
        <div className="flex flex-col items-center justify-center gap-2 py-16 text-center">
          <div className="text-white/20">
            <DownloadIcon width={36} height={36} />
          </div>
          <div className="text-sm text-white/50">No downloads yet</div>
          <div className="max-w-sm text-xs text-white/30">
            Pick a movie or episode and choose Download — progress shows up
            here.
          </div>
        </div>
      ) : (
        <div className="flex flex-col gap-2">
          {downloads.map((d) => (
            <JobCard
              key={d.id}
              job={d}
              onCancel={onCancel}
              onPause={onPause}
              onResume={onResume}
              onSendToRclonecp={onSendToRclonecp}
            />
          ))}
        </div>
      )}
    </div>
  );
}

const iconButton =
  "shrink-0 rounded-lg border border-white/10 bg-ink-700 p-1.5 text-white/50 hover:border-accent/60 hover:text-white";

function JobCard({
  job: d,
  onCancel,
  onPause,
  onResume,
  onSendToRclonecp,
}: {
  job: DownloadProgress;
  onCancel: (id: string) => void;
  onPause: (id: string) => void;
  onResume: (id: string) => void;
  onSendToRclonecp?: (id: string) => void;
}) {
  const active = isActive(d);
  const episode = isEpisode(d);
  const showBytes = d.total > 0 && d.status === "in_progress";
  const sub = d.status === "failed" && d.error ? "" : detail(d);
  return (
    <div className="animate-fade-in rounded-2xl border border-white/5 bg-ink-700/50 p-4">
      <div className="flex items-center gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-2.5">
            <span
              className="shrink-0 text-white/45"
              title={episode ? "TV episode" : "Movie"}
            >
              {episode ? (
                <TvIcon width={15} height={15} />
              ) : (
                <FilmIcon width={15} height={15} />
              )}
            </span>
            <span className="truncate text-[13px] font-medium text-white/90">
              {d.name}
            </span>
            <span
              className={`shrink-0 rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide ${STATUS_STYLE[d.status]}`}
            >
              {STATUS_LABEL[d.status]}
            </span>
            <span className="shrink-0 rounded-full bg-white/5 px-2 py-0.5 text-[10px] uppercase tracking-wide text-white/40">
              {episode ? "episode" : "movie"}
            </span>
            {d.origin && (
              <span
                className="shrink-0 rounded-full bg-sky-500/15 px-2 py-0.5 text-[10px] font-semibold text-sky-300"
                title={`Downloading on ${d.origin}`}
              >
                ⇄ {d.origin}
              </span>
            )}
          </div>
          <div className="mt-1 truncate text-xs text-white/40">
            {d.status === "failed" && d.error ? (
              <span className="text-red-300">{d.error}</span>
            ) : (
              sub || STATUS_LABEL[d.status]
            )}
          </div>
        </div>
        <div className="shrink-0 text-right text-xs text-white/45">
          {showBytes && (
            <div>
              {formatBytes(d.bytes)} / {formatBytes(d.total)}
            </div>
          )}
          {d.status === "in_progress" && d.speed > 0 && (
            <div className="text-accent-soft">
              {formatSpeed(d.speed)}
              {d.eta ? ` · ETA ${d.eta}` : ""}
            </div>
          )}
        </div>
        {/* Remote jobs: the files are on another machine, so rclonecp
            handoff doesn't apply, and the daemon supports cancel but not
            pause/resume. */}
        {d.status === "completed" && !d.origin && onSendToRclonecp && (
          <button
            onClick={() => onSendToRclonecp(d.id)}
            title="Send to rclonecp (add cover art & copy onward)"
            className={iconButton}
          >
            <SendIcon width={14} height={14} />
          </button>
        )}
        {(d.status === "pending" || d.status === "in_progress") && !d.origin && (
          <button
            onClick={() => onPause(d.id)}
            title="Pause download"
            className={iconButton}
          >
            <PauseIcon width={14} height={14} />
          </button>
        )}
        {d.status === "paused" && !d.origin && (
          <button
            onClick={() => onResume(d.id)}
            title="Resume download (restarts from the beginning)"
            className={iconButton}
          >
            <PlayIcon width={14} height={14} />
          </button>
        )}
        {active && (
          <button
            onClick={() => onCancel(d.id)}
            title="Cancel download"
            className="shrink-0 rounded-lg border border-red-400/30 bg-red-500/10 p-1.5 text-red-300 hover:border-red-400/60 hover:bg-red-500/20"
          >
            <CloseIcon width={14} height={14} />
          </button>
        )}
      </div>
      {(active || d.status === "completed") && (
        <ProgressTrack
          className="mt-3"
          percent={d.status === "completed" ? 100 : d.percent}
          paused={d.status === "paused"}
          indeterminate={d.status === "in_progress" && d.total <= 0}
        />
      )}
    </div>
  );
}

function ProgressTrack({
  percent,
  paused,
  indeterminate,
  className = "",
}: {
  percent: number;
  paused: boolean;
  indeterminate: boolean;
  className?: string;
}) {
  const width = indeterminate ? 100 : Math.max(0, Math.min(100, percent));
  return (
    <div
      className={`h-1.5 w-full overflow-hidden rounded-full bg-white/10 ${className}`}
    >
      <div
        className={`h-full rounded-full transition-all duration-300 ${
          paused
            ? "bg-amber-400/80"
            : "bg-gradient-to-r from-accent-soft via-accent to-accent-dark"
        } ${indeterminate ? "animate-pulse" : ""}`}
        style={{ width: `${width}%` }}
      />
    </div>
  );
}
