// The sidebar card while downloads run, as in qbt-dl: the job in flight,
// its progress, speed and time left, and how many wait behind it. Clicking
// it opens Downloads.
import type { DownloadProgress } from "../lib/types";
import { formatBytes, formatSpeed } from "../lib/format";
import { DownloadIcon } from "./icons";

export function TransferCard({
  jobs,
  onOpen,
}: {
  // Active jobs, newest first.
  jobs: DownloadProgress[];
  onOpen: () => void;
}) {
  // The oldest in-flight job is the one rclone is on; a queue of only
  // pending (or paused) jobs is about to start or waiting on the user.
  const running = jobs.filter((j) => j.status === "in_progress");
  const job = running[running.length - 1] ?? jobs[jobs.length - 1];
  const sized = job.total > 0;
  const progress = Math.max(0, Math.min(100, job.percent));
  const speed = job.status === "in_progress" ? formatSpeed(job.speed) : "";
  const figure =
    job.status === "pending"
      ? "Queued"
      : job.status === "paused"
      ? "Paused"
      : sized
      ? `${Math.floor(progress)}%`
      : "…";
  const detail = [
    sized ? `${formatBytes(job.bytes)} of ${formatBytes(job.total)}` : "",
    job.status === "in_progress" && job.eta ? `${job.eta} left` : "",
  ]
    .filter(Boolean)
    .join(" · ");
  const waiting = jobs.length - running.length;

  return (
    <button
      onClick={onOpen}
      title="Open Downloads"
      style={{ ["--wails-draggable" as any]: "no-drag" }}
      className="mx-3 mb-1 shrink-0 animate-fade-in rounded-xl border border-white/[0.06] bg-gradient-to-b from-white/[0.05] to-white/[0.015] p-3 text-left shadow-card transition-colors hover:border-white/[0.12]"
    >
      <div className="flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-[0.14em] text-accent-soft">
        <DownloadIcon width={12} height={12} />
        {running.length > 0 ? "Downloading" : "Queued"}
        {jobs.length > 1 && (
          <span className="ml-auto normal-case tracking-normal text-white/35">
            {waiting} waiting
          </span>
        )}
      </div>
      <div className="mt-2 truncate text-[11px] text-white/65" title={job.name}>
        {job.name}
        {job.origin && <span className="text-white/35"> · {job.origin}</span>}
      </div>
      <div className="mt-0.5 flex items-baseline gap-1">
        <span className="text-[15px] font-semibold tabular-nums tracking-tight text-white/90">
          {figure}
        </span>
        {speed && (
          <span className="text-[10.5px] tabular-nums text-white/35">{speed}</span>
        )}
      </div>
      <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-white/[0.07]">
        <div
          className={`h-full rounded-full transition-[width] duration-700 ease-out ${
            job.status === "paused"
              ? "bg-amber-400/80"
              : "bg-gradient-to-r from-accent-soft via-accent to-accent-dark"
          } ${!sized && job.status === "in_progress" ? "w-1/3 animate-pulse" : ""}`}
          style={sized || job.status !== "in_progress" ? { width: `${Math.max(progress, 2)}%` } : undefined}
        />
      </div>
      {detail && (
        <div className="mt-1 truncate text-[10.5px] tabular-nums text-white/35">
          {detail}
        </div>
      )}
    </button>
  );
}
