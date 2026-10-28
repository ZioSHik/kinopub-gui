import { useEffect, useState } from "react";
import clsx from "clsx";
import {
  ArrowUpCircle,
  Bookmark,
  ChevronRight,
  Clapperboard,
  Film,
  Flame,
  History,
  LayoutGrid,
  Library as LibraryIcon,
  Loader2,
  PanelLeftClose,
  PanelLeftOpen,
  PlayCircle,
  Settings as SettingsIcon,
  ShieldAlert,
  Unplug,
  User as UserIcon,
  WifiOff,
} from "lucide-react";
import { useApp } from "./store";
import type { KPStatus, KPUser } from "./api";
import { type Page, legacyPage, pushRoute, useRoute } from "./router";
import { useI18n } from "./i18n";
import { TopsPage } from "./pages/Tops";
import { DiscoverPage } from "./pages/Discover";
import { CollectionsPage } from "./pages/Collections";
import { WatchingPage } from "./pages/Watching";
import { BookmarksPage } from "./pages/Bookmarks";
import { HistoryPage } from "./pages/History";
import { DownloadPage } from "./pages/Download";
import { LibraryPage } from "./pages/Library";
import { DoctorPage } from "./pages/Doctor";
import { SettingsPage } from "./pages/Settings";
import { ProfilePage } from "./pages/Profile";
import { AudioMenuModal } from "./components/AudioMenuModal";
import { Toasts } from "./components/Toasts";

type NavItem = { id: Page; label: string; icon: any };

// The rail carries only the places you go to *do* something day to day. Doctor
// is a maintenance tool you reach for a few times a year, so it lost its
// permanent slot and now hangs off Settings — the route still exists, so old
// links and bookmarks ("#/doctor") keep working.
// The Queue used to be its own entry. It was merged into the Library: an active
// download and the file it produces are the same title, and splitting them meant
// a finished download was listed in both places at once. The "queue" route still
// resolves (old links, bookmarks) — it just lands on the Library.
// Collections, I'm watching, Bookmarks and History were chips buried inside the
// Catalog, sharing a page with a search box and filters that applied to none of
// them. Each is a destination in its own right, so each got a rail entry — one
// click from anywhere, and linkable.
//
// Six entries is more than a flat list carries comfortably, so they're grouped by
// what they answer: browse kino.watch → your lists on kino.watch → files on this
// machine. A hairline between groups is enough; the groups need no headings.
const NAV: NavItem[][] = [
  [
    { id: "tops", label: "What's new", icon: Flame },
    { id: "discover", label: "Catalog", icon: Clapperboard },
    { id: "collections", label: "Collections", icon: LayoutGrid },
  ],
  [
    { id: "watching", label: "I'm watching", icon: PlayCircle },
    { id: "bookmarks", label: "Bookmarks", icon: Bookmark },
    { id: "history", label: "History", icon: History },
  ],
  [{ id: "library", label: "Offline library", icon: LibraryIcon }],
];

// The mobile top bar is a single scrolling row, so it takes the same entries
// flattened — a hairline divider would be invisible there anyway.
const NAV_FLAT: NavItem[] = NAV.flat();

// Settings is its own nav entry, sitting just under the account card at the
// bottom of the rail — it used to be reachable only by clicking that card, which
// made the profile and the app preferences fight over one target.
const SETTINGS_NAV: NavItem = { id: "settings", label: "Settings", icon: SettingsIcon };

// legacyPage moved into the router (pushRoute needs it to compare RENDERED
// pages before aborting in-flight requests); re-exported so existing imports
// keep working.
export { legacyPage } from "./router";

