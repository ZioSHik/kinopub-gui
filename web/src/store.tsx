import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  api,
  type AuthStatus,
  type FFmpegStatus,
  type JobView,
  type Settings,
  type Snapshot,
  type UpdateStatus,
} from "./api";

export interface Toast {
  id: number;
  kind: "info" | "success" | "error";
  message: string;
}

interface AppContextValue {
  connected: boolean;
  version: string;
  jobs: JobView[];
  auth: AuthStatus;
  ffmpeg: FFmpegStatus;
  settings: Settings;
  settingsLoaded: boolean;
  update: UpdateStatus | null;
  refreshUpdate: (force?: boolean) => Promise<void>;
  setSettingsLocal: (s: Settings) => void;
  setAuthLocal: (a: AuthStatus) => void;
  refresh: () => void;
  toasts: Toast[];
  toast: (message: string, kind?: Toast["kind"]) => void;
  dismissToast: (id: number) => void;
}

const AppContext = createContext<AppContextValue | null>(null);

const emptyAuth: AuthStatus = { loggedIn: false };
const emptyFFmpeg: FFmpegStatus = { ffmpegFound: false, ffprobeFound: false };
const emptySettings: Settings = {
  outputPath: "",
  quality: "1080p",
  container: "mkv",
  concurrency: 2,
  retries: 5,
  minIntervalMs: 0,
  proxy: "",
  verbosity: "normal",
  noChunked: false,
  theme: "cinematic",
  libraryDirs: null,
};

function sortJobs(jobs: JobView[]): JobView[] {
  return [...jobs].sort(
    (a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime(),
  );
}

export function AppProvider({ children }: { children: ReactNode }) {
  const [connected, setConnected] = useState(false);
  const [version, setVersion] = useState("");
  const [jobs, setJobs] = useState<JobView[]>([]);
  const [auth, setAuth] = useState<AuthStatus>(emptyAuth);
  const [ffmpeg, setFFmpeg] = useState<FFmpegStatus>(emptyFFmpeg);
  const [settings, setSettings] = useState<Settings>(emptySettings);
  const [settingsLoaded, setSettingsLoaded] = useState(false);
  const [update, setUpdate] = useState<UpdateStatus | null>(null);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const toastSeq = useRef(0);

  const refreshUpdate = (force = false): Promise<void> =>
    api
      .checkUpdate(force)
      .then((u) => setUpdate(u))
      .catch(() => {});

  const toast = (message: string, kind: Toast["kind"] = "info") => {
    const id = ++toastSeq.current;
    setToasts((t) => [...t, { id, kind, message }]);
    window.setTimeout(() => {
      setToasts((t) => t.filter((x) => x.id !== id));
    }, kind === "error" ? 6500 : 4000);
  };
  const dismissToast = (id: number) => setToasts((t) => t.filter((x) => x.id !== id));

  const applySnapshot = (snap: Snapshot) => {
    setVersion(snap.version);
    setJobs(sortJobs(snap.jobs || []));
    setAuth(snap.auth || emptyAuth);
    setFFmpeg(snap.ffmpeg || emptyFFmpeg);
    setSettings(snap.settings || emptySettings);
    setSettingsLoaded(true);
  };

  const refresh = () => {
    api.state().then(applySnapshot).catch(() => {});
  };

  useEffect(() => {
    let es: EventSource | null = null;
    let stopped = false;
    let retry: number | undefined;

    const connect = () => {
      if (stopped) return;
      es = new EventSource("/api/events");
      es.onopen = () => {
        setConnected(true);
        // Re-check on every (re)connect. After a self-update restart this runs
        // against the fresh process (empty cache), so the banner clears.
        refreshUpdate();
      };
      es.onerror = () => {
        setConnected(false);
        es?.close();
        retry = window.setTimeout(connect, 1500);
      };
      es.onmessage = (ev) => {
        if (!ev.data) return;
        let parsed: { type: string; data: unknown };
        try {
          parsed = JSON.parse(ev.data);
        } catch {
          return;
        }
        switch (parsed.type) {
          case "snapshot":
            applySnapshot(parsed.data as Snapshot);
            break;
          case "job": {
            const job = parsed.data as JobView;
            setJobs((cur) => {
              const idx = cur.findIndex((j) => j.id === job.id);
              if (idx === -1) return sortJobs([...cur, job]);
              const next = cur.slice();
              next[idx] = job;
              return next;
            });
            break;
          }
          case "job_removed": {
            const id = (parsed.data as { id: string }).id;
            setJobs((cur) => cur.filter((j) => j.id !== id));
            break;
          }
          case "auth":
            setAuth(parsed.data as AuthStatus);
            break;
          case "settings":
            setSettings(parsed.data as Settings);
            break;
        }
      };
    };

    connect();
    return () => {
      stopped = true;
      if (retry) window.clearTimeout(retry);
      es?.close();
    };
  }, []);

  const value = useMemo<AppContextValue>(
    () => ({
      connected,
      version,
      jobs,
      auth,
      ffmpeg,
      settings,
      settingsLoaded,
      update,
      refreshUpdate,
      setSettingsLocal: setSettings,
      setAuthLocal: setAuth,
      refresh,
      toasts,
      toast,
      dismissToast,
    }),
    [connected, version, jobs, auth, ffmpeg, settings, settingsLoaded, update, toasts],
  );

  return <AppContext.Provider value={value}>{children}</AppContext.Provider>;
}

export function useApp(): AppContextValue {
  const ctx = useContext(AppContext);
  if (!ctx) throw new Error("useApp must be used within AppProvider");
  return ctx;
}
