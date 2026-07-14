import { useEffect, useState } from "react";
import { api, onEvent } from "../lib/api";
import type {
  AppConfig,
  ReindexProgress,
  Status,
  UpdateInfo,
} from "../lib/types";

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
    syncPeer: "",
  });
  const [saving, setSaving] = useState(false);
  const [indexing, setIndexing] = useState<
    null | "reindex" | "update" | "sync"
  >(null);
  const [progress, setProgress] = useState<ReindexProgress | null>(null);
  const [syncMsg, setSyncMsg] = useState("");

  // App version + self-update state.
  const [appVersion, setAppVersion] = useState("");
  const [updateInfo, setUpdateInfo] = useState<UpdateInfo | null>(null);
  const [checking, setChecking] = useState(false);
  const [updating, setUpdating] = useState(false);
  const [updateMsg, setUpdateMsg] = useState("");

  useEffect(() => {
    api.getConfig().then(setCfg).catch(() => {});
    api.appVersion().then(setAppVersion).catch(() => {});
    // Check for a newer GUI release in the background on open.
    api
      .checkUpdate()
      .then(setUpdateInfo)
      .catch(() => {});
  }, []);

  useEffect(() => {
    return onEvent<{ message: string }>("gui-update:progress", (d) =>
      setUpdateMsg(d.message)
    );
  }, []);

  useEffect(() => {
    const off = onEvent<ReindexProgress>("reindex:progress", setProgress);
    const offDone = onEvent<{
      mode?: "reindex" | "update";
      count: number;
      added?: number;
      error?: string;
    }>("reindex:done", (d) => {
      setIndexing(null);
      if (d.error) onToast(d.error, "error");
      else {
        if (d.mode === "update") {
          onToast(
            d.added
              ? `Added ${d.added} new item${d.added === 1 ? "" : "s"}`
              : "Library already up to date"
          );
        } else {
          onToast(`Indexed ${d.count} items`);
        }
        onReindexed();
      }
    });
    return () => {
      off();
      offDone();
    };
  }, [onReindexed, onToast]);

  useEffect(() => {
    const off = onEvent<{ message: string }>("sync:progress", (d) =>
      setSyncMsg(d.message)
    );
    const offDone = onEvent<{
      updated?: boolean;
      upToDate?: boolean;
      count?: number;
      source?: string;
      error?: string;
    }>("sync:done", (d) => {
      setIndexing(null);
      setSyncMsg("");
      if (d.error) onToast(d.error, "error");
      else if (d.upToDate) onToast("Already up to date — no newer cache found");
      else {
        onToast(
          `Synced ${d.count?.toLocaleString() ?? ""} items${
            d.source ? ` from ${d.source}` : ""
          }`
        );
        onReindexed();
      }
    });
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
    setIndexing("reindex");
    setProgress(null);
    try {
      await api.reindex();
    } catch (e: any) {
      setIndexing(null);
      onToast(String(e?.message ?? e), "error");
    }
  };

  const update = async () => {
    setIndexing("update");
    setProgress(null);
    try {
      await api.update();
    } catch (e: any) {
      setIndexing(null);
      onToast(String(e?.message ?? e), "error");
    }
  };

  const sync = async () => {
    setIndexing("sync");
    setProgress(null);
    setSyncMsg("Looking for other computers…");
    try {
      await api.syncFromLAN();
    } catch (e: any) {
      setIndexing(null);
      setSyncMsg("");
      onToast(String(e?.message ?? e), "error");
    }
  };

  const checkForUpdate = async () => {
    setChecking(true);
    try {
      const info = await api.checkUpdate();
      setUpdateInfo(info);
      if (info.error) onToast(info.error, "error");
      else if (!info.available) onToast(`You're up to date (v${info.current})`);
    } catch (e: any) {
      onToast(String(e?.message ?? e), "error");
    } finally {
      setChecking(false);
    }
  };

  const installUpdate = async () => {
    setUpdating(true);
    setUpdateMsg("Starting update…");
    try {
      // On success the backend relaunches the app, so this call may not return.
      await api.applyUpdate();
    } catch (e: any) {
      setUpdating(false);
      setUpdateMsg("");
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
            {indexing === "sync"
              ? syncMsg || "Syncing…"
              : progress
              ? `${progress.server} · ${progress.library} · ${progress.items.toLocaleString()} items`
              : "Connecting…"}
          </div>
        )}

        <div className="flex flex-wrap gap-3">
          <button
            onClick={update}
            disabled={!!indexing}
            className="rounded-lg bg-accent px-4 py-2 text-sm font-semibold text-ink-900 transition-colors hover:bg-accent-soft disabled:opacity-50"
          >
            {indexing === "update" ? "Updating…" : "Update library"}
          </button>
          <button
            onClick={sync}
            disabled={!!indexing}
            className="rounded-lg bg-white/10 px-4 py-2 text-sm font-semibold text-white transition-colors hover:bg-white/20 disabled:opacity-50"
          >
            {indexing === "sync" ? "Syncing…" : "Sync from LAN"}
          </button>
          <button
            onClick={reindex}
            disabled={!!indexing}
            className="rounded-lg bg-white/10 px-4 py-2 text-sm font-semibold text-white transition-colors hover:bg-white/20 disabled:opacity-50"
          >
            {indexing === "reindex" ? "Reindexing…" : "Reindex library"}
          </button>
        </div>
        <p className="text-xs text-white/30">
          Update fetches only newly added titles. Sync from LAN pulls the cache
          from the computer set in Preferences below (or auto-discovers one if
          blank). Reindex rebuilds the whole library from scratch.
        </p>
      </section>

      <section className="space-y-4 rounded-2xl border border-white/5 bg-ink-700/50 p-6">
        <h2 className="text-base font-semibold text-white">Preferences</h2>
        {field(
          "Download directory",
          "downloadDir",
          "~/Downloads/Plex",
          "Where rclone saves downloaded media. ~ is expanded to your home directory. Defaults to ~/Downloads when empty."
        )}
        {field("mpv path", "mpvPath", "mpv", "Override if mpv is not on your PATH.")}
        {field(
          "rclone path",
          "rclonePath",
          "rclone",
          "Override if rclone is not on your PATH."
        )}
        {field(
          "Sync from computer (LAN)",
          "syncPeer",
          "e.g. ghost-2.local",
          "Hostname or IP of another computer running GoplexCLI to pull the cache from with “Sync from LAN”. Leave blank to auto-discover."
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

      <section className="space-y-4 rounded-2xl border border-white/5 bg-ink-700/50 p-6">
        <h2 className="text-base font-semibold text-white">About</h2>
        <p className="text-sm text-white/50">
          GoplexCLI{" "}
          <span className="font-medium text-white/80">
            {appVersion ? `v${appVersion}` : "…"}
          </span>
        </p>

        {appVersion === "dev" ? (
          <p className="text-xs text-white/30">
            Development build — in-app updates are disabled.
          </p>
        ) : (
          <>
            {updateInfo?.available && (
              <div className="rounded-lg bg-ink-800 p-3 text-sm text-white/70">
                <div>
                  Update available:{" "}
                  <span className="font-semibold text-accent">
                    v{updateInfo.latest}
                  </span>
                </div>
                {updating && updateMsg && (
                  <div className="mt-1 text-xs text-white/50">
                    <span className="mr-2 inline-block h-2 w-2 animate-pulse rounded-full bg-accent align-middle" />
                    {updateMsg}
                  </div>
                )}
              </div>
            )}
            <div className="flex flex-wrap items-center gap-3">
              {updateInfo?.available && (
                <button
                  onClick={installUpdate}
                  disabled={updating}
                  className="rounded-lg bg-accent px-4 py-2 text-sm font-semibold text-ink-900 transition-colors hover:bg-accent-soft disabled:opacity-50"
                >
                  {updating
                    ? "Updating…"
                    : `Download & install v${updateInfo.latest}`}
                </button>
              )}
              <button
                onClick={checkForUpdate}
                disabled={checking || updating}
                className="rounded-lg bg-white/10 px-4 py-2 text-sm font-semibold text-white transition-colors hover:bg-white/20 disabled:opacity-50"
              >
                {checking ? "Checking…" : "Check for updates"}
              </button>
            </div>
          </>
        )}
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
