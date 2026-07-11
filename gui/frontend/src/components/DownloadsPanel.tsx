import type { DownloadProgress } from "../lib/types";
import { formatBytes, formatSpeed } from "../lib/format";
import { DownloadIcon } from "./icons";

interface Props {
  downloads: DownloadProgress[];
}

const STATUS_LABEL: Record<DownloadProgress["status"], string> = {
  pending: "Queued",
  in_progress: "Downloading",
  completed: "Completed",
  failed: "Failed",
};

export function DownloadsPanel({ downloads }: Props) {
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

  return (
    <div className="space-y-3 pb-8">
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
                {d.error && <span className="text-red-400"> · {d.error}</span>}
              </div>
            </div>
            <div className="shrink-0 text-sm font-semibold tabular-nums text-white/70">
              {Math.round(d.percent)}%
            </div>
          </div>
          <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-ink-500">
            <div
              className={`h-full rounded-full transition-all duration-300 ${
                d.status === "failed"
                  ? "bg-red-500"
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
