import { useState } from "react";
import clsx from "clsx";
import {
  ArrowUpCircle,
  Clapperboard,
  Film,
  Library as LibraryIcon,
  ListVideo,
  Loader2,
  PanelLeftClose,
  PanelLeftOpen,
  PlugZap,
  ShieldAlert,
  Stethoscope,
  Unplug,
  User as UserIcon,
  WifiOff,
} from "lucide-react";
import { useApp } from "./store";
import type { KPStatus, KPUser } from "./api";
import { type Page, pushRoute, useRoute } from "./router";
import { useI18n } from "./i18n";
import { LangSwitcher } from "./components/LangSwitcher";
import { DiscoverPage } from "./pages/Discover";
import { DownloadPage } from "./pages/Download";
import { QueuePage } from "./pages/Queue";
import { LibraryPage } from "./pages/Library";
import { DoctorPage } from "./pages/Doctor";
import { SettingsPage } from "./pages/Settings";
import { AudioMenuModal } from "./components/AudioMenuModal";
import { Toasts } from "./components/Toasts";

const NAV: { id: Page; label: string; icon: any }[] = [
  { id: "discover", label: "Catalog", icon: Clapperboard },
  { id: "queue", label: "Queue", icon: ListVideo },
  { id: "library", label: "Library", icon: LibraryIcon },
  { id: "doctor", label: "Doctor", icon: Stethoscope },
];

