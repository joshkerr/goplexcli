import { useEffect, useState } from "react";
import { api, onEvent } from "../lib/api";
import type {
  AppConfig,
  ReindexProgress,
  RemoteServer,
  Status,
  UpdateInfo,
} from "../lib/types";

interface Props {
  status: Status;
  // Library index/sync state lives in App (shared with the header sync button,
  // which must reflect ops started here and vice versa); the event listeners
  // driving it are up there too, so this panel only renders and triggers.
  indexing: null | "reindex" | "update" | "sync";
  progress: ReindexProgress | null;
  syncMsg: string;
  onUpdate: () => void;
  onSync: () => void;
  onReindex: () => void;
  onToast: (msg: string, kind?: "info" | "error") => void;
  // Called after the remote-server list is saved, so App can refresh the
  // download-target picker and the remote-jobs poll.
  onRemoteServersChanged: () => void;
}

export function Settings({
  status,
  indexing,
  progress,
  syncMsg,
  onUpdate,
  onSync,
  onReindex,
  onToast,
  onRemoteServersChanged,
}: Props) {
  const [cfg, setCfg] = useState<AppConfig>({
    downloadDir: "",
    mpvPath: "",
    rclonePath: "",
    rclonecpPath: "",
    autoSendRclonecp: false,
    slowDeviceMode: false,
    sortDownloads: false,
    syncPeer: "",
  });
  const [saving, setSaving] = useState(false);

  // Remote download servers (`goplexcli serve` daemons on other machines).
  // Edited locally and persisted by the main Save button.
  const [servers, setServers] = useState<RemoteServer[]>([]);
  // Per-row probe outcome, keyed by row index.
  const [testResults, setTestResults] = useState<Record<number, string>>({});

  // App version + self-update state.
  const [appVersion, setAppVersion] = useState("");
  const [updateInfo, setUpdateInfo] = useState<UpdateInfo | null>(null);
  const [checking, setChecking] = useState(false);
  const [updating, setUpdating] = useState(false);
  const [updateMsg, setUpdateMsg] = useState("");

  useEffect(() => {
    api.getConfig().then(setCfg).catch(() => {});
    api.listRemoteServers().then(setServers).catch(() => {});
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

  const save = async () => {
    setSaving(true);
    try {
      await api.saveConfig(cfg);
      // Drop rows the user never filled in rather than failing validation.
      const kept = servers.filter((s) => s.url.trim() !== "");
      await api.saveRemoteServers(kept);
      setServers(await api.listRemoteServers()); // pick up cleaned names
      onRemoteServersChanged();
      onToast("Settings saved");
    } catch (e: any) {
      onToast(String(e?.message ?? e), "error");
    } finally {
      setSaving(false);
    }
  };

  const updateServer = (i: number, patch: Partial<RemoteServer>) => {
    setServers((prev) => prev.map((s, j) => (j === i ? { ...s, ...patch } : s)));
  };

  const testServer = async (i: number) => {
    const s = servers[i];
    setTestResults((prev) => ({ ...prev, [i]: "Testing…" }));
    try {
      const r = await api.testRemoteServer(s.url.trim(), s.token.trim());
      let msg: string;
      if (!r.online) msg = `Offline — ${r.error}`;
      else if (r.error) msg = `Reachable, but the token was rejected — ${r.error}`;
      else msg = `Online — ${r.name} (goplexcli v${r.version}, ${r.platform})`;
      setTestResults((prev) => ({ ...prev, [i]: msg }));
    } catch (e: any) {
      setTestResults((prev) => ({ ...prev, [i]: String(e?.message ?? e) }));
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
    key: Exclude<
      keyof AppConfig,
      "autoSendRclonecp" | "slowDeviceMode" | "sortDownloads"
    >,
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
            onClick={onUpdate}
            disabled={!!indexing}
            className="rounded-lg bg-accent px-4 py-2 text-sm font-semibold text-ink-900 transition-colors hover:bg-accent-soft disabled:opacity-50"
          >
            {indexing === "update" ? "Updating…" : "Update library"}
          </button>
          <button
            onClick={onSync}
            disabled={!!indexing}
            className="rounded-lg bg-white/10 px-4 py-2 text-sm font-semibold text-white transition-colors hover:bg-white/20 disabled:opacity-50"
          >
            {indexing === "sync" ? "Syncing…" : "Sync from LAN"}
          </button>
          <button
            onClick={onReindex}
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
        <label className="flex cursor-pointer items-center gap-2.5 text-sm text-white/70">
          <input
            type="checkbox"
            checked={cfg.sortDownloads}
            onChange={(e) =>
              setCfg({ ...cfg, sortDownloads: e.target.checked })
            }
            className="h-4 w-4 accent-accent"
          />
          Sort downloads into Movies and TV Shows subfolders — episodes are
          filed under TV Shows/&lt;show&gt;
        </label>
        {field("mpv path", "mpvPath", "mpv", "Override if mpv is not on your PATH.")}
        {field(
          "rclone path",
          "rclonePath",
          "rclone",
          "Override if rclone is not on your PATH."
        )}
        {field(
          "rclonecp path",
          "rclonecpPath",
          "rclonecp-gui",
          "Override if the rclonecp GUI is not on your PATH. Completed downloads can be sent to rclonecp to embed cover art and copy onward."
        )}
        <label className="flex cursor-pointer items-center gap-2.5 text-sm text-white/70">
          <input
            type="checkbox"
            checked={cfg.autoSendRclonecp}
            onChange={(e) =>
              setCfg({ ...cfg, autoSendRclonecp: e.target.checked })
            }
            className="h-4 w-4 accent-accent"
          />
          Automatically send completed downloads to rclonecp
        </label>
        <label className="flex cursor-pointer items-center gap-2.5 text-sm text-white/70">
          <input
            type="checkbox"
            checked={cfg.slowDeviceMode}
            onChange={(e) =>
              setCfg({ ...cfg, slowDeviceMode: e.target.checked })
            }
            className="h-4 w-4 accent-accent"
          />
          Slow-device write buffer — smooths downloads onto SD cards and USB
          drives (uses up to 2 GB RAM during transfers)
        </label>
        {field(
          "Sync from computer (LAN)",
          "syncPeer",
          "e.g. ghost-2.local",
          "Hostname or IP of another computer running GoplexCLI to pull the cache from with “Sync from LAN”. Leave blank to auto-discover."
        )}
        <div className="flex items-center gap-3 pt-1 text-xs">
          <Availability label="mpv" ok={status.mpvAvailable} />
          <Availability label="rclone" ok={status.rcloneAvailable} />
          <Availability label="rclonecp" ok={status.rclonecpAvailable} />
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
        <h2 className="text-base font-semibold text-white">
          Remote download servers
        </h2>
        <p className="text-xs text-white/30">
          Send downloads to another computer running{" "}
          <code className="text-white/50">goplexcli serve</code> — they run
          there with that machine's rclone and download folder, and keep going
          after this app closes. The serve command prints the URL and token to
          enter here. Remote downloads appear in the Downloads panel with a ⇄
          badge.
        </p>
        {servers.map((s, i) => (
          <div
            key={i}
            className="space-y-2 rounded-xl border border-white/10 bg-ink-800 p-4"
          >
            <div className="grid grid-cols-2 gap-2">
              <input
                value={s.name}
                onChange={(e) => updateServer(i, { name: e.target.value })}
                placeholder="Name (e.g. media-server)"
                className="rounded-lg border border-white/10 bg-ink-900 px-3 py-2 text-sm text-white placeholder-white/30 outline-none focus:border-accent/60"
              />
              <input
                value={s.url}
                onChange={(e) => updateServer(i, { url: e.target.value })}
                placeholder="http://192.168.1.50:47821"
                className="rounded-lg border border-white/10 bg-ink-900 px-3 py-2 text-sm text-white placeholder-white/30 outline-none focus:border-accent/60"
              />
            </div>
            <input
              value={s.token}
              onChange={(e) => updateServer(i, { token: e.target.value })}
              placeholder="Access token (printed by goplexcli serve)"
              className="w-full rounded-lg border border-white/10 bg-ink-900 px-3 py-2 text-sm text-white placeholder-white/30 outline-none focus:border-accent/60"
            />
            <div className="flex items-center gap-3">
              <label className="flex cursor-pointer items-center gap-2 text-sm text-white/70">
                <input
                  type="checkbox"
                  checked={s.enabled}
                  onChange={(e) => updateServer(i, { enabled: e.target.checked })}
                  className="h-4 w-4 accent-accent"
                />
                Enabled
              </label>
              <button
                onClick={() => testServer(i)}
                className="rounded-lg bg-white/10 px-3 py-1.5 text-xs font-semibold text-white hover:bg-white/20"
              >
                Test
              </button>
              <button
                onClick={() => {
                  setServers((prev) => prev.filter((_, j) => j !== i));
                  setTestResults({});
                }}
                className="rounded-lg bg-white/10 px-3 py-1.5 text-xs font-semibold text-red-300 hover:bg-red-500/20"
              >
                Remove
              </button>
              {testResults[i] && (
                <span className="min-w-0 truncate text-xs text-white/50">
                  {testResults[i]}
                </span>
              )}
            </div>
          </div>
        ))}
        <div className="flex flex-wrap gap-3">
          <button
            onClick={() =>
              setServers((prev) => [
                ...prev,
                { name: "", url: "", token: "", enabled: true },
              ])
            }
            className="rounded-lg bg-white/10 px-4 py-2 text-sm font-semibold text-white transition-colors hover:bg-white/20"
          >
            Add server
          </button>
          <button
            onClick={save}
            disabled={saving}
            className="rounded-lg bg-accent px-4 py-2 text-sm font-semibold text-ink-900 transition-colors hover:bg-accent-soft disabled:opacity-50"
          >
            {saving ? "Saving…" : "Save settings"}
          </button>
        </div>
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
