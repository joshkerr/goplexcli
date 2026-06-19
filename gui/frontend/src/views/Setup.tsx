import { useEffect, useState } from "react";
import { api, onEvent } from "../lib/api";
import type { ReindexProgress, Server, Status } from "../lib/types";
import { FilmIcon } from "../components/icons";

interface Props {
  status: Status;
  onReady: () => void;
  onToast: (msg: string, kind?: "info" | "error") => void;
}

type Step = "login" | "servers" | "index";

export function Setup({ status, onReady, onToast }: Props) {
  // If already configured but no cache, jump straight to indexing.
  const [step, setStep] = useState<Step>(
    status.configured ? "index" : "login"
  );
  const [servers, setServers] = useState<Server[]>([]);

  return (
    <div className="flex h-full w-full items-center justify-center bg-gradient-to-b from-ink-900 to-ink-800 p-6">
      <div className="w-full max-w-md">
        <div className="mb-8 flex flex-col items-center text-center">
          <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-gradient-to-br from-accent to-accent-dark text-ink-900 shadow-glow">
            <FilmIcon width={28} height={28} />
          </div>
          <h1 className="mt-4 text-2xl font-semibold tracking-tight text-white">
            GoplexCLI
          </h1>
          <p className="mt-1 text-sm text-white/50">
            Browse and stream your Plex library
          </p>
        </div>

        <div className="rounded-2xl border border-white/10 bg-ink-700/70 p-6 shadow-card">
          {step === "login" && (
            <LoginForm
              onToast={onToast}
              onLoggedIn={(srv) => {
                setServers(srv);
                setStep("servers");
              }}
            />
          )}
          {step === "servers" && (
            <ServerSelect
              servers={servers}
              onToast={onToast}
              onSaved={() => setStep("index")}
            />
          )}
          {step === "index" && <IndexStep onToast={onToast} onReady={onReady} />}
        </div>
      </div>
    </div>
  );
}

function LoginForm({
  onLoggedIn,
  onToast,
}: {
  onLoggedIn: (s: Server[]) => void;
  onToast: (m: string, k?: "info" | "error") => void;
}) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      const servers = await api.login(username, password);
      onLoggedIn(servers);
    } catch (err: any) {
      onToast(String(err?.message ?? err), "error");
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} className="space-y-4">
      <div>
        <label className="mb-1.5 block text-xs font-medium text-white/50">
          Plex username or email
        </label>
        <input
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          autoFocus
          className="w-full rounded-lg border border-white/10 bg-ink-800 px-3 py-2.5 text-sm text-white placeholder-white/30 outline-none focus:border-accent/60"
          placeholder="you@example.com"
        />
      </div>
      <div>
        <label className="mb-1.5 block text-xs font-medium text-white/50">
          Password
        </label>
        <input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          className="w-full rounded-lg border border-white/10 bg-ink-800 px-3 py-2.5 text-sm text-white placeholder-white/30 outline-none focus:border-accent/60"
          placeholder="••••••••"
        />
      </div>
      <button
        type="submit"
        disabled={busy || !username || !password}
        className="w-full rounded-lg bg-accent py-2.5 text-sm font-semibold text-ink-900 transition-colors hover:bg-accent-soft disabled:cursor-not-allowed disabled:opacity-50"
      >
        {busy ? "Signing in…" : "Sign in"}
      </button>
      <p className="text-center text-xs text-white/30">
        Credentials are sent only to Plex; only the auth token is stored locally.
      </p>
    </form>
  );
}

