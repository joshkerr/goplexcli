import { useState } from "react";
import { api } from "../lib/api";
import type { Library } from "../lib/types";
import Select from "./Select";

interface Props {
  // Called with the new library's display name after AddLibrary succeeds and
  // the owner should refetch GetLibraryInfo.
  onAdded: (name: string) => void;
  onToast: (msg: string, kind?: "info" | "error") => void;
  // Configured rclone remotes ("gdrive", …) offered as prefix chips.
  rcloneRemotes?: string[];
  // Compact variant for the Setup wizard: no name/rclone fields.
  compact?: boolean;
}

const INPUT =
  "w-full rounded-lg border border-white/10 bg-ink-800 px-3 py-2.5 text-sm text-white placeholder-white/30 outline-none focus:border-accent/60";

const TYPE_OPTIONS = [
  { value: "movie" as const, label: "Movies" },
  { value: "show" as const, label: "TV Shows" },
];

/** The "Add folder…" form: native folder picker (or a typed path), media
 * type, optional display name and optional rclone path. */
export function LibraryForm({ onAdded, onToast, rcloneRemotes, compact }: Props) {
  const [path, setPath] = useState("");
  const [type, setType] = useState<Library["type"]>("movie");
  const [name, setName] = useState("");
  const [remote, setRemote] = useState("");
  const [picking, setPicking] = useState(false);
  const [adding, setAdding] = useState(false);

  const folderName = path.replace(/[\\/]+$/, "").split(/[\\/]/).pop() ?? "";

  const choose = async () => {
    setPicking(true);
    try {
      const p = await api.pickLibraryFolder();
      if (p) setPath(p);
    } catch (e: any) {
      onToast(String(e?.message ?? e), "error");
    } finally {
      setPicking(false);
    }
  };

  const add = async () => {
    const p = path.trim();
    if (!p) return;
    setAdding(true);
    try {
      await api.addLibrary(p, type, name.trim(), remote.trim());
      onAdded(name.trim() || folderName || p);
      setPath("");
      setName("");
      setRemote("");
    } catch (e: any) {
      onToast(String(e?.message ?? e), "error");
    } finally {
      setAdding(false);
    }
  };

  // Prefix the rclone path with a remote name, replacing any remote already
  // typed ("gdrive:Media" → "dropbox:Media").
  const useRemote = (r: string) =>
    setRemote((cur) => `${r}:${cur.replace(/^[^:]*:/, "")}`);

  return (
    <div className="space-y-3">
      <div>
        <label className="mb-1.5 block text-xs font-medium text-white/50">
          Folder
        </label>
        <div className="flex gap-2">
          <input
            value={path}
            onChange={(e) => setPath(e.target.value)}
            placeholder="/Volumes/Media/Movies"
            className={INPUT}
          />
          <button
            type="button"
            onClick={choose}
            disabled={picking}
            className="shrink-0 rounded-lg bg-white/10 px-3 py-2 text-sm font-semibold text-white transition-colors hover:bg-white/20 disabled:opacity-50"
          >
            {picking ? "Choosing…" : "Choose folder"}
          </button>
        </div>
      </div>

      <div className={compact ? "" : "grid grid-cols-2 gap-3"}>
        <div>
          <label className="mb-1.5 block text-xs font-medium text-white/50">
            Type
          </label>
          <Select<Library["type"]>
            value={type}
            onChange={setType}
            options={TYPE_OPTIONS}
            className="w-full bg-ink-800 px-3 py-2.5 text-sm text-white"
            aria-label="Library type"
          />
        </div>
        {!compact && (
          <div>
            <label className="mb-1.5 block text-xs font-medium text-white/50">
              Name
            </label>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={folderName || "Folder name"}
              className={INPUT}
            />
          </div>
        )}
      </div>

      {!compact && (
        <div>
          <label className="mb-1.5 block text-xs font-medium text-white/50">
            rclone path (optional)
          </label>
          <input
            value={remote}
            onChange={(e) => setRemote(e.target.value)}
            placeholder="gdrive:Media/Movies"
            className={INPUT}
          />
          {rcloneRemotes && rcloneRemotes.length > 0 && (
            <div className="mt-1.5 flex flex-wrap gap-1.5">
              {rcloneRemotes.map((r) => (
                <button
                  key={r}
                  type="button"
                  onClick={() => useRemote(r)}
                  className="rounded-full bg-white/10 px-2.5 py-0.5 text-[11px] font-medium text-white/70 transition-colors hover:bg-white/20 hover:text-white"
                >
                  {r}:
                </button>
              ))}
            </div>
          )}
          <p className="mt-1 text-xs text-white/30">
            e.g. gdrive:Media/Movies — lets other computers download these
            files with rclone; also used to list cloud folders faster than
            walking the mount.
          </p>
        </div>
      )}

      <button
        type="button"
        onClick={add}
        disabled={adding || !path.trim()}
        className={`rounded-lg bg-accent px-4 py-2 text-sm font-semibold text-ink-900 transition-colors hover:bg-accent-soft disabled:opacity-50 ${
          compact ? "w-full py-2.5" : ""
        }`}
      >
        {adding ? "Adding…" : "Add library"}
      </button>
    </div>
  );
}
