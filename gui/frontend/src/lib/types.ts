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
  name: string;
  percent: number;
  status: "pending" | "in_progress" | "completed" | "failed";
  bytes: number;
  total: number;
  error: string;
}

export type Category =
  | "movies"
  | "tv-shows"
  | "recently-added-movies"
  | "recently-added-tv"
  | "continue-watching";
