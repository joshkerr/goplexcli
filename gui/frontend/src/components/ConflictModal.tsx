import type { DownloadConflict } from "../lib/types";

// ConflictModal warns that some destination files already exist before a batch
// of downloads starts, and asks how to proceed: replace them all, skip the
// conflicting files, or cancel the whole batch. Rendered above DetailModal
// (z-50), so it sits at z-[60].
export function ConflictModal({
  conflicts,
  onChoose,
}: {
  conflicts: DownloadConflict[];
  onChoose: (choice: "replace" | "skip" | "cancel") => void;
}) {
  const many = conflicts.length > 1;
  return (
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center p-6 animate-fade-in"
      onClick={() => onChoose("cancel")}
    >
      <div className="absolute inset-0 bg-black/70 backdrop-blur-sm" />
      <div
        className="relative z-10 flex max-h-[70vh] w-full max-w-md flex-col overflow-hidden rounded-2xl border border-white/10 bg-ink-700 shadow-card"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="border-b border-white/5 px-6 py-4">
          <h3 className="text-lg font-semibold text-white">
            {many
              ? `${conflicts.length} files already exist`
              : "File already exists"}
          </h3>
          <p className="mt-1 text-sm text-white/50">
            {many ? "These files are" : "This file is"} already in the download
            folder. Replace {many ? "them" : "it"}, skip{" "}
            {many ? "them" : "it"}, or cancel the download?
          </p>
        </div>
        <ul className="flex-1 space-y-1 overflow-y-auto px-6 py-3">
          {conflicts.map((c) => (
            <li
              key={c.dest}
              title={c.dest}
              className="truncate text-sm text-white/70"
            >
              {c.name}
            </li>
          ))}
        </ul>
        <div className="flex flex-wrap justify-end gap-2 border-t border-white/5 px-6 py-4">
          <button
            onClick={() => onChoose("cancel")}
            className="rounded-lg bg-white/10 px-3.5 py-2 text-sm font-semibold text-white transition-colors hover:bg-white/20"
          >
            Cancel
          </button>
          <button
            onClick={() => onChoose("skip")}
            className="rounded-lg bg-white/10 px-3.5 py-2 text-sm font-semibold text-white transition-colors hover:bg-white/20"
          >
            Skip existing
          </button>
          <button
            onClick={() => onChoose("replace")}
            className="rounded-lg bg-red-500/80 px-3.5 py-2 text-sm font-semibold text-white transition-colors hover:bg-red-500"
          >
            Replace all
          </button>
        </div>
      </div>
    </div>
  );
}