export default function App() {
  const { connected, ready, version, jobs, kpauth, kpUser, kpUserError, ffmpeg, update } = useApp();
  const { t } = useI18n();
  // The URL hash is the single source of truth for the active page (and, within
  // a page, the open collection/card) so reloads and browser back/forward
  // restore the exact view.
  // Legacy hashes are normalized here, so the rail highlight and the rendered
  // page always agree on where we are:
  //   "#/queue"            → the Library, which absorbed the queue;
  //   "#/discover/b/<id>"  → Bookmarks, and
  //   "#/discover/c/<id>"  → Collections, back when a folder or a подборка
  //                          opened inside the Catalog. Both pages read their id
  //                          off the route regardless of the page segment, so an
  //                          old link still lands on the right one.
  const route = useRoute();
  const rawPage = route.page;
  const page: Page = legacyPage(rawPage, route.bookmarkId, route.collectionId);
  const [collapsed, setCollapsed] = useState<boolean>(() => {
    try { return localStorage.getItem("sidebarCollapsed") === "1"; } catch { return false; }
  });
  const toggleCollapsed = () =>
    setCollapsed((v) => {
      const next = !v;
      try { localStorage.setItem("sidebarCollapsed", next ? "1" : "0"); } catch { /* Keep the session usable when storage is blocked. */ }
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
        <nav className="mt-6 flex-1">
          {NAV.map((group, i) => (
            <div
              key={i}
              className={clsx("space-y-1", i > 0 && "mt-3 border-t border-white/[0.06] pt-3")}
            >
              {group.map((n) => (
                <SideNavItem
                  key={n.id}
                  item={n}
                  collapsed={collapsed}
                  active={page === n.id}
                  badge={n.id === "library" ? activeJobs : 0}
                  onClick={() => navigate(n.id)}
                />
              ))}
            </div>
          ))}
        </nav>
        <ProfileCard
          collapsed={collapsed}
          kpauth={kpauth}
          kpUser={kpUser}
          kpUserError={kpUserError}
          active={page === "profile"}
          onClick={() => navigate("profile")}
        />
        <nav className="space-y-1">
          <SideNavItem
            item={SETTINGS_NAV}
            collapsed={collapsed}
            active={page === SETTINGS_NAV.id}
            onClick={() => navigate(SETTINGS_NAV.id)}
          />
        </nav>
        <SystemFooter
          ffmpegFound={ffmpeg.ffmpegFound}
          version={version}
          connected={connected}
          ready={ready}
          collapsed={collapsed}
          updateLatest={update?.updateAvailable ? update.latest || "" : ""}
          onOpenSettings={() => navigate("settings")}
        />
      </aside>

      {/* Main */}
      <div className="flex min-w-0 flex-1 flex-col">
        {/* Mobile top bar — the desktop rail is hidden here, so this row carries the
            brand, the same nav entries, and the update nudge. Desktop has no top
            bar at all: the rail already holds nav, account, health and updates,
            and the language switch now lives in Settings. */}
        <div className="flex items-center gap-2 border-b border-white/[0.06] px-3 py-2 md:hidden">
          <Brand compact />
          <nav className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto">
            {[...NAV_FLAT, SETTINGS_NAV].map((n) => (
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
          {update?.updateAvailable && (
            <button
              onClick={() => navigate("settings")}
              className="chip shrink-0 border-gold-500/30 bg-gold-500/[0.12] text-gold-300 hover:bg-gold-500/[0.2]"
              title={t("A new version is available")}
            >
              <ArrowUpCircle className="h-3.5 w-3.5" />
              <span className="hidden sm:inline">{t("Update {v}", { v: update.latest || "" })}</span>
            </button>
          )}
        </div>

        <main className="flex-1 px-4 py-6 md:px-8 md:py-8">
          {/* Starting a download used to jump to the Library, which threw away
              whatever the user was browsing after a single click. Queueing is a
              background act, so no page moves the user anywhere any more: the
              title card stays open and reports the outcome on its own download
              button, and the rail badge counts the running jobs for anyone who
              wants to look. */}
          {page === "tops" && (
            <TopsPage onSignIn={() => navigate("profile")} onOpenSettings={() => navigate("settings")} />
          )}
          {page === "discover" && (
            <DiscoverPage onSignIn={() => navigate("profile")} onOpenSettings={() => navigate("settings")} />
          )}
          {page === "collections" && (
            <CollectionsPage onSignIn={() => navigate("profile")} onOpenSettings={() => navigate("settings")} />
          )}
          {page === "watching" && (
            <WatchingPage onSignIn={() => navigate("profile")} onOpenSettings={() => navigate("settings")} />
          )}
          {page === "bookmarks" && (
            <BookmarksPage onSignIn={() => navigate("profile")} onOpenSettings={() => navigate("settings")} />
          )}
          {page === "history" && (
            <HistoryPage onSignIn={() => navigate("profile")} onOpenSettings={() => navigate("settings")} />
          )}
          {page === "download" && <DownloadPage onSignIn={() => navigate("profile")} />}
          {page === "library" && <LibraryPage onNew={() => navigate("download")} />}
          {page === "doctor" && <DoctorPage />}
          {page === "settings" && <SettingsPage />}
          {page === "profile" && <ProfilePage />}
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
  return <img src="./favicon.svg" alt="kino.watch" className="h-9 w-9 shrink-0 rounded-xl shadow-glow" />;
}

function Brand({ compact }: { compact?: boolean }) {
  const { t } = useI18n();
  return (
    <div className="flex items-center gap-2.5">
      <BrandMark />
      <div className={clsx(compact && "hidden sm:block")}>
        <div className="text-sm font-bold leading-tight text-slate-100">kino.watch</div>
        <div className="text-[11px] leading-tight text-slate-500">{t("downloader")}</div>
      </div>
    </div>
  );
}

// SideNavItem is one row of the desktop rail. Shared by the main nav list and
// the pinned Settings entry so both stay pixel-identical.
function SideNavItem({
  item,
  collapsed,
  active,
  badge = 0,
  onClick,
}: {
  item: NavItem;
  collapsed: boolean;
  active: boolean;
  badge?: number;
  onClick: () => void;
}) {
  const { t } = useI18n();
  return (
    <button
      onClick={onClick}
      title={collapsed ? t(item.label) : undefined}
      className={clsx("nav-item relative w-full", collapsed && "justify-center px-0", active && "nav-item-active")}
    >
      <item.icon className="h-[18px] w-[18px] shrink-0" />
      {!collapsed && <span className="flex-1 text-left">{t(item.label)}</span>}
      {badge > 0 &&
        (collapsed ? (
          <span className="absolute right-1 top-1 h-2 w-2 rounded-full bg-gold-500" />
        ) : (
          <span className="inline-flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full bg-gold-500 px-1.5 text-[10px] font-bold leading-none text-ink-950">
            {badge}
          </span>
        ))}
    </button>
  );
}

// ProfileCard shows the signed-in account with subscription days, or a sign-in
// prompt when logged out. Both states collapse to a single icon/avatar, and both
// lead to the Profile page — app preferences have their own nav entry.
function ProfileCard({
  collapsed,
  kpauth,
  kpUser,
  kpUserError,
  active: isActive,
  onClick,
}: {
  collapsed: boolean;
  kpauth: KPStatus;
  kpUser: KPUser | null;
  kpUserError: boolean;
  active: boolean;
  onClick: () => void;
}) {
  const { t } = useI18n();

  if (!kpauth.loggedIn) {
    // A bare "Sign in" button gave no hint where it leads. Spell the destination
    // out with a subtitle and a chevron.
    return (
      <button
        onClick={onClick}
        title={collapsed ? t("Sign in in Profile") : undefined}
        className={clsx(
          "mb-2 flex items-center gap-2.5 rounded-xl border border-gold-500/25 bg-gold-500/[0.08] p-2.5 text-gold-300 transition hover:bg-gold-500/[0.16]",
          isActive && "ring-1 ring-gold-500/40",
          collapsed && "justify-center",
        )}
      >
        <ShieldAlert className="h-5 w-5 shrink-0" />
        {!collapsed && (
          <>
            <span className="min-w-0 flex-1 text-left">
              <span className="block text-sm font-semibold leading-tight">{t("Sign in")}</span>
              <span className="block text-[11px] font-medium leading-tight text-gold-300/70">{t("in Profile")}</span>
            </span>
            <ChevronRight className="h-4 w-4 shrink-0 text-gold-300/60" />
          </>
        )}
      </button>
    );
  }

  // Logged in, but the profile (and therefore subscription) isn't known yet:
  // either the first fetch is in flight or the kino.watch host is unreachable
  // (e.g. VPN off). Say so honestly rather than defaulting to "No subscription".
  if (!kpUser) {
    const status = kpUserError ? t("Can't reach kino.watch") : t("Checking subscription…");
    return (
      <button
        onClick={onClick}
        title={collapsed ? `${t("Signed in")} · ${status}` : undefined}
        className={clsx(
          "mb-2 flex items-center gap-2.5 rounded-xl border border-white/[0.06] bg-white/[0.03] p-2 text-left transition hover:bg-white/[0.06]",
          isActive && "border-gold-500/30 bg-gold-500/[0.1]",
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
        isActive && "border-gold-500/30 bg-gold-500/[0.1]",
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

// OFFLINE_GRACE_MS is how long the SSE link may be down before we say so. It
// outlasts the store's own 1.5s reconnect delay, so a page load (which starts
// disconnected) and a routine reconnect both pass in silence — only a link
// that stays down gets a banner.
const OFFLINE_GRACE_MS = 3000;

// useSettled reports `value` only after it has held for delayMs — a flag that
// flickers on and off faster than that never reaches the UI.
export function useSettled(value: boolean, delayMs: number): boolean {
  const [settled, setSettled] = useState(false);
  useEffect(() => {
    if (!value) {
      setSettled(false);
      return;
    }
    const id = window.setTimeout(() => setSettled(true), delayMs);
    return () => window.clearTimeout(id);
  }, [value, delayMs]);
  return settled;
}

function SystemFooter({
  ffmpegFound,
  version,
  connected,
  ready,
  collapsed,
  updateLatest,
  onOpenSettings,
}: {
  ffmpegFound: boolean;
  version: string;
  connected: boolean;
  /** False until the first snapshot lands — see the store's `ready`. */
  ready: boolean;
  collapsed: boolean;
  /** Newer version on offer, or "" when the app is up to date. */
  updateLatest: string;
  onOpenSettings: () => void;
}) {
  const { t } = useI18n();
  // A fresh page starts disconnected and with ffmpeg unknown, so rendering those
  // zero values verbatim flashed both alarms for the split second before the
  // first snapshot arrived. Neither problem is reported until it's actually
  // known: ffmpeg waits for the snapshot, the link waits out the grace period.
  const offline = useSettled(!connected, OFFLINE_GRACE_MS);
  const ffmpegMissing = ready && !ffmpegFound;
  // "Everything works" is not news: when the app is live and ffmpeg is present,
  // both facts collapse into one quiet dot next to the version (details in the
  // tooltip). Only a real problem — the page went stale, or ffmpeg is missing
  // and downloads can't run — earns a row of its own, with the fix attached.
  const healthy = ready && connected && ffmpegFound;
  const okTitle = `${t("App connected")} · ${t("ffmpeg ready")}`;

  if (collapsed) {
    return (
      <div className="mt-2 flex flex-col items-center gap-2 border-t border-white/[0.06] pt-3">
        {healthy ? (
          <span
            title={okTitle}
            className="h-1.5 w-1.5 rounded-full bg-emerald-400 shadow-[0_0_6px_1px_rgba(52,211,153,0.6)]"
          />
        ) : (
          <>
            {offline && (
              <span
                title={t("Reconnecting to app…")}
                className="grid h-7 w-7 place-items-center rounded-lg bg-amber-400/10 text-amber-400 animate-pulse-soft"
              >
                <Unplug className="h-3.5 w-3.5" />
              </span>
            )}
            {ffmpegMissing && (
              <button
                onClick={onOpenSettings}
                title={t("ffmpeg missing — install it in Settings")}
                className="grid h-7 w-7 place-items-center rounded-lg bg-ember-500/10 text-ember-400 transition hover:bg-ember-500/20"
              >
                <Film className="h-3.5 w-3.5" />
              </button>
            )}
          </>
        )}
        {updateLatest && (
          <button
            onClick={onOpenSettings}
            title={t("Update {v}", { v: updateLatest })}
            className="grid h-7 w-7 place-items-center rounded-lg bg-gold-500/10 text-gold-300 transition hover:bg-gold-500/20"
          >
            <ArrowUpCircle className="h-3.5 w-3.5" />
          </button>
        )}
      </div>
    );
  }

  return (
    <div className="mt-2 space-y-1.5 border-t border-white/[0.06] pt-3">
      {/* The link to the local app backend (SSE) — not the kino.watch/internet
          connection. Surfaced only while it's down, since a live page is the
          normal state and saying so every second teaches the user nothing. */}
      {offline && (
        <div className="flex items-center gap-2.5 rounded-xl border border-amber-400/20 bg-amber-400/[0.07] px-2 py-1.5">
          <Unplug className="h-4 w-4 shrink-0 text-amber-400" />
          <span className="flex-1 text-[12px] font-medium text-amber-200">{t("Reconnecting to app…")}</span>
          <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-amber-400 animate-pulse-soft" />
        </div>
      )}

      {/* ffmpeg missing means downloads can't be assembled at all — the one
          system fact worth a full row, and it comes with its own fix. */}
      {ffmpegMissing && (
        <button
          onClick={onOpenSettings}
          title={t("ffmpeg missing — install it in Settings")}
          className="flex w-full items-center gap-2.5 rounded-xl border border-ember-500/25 bg-ember-500/[0.07] px-2 py-1.5 text-left transition hover:bg-ember-500/[0.14]"
        >
          <Film className="h-4 w-4 shrink-0 text-ember-400" />
          <span className="min-w-0 flex-1">
            <span className="block text-[12px] font-medium leading-tight text-slate-200">{t("ffmpeg missing")}</span>
            <span className="block text-[11px] font-medium leading-tight text-ember-400">{t("Install in Settings")}</span>
          </span>
          <ChevronRight className="h-4 w-4 shrink-0 text-slate-500" />
        </button>
      )}

      {/* The update nudge used to sit in the top header; with that bar gone it
          lands here, next to the version it's about, and leads to the same
          Settings card that performs the update. */}
      {updateLatest && (
        <button
          onClick={onOpenSettings}
          title={t("A new version is available")}
          className="flex w-full items-center gap-2.5 rounded-xl border border-gold-500/25 bg-gold-500/[0.07] px-2 py-1.5 text-left transition hover:bg-gold-500/[0.14]"
        >
          <ArrowUpCircle className="h-4 w-4 shrink-0 text-gold-400" />
          <span className="min-w-0 flex-1">
            <span className="block text-[12px] font-medium leading-tight text-slate-200">{t("Update available")}</span>
            <span className="block truncate text-[11px] font-medium leading-tight text-gold-300/80">{updateLatest}</span>
          </span>
          <ChevronRight className="h-4 w-4 shrink-0 text-slate-500" />
        </button>
      )}

      <div className="flex items-center justify-center gap-1.5 text-[11px] text-slate-600" title={healthy ? okTitle : undefined}>
        {healthy && <span className="h-1.5 w-1.5 rounded-full bg-emerald-400 shadow-[0_0_6px_1px_rgba(52,211,153,0.6)]" />}
        <span>{version}</span>
      </div>
    </div>
  );
}
