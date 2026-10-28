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
  state: "pending" | "running" | "completed" | "failed" | "deferred" | "paused";
  percent: number;
  bytes: number;
  total: number;
  totalApprox?: boolean; // total is an estimate (HLS), not a known size
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
  | "canceled"
  | "paused";

export interface JobView {
  id: string;
  url: string;
  status: JobStatus;
  title: string;
  posterUrl?: string;
  outputPath: string;
  dryRun: boolean;
  quality: string;
  container?: string;
  audioSpecs?: AudioSpec[];
  audioInclude?: string[];
  audioExclude?: string[];
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  plan?: PlanView;
  episodes: EpisodeView[];
  /** The run's explicit episode selection ("S1E2" keys) before it resolves. */
  selectedEpisodes?: string[];
  summary?: SummaryView;
  error?: string;
  pendingAudio?: AudioRequestView | null;
  logs: LogEntry[];
}

// Leftovers is the partial download data a job still holds on disk (.hls-tmp
// segment dirs, .tmp part files) — what removing its card would strand.
export interface Leftovers {
  bytes: number;
  items: number;
  // conflict: another job can still resume the same files, so they stay put.
  conflict: boolean;
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
  proxy: string;
  verbosity: string;
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
// Official kino.watch API (device-code auth + discovery)
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
  resumeTime?: number; // saved playback position in seconds (0 = nothing to resume)
  duration?: number; // total runtime in seconds (0 = unknown)
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
  codec?: string;
  channels?: number;
  surround: boolean;
}

export interface AudioSpec {
  require: string[];
  forbid: string[];
  source?: { language: string; type: string; author: string; codec: string; channels: number };
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

// kino.watch's own chart lists (/v1/items/{fresh,hot,popular}). They are ranked
// server-side, so they can't be reproduced by sorting the catalog — and they
// take a content type only, no genre/year/rating narrowing.
export type TopKind = "fresh" | "hot" | "popular";

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
  proxy: string;
  seasons: string;
  episodes: string;
  episodeKeys?: string[];
  audio: string;
  audioSpecs?: AudioSpec[];
  audioMenu: boolean;
  force: boolean;
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
  versions?: LibraryEpisode[];
  requestedQuality?: string;
  quality?: string;
  allAudio?: boolean;
  audio?: string[];
  audioFallback?: boolean;
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
  type?: string;
  isMovie: boolean;
  genres?: string[];
  count: number;
  totalBytes: number;
  updatedAt: string;
  episodes: LibraryEpisode[];
}

export interface LibraryResponse {
  series: LibrarySeries[];
  dirs: string[];
}

export interface DownloadedEpisode {
  path?: string;
  requestedQuality?: string;
  quality?: string;
  allAudio?: boolean;
  key: string;
  season: number;
  episode: number;
  resolution?: string;
  exists: boolean;
  // The voiceover this episode actually came out in (HLS track names), and
  // whether it was a substitute for one the episode did not offer.
  audio?: string[];
  audioFallback?: boolean;
}

