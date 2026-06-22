// Typed REST client mirroring the Go backend (internal/gui).

export interface LogEntry {
  time: string;
  level: string;
  component?: string;
  message: string;
  fields?: Record<string, unknown>;
}

export interface TrackView {
  label: string;
  percent: number;
  done: number;
  total: number;
  bytes: number;
  approxTotal: number;
}

export interface EpisodeView {
  key: string;
  season: number;
  episode: number;
  title: string;
  state: "pending" | "running" | "completed" | "failed" | "deferred";
  percent: number;
  bytes: number;
  total: number;
  speedBps: number;
  etaSeconds: number;
  segDone: number;
  segTotal: number;
  tracks?: TrackView[];
  attempts: number;
  error?: string;
}

export interface PlanView {
  title: string;
  total: number;
  alreadyCompleted: number;
  seasons?: Record<string, number>;
}

export interface SummaryView {
  total: number;
  succeeded: number;
  failed: number;
  skipped: number;
}

export interface AudioTrackInfo {
  Index: number;
  Name: string;
  Language: string;
}

export interface AudioRequestView {
  tracks: AudioTrackInfo[];
  timeoutSeconds: number;
  deadlineUnix: number;
}

export type JobStatus =
  | "queued"
  | "resolving"
  | "running"
  | "completed"
  | "failed"
  | "canceled";

export interface JobView {
  id: string;
  url: string;
  status: JobStatus;
  title: string;
  posterUrl?: string;
  outputPath: string;
  dryRun: boolean;
  quality: string;
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  plan?: PlanView;
  episodes: EpisodeView[];
  summary?: SummaryView;
  error?: string;
  pendingAudio?: AudioRequestView | null;
  logs: LogEntry[];
}

export interface AuthStatus {
  loggedIn: boolean;
}

export interface FFmpegStatus {
  ffmpegFound: boolean;
  ffmpegPath?: string;
  ffmpegVersion?: string;
  ffprobeFound: boolean;
  ffprobePath?: string;
}

export interface DepsView {
  ffmpeg: FFmpegStatus;
  installSupported: boolean;
  managed: boolean;
  source?: string;
}

export interface UpdateStatus {
  current: string;
  latest?: string;
  updateAvailable: boolean;
  releaseUrl?: string;
  notes?: string;
  assetName?: string;
  supported: boolean;
  note?: string;
}

export interface Settings {
  outputPath: string;
  quality: string;
  container: string;
  concurrency: number;
  retries: number;
  minIntervalMs: number;
  proxy: string;
  verbosity: string;
  noChunked: boolean;
  theme: string;
  libraryDirs: string[] | null;
}

export interface Snapshot {
  version: string;
  jobs: JobView[];
  kpauth: KPStatus;
  ffmpeg: FFmpegStatus;
  settings: Settings;
}

// ---------------------------------------------------------------------------
// Official kino.pub API (device-code auth + discovery)
// ---------------------------------------------------------------------------

export interface KPStatus {
  loggedIn: boolean;
  pending: boolean;
  userCode?: string;
  verificationUri?: string;
  expiresAt?: number;
  error?: string;
}

export interface KPUser {
  username: string;
  avatar?: string;
  subscriptionActive: boolean;
  subscriptionDays: number;
  subscriptionEnd?: number;
}

export interface StreamInfo {
  manifestUrl: string;
  playUrl: string; // same-origin signed proxy URL for hls.js
  title: string;
}

export interface DiscoverItem {
  id: string;
  type: string;
  title: string;
  originalTitle?: string;
  year: number;
  poster: string;
  director?: string;
  rating: number;
  imdbRating: number;
  kinopoiskRating: number;
  genres?: string[];
  isSerial: boolean;
  subtitle?: string;
  watchedAt?: number;
  season?: number;
  episode?: number;
}

export interface DiscoverPage {
  items: DiscoverItem[];
  page: number;
  hasMore: boolean;
  total: number;
}

export interface DiscoverAudio {
  index: number;
  lang: string;
  type: string;
  author: string;
  label: string;
  filter: string;
}

export interface DiscoverEpisode {
  season: number;
  episode: number;
  title: string;
  watched: boolean;
}

export interface DiscoverSeason {
  number: number;
  episodes: DiscoverEpisode[];
}

export interface DiscoverDetail extends DiscoverItem {
  plot?: string;
  cast?: string;
  countries?: string[];
  durationMin?: number;
  audios: DiscoverAudio[];
  seasons?: DiscoverSeason[];
  episodeCount: number;
  itemUrl: string;
  qualities?: string[]; // distinct downloadable resolutions, highest first
}

export interface DiscoverCollection {
  id: string;
  title: string;
  poster: string;
}

