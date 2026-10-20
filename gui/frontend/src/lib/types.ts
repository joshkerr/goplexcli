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
  rclonecpAvailable: boolean;
  serverNames: string[] | null;
  hasPlex: boolean; // a Plex server is configured
  hasLibraries: boolean; // at least one enabled local library
  libraryNames: string[] | null;
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
  rclonecpPath: string;
  autoSendRclonecp: boolean;
  slowDeviceMode: boolean;
  sortDownloads: boolean;
  syncPeer: string;
  tmdbApiKey: string; // TMDB API key (v3 key or v4 read token)
  // Base URL other devices use to reach this machine's library server, e.g.
  // http://192.168.0.155:47822. Blank = auto (first LAN IP + port 47822).
  libraryUrl: string;
  libraryName: string; // label for locally indexed items (blank = hostname)
  libraryProbe: boolean; // run ffprobe for exact durations (slow on cloud mounts)
  artworkEmbed: boolean; // embed cover art into completed downloads
  artworkOrientation: "horizontal" | "vertical";
}

// A folder of media that goplexcli indexes itself (no Plex) and serves from
// this machine. Edited in Settings → Local libraries.
export interface Library {
  name: string;
  path: string;
  type: "movie" | "show";
  remote: string; // optional rclone path, e.g. gdrive:Media/Movies
  enabled: boolean;
  items: number; // indexed item count
}

export interface LibraryInfo {
  libraries: Library[] | null;
  serverName: string;
  serverURL: string;
  serverPort: number;
  tmdbConfigured: boolean;
  rcloneRemotes: string[] | null;
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
  eta: string; // rclone's remaining-time estimate ("" if unknown)
  error: string;
  // Wall-clock queue time (unix ms). seq only orders jobs from one machine;
  // queuedAt orders local and remote jobs in the merged Downloads list.
  queuedAt?: number;
  // Name of the remote server the job runs on; absent for local downloads.
  origin?: string;
  // Plex media type; absent on entries recorded before the field existed.
  mediaType?: "movie" | "episode";
  // Plex title (the show for episodes) and year, when the job was started
  // from a library item.
  title?: string;
  year?: number;
  // Destination path on the machine running the job.
  dest?: string;
}

// A registered remote download server (a `goplexcli serve` daemon on another
// machine), as edited in Settings.
export interface RemoteServer {
  name: string;
  url: string;
  token: string;
  enabled: boolean;
}

// Result of probing a remote server from Settings: distinguishes offline
// (online=false) from reachable-but-wrong-token (online=true with error).
export interface RemoteTest {
  online: boolean;
  name: string;
  version: string;
  platform: string;
  error?: string;
}

// A requested download whose destination file already exists on disk.
export interface DownloadConflict {
  name: string;
  dest: string;
}

// What Download does with items whose destination file already exists:
// "replace" deletes the existing file first, "skip" drops those transfers,
// and "" overwrites in place (the historical behavior).
export type OnExisting = "" | "replace" | "skip";

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
  | "continue-watching"
  | "favorites-movies"
  | "favorites-tv";

// SortField mirrors the fields honored by the Movies grid backend (see
// sortMovieItems in gui/media.go).
export type SortField = "title" | "year" | "added" | "rating" | "duration";

// BrowseOptions is the filters + sort order passed to ListCategory. The
// Movies and Favorites grids honor it; other categories keep a fixed order.
export interface BrowseOptions {
  genre: string;
  sortField: SortField;
  desc: boolean;
  hideForeign: boolean;
}

// MatchCandidate is one TMDB search hit offered when fixing a wrong match
// for a locally indexed movie or show (see SearchMatches).
export interface MatchCandidate {
  id: number;
  kind: "movie" | "tv";
  title: string;
  year: number;
  overview: string;
  rating: number;
  posterURL: string; // may be ""
}

// MatchResult is what ApplyMatch returns: the matched title and the card key
// to reopen (a show's key changes when the match renames it).
export interface MatchResult {
  title: string;
  key: string;
  rebuilt: number;
}

// UpdateInfo reports whether a newer GUI release is available (see CheckUpdate).
export interface UpdateInfo {
  current: string;
  latest: string;
  available: boolean;
  notesURL: string;
  error: string;
}
