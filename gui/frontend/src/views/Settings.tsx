import { useEffect, useState } from "react";
import { api, onEvent } from "../lib/api";
import type { AppConfig, ReindexProgress, Status } from "../lib/types";

interface Props {
  status: Status;
  onReindexed: () => void;
  onToast: (msg: string, kind?: "info" | "error") => void;
}

export function Settings({ status, onReindexed, onToast }: Props) {
  const [cfg, setCfg] = useState<AppConfig>({
    downloadDir: "",
    mpvPath: "",
    rclonePath: "",
  });
  const [saving, setSaving] = useState(false);
  const [indexing, setIndexing] = useState(false);
  const [progress, setProgress] = useState<ReindexProgress | null>(null);

  useEffect(() => {
    api.getConfig().then(setCfg).catch(() => {});
  }, []);

  useEffect(() => {
    const off = onEvent<ReindexProgress>("reindex:progress", setProgress);
    const offDone = onEvent<{ count: number; error?: string }>(
      "reindex:done",
      (d) => {
        setIndexing(false);
        if (d.error) onToast(d.error, "error");
        else {
          onToast(`Indexed ${d.count} items`);
          onReindexed();
        }
      }
    );
    return () => {
      off();
      offDone();
    };
  }, [onReindexed, onToast]);

  const save = async () => {
    setSaving(true);
    try {
      await api.saveConfig(cfg);
      onToast("Settings saved");
    } catch (e: any) {
      onToast(String(e?.message ?? e), "error");
    } finally {
      setSaving(false);
    }
  };

  const reindex = async () => {
    setIndexing(true);
    setProgress(null);
    try {
      await api.reindex();
    } catch (e: any) {
      setIndexing(false);
      onToast(String(e?.message ?? e), "error");
    }
  };

  const field = (
    label: string,
    key: keyof AppConfig,
    placeholder: string,
    hint?: string
  ) => (
    <div>
      <label className="mb-1.5 block text-xs font-medium text-white/50">
        {label}
      </label>
      <input
        value={cfg[key]}
        onChange={(e) => setCfg({ ...cfg, [key]: e.target.value })}
        placeholder={placeholder}
        className="w-full rounded-lg border border-white/10 bg-ink-800 px-3 py-2.5 text-sm text-white placeholder-white/30 outline-none focus:border-accent/60"
      />
      {hint && <p className="mt-1 text-xs text-white/30">{hint}</p>}
    </div>
  );

  return (
    <div className="mx-auto max-w-2xl space-y-8 pb-10">
      <section className="space-y-4 rounded-2xl border border-white/5 bg-ink-700/50 p-6">
        <h2 className="text-base font-semibold text-white">Library</h2>
        <div className="grid grid-cols-2 gap-4 text-sm">
          <Stat label="Movies" value={status.movieCount} />
          <Stat label="TV Shows" value={status.showCount} />
          <Stat label="Episodes" value={status.episodeCount} />
          <Stat label="Total items" value={status.cacheCount} />
        </div>
        {status.lastUpdated && (
          <p className="text-xs text-white/40">
            Last indexed {status.lastUpdated}
          </p>
        )}

        {indexing && (
          <div className="rounded-lg bg-ink-800 p-3 text-xs text-white/60">
            <span className="mr-2 inline-block h-2 w-2 animate-pulse rounded-full bg-accent align-middle" />
            {progress
              ? `${progress.server} · ${progress.library} · ${progress.items.toLocaleString()} items`
              : "Connecting…"}
          </div>
        )}

        <button
          onClick={reindex}
          disabled={indexing}
          className="rounded-lg bg-white/10 px-4 py-2 text-sm font-semibold text-white transition-colors hover:bg-white/20 disabled:opacity-50"
        >
          {indexing ? "Reindexing…" : "Reindex library"}
        </button>
      </section>

      <section className="space-y-4 rounded-2xl border border-white/5 bg-ink-700/50 p-6">
        <h2 className="text-base font-semibold text-white">Preferences</h2>
        {field(
          "Download directory",
          "downloadDir",
          "~/Downloads/Plex",
          "Where rclone saves downloaded media. ~ is expanded to your home directory."
        )}
        {field("mpv path", "mpvPath", "mpv", "Override if mpv is not on your PATH.")}
        {field(
          "rclone path",
          "rclonePath",
          "rclone",
          "Override if rclone is not on your PATH."
        )}
        <div className="flex items-center gap-3 pt-1 text-xs">
          <Availability label="mpv" ok={status.mpvAvailable} />
          <Availability label="rclone" ok={status.rcloneAvailable} />
        </div>
        <button
          onClick={save}
          disabled={saving}
          className="rounded-lg bg-accent px-4 py-2 text-sm font-semibold text-ink-900 transition-colors hover:bg-accent-soft disabled:opacity-50"
        >
          {saving ? "Saving…" : "Save settings"}
        </button>
      </section>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-lg bg-ink-800 px-4 py-3">
      <div className="text-2xl font-semibold tabular-nums text-white">
        {value.toLocaleString()}
      </div>
      <div className="text-xs text-white/40">{label}</div>
    </div>
  );
}

function Availability({ label, ok }: { label: string; ok: boolean }) {
  return (
    <span
      className={`flex items-center gap-1.5 rounded-full px-2.5 py-1 font-medium ${
        ok ? "bg-emerald-500/15 text-emerald-300" : "bg-red-500/15 text-red-300"
      }`}
    >
      <span
        className={`h-1.5 w-1.5 rounded-full ${
          ok ? "bg-emerald-400" : "bg-red-400"
        }`}
      />
      {label} {ok ? "found" : "missing"}
    </span>
  );
}