export interface DiscoverBookmark {
  id: string;
  title: string;
  count: number;
}

export interface NamedRef {
  id: string;
  title: string;
}

export interface ItemsQuery {
  type?: string;
  sort?: string;
  genre?: string;
  country?: string;
  yearFrom?: number;
  yearTo?: number;
  imdbFrom?: number;
  imdbTo?: number;
  kpFrom?: number;
  kpTo?: number;
  ac3?: boolean;
  subtitles?: boolean;
  page?: number;
}

export interface RunRequest {
  url: string;
  outputPath: string;
  quality: string;
  container: string;
  concurrency: number;
  retries: number;
  minIntervalMs: number;
  proxy: string;
  seasons: string;
  episodes: string;
  episodeKeys?: string[];
  audio: string;
  audioMenu: boolean;
  force: boolean;
  noChunked: boolean;
  dryRun: boolean;
  ffmpegArgs: string;
  ffmpegPath: string;
  userAgent: string;
  verbosity: string;
}

export interface StartRequest extends RunRequest {
  seedTitle: string;
  seedPoster: string;
  seedTitles: Record<string, string> | null;
}

export interface PreviewEpisode {
  key: string;
  season: number;
  episode: number;
  title: string;
  durationSeconds: number;
  completed: boolean;
  selected: boolean;
}

export interface PreviewSeason {
  number: number;
  episodes: PreviewEpisode[];
}

export interface PreviewResponse {
  seriesId: string;
  title: string;
  originalTitle?: string;
  description?: string;
  posterUrl?: string;
  seasons: PreviewSeason[];
  total: number;
  selected: number;
  alreadyCompleted: number;
  source: string;
  logs?: LogEntry[];
}

export interface LibraryEpisode {
  key: string;
  season: number;
  episode: number;
  title: string;
  path: string;
  exists: boolean;
  bytes: number;
  resolution?: string;
  completedAt: string;
}

export interface LibrarySeries {
  dir: string;
  stateFile: string;
  seriesId: string;
  title: string;
  originalTitle?: string;
  description?: string;
  posterUrl?: string;
  inputUrl?: string;
  count: number;
  totalBytes: number;
  updatedAt: string;
  episodes: LibraryEpisode[];
}

export interface LibraryResponse {
  series: LibrarySeries[];
  dirs: string[];
}

export interface DoctorRequest {
  outputDir: string;
  fix: boolean;
  cleanTmp: boolean;
}

export interface DoctorIssue {
  key?: string;
  season?: number;
  episode?: number;
  kind: string;
  detail: string;
  statePath?: string;
  stateBytes?: number;
  actualBytes?: number;
}

export interface DoctorReport {
  stateFile: string;
  seriesId?: string;
  seriesTitle?: string;
  totalInState: number;
  healthy: number;
  fixed: boolean;
  hasIssues: boolean;
  issues: DoctorIssue[] | null;
  logs?: LogEntry[];
}

export interface FSEntry {
  name: string;
  path: string;
}

export interface FSListing {
  path: string;
  parent: string;
  dirs: FSEntry[];
}

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body !== undefined ? { "Content-Type": "application/json" } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  let data: unknown = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = text;
    }
  }
  if (!res.ok) {
    const msg =
      (data && typeof data === "object" && "error" in data
        ? String((data as { error: unknown }).error)
        : "") || `HTTP ${res.status}`;
    throw new Error(msg);
  }
  return data as T;
}

