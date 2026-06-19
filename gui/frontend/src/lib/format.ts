// Small formatting helpers shared across components.

/** Formats a duration given in milliseconds as e.g. "1h 47m" or "23m". */
export function formatDuration(ms: number): string {
  if (!ms || ms <= 0) return "";
  const totalMin = Math.round(ms / 60000);
  const h = Math.floor(totalMin / 60);
  const m = totalMin % 60;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

/** Formats a 0-10 rating to one decimal, or "" when absent. */
export function formatRating(rating: number): string {
  if (!rating || rating <= 0) return "";
  return rating.toFixed(1);
}

/** Formats bytes as a human-readable size. */
export function formatBytes(bytes: number): string {
  if (!bytes || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.min(
    units.length - 1,
    Math.floor(Math.log(bytes) / Math.log(1024))
  );
  return `${(bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}