function ServerSelect({
  servers,
  onSaved,
  onToast,
}: {
  servers: Server[];
  onSaved: () => void;
  onToast: (m: string, k?: "info" | "error") => void;
}) {
  const [selected, setSelected] = useState<Set<string>>(
    new Set(servers.filter((s) => s.owned).map((s) => s.url))
  );
  const [busy, setBusy] = useState(false);

  const toggle = (url: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      next.has(url) ? next.delete(url) : next.add(url);
      return next;
    });

  const save = async () => {
    setBusy(true);
    try {
      const chosen = servers
        .filter((s) => selected.has(s.url))
        .map((s) => ({ name: s.name, url: s.url }));
      await api.saveServers(chosen);
      onSaved();
    } catch (err: any) {
      onToast(String(err?.message ?? err), "error");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-base font-semibold text-white">Choose servers</h2>
        <p className="text-sm text-white/50">
          Select the Plex servers to index.
        </p>
      </div>
      <div className="max-h-64 space-y-2 overflow-y-auto">
        {servers.map((s) => (
          <label
            key={s.url}
            className={`flex cursor-pointer items-center gap-3 rounded-lg border px-3 py-2.5 transition-colors ${
              selected.has(s.url)
                ? "border-accent/50 bg-accent/10"
                : "border-white/10 bg-ink-800 hover:bg-ink-600"
            }`}
          >
            <input
              type="checkbox"
              checked={selected.has(s.url)}
              onChange={() => toggle(s.url)}
              className="h-4 w-4 accent-accent"
            />
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm font-medium text-white/90">
                {s.name}
              </div>
              <div className="truncate text-xs text-white/40">{s.url}</div>
            </div>
            {s.local && (
              <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-emerald-300">
                LAN
              </span>
            )}
          </label>
        ))}
      </div>
      <button
        onClick={save}
        disabled={busy || selected.size === 0}
        className="w-full rounded-lg bg-accent py-2.5 text-sm font-semibold text-ink-900 transition-colors hover:bg-accent-soft disabled:opacity-50"
      >
        {busy ? "Saving…" : "Continue"}
      </button>
    </div>
  );
}

function IndexStep({
  onReady,
  onToast,
}: {
  onReady: () => void;
  onToast: (m: string, k?: "info" | "error") => void;
}) {
  const [busy, setBusy] = useState(false);
  const [progress, setProgress] = useState<ReindexProgress | null>(null);

  useEffect(() => {
    const off = onEvent<ReindexProgress>("reindex:progress", setProgress);
    const offDone = onEvent<{ count: number; error?: string }>(
      "reindex:done",
      (d) => {
        setBusy(false);
        if (d.error) {
          onToast(d.error, "error");
        } else {
          onToast(`Indexed ${d.count} items`);
          onReady();
        }
      }
    );
    return () => {
      off();
      offDone();
    };
  }, [onReady, onToast]);

  const start = async () => {
    setBusy(true);
    setProgress(null);
    try {
      await api.reindex();
    } catch (err: any) {
      setBusy(false);
      onToast(String(err?.message ?? err), "error");
    }
  };

  return (
    <div className="space-y-5 text-center">
      <div>
        <h2 className="text-base font-semibold text-white">Build your library</h2>
        <p className="mt-1 text-sm text-white/50">
          Index your media so it's available for instant browsing.
        </p>
      </div>

      {busy && (
        <div className="space-y-2 rounded-lg bg-ink-800 p-4 text-left">
          <div className="flex items-center gap-2 text-sm text-white/80">
            <span className="h-2 w-2 animate-pulse rounded-full bg-accent" />
            {progress
              ? `${progress.server} · ${progress.library}`
              : "Connecting…"}
          </div>
          {progress && (
            <div className="text-xs text-white/40">
              {progress.items.toLocaleString()} items
              {progress.total > 0 && ` of ${progress.total.toLocaleString()}`}
              {progress.servers > 1 &&
                ` · server ${progress.serverNum}/${progress.servers}`}
            </div>
          )}
        </div>
      )}

      <button
        onClick={start}
        disabled={busy}
        className="w-full rounded-lg bg-accent py-2.5 text-sm font-semibold text-ink-900 transition-colors hover:bg-accent-soft disabled:opacity-50"
      >
        {busy ? "Indexing…" : "Build library"}
      </button>
      {!busy && (
        <button
          onClick={onReady}
          className="text-xs text-white/40 hover:text-white/70"
        >
          Skip for now
        </button>
      )}
    </div>
  );
}