export default function App() {
  const { connected, version, jobs, kpauth, kpUser, kpUserError, ffmpeg, update } = useApp();
  const { t } = useI18n();
  // The URL hash is the single source of truth for the active page (and, within
  // a page, the open collection/card) so reloads and browser back/forward
  // restore the exact view.
  const page = useRoute().page;
  const [collapsed, setCollapsed] = useState<boolean>(() => localStorage.getItem("sidebarCollapsed") === "1");
  const toggleCollapsed = () =>
    setCollapsed((v) => {
      const next = !v;
      localStorage.setItem("sidebarCollapsed", next ? "1" : "0");
      return next;
    });

  // Top-level navigation drops any open card/collection and pushes a fresh page
  // route, so browser-back returns to wherever the user was.
  const navigate = (p: Page) => pushRoute({ page: p });

  const activeJobs = jobs.filter((j) => !["completed", "failed", "canceled"].includes(j.status)).length;
  const audioJob = jobs.find((j) => j.pendingAudio);

  return (
    <div className="flex min-h-screen">
      {/* Sidebar */}
      <aside
        className={clsx(
          "sticky top-0 hidden h-screen shrink-0 flex-col border-r border-white/[0.06] bg-ink-900/60 p-3 backdrop-blur-sm transition-[width] duration-200 md:flex",
          collapsed ? "w-[68px]" : "w-60",
        )}
      >
        <div className={clsx("flex items-center", collapsed ? "flex-col gap-2" : "justify-between gap-2")}>
          {collapsed ? <BrandMark /> : <Brand />}
          <button
            onClick={toggleCollapsed}
            className="rounded-lg p-1.5 text-slate-500 transition hover:bg-white/[0.06] hover:text-slate-300"
            title={collapsed ? t("Expand sidebar") : t("Collapse sidebar")}
          >
            {collapsed ? <PanelLeftOpen className="h-[18px] w-[18px]" /> : <PanelLeftClose className="h-[18px] w-[18px]" />}
          </button>
        </div>
        <nav className="mt-6 flex-1 space-y-1">
          {NAV.map((n) => (
            <button
              key={n.id}
              onClick={() => navigate(n.id)}
              title={collapsed ? t(n.label) : undefined}
              className={clsx("nav-item relative w-full", collapsed && "justify-center px-0", page === n.id && "nav-item-active")}
            >
              <n.icon className="h-[18px] w-[18px] shrink-0" />
              {!collapsed && <span className="flex-1 text-left">{t(n.label)}</span>}
              {n.id === "queue" && activeJobs > 0 &&
                (collapsed ? (
                  <span className="absolute right-1 top-1 h-2 w-2 rounded-full bg-gold-500" />
                ) : (
                  <span className="inline-flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full bg-gold-500 px-1.5 text-[10px] font-bold leading-none text-ink-950">
                    {activeJobs}
                  </span>
                ))}
            </button>
          ))}
        </nav>
        <ProfileCard
          collapsed={collapsed}
          kpauth={kpauth}
          kpUser={kpUser}
          kpUserError={kpUserError}
          onClick={() => navigate("settings")}
        />
        <SystemFooter ffmpegFound={ffmpeg.ffmpegFound} version={version} connected={connected} collapsed={collapsed} />
      </aside>

      {/* Main */}
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-30 flex transform-gpu items-center gap-3 border-b border-white/[0.06] bg-ink-950/70 px-4 py-3 backdrop-blur-md md:px-8">
          {/* Mobile nav */}
          <div className="flex items-center gap-1 md:hidden">
            <Brand compact />
          </div>
          <div className="ml-auto flex items-center gap-2">
            {update?.updateAvailable && (
              <button
                onClick={() => navigate("settings")}
                className="chip border-gold-500/30 bg-gold-500/[0.12] text-gold-300 hover:bg-gold-500/[0.2]"
                title={t("A new version is available")}
              >
                <ArrowUpCircle className="h-3.5 w-3.5" />
                <span className="hidden sm:inline">{t("Update {v}", { v: update.latest || "" })}</span>
                <span className="sm:hidden">{t("Update")}</span>
              </button>
            )}
            <LangSwitcher />
          </div>
        </header>

        {/* Mobile tab bar */}
        <nav className="flex items-center gap-1 overflow-x-auto border-b border-white/[0.06] px-3 py-2 md:hidden">
          {NAV.map((n) => (
            <button
              key={n.id}
              onClick={() => navigate(n.id)}
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
          {page === "discover" && (
            <DiscoverPage onStarted={() => navigate("queue")} onOpenSettings={() => navigate("settings")} />
          )}
          {page === "download" && (
            <DownloadPage onStarted={() => navigate("queue")} onSignIn={() => navigate("settings")} />
          )}
          {page === "queue" && <QueuePage onNew={() => navigate("download")} />}
          {page === "library" && <LibraryPage />}
          {page === "doctor" && <DoctorPage />}
          {page === "settings" && <SettingsPage />}
        </main>
      </div>

      {audioJob && <AudioMenuModal key={audioJob.id} job={audioJob} />}
      <Toasts />
    </div>
  );
}

// BrandMark is the bare app icon. Single source of truth for the mark: the same
// favicon.svg the browser tab uses and that package-macos.sh bakes into
// AppIcon.icns. Stays visible even when the sidebar is collapsed.
function BrandMark() {
  return <img src="./favicon.svg" alt="kino.pub" className="h-9 w-9 shrink-0 rounded-xl shadow-glow" />;
}

function Brand({ compact }: { compact?: boolean }) {
  const { t } = useI18n();
  return (
    <div className="flex items-center gap-2.5">
      <BrandMark />
      <div className={clsx(compact && "hidden sm:block")}>
        <div className="text-sm font-bold leading-tight text-slate-100">kino.pub</div>
        <div className="text-[11px] leading-tight text-slate-500">{t("downloader")}</div>
      </div>
    </div>
  );
}

// ProfileCard shows the signed-in account with subscription days, or a sign-in
// prompt when logged out. Both states collapse to a single icon/avatar.
function ProfileCard({
  collapsed,
  kpauth,
  kpUser,
  kpUserError,
  onClick,
}: {
  collapsed: boolean;
  kpauth: KPStatus;
  kpUser: KPUser | null;
  kpUserError: boolean;
  onClick: () => void;
}) {
  const { t } = useI18n();

  if (!kpauth.loggedIn) {
    return (
      <button
        onClick={onClick}
        title={collapsed ? t("Sign in") : undefined}
        className={clsx(
          "mb-2 flex items-center gap-2.5 rounded-xl border border-gold-500/25 bg-gold-500/[0.08] p-2.5 text-gold-300 transition hover:bg-gold-500/[0.16]",
          collapsed && "justify-center",
        )}
      >
        <ShieldAlert className="h-5 w-5 shrink-0" />
        {!collapsed && <span className="text-sm font-medium">{t("Sign in")}</span>}
      </button>
    );
  }

  // Logged in, but the profile (and therefore subscription) isn't known yet:
  // either the first fetch is in flight or the kino.pub host is unreachable
  // (e.g. VPN off). Say so honestly rather than defaulting to "No subscription".
  if (!kpUser) {
    const status = kpUserError ? t("Can't reach kino.pub") : t("Checking subscription…");
    return (
      <button
        onClick={onClick}
        title={collapsed ? `${t("Signed in")} · ${status}` : undefined}
        className={clsx(
          "mb-2 flex items-center gap-2.5 rounded-xl border border-white/[0.06] bg-white/[0.03] p-2 text-left transition hover:bg-white/[0.06]",
          collapsed && "justify-center",
        )}
      >
        <div
          className={clsx(
            "grid h-9 w-9 shrink-0 place-items-center rounded-full bg-ink-800 ring-2",
            kpUserError ? "ring-amber-400/60" : "ring-slate-600/60",
          )}
        >
          {kpUserError ? (
            <WifiOff className="h-[18px] w-[18px] text-amber-300" />
          ) : (
            <Loader2 className="h-[18px] w-[18px] animate-spin text-slate-400" />
          )}
        </div>
        {!collapsed && (
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-semibold text-slate-200">{t("Signed in")}</div>
            <div className={clsx("text-[11px] font-medium", kpUserError ? "text-amber-300" : "text-slate-500")}>
              {status}
            </div>
          </div>
        )}
      </button>
    );
  }

  const active = kpUser.subscriptionActive;
  const days = kpUser.subscriptionDays;
  const name = kpUser.username || t("Signed in");
  const ring = !active ? "ring-ember-500/60" : days <= 14 ? "ring-amber-400/70" : "ring-emerald-400/70";
  const subText = !active ? "text-ember-400" : days <= 14 ? "text-amber-300" : "text-emerald-400";

  return (
    <button
      onClick={onClick}
      title={collapsed ? `${name} · ${active ? t("{n} days left", { n: days }) : t("No subscription")}` : undefined}
      className={clsx(
        "mb-2 flex items-center gap-2.5 rounded-xl border border-white/[0.06] bg-white/[0.03] p-2 text-left transition hover:bg-white/[0.06]",
        collapsed && "justify-center",
      )}
    >
      <div className={clsx("grid h-9 w-9 shrink-0 place-items-center rounded-full bg-ink-800 ring-2", ring)}>
        <UserIcon className="h-[18px] w-[18px] text-slate-300" />
      </div>
      {!collapsed && (
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-semibold text-slate-200">{name}</div>
          <div className={clsx("text-[11px] font-medium", subText)}>
            {active ? t("{n} days left", { n: days }) : t("No subscription")}
          </div>
        </div>
      )}
    </button>
  );
}

function SystemFooter({
  ffmpegFound,
  version,
  connected,
  collapsed,
}: {
  ffmpegFound: boolean;
  version: string;
  connected: boolean;
  collapsed: boolean;
}) {
  const { t } = useI18n();
  if (collapsed) {
    return (
      <div className="mt-2 flex flex-col items-center gap-2 border-t border-white/[0.06] pt-3">
        <span
          title={connected ? t("App connected") : t("Reconnecting to app…")}
          className={clsx(
            "grid h-7 w-7 place-items-center rounded-lg",
            connected ? "bg-emerald-500/10 text-emerald-400" : "bg-amber-400/10 text-amber-400 animate-pulse-soft",
          )}
        >
          {connected ? <PlugZap className="h-3.5 w-3.5" /> : <Unplug className="h-3.5 w-3.5" />}
        </span>
        <span
          title={ffmpegFound ? t("ffmpeg ready") : t("ffmpeg missing")}
          className={clsx(
            "grid h-7 w-7 place-items-center rounded-lg",
            ffmpegFound ? "bg-emerald-500/10 text-emerald-400" : "bg-ember-500/10 text-ember-400",
          )}
        >
          <Film className="h-3.5 w-3.5" />
        </span>
      </div>
    );
  }
  return (
    <div className="mt-2 space-y-1.5 border-t border-white/[0.06] pt-3">
      <div className="space-y-0.5 rounded-xl border border-white/[0.06] bg-white/[0.02] p-1.5">
        {/* Link to the local app backend (SSE) — not the kino.pub/internet
            connection. Green means this page is receiving live updates. */}
        <div className="flex items-center gap-2.5 px-1.5 py-1">
          <span
            className={clsx(
              "grid h-7 w-7 shrink-0 place-items-center rounded-lg",
              connected ? "bg-emerald-500/10 text-emerald-400" : "bg-amber-400/10 text-amber-400",
            )}
          >
            {connected ? <PlugZap className="h-4 w-4" /> : <Unplug className="h-4 w-4" />}
          </span>
          <span className="flex-1 text-[13px] font-medium text-slate-300">
            {connected ? t("App connected") : t("Reconnecting to app…")}
          </span>
          <span
            className={clsx(
              "h-1.5 w-1.5 rounded-full",
              connected
                ? "bg-emerald-400 shadow-[0_0_6px_1px_rgba(52,211,153,0.6)]"
                : "bg-amber-400 animate-pulse-soft",
            )}
          />
        </div>
        {/* ffmpeg */}
        <div className="flex items-center gap-2.5 px-1.5 py-1">
          <span
            className={clsx(
              "grid h-7 w-7 shrink-0 place-items-center rounded-lg",
              ffmpegFound ? "bg-emerald-500/10 text-emerald-400" : "bg-ember-500/10 text-ember-400",
            )}
          >
            <Film className="h-4 w-4" />
          </span>
          <span className="flex-1 text-[13px] font-medium text-slate-300">
            {ffmpegFound ? t("ffmpeg ready") : t("ffmpeg missing")}
          </span>
          {!ffmpegFound && <span className="h-1.5 w-1.5 rounded-full bg-ember-500" />}
        </div>
      </div>
      <div className="text-center text-[11px] text-slate-600">{version}</div>
    </div>
  );
}