export const api = {
  state: () => req<Snapshot>("GET", "/api/state"),
  ffmpeg: () => req<FFmpegStatus>("GET", "/api/ffmpeg"),
  deps: () => req<DepsView>("GET", "/api/deps"),
  installDeps: () => req<DepsView>("POST", "/api/deps/install"),
  checkUpdate: (force = false) => req<UpdateStatus>("GET", `/api/update${force ? "?force=1" : ""}`),
  applyUpdate: () => req<{ updated: boolean; version: string; restarting: boolean }>("POST", "/api/update/apply"),
  getSettings: () => req<Settings>("GET", "/api/settings"),
  saveSettings: (s: Settings) => req<Settings>("PUT", "/api/settings", s),
  preview: (r: Partial<RunRequest>) => req<PreviewResponse>("POST", "/api/preview", r),
  jobs: () => req<JobView[]>("GET", "/api/jobs"),
  startJob: (r: Partial<StartRequest>) => req<JobView>("POST", "/api/jobs", r),
  cancelJob: (id: string) => req<{ canceling: boolean }>("POST", `/api/jobs/${id}/cancel`),
  deleteJob: (id: string) => req<{ removed: boolean }>("DELETE", `/api/jobs/${id}`),
  clearJobs: () => req<{ removed: number }>("POST", "/api/jobs/clear"),
  answerAudio: (id: string, indices: number[]) =>
    req<{ ok: boolean }>("POST", `/api/jobs/${id}/audio`, { indices }),
  doctor: (r: DoctorRequest) => req<DoctorReport>("POST", "/api/doctor", r),
  library: () => req<LibraryResponse>("GET", "/api/library"),
  deleteLibrary: (dir: string) => req<{ deleted: boolean }>("POST", "/api/library/delete", { dir }),
  deleteLibraryEpisode: (dir: string, key: string) =>
    req<{ deleted: boolean }>("POST", "/api/library/delete-episode", { dir, key }),
  openPath: (path: string, reveal = false) => req<{ ok: boolean }>("POST", "/api/open", { path, reveal }),
  fs: (path: string) => req<FSListing>("GET", `/api/fs?path=${encodeURIComponent(path)}`),

  // Official kino.pub API auth (device-code).
  kpStatus: () => req<KPStatus>("GET", "/api/kp/status"),
  kpUser: () => req<KPUser>("GET", "/api/kp/user"),
  kpLogin: () => req<KPStatus>("POST", "/api/kp/login"),
  kpLogout: () => req<KPStatus>("POST", "/api/kp/logout"),

  // Discovery.
  stream: (id: string, season?: number, episode?: number) => {
    const p = new URLSearchParams({ id });
    if (season != null) p.set("season", String(season));
    if (episode != null) p.set("episode", String(episode));
    return req<StreamInfo>("GET", `/api/discover/stream?${p.toString()}`);
  },
  discoverSearch: (q: string, page = 1) =>
    req<DiscoverPage>("GET", `/api/discover/search?q=${encodeURIComponent(q)}&page=${page}`),
  discoverItems: (query: ItemsQuery) => {
    const p = new URLSearchParams();
    if (query.type) p.set("type", query.type);
    if (query.sort) p.set("sort", query.sort);
    if (query.genre) p.set("genre", query.genre);
    if (query.country) p.set("country", query.country);
    if (query.yearFrom) p.set("yearFrom", String(query.yearFrom));
    if (query.yearTo) p.set("yearTo", String(query.yearTo));
    if (query.imdbFrom) p.set("imdbFrom", String(query.imdbFrom));
    if (query.imdbTo) p.set("imdbTo", String(query.imdbTo));
    if (query.kpFrom) p.set("kpFrom", String(query.kpFrom));
    if (query.kpTo) p.set("kpTo", String(query.kpTo));
    if (query.ac3) p.set("ac3", "1");
    if (query.subtitles) p.set("subtitles", "1");
    if (query.page) p.set("page", String(query.page));
    return req<DiscoverPage>("GET", `/api/discover/items?${p.toString()}`);
  },
  discoverCollections: (sort = "", page = 1) =>
    req<{ items: DiscoverCollection[] }>(
      "GET",
      `/api/discover/collections?sort=${encodeURIComponent(sort)}&page=${page}`,
    ),
  discoverCollection: (id: string, page = 1) =>
    req<DiscoverPage>("GET", `/api/discover/collection?id=${encodeURIComponent(id)}&page=${page}`),
  discoverBookmarks: () => req<{ items: DiscoverBookmark[] }>("GET", "/api/discover/bookmarks"),
  discoverBookmark: (id: string, page = 1) =>
    req<DiscoverPage>("GET", `/api/discover/bookmark?id=${encodeURIComponent(id)}&page=${page}`),
  discoverGenres: (type?: string) =>
    req<{ items: NamedRef[] }>("GET", `/api/discover/genres${type ? `?type=${encodeURIComponent(type)}` : ""}`),
  discoverCountries: () => req<{ items: NamedRef[] }>("GET", "/api/discover/countries"),
  discoverHistory: (page = 1) => req<DiscoverPage>("GET", `/api/discover/history?page=${page}`),
  discoverWatching: (subscribed = false, type = "serials", page = 1) =>
    req<DiscoverPage>(
      "GET",
      `/api/discover/watching?type=${type}&subscribed=${subscribed ? 1 : 0}&page=${page}`,
    ),
  discoverItem: (id: string) =>
    req<DiscoverDetail>("GET", `/api/discover/item?id=${encodeURIComponent(id)}`),
  discoverSimilar: (id: string) =>
    req<{ items: DiscoverItem[] }>("GET", `/api/discover/similar?id=${encodeURIComponent(id)}`),
};

export const imgURL = (u?: string) => (u ? `/api/img?u=${encodeURIComponent(u)}` : "");
