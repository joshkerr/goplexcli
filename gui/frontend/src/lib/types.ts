// Mirrors the Go DTOs bound from the backend (gui/*.go).

export interface Status {
  configured: boolean;
  hasCache: boolean;
  cacheCount: number;
  lastUpdated: string;
  movieCount: number;
  showCount: number;
  episodeCount: number;
  mpvAvailable: boolean;
  rcloneAvailable: boolean;
  serverNames: string[] | null;
}

export interface Server {
  name: string;
  url: string;
  local: boolean;
  owned: boolean;
}

export interface ServerSelection {
  name: string;
  url: string;
}

export interface AppConfig {
  downloadDir: string;
  mpvPath: string;
  rclonePath: string;
  syncPeer: string;
}

// MediaCard is the lightweight row used by the poster grid (see GetItem for
// full details when a card is opened).
export interface MediaCard {
  key: string;
  type: "movie" | "show" | "episode";
  title: string;
  year: number;
  displayTitle: string;
  thumbURL: string;
  progressPct: number;
  viewCount: number;
  episodeCount: number;
  newCount: number; // recently added episodes; set only on New Episodes show cards
}

export interface Person {
  name: string;
  role: "director" | "actor";
  count: number; // movies with this tag
}

export interface Media {
  key: string;
  type: "movie" | "show" | "episode";
  title: string;
  displayTitle: string;
  year: number;
  summary: string;
  rating: number;
  duration: number;
  contentRating: string;
  studio: string;
  director: string;
  genre: string;
  cast: string;
  parentTitle: string;
  grandTitle: string;
  index: number;
  parentIndex: number;
  viewOffset: number;
  viewCount: number;
  progressPct: number;
  thumbURL: string;
  serverName: string;
  episodeCount: number;
}

export interface Season {
  season: number;
  episodeCount: number;
}

export interface ReindexProgress {
  server: string;
  library: string;
  items: number;
  total: number;
  serverNum: number;
  servers: number;
  libNum: number;
  libraries: number;
}

export interface DownloadProgress {
  id: string;
  seq: number; // monotonically increasing; higher = added later
  name: string;
  percent: number;
  status:
    | "pending"
    | "in_progress"
    | "paused"
    | "completed"
    | "failed"
    | "cancelled";
  bytes: number;
  total: number;
  speed: number; // bytes/sec (0 if unknown)
  error: string;
}

export interface PlaybackStatus {
  stage: "preparing" | "starting" | "playing" | "warning" | "stopped";
  title: string;
  count: number;
  detail: string; // warning message; empty for other stages
}

export type Category =
  | "movies"
  | "tv-shows"
  | "recently-added-movies"
  | "recently-added-tv"
  | "continue-watching";

// SortField mirrors the fields honored by the Movies grid backend (see
// sortMovieItems in gui/media.go).
export type SortField = "title" | "year" | "added" | "rating" | "duration";

// BrowseOptions is the genre filter + sort order passed to ListCategory. Only
// the Movies grid honors it.
export interface BrowseOptions {
  genre: string;
  sortField: SortField;
  desc: boolean;
}

// UpdateInfo reports whether a newer GUI release is available (see CheckUpdate).
export interface UpdateInfo {
  current: string;
  latest: string;
  available: boolean;
  notesURL: string;
  error: string;
}
