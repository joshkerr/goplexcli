// Thin typed wrapper around the Wails-injected bindings. At runtime Wails
// exposes bound Go methods on window.go.main.App and an event API on
// window.runtime, so we call those directly rather than depending on generated
// binding files. This keeps the frontend buildable on its own (vite build).

import type {
  AppConfig,
  BrowseOptions,
  Category,
  DownloadProgress,
  Media,
  MediaCard,
  Season,
  Server,
  ServerSelection,
  Status,
  UpdateInfo,
} from "./types";

type WailsApp = {
  GetStatus(): Promise<Status>;
  Login(username: string, password: string): Promise<Server[]>;
  SaveServers(selections: ServerSelection[]): Promise<void>;
  GetConfig(): Promise<AppConfig>;
  SaveConfig(cfg: AppConfig): Promise<void>;
  Reindex(): Promise<void>;
  Update(): Promise<void>;
  ListCategory(category: string, opts: BrowseOptions): Promise<MediaCard[]>;
  MovieGenres(): Promise<string[]>;
  WarmPosters(urls: string[]): Promise<void>;
  SyncFromLAN(): Promise<void>;
  AppVersion(): Promise<string>;
  CheckUpdate(): Promise<UpdateInfo>;
  ApplyUpdate(): Promise<void>;
  Search(query: string): Promise<MediaCard[]>;
  GetItem(key: string): Promise<Media>;
  GetSeasons(showTitle: string): Promise<Season[]>;
  GetEpisodes(showTitle: string, season: number): Promise<Media[]>;
  Play(keys: string[], resume: boolean): Promise<void>;
  Download(keys: string[], destOverride: string): Promise<void>;
  ListDownloads(): Promise<DownloadProgress[] | null>;
  CancelDownload(id: string): Promise<void>;
  ClearDownloadHistory(): Promise<void>;
};

type WailsRuntime = {
  EventsOn(event: string, cb: (data: any) => void): () => void;
  EventsOff(event: string): void;
};

declare global {
  interface Window {
    go?: { main?: { App?: WailsApp } };
    runtime?: WailsRuntime;
  }
}

/**
 * True when running on macOS. The GUI only ever runs inside the Wails webview
 * (WKWebView on macOS, WebView2 on Windows), so the user-agent platform string
 * is a reliable signal. Used to reserve space for the inset traffic-light
 * window controls, which overlay the top-left of the content.
 */
export const isMac =
  typeof navigator !== "undefined" &&
  /Mac|iPhone|iPad|iPod/.test(navigator.platform || navigator.userAgent);

function app(): WailsApp {
  const a = window.go?.main?.App;
  if (!a) {
    throw new Error(
      "Wails bindings unavailable - run the app via `wails dev` or a built binary."
    );
  }
  return a;
}

// In development mode Vite may render React just before Wails has injected its
// bindings into the webview. Wait briefly instead of turning that harmless
// startup race into a permanent loading screen.
async function waitForApp(timeoutMs = 5000): Promise<WailsApp> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const bound = window.go?.main?.App;
    if (bound) return bound;
    await new Promise((resolve) => window.setTimeout(resolve, 50));
  }
  return app(); // Throw the existing actionable error after the timeout.
}

export const api = {
  getStatus: async () => (await waitForApp()).GetStatus(),
  login: (u: string, p: string) => app().Login(u, p),
  saveServers: (s: ServerSelection[]) => app().SaveServers(s),
  getConfig: () => app().GetConfig(),
  saveConfig: (c: AppConfig) => app().SaveConfig(c),
  reindex: () => app().Reindex(),
  update: () => app().Update(),
  listCategory: (c: Category, opts: BrowseOptions) =>
    app().ListCategory(c, opts),
  movieGenres: () => app().MovieGenres(),
  warmPosters: (urls: string[]) => app().WarmPosters(urls),
  syncFromLAN: () => app().SyncFromLAN(),
  appVersion: () => app().AppVersion(),
  checkUpdate: () => app().CheckUpdate(),
  applyUpdate: () => app().ApplyUpdate(),
  search: (q: string) => app().Search(q),
  getItem: (key: string) => app().GetItem(key),
  getSeasons: (show: string) => app().GetSeasons(show),
  getEpisodes: (show: string, season: number) =>
    app().GetEpisodes(show, season),
  play: (keys: string[], resume: boolean) => app().Play(keys, resume),
  download: (keys: string[], dest: string) => app().Download(keys, dest),
  listDownloads: async () => (await app().ListDownloads()) ?? [],
  cancelDownload: (id: string) => app().CancelDownload(id),
  clearDownloadHistory: () => app().ClearDownloadHistory(),
};

/** Subscribe to a backend event. Returns an unsubscribe function. */
export function onEvent<T = any>(
  event: string,
  cb: (data: T) => void
): () => void {
  const rt = window.runtime;
  if (!rt) return () => {};
  return rt.EventsOn(event, cb);
}
