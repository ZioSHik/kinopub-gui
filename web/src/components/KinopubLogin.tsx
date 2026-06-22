import { useState } from "react";
import { CheckCircle2, ExternalLink, KeyRound, Loader2, LogOut } from "lucide-react";
import { api } from "../api";
import { useApp } from "../store";
import { useI18n } from "../i18n";

// KinopubLogin drives the official-API device-code sign-in. The server polls in
// the background and broadcasts a "kpauth" event on success, which flips the
// store to loggedIn — so this component only kicks off the flow and renders the
// activation code while pending.
export function KinopubLogin() {
  const { kpauth, setKpAuthLocal, toast } = useApp();
  const { t } = useI18n();
  const [starting, setStarting] = useState(false);

  const start = async () => {
    setStarting(true);
    try {
      const st = await api.kpLogin();
      setKpAuthLocal(st);
      toast(t("Enter the code on kino.pub/device to finish signing in"), "info");
    } catch (e: any) {
      toast(e.message || t("Login failed"), "error");
    } finally {
      setStarting(false);
    }
  };

  const logout = async () => {
    try {
      const st = await api.kpLogout();
      setKpAuthLocal(st);
      toast(t("Logged out"), "success");
    } catch (e: any) {
      toast(e.message || t("Logout failed"), "error");
    }
  };

  return (
    <div className="card p-5">
      <h2 className="mb-1 flex items-center gap-2 text-sm font-semibold text-slate-200">
        <KeyRound className="h-4 w-4 text-gold-400" /> {t("kino.pub account (API)")}
      </h2>
      <p className="mb-4 text-xs text-slate-500">
        {t("Sign in once with a device code to search the catalog, preview voiceovers, and download titles.")}
      </p>

      {kpauth.loggedIn ? (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-emerald-500/25 bg-emerald-500/[0.07] px-4 py-3">
          <span className="flex items-center gap-2 text-sm font-medium text-emerald-300">
            <CheckCircle2 className="h-4 w-4" /> {t("Signed in to kino.pub")}
          </span>
          <button className="btn-ghost px-3 py-1.5 text-xs" onClick={logout}>
            <LogOut className="h-3.5 w-3.5" /> {t("Logout")}
          </button>
        </div>
      ) : kpauth.pending && kpauth.userCode ? (
        <div className="space-y-3 rounded-xl border border-gold-500/25 bg-gold-500/[0.06] p-4">
          <p className="text-sm text-slate-300">
            {t("Open the link and enter this code:")}
          </p>
          <div className="flex flex-wrap items-center gap-3">
            <code className="select-all rounded-lg bg-ink-950/60 px-4 py-2 font-mono text-2xl font-bold tracking-[0.3em] text-gold-300">
              {kpauth.userCode}
            </code>
            <a
              href={kpauth.verificationUri || "https://kino.pub/device"}
              target="_blank"
              rel="noreferrer"
              className="btn-ghost px-3 py-2 text-sm"
            >
              <ExternalLink className="h-4 w-4" /> {kpauth.verificationUri || "kino.pub/device"}
            </a>
          </div>
          <p className="flex items-center gap-2 text-xs text-slate-500">
            <Loader2 className="h-3.5 w-3.5 animate-spin" /> {t("Waiting for confirmation…")}
          </p>
          {kpauth.error && <p className="text-xs text-ember-400">{kpauth.error}</p>}
        </div>
      ) : (
        <div className="space-y-3">
          <button className="btn-primary" onClick={start} disabled={starting}>
            {starting ? <Loader2 className="h-4 w-4 animate-spin" /> : <KeyRound className="h-4 w-4" />}
            {t("Sign in to kino.pub")}
          </button>
          {kpauth.error && <p className="text-xs text-ember-400">{kpauth.error}</p>}
        </div>
      )}
    </div>
  );
}
