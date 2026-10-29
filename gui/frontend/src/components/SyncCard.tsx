// The sidebar card for a library sync, update or reindex, beside the
// download card: the stage or the server and library being read, how far it
// has got, then the result. A failure keeps its error on the card until it's
// dismissed. Clicking it opens Settings, where the library controls live.
import type { ReindexProgress } from "../lib/types";
import { CloseIcon, SyncIcon } from "./icons";

export type LibraryOp = "update" | "sync" | "reindex";

export interface SyncCardState {
  op: LibraryOp;
  phase: "running" | "done" | "failed";
  // The stage while running (when there's no progress to show), the result
  // when done, the error when failed.
  message: string;
  progress?: ReindexProgress | null;
}

const TITLES: Record<LibraryOp, Record<SyncCardState["phase"], string>> = {
  sync: { running: "Syncing library", done: "Library synced", failed: "Sync failed" },
  update: { running: "Updating library", done: "Library updated", failed: "Update failed" },
  reindex: { running: "Reindexing", done: "Reindex complete", failed: "Reindex failed" },
};

export function SyncCard({
  card,
  onOpen,
  onDismiss,
}: {
  card: SyncCardState;
  onOpen: () => void;
  onDismiss: () => void;
}) {
  const { phase, progress: p } = card;
  const running = phase === "running";
  const failed = phase === "failed";
  const sized = running && !!p && p.total > 0;
  const percent = sized ? Math.max(0, Math.min(100, (p!.items / p!.total) * 100)) : 0;
  const line = running && p ? `${p.server} · ${p.library}` : card.message;
  const detail =
    running && p
      ? [
          `${p.items.toLocaleString()}${sized ? ` of ${p.total.toLocaleString()}` : ""} items`,
          p.libraries > 1 ? `library ${p.libNum}/${p.libraries}` : "",
          p.servers > 1 ? `server ${p.serverNum}/${p.servers}` : "",
        ]
          .filter(Boolean)
          .join(" · ")
      : "";

  return (
    <div
      role="status"
      style={{ ["--wails-draggable" as any]: "no-drag" }}
      className={`relative shrink-0 animate-fade-in rounded-xl border bg-gradient-to-b p-3 shadow-card transition-colors ${
        failed
          ? "border-red-500/25 from-red-500/[0.08] to-red-500/[0.02]"
          : "border-white/[0.06] from-white/[0.05] to-white/[0.015] hover:border-white/[0.12]"
      }`}
    >
      <button onClick={onOpen} title="Open Settings" className="block w-full text-left">
        <div
          className={`flex items-center gap-1.5 pr-5 text-[10px] font-semibold uppercase tracking-[0.14em] ${
            failed ? "text-red-300" : "text-accent-soft"
          }`}
        >
          <SyncIcon width={12} height={12} className={running ? "animate-spin" : ""} />
          {TITLES[card.op][phase]}
        </div>
        <div
          className={`mt-2 text-[11px] ${
            failed ? "line-clamp-6 break-words text-red-200/90" : "truncate text-white/65"
          }`}
          title={line}
        >
          {line}
        </div>
        {sized && (
          <div className="mt-0.5 text-[15px] font-semibold tabular-nums tracking-tight text-white/90">
            {Math.floor(percent)}%
          </div>
        )}
        {running && (
          <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-white/[0.07]">
            <div
              className={`h-full rounded-full bg-gradient-to-r from-accent-soft via-accent to-accent-dark transition-[width] duration-700 ease-out ${
                sized ? "" : "w-1/3 animate-pulse"
              }`}
              style={sized ? { width: `${Math.max(percent, 2)}%` } : undefined}
            />
          </div>
        )}
        {detail && (
          <div className="mt-1 truncate text-[10.5px] tabular-nums text-white/35">{detail}</div>
        )}
      </button>
      {!running && (
        <button
          onClick={onDismiss}
          title="Dismiss"
          aria-label="Dismiss"
          className="touch-target absolute right-2 top-2 rounded-md p-1 text-white/35 hover:bg-white/10 hover:text-white"
        >
          <CloseIcon width={12} height={12} />
        </button>
      )}
    </div>
  );
}
