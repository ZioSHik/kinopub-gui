import { useState, type ReactNode } from "react";
import { Chrome, Compass, Flame, KeyRound, LogOut, ShieldCheck } from "lucide-react";
import { api } from "../api";
import { useApp } from "../store";
import { useI18n } from "../i18n";
import { Field, Spinner } from "./ui";

// Yandex Browser has no lucide icon — render its recognizable red "Я" mark.
function YandexMark() {
  return (
    <span className="grid h-5 w-5 place-items-center rounded-[5px] bg-red-600 text-[13px] font-bold leading-none text-white">
      Я
    </span>
  );
}

const browsers: { id: string; label: string; icon: ReactNode }[] = [
  { id: "safari", label: "Safari", icon: <Compass className="h-5 w-5 text-gold-400" /> },
  { id: "chrome", label: "Chrome", icon: <Chrome className="h-5 w-5 text-gold-400" /> },
  { id: "firefox", label: "Firefox", icon: <Flame className="h-5 w-5 text-gold-400" /> },
  { id: "yandex", label: "Yandex", icon: <YandexMark /> },
  { id: "auto", label: "Auto", icon: <ShieldCheck className="h-5 w-5 text-gold-400" /> },
];

export function LoginPanel({ onDone }: { onDone?: () => void }) {
  const { auth, setAuthLocal, toast } = useApp();
  const { t } = useI18n();
  const [cookie, setCookie] = useState("");
  // Default to THIS browser's User-Agent. cf_clearance is bound to the UA that
  // solved Cloudflare's challenge — if you open this app in the same browser you
  // import cookies from, the UA matches exactly and requests pass.
  const [userAgent, setUserAgent] = useState(() => navigator.userAgent);
  const [busy, setBusy] = useState(false);

  const submit = async (browser: string) => {
    setBusy(true);
    try {
      const st = await api.login({
        cookie: browser ? "" : cookie.trim(),
        userAgent: userAgent.trim(),
        browser,
      });
      setAuthLocal(st);
      toast(t("Credentials saved"), "success");
      setCookie("");
      onDone?.();
    } catch (e: any) {
      toast(e.message || t("Login failed"), "error");
    } finally {
      setBusy(false);
    }
  };

  const logout = async () => {
    setBusy(true);
    try {
      const st = await api.logout();
      setAuthLocal(st);
      toast(t("Logged out"), "info");
    } catch (e: any) {
      toast(e.message || t("Logout failed"), "error");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-5">
      {auth.loggedIn && (
        <div className="flex items-center justify-between gap-3 rounded-xl border border-emerald-500/20 bg-emerald-500/[0.07] px-4 py-3">
          <div className="min-w-0">
            <p className="flex items-center gap-2 text-sm font-medium text-emerald-300">
              <ShieldCheck className="h-4 w-4" /> {t("Signed in")}
            </p>
            <p className="mt-1 truncate text-xs text-slate-400">
              {t("Cookie {preview} · {n} keys", {
                preview: auth.cookiePreview || "",
                n: auth.cookieKeys?.length || 0,
              })}
            </p>
          </div>
          <button className="btn-danger" onClick={logout} disabled={busy}>
            <LogOut className="h-4 w-4" /> {t("Logout")}
          </button>
        </div>
      )}

      <div className="rounded-xl border border-white/[0.06] bg-ink-900/40 p-4 text-sm text-slate-400">
        <p className="mb-1 font-medium text-slate-300">{t("Why this is needed")}</p>
        {t(
          "kino.pub sits behind Cloudflare. Paste the Cookie header from a logged-in browser session (DevTools → Network → request headers), or auto-import it from a browser below. The User-Agent must match the browser that issued the cookies.",
        )}
      </div>

      <Field label={t("Cookie header")} hint={t("e.g. cf_clearance=…; _identity=…; PHPSESSID=…")}>
        <textarea
          className="input min-h-[80px] font-mono text-xs"
          placeholder="cf_clearance=...; _identity=..."
          value={cookie}
          onChange={(e) => setCookie(e.target.value)}
        />
      </Field>

      <Field
        label={t("User-Agent")}
        hint={t(
          "Pre-filled with this browser's UA. It must match the browser the cookies came from — for browser import, open this app in that same browser.",
        )}
      >
        <input
          className="input font-mono text-xs"
          placeholder="Mozilla/5.0 (Macintosh; …) Safari/605.1.15"
          value={userAgent}
          onChange={(e) => setUserAgent(e.target.value)}
        />
      </Field>

      <div className="flex flex-wrap items-center gap-3">
        <button className="btn-primary" onClick={() => submit("")} disabled={busy || !cookie.trim()}>
          {busy ? <Spinner className="h-4 w-4" /> : <KeyRound className="h-4 w-4" />}
          {t("Save cookie")}
        </button>
      </div>

      <div>
        <p className="label">{t("Or import from a browser")}</p>
        <div className="grid grid-cols-3 gap-2 sm:grid-cols-5">
          {browsers.map((b) => (
            <button
              key={b.id}
              className="btn-ghost flex-col gap-1.5 py-3"
              onClick={() => submit(b.id)}
              disabled={busy}
            >
              {b.icon}
              <span className="text-xs">{b.label}</span>
            </button>
          ))}
        </div>
        <p className="mt-2 text-xs text-slate-500">
          {t(
            "Tip: open this app in the same browser you import from — the User-Agent above then matches the imported cf_clearance, which Cloudflare requires. On macOS, allow Keychain access when prompted (Yandex/Chrome) or grant Full Disk Access (Safari).",
          )}
        </p>
      </div>
    </div>
  );
}
