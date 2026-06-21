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
  userAgent?: string;
  cookiePreview?: string;
  cookieKeys?: string[];
}

export interface FFmpegStatus {
  ffmpegFound: boolean;
  ffmpegPath?: string;
  ffmpegVersion?: string;
  ffprobeFound: boolean;
  ffprobePath?: string;
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
  auth: AuthStatus;
  ffmpeg: FFmpegStatus;
  settings: Settings;
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
  audio: string;
  audioMenu: boolean;
  force: boolean;
  noChunked: boolean;
  dryRun: boolean;
  ffmpegArgs: string;
  ffmpegPath: string;
  userAgent: string;
  cookie: string;
  browser: string;
  headers: Record<string, string> | null;
  feedFile: string;
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
  skipProbe: boolean;
  cookie: string;
  browser: string;
  userAgent: string;
  proxy: string;
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
  skipped: number;
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
  auth: () => req<AuthStatus>("GET", "/api/auth"),
  login: (body: { cookie: string; userAgent: string; browser: string }) =>
    req<AuthStatus>("POST", "/api/auth/login", body),
  logout: () => req<AuthStatus>("POST", "/api/auth/logout"),
  ffmpeg: () => req<FFmpegStatus>("GET", "/api/ffmpeg"),
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
  openPath: (path: string, reveal = false) => req<{ ok: boolean }>("POST", "/api/open", { path, reveal }),
  fs: (path: string) => req<FSListing>("GET", `/api/fs?path=${encodeURIComponent(path)}`),
};

export const imgURL = (u?: string) => (u ? `/api/img?u=${encodeURIComponent(u)}` : "");
