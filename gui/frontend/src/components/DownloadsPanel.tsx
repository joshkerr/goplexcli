import type { DownloadProgress } from "../lib/types";
import { formatBytes, formatSpeed } from "../lib/format";
import { CloseIcon, DownloadIcon, PauseIcon, PlayIcon, SendIcon } from "./icons";

interface Props {
  downloads: DownloadProgress[];
  onCancel: (id: string) => void;
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

// Active entries (including paused ones) show control buttons and are kept by
// Clear History.
function isActive(d: DownloadProgress) {
  return (
    d.status === "pending" ||
    d.status === "in_progress" ||
    d.status === "paused"
  );
}

export function DownloadsPanel({
  downloads,
  onCancel,
  onPause,
  onResume,
  onClearHistory,
  onSendToRclonecp,
}: Props) {
  if (downloads.length === 0) {
    return (
      <div className="flex h-full min-h-[40vh] flex-col items-center justify-center text-center text-white/40">
        <DownloadIcon width={40} height={40} />
        <div className="mt-3 text-base font-medium">No downloads yet</div>
        <div className="mt-1 text-sm">
          Pick a movie or episode and choose Download.
        </div>
      </div>
    );
  }

  const hasHistory = downloads.some((d) => !isActive(d));

  return (
    <div className="space-y-3 pb-8">
      {hasHistory && (
        <div className="flex justify-end">
          <button
            onClick={onClearHistory}
            className="rounded-lg border border-white/10 bg-ink-700 px-3 py-1.5 text-xs font-semibold text-white/70 hover:border-accent/60 hover:text-white"
          >
            Clear History
          </button>
        </div>
      )}
      {downloads.map((d) => (
        <div
          key={d.id}
          className="rounded-xl border border-white/5 bg-ink-700/60 p-4"
        >
          <div className="flex items-center justify-between gap-4">
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm font-medium text-white/90">
                {d.name}
              </div>
              <div className="mt-0.5 text-xs text-white/40">
                {STATUS_LABEL[d.status]}
                {d.total > 0 && (
                  <>
                    {" · "}
                    {formatBytes(d.bytes)} / {formatBytes(d.total)}
                  </>
                )}
                {d.status === "in_progress" && d.speed > 0 && (
                  <span className="text-white/60"> · {formatSpeed(d.speed)}</span>
                )}
                {d.status === "in_progress" && d.eta && (
                  <span className="text-white/60"> · ETA {d.eta}</span>
                )}
                {d.error && <span className="text-red-400"> · {d.error}</span>}
              </div>
            </div>
            <div className="shrink-0 text-sm font-semibold tabular-nums text-white/70">
              {Math.round(d.percent)}%
            </div>
            {d.status === "completed" && onSendToRclonecp && (
              <button
                onClick={() => onSendToRclonecp(d.id)}
                title="Send to rclonecp (add cover art & copy onward)"
                className="shrink-0 rounded-lg border border-white/10 bg-ink-700 p-1.5 text-white/50 hover:border-accent/60 hover:text-white"
              >
                <SendIcon width={14} height={14} />
              </button>
            )}
            {(d.status === "pending" || d.status === "in_progress") && (
              <button
                onClick={() => onPause(d.id)}
                title="Pause download"
                className="shrink-0 rounded-lg border border-white/10 bg-ink-700 p-1.5 text-white/50 hover:border-accent/60 hover:text-white"
              >
                <PauseIcon width={14} height={14} />
              </button>
            )}
            {d.status === "paused" && (
              <button
                onClick={() => onResume(d.id)}
                title="Resume download (restarts from the beginning)"
                className="shrink-0 rounded-lg border border-white/10 bg-ink-700 p-1.5 text-white/50 hover:border-accent/60 hover:text-white"
              >
                <PlayIcon width={14} height={14} />
              </button>
            )}
            {isActive(d) && (
              <button
                onClick={() => onCancel(d.id)}
                title="Cancel download"
                className="shrink-0 rounded-lg border border-white/10 bg-ink-700 p-1.5 text-white/50 hover:border-red-400/60 hover:text-red-400"
              >
                <CloseIcon width={14} height={14} />
              </button>
            )}
          </div>
          <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-ink-500">
            <div
              className={`h-full rounded-full transition-all duration-300 ${
                d.status === "failed"
                  ? "bg-red-500"
                  : d.status === "cancelled"
                  ? "bg-white/20"
                  : d.status === "paused"
                  ? "bg-amber-400/80"
                  : d.status === "completed"
                  ? "bg-emerald-500"
                  : "bg-accent"
              }`}
              style={{ width: `${Math.max(2, Math.min(100, d.percent))}%` }}
            />
          </div>
        </div>
      ))}
    </div>
  );
}
