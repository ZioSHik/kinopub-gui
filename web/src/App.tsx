import { useState } from "react";
import clsx from "clsx";
import {
  Activity,
  Download,
  Library as LibraryIcon,
  ListVideo,
  ShieldAlert,
  ShieldCheck,
  Stethoscope,
  Settings as SettingsIcon,
  Wifi,
  WifiOff,
} from "lucide-react";
import { useApp } from "./store";
import { useI18n } from "./i18n";
import { LangSwitcher } from "./components/LangSwitcher";
import { DownloadPage } from "./pages/Download";
import { QueuePage } from "./pages/Queue";
import { LibraryPage } from "./pages/Library";
import { DoctorPage } from "./pages/Doctor";
import { SettingsPage } from "./pages/Settings";
import { AuthModal } from "./components/AuthModal";
import { AudioMenuModal } from "./components/AudioMenuModal";
import { Toasts } from "./components/Toasts";

type Page = "download" | "queue" | "library" | "doctor" | "settings";

const NAV: { id: Page; label: string; icon: any }[] = [
  { id: "download", label: "Download", icon: Download },
  { id: "queue", label: "Queue", icon: ListVideo },
  { id: "library", label: "Library", icon: LibraryIcon },
  { id: "doctor", label: "Doctor", icon: Stethoscope },
  { id: "settings", label: "Settings", icon: SettingsIcon },
];

export default function App() {
  const { connected, version, jobs, auth, ffmpeg } = useApp();
  const { t } = useI18n();
  const [page, setPage] = useState<Page>("download");
  const [authOpen, setAuthOpen] = useState(false);

  const activeJobs = jobs.filter((j) => !["completed", "failed", "canceled"].includes(j.status)).length;
  const audioJob = jobs.find((j) => j.pendingAudio);

  return (
    <div className="flex min-h-screen">
      {/* Sidebar */}
      <aside className="sticky top-0 hidden h-screen w-60 shrink-0 flex-col border-r border-white/[0.06] bg-ink-900/60 p-4 backdrop-blur-sm md:flex">
        <Brand />
        <nav className="mt-6 flex-1 space-y-1">
          {NAV.map((n) => (
            <button
              key={n.id}
              onClick={() => setPage(n.id)}
              className={clsx("nav-item w-full", page === n.id && "nav-item-active")}
            >
              <n.icon className="h-[18px] w-[18px]" />
              <span className="flex-1 text-left">{t(n.label)}</span>
              {n.id === "queue" && activeJobs > 0 && (
                <span className="rounded-full bg-gold-500 px-1.5 py-0.5 text-[10px] font-bold text-ink-950">
                  {activeJobs}
                </span>
              )}
            </button>
          ))}
        </nav>
        <SystemFooter ffmpegFound={ffmpeg.ffmpegFound} version={version} connected={connected} />
      </aside>

      {/* Main */}
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-30 flex items-center gap-3 border-b border-white/[0.06] bg-ink-950/70 px-4 py-3 backdrop-blur-md md:px-8">
          {/* Mobile nav */}
          <div className="flex items-center gap-1 md:hidden">
            <Brand compact />
          </div>
          <div className="ml-auto flex items-center gap-2">
            <LangSwitcher />
            <span
              className={clsx(
                "chip hidden sm:inline-flex",
                connected
                  ? "border-emerald-500/20 bg-emerald-500/[0.08] text-emerald-300"
                  : "border-ember-500/25 bg-ember-500/[0.1] text-ember-400",
              )}
            >
              {connected ? <Wifi className="h-3.5 w-3.5" /> : <WifiOff className="h-3.5 w-3.5" />}
              {connected ? t("Live") : t("Reconnecting…")}
            </span>
            <button
              onClick={() => setAuthOpen(true)}
              className={clsx(
                "chip transition",
                auth.loggedIn
                  ? "border-emerald-500/25 bg-emerald-500/[0.08] text-emerald-300 hover:bg-emerald-500/[0.14]"
                  : "border-gold-500/30 bg-gold-500/[0.1] text-gold-300 hover:bg-gold-500/[0.16]",
              )}
            >
              {auth.loggedIn ? <ShieldCheck className="h-3.5 w-3.5" /> : <ShieldAlert className="h-3.5 w-3.5" />}
              {auth.loggedIn ? t("Signed in") : t("Sign in")}
            </button>
          </div>
        </header>

        {/* Mobile tab bar */}
        <nav className="flex items-center gap-1 overflow-x-auto border-b border-white/[0.06] px-3 py-2 md:hidden">
          {NAV.map((n) => (
            <button
              key={n.id}
              onClick={() => setPage(n.id)}
              className={clsx(
                "flex shrink-0 items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm",
                page === n.id ? "bg-gold-500/[0.14] text-gold-300" : "text-slate-400",
              )}
            >
              <n.icon className="h-4 w-4" />
              {t(n.label)}
            </button>
          ))}
        </nav>

        <main className="flex-1 px-4 py-6 md:px-8 md:py-8">
          {page === "download" && (
            <DownloadPage onStarted={() => setPage("queue")} onSignIn={() => setAuthOpen(true)} />
          )}
          {page === "queue" && <QueuePage onNew={() => setPage("download")} />}
          {page === "library" && <LibraryPage />}
          {page === "doctor" && <DoctorPage />}
          {page === "settings" && <SettingsPage />}
        </main>
      </div>

      <AuthModal open={authOpen} onClose={() => setAuthOpen(false)} />
      {audioJob && <AudioMenuModal key={audioJob.id} job={audioJob} />}
      <Toasts />
    </div>
  );
}

function Brand({ compact }: { compact?: boolean }) {
  const { t } = useI18n();
  return (
    <div className="flex items-center gap-2.5">
      <div className="grid h-9 w-9 place-items-center rounded-xl bg-gradient-to-br from-gold-400 to-gold-600 text-ink-950 shadow-glow">
        <Activity className="h-5 w-5" />
      </div>
      <div className={clsx(compact && "hidden sm:block")}>
        <div className="text-sm font-bold leading-tight text-slate-100">kino.pub</div>
        <div className="text-[11px] leading-tight text-slate-500">{t("downloader")}</div>
      </div>
    </div>
  );
}

function SystemFooter({
  ffmpegFound,
  version,
  connected,
}: {
  ffmpegFound: boolean;
  version: string;
  connected: boolean;
}) {
  const { t } = useI18n();
  return (
    <div className="space-y-2 border-t border-white/[0.06] pt-3 text-xs text-slate-500">
      <div className="flex items-center gap-2">
        <span className={clsx("h-2 w-2 rounded-full", ffmpegFound ? "bg-emerald-400" : "bg-ember-500")} />
        {ffmpegFound ? t("ffmpeg ready") : t("ffmpeg missing")}
      </div>
      <div className="flex items-center gap-2">
        <span className={clsx("h-2 w-2 rounded-full", connected ? "bg-emerald-400" : "bg-amber-400 animate-pulse-soft")} />
        {connected ? t("connected") : t("reconnecting")}
      </div>
      <div className="pt-1 text-slate-600">{version ? `v${version}` : ""}</div>
    </div>
  );
}