export interface DownloadedResponse {
  id: string;
  dir?: string;
  episodes: DownloadedEpisode[];
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
  /** Repair/cleanup was asked for but refused: unfinished downloads use this folder. */
  repairBlocked?: boolean;
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

// A request that never settles is worse than one that fails: the browser allows
// only a handful of connections per origin, so a few hung calls starve the pool
// and everything after them — including a plain page reload — queues behind them.
// Nothing this API does is legitimately slower than a minute, so bound them all.
const REQUEST_TIMEOUT_MS = 60_000;

const ABORT_NAVIGATION = "navigation";
const ABORT_TIMEOUT = "timeout";

// A request still waiting for a response, with the abort cause tracked on our
// side as well as on signal.reason: WebKit only added AbortSignal.reason in
// Safari 15.4, and on macOS 12.0–12.2 (which this app explicitly supports)
// abort(reason) is silently ignored — classification would otherwise fall
// through to a raw AbortError toast on every navigation.
type PendingRequest = { ctrl: AbortController; cause: string };

// Requests still waiting for a response. Tracked so navigation can hand their
// connections back. Only reads live here — see req() for why mutations are
// never navigation-aborted.
const inFlight = new Set<PendingRequest>();

// abortPendingRequests cancels every request still in flight.
//
// Over HTTP/1.1 a browser opens only ~6 connections per origin, and a request
// blocked on an unreachable upstream holds one for its entire timeout. Measured
// against this app: with 8 such requests outstanding, an endpoint the server
// answers in 13ms had still not returned after 15 seconds. So leaving a page has
// to release its sockets, not merely drop its React state — otherwise clicking
// through a few tabs stalls everything that comes after.
export function abortPendingRequests() {
  for (const p of inFlight) {
    p.cause = ABORT_NAVIGATION;
    p.ctrl.abort(ABORT_NAVIGATION);
  }
  inFlight.clear();
}

// The message a navigation-aborted request rejects with. It is a sentinel rather
// than prose because it rides the app's ordinary `toast(e.message || fallback)`
// path, where toast() recognises and drops it — so leaving a page still shows no
// error for the requests that leaving it cancelled.
//
// The alternative — never settling the promise — also stayed silent, but every
// caller then sat suspended on it forever, pinning its async frame and whatever
// that closed over (page props, loaded items) for the life of the tab. One
// abandoned frame per cancelled request, and a tab is left open for hours.
export const NAV_ABORT_MESSAGE = "kp:navigation-aborted";

// isNavigationAbort reports whether a caught value is that sentinel. It accepts
// the raw error and a bare message string, since both forms travel through the
// error paths (usePaged hands `e.message` to its onAppendError).
export function isNavigationAbort(e: unknown): boolean {
  const msg = typeof e === "string" ? e : e instanceof Error ? e.message : "";
  return msg === NAV_ABORT_MESSAGE;
}

interface ReqOpts {
  /**
   * Client-side deadline; 0 disables it. Reserved for operations the server
   * deliberately budgets minutes for (ffmpeg install, self-update, doctor):
   * aborting those at the blanket 60s reported a failure while the work kept
   * running server-side — the user retried a job that was already underway.
   */
  timeoutMs?: number;
}

async function req<T>(method: string, path: string, body?: unknown, opts: ReqOpts = {}): Promise<T> {
  const ctrl = new AbortController();
  const pending: PendingRequest = { ctrl, cause: "" };
  // Only reads are navigation-abortable. A mutation (sign-in, delete+purge, a
  // doctor run) is an action the user asked for: killing it on a page switch
  // silently cancels it server-side too (handlers run on r.Context()) with no
  // feedback anywhere. Reads are re-issued by the next page; mutations are few,
  // short, and their effects matter.
  if (method === "GET") inFlight.add(pending);
  const timeoutMs = opts.timeoutMs ?? REQUEST_TIMEOUT_MS;
  const timer =
    timeoutMs > 0
      ? setTimeout(() => {
          pending.cause = pending.cause || ABORT_TIMEOUT;
          ctrl.abort(ABORT_TIMEOUT);
        }, timeoutMs)
      : undefined;
  let res: Response;
  let text: string;
  try {
    res = await fetch(path, {
      method,
      headers: body !== undefined ? { "Content-Type": "application/json" } : undefined,
      body: body !== undefined ? JSON.stringify(body) : undefined,
      signal: ctrl.signal,
    });
    // The body is read inside the same guard as the headers: a response that
    // stalls mid-body is just as hung as one that never answered, and reading it
    // after the finally below would escape both the timeout (timer cleared) and
    // the navigation abort (controller already dropped from inFlight), pinning
    // one of the ~6 per-origin sockets forever.
    text = await res.text();
  } catch (e) {
    const cause = pending.cause || (ctrl.signal as { reason?: unknown }).reason;
    if (cause === ABORT_NAVIGATION) {
      // The caller navigated away, so its component is unmounting and nothing is
      // waiting for this answer. Reject with the sentinel rather than a real
      // error: it settles the promise — releasing the caller's suspended frame —
      // while the error surfaces recognise it and stay quiet, so no stale page's
      // failure is announced on whatever page is now on screen.
      throw new Error(NAV_ABORT_MESSAGE);
    }
    if (cause === ABORT_TIMEOUT) throw new Error(`Request timed out: ${method} ${path}`);
    throw e;
  } finally {
    if (timer !== undefined) clearTimeout(timer);
    inFlight.delete(pending);
  }
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

// Autosave requests must reach the server in edit order, including a final
// flush while leaving Settings. A rejected write must not block later edits.
let settingsWriteTail: Promise<unknown> = Promise.resolve();
function saveSettingsInOrder(settings: Settings): Promise<Settings> {
  const payload = { ...settings, libraryDirs: settings.libraryDirs ? [...settings.libraryDirs] : settings.libraryDirs };
  const result = settingsWriteTail.then(() => req<Settings>("PUT", "/api/settings", payload));
  settingsWriteTail = result.catch(() => undefined);
  return result;
}

export const api = {
  state: () => req<Snapshot>("GET", "/api/state"),
  ffmpeg: () => req<FFmpegStatus>("GET", "/api/ffmpeg"),
  deps: () => req<DepsView>("GET", "/api/deps"),
  // The server budgets these ten minutes on a detached context (a 40–80 MB
  // ffmpeg build or an app binary over a slow route): no client-side deadline,
  // or a slow install is reported failed while it keeps running server-side.
  installDeps: () => req<DepsView>("POST", "/api/deps/install", undefined, { timeoutMs: 0 }),
  checkUpdate: (force = false) => req<UpdateStatus>("GET", `/api/update${force ? "?force=1" : ""}`),
  applyUpdate: () =>
    req<{ updated: boolean; version: string; restarting: boolean }>("POST", "/api/update/apply", undefined, {
      timeoutMs: 0,
    }),
  getSettings: () => req<Settings>("GET", "/api/settings"),
  saveSettings: saveSettingsInOrder,
  preview: (r: Partial<RunRequest>) => req<PreviewResponse>("POST", "/api/preview", r),
  jobs: () => req<JobView[]>("GET", "/api/jobs"),
  startJob: (r: Partial<StartRequest>) => req<JobView>("POST", "/api/jobs", r),
  cancelJob: (id: string) => req<{ canceling: boolean }>("POST", `/api/jobs/${id}/cancel`),
  retryJob: (id: string) => req<{ ok: boolean }>("POST", `/api/jobs/${id}/retry`),
  retryEpisode: (id: string, season: number, episode: number) =>
    req<{ ok: boolean }>("POST", `/api/jobs/${id}/retry-episode`, { season, episode }),
  prioritizeEpisode: (id: string, season: number, episode: number) =>
    req<{ ok: boolean }>("POST", `/api/jobs/${id}/prioritize-episode`, { season, episode }),
  prioritizeJob: (id: string) => req<{ ok: boolean }>("POST", `/api/jobs/${id}/prioritize`),
  pauseJob: (id: string) => req<{ ok: boolean }>("POST", `/api/jobs/${id}/pause`),
  resumeJob: (id: string) => req<{ ok: boolean }>("POST", `/api/jobs/${id}/resume`),
  pauseEpisode: (id: string, season: number, episode: number) =>
    req<{ ok: boolean }>("POST", `/api/jobs/${id}/pause-episode`, { season, episode }),
  cancelEpisode: (id: string, season: number, episode: number) =>
    req<{ ok: boolean }>("POST", `/api/jobs/${id}/cancel-episode`, { season, episode }),
  resumeEpisode: (id: string, season: number, episode: number) =>
    req<{ ok: boolean }>("POST", `/api/jobs/${id}/resume-episode`, { season, episode }),
  // purge also deletes the partial download data the job was holding for a
  // resume — ask the user first, it can be gigabytes (which is also why the
  // deadline is generous: the server walks and deletes them synchronously).
  deleteJob: (id: string, purge = false) =>
    req<{ removed: boolean }>("DELETE", `/api/jobs/${id}${purge ? "?purge=1" : ""}`, undefined, {
      timeoutMs: 300_000,
    }),
  jobLeftovers: (id: string) => req<Leftovers>("GET", `/api/jobs/${id}/leftovers`),
  clearJobs: () => req<{ removed: number }>("POST", "/api/jobs/clear"),
  answerAudio: (id: string, indices: number[]) =>
    req<{ ok: boolean }>("POST", `/api/jobs/${id}/audio`, { indices }),
  // The doctor runs on the request context with a 5-minute server budget; a
  // client-side abort would cancel a repair mid-run.
  doctor: (r: DoctorRequest) => req<DoctorReport>("POST", "/api/doctor", r, { timeoutMs: 0 }),
  library: () => req<LibraryResponse>("GET", "/api/library"),
  libraryDownloaded: (id: string, outputPath = "") =>
    req<DownloadedResponse>("GET", `/api/library/downloaded?id=${encodeURIComponent(id)}${outputPath ? `&outputPath=${encodeURIComponent(outputPath)}` : ""}`),
  deleteLibrary: (dir: string) => req<{ deleted: boolean }>("POST", "/api/library/delete", { dir }),
  deleteLibraryEpisode: (dir: string, key: string) =>
    req<{ deleted: boolean }>("POST", "/api/library/delete-episode", { dir, key }),
  openPath: (path: string, reveal = false) => req<{ ok: boolean }>("POST", "/api/open", { path, reveal }),
  fs: (path: string) => req<FSListing>("GET", `/api/fs?path=${encodeURIComponent(path)}`),

  // Official kino.watch API auth (device-code).
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
  // Report playback progress so the title appears in History / continue-watching
  // and can be resumed. season/episode are 0 for movies.
  markTime: (id: string, time: number, season?: number, episode?: number) =>
    req<{ ok: boolean }>("POST", "/api/discover/marktime", {
      id,
      time: Math.max(0, Math.floor(time)),
      season: season ?? 0,
      episode: episode ?? 0,
    }),
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
  // A kino.watch chart. type narrows it to movies/serials/… and is the only
  // narrowing the endpoint accepts; "" means all types.
  discoverTop: (kind: TopKind, type = "", page = 1) => {
    const p = new URLSearchParams({ kind, page: String(page) });
    if (type) p.set("type", type);
    return req<DiscoverPage>("GET", `/api/discover/top?${p.toString()}`);
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
