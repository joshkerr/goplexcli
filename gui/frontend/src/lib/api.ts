// Thin typed wrapper around the Wails-injected bindings. At runtime Wails
// exposes bound Go methods on window.go.main.App and an event API on
// window.runtime, so we call those directly rather than depending on generated
// binding files. This keeps the frontend buildable on its own (vite build).

import type {
  AppConfig,
  Category,
  Media,
  MediaCard,
  Season,
  Server,
  ServerSelection,
  Status,
} from "./types";

type WailsApp = {
  GetStatus(): Promise<Status>;
  Login(username: string, password: string): Promise<Server[]>;
  SaveServers(selections: ServerSelection[]): Promise<void>;
  GetConfig(): Promise<AppConfig>;
  SaveConfig(cfg: AppConfig): Promise<void>;
  Reindex(): Promise<void>;
  ListCategory(category: string): Promise<MediaCard[]>;
  Search(query: string): Promise<MediaCard[]>;
  GetItem(key: string): Promise<Media>;
  GetSeasons(showTitle: string): Promise<Season[]>;
  GetEpisodes(showTitle: string, season: number): Promise<Media[]>;
  Play(keys: string[], resume: boolean): Promise<void>;
  Download(keys: string[], destOverride: string): Promise<void>;
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
  listCategory: (c: Category) => app().ListCategory(c),
  search: (q: string) => app().Search(q),
  getItem: (key: string) => app().GetItem(key),
  getSeasons: (show: string) => app().GetSeasons(show),
  getEpisodes: (show: string, season: number) =>
    app().GetEpisodes(show, season),
  play: (keys: string[], resume: boolean) => app().Play(keys, resume),
  download: (keys: string[], dest: string) => app().Download(keys, dest),
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
