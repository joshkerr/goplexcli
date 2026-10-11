import { useState } from "react";
import { api } from "../lib/api";
import type { Library } from "../lib/types";

interface Props {
  libraries: Library[];
  // Called after any change so the owner can refetch GetLibraryInfo.
  onChanged: () => void;
  onToast: (msg: string, kind?: "info" | "error") => void;
  // Compact rows (no enable toggle, no remote) for the Setup wizard.
  compact?: boolean;
}

/** The rows under Settings → Local libraries (and the Setup wizard): one per
 * indexed folder with its type, path, rclone remote, item count and controls.
 * Remove confirms inline (click → "Remove?" → click) because a window.confirm
 * dialog blocks the webview under automation. */
export function LibraryList({ libraries, onChanged, onToast, compact }: Props) {
  // Name of the row whose Remove button is awaiting its second click.
  const [confirming, setConfirming] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  const run = async (name: string, fn: () => Promise<void>) => {
    setBusy(name);
    try {
      await fn();
      onChanged();
    } catch (e: any) {
      onToast(String(e?.message ?? e), "error");
    } finally {
      setBusy(null);
    }
  };

  const remove = (lib: Library) => {
    if (confirming !== lib.name) {
      setConfirming(lib.name);
      return;
    }
    setConfirming(null);
    run(lib.name, () => api.removeLibrary(lib.name));
  };

  if (libraries.length === 0) {
    return (
      <p className="text-xs text-white/30">
        No local folders yet. Add one below to index it without Plex.
      </p>
    );
  }

  return (
    <div className="space-y-2">
      {libraries.map((lib) => (
        <div
          key={lib.name}
          className={`flex items-center gap-3 rounded-xl border border-white/10 bg-ink-800 px-4 py-3 ${
            lib.enabled ? "" : "opacity-60"
          }`}
        >
          {!compact && (
            <input
              type="checkbox"
              checked={lib.enabled}
              disabled={busy === lib.name}
              onChange={(e) =>
                run(lib.name, () =>
                  api.setLibraryEnabled(lib.name, e.target.checked)
                )
              }
              title={lib.enabled ? "Enabled" : "Disabled"}
              className="h-4 w-4 shrink-0 accent-accent"
            />
          )}
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              <span className="truncate text-sm font-medium text-white/90">
                {lib.name}
              </span>
              <TypeBadge type={lib.type} />
            </div>
            <div className="truncate text-xs text-white/40" title={lib.path}>
              {lib.path}
            </div>
            {!compact && lib.remote && (
              <div
                className="truncate text-xs text-white/30"
                title={lib.remote}
              >
                rclone: <code className="text-white/50">{lib.remote}</code>
              </div>
            )}
          </div>
          <span className="shrink-0 text-xs tabular-nums text-white/40">
            {lib.items.toLocaleString()} {lib.items === 1 ? "item" : "items"}
          </span>
          <button
            onClick={() => remove(lib)}
            onBlur={() => setConfirming((c) => (c === lib.name ? null : c))}
            disabled={busy === lib.name}
            className={`shrink-0 rounded-lg px-3 py-1.5 text-xs font-semibold transition-colors disabled:opacity-50 ${
              confirming === lib.name
                ? "bg-red-500/30 text-red-200 hover:bg-red-500/40"
                : "bg-white/10 text-red-300 hover:bg-red-500/20"
            }`}
          >
            {confirming === lib.name ? "Remove?" : "Remove"}
          </button>
        </div>
      ))}
    </div>
  );
}

export function TypeBadge({ type }: { type: Library["type"] }) {
  return (
    <span
      className={`shrink-0 rounded px-1.5 py-0.5 text-[10px] font-semibold ${
        type === "show"
          ? "bg-sky-500/15 text-sky-300"
          : "bg-emerald-500/15 text-emerald-300"
      }`}
    >
      {type === "show" ? "TV Shows" : "Movies"}
    </span>
  );
}
