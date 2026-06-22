import { useState } from "react";
import clsx from "clsx";
import {
  AlertTriangle,
  Ban,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Clock,
  Hourglass,
  Loader2,
  ScrollText,
  Trash2,
  XCircle,
} from "lucide-react";
import { api, type EpisodeView, type JobView } from "../api";
import { useApp } from "../store";
import { useI18n, looksLikeTimeout } from "../i18n";
import { bytes, clockTime, eta, relTime, speed } from "../lib/format";
import { PosterImage, ProgressBar } from "./ui";

function StatusBadge({ status }: { status: JobView["status"] }) {
  const { t } = useI18n();
  const map: Record<JobView["status"], { label: string; cls: string; icon: any; spin?: boolean }> = {
    queued: { label: "Queued", cls: "border-white/10 bg-white/[0.04] text-slate-400", icon: Clock },
    resolving: { label: "Resolving", cls: "border-gold-500/30 bg-gold-500/10 text-gold-300", icon: Loader2, spin: true },
    running: { label: "Downloading", cls: "border-gold-500/30 bg-gold-500/10 text-gold-300", icon: Loader2, spin: true },
    completed: { label: "Completed", cls: "border-emerald-500/25 bg-emerald-500/10 text-emerald-300", icon: CheckCircle2 },
    failed: { label: "Failed", cls: "border-ember-500/30 bg-ember-500/10 text-ember-400", icon: XCircle },
    canceled: { label: "Canceled", cls: "border-white/10 bg-white/[0.04] text-slate-400", icon: Ban },
  };
  const it = map[status];
  return (
    <span className={clsx("chip", it.cls)}>
      <it.icon className={clsx("h-3.5 w-3.5", it.spin && "animate-spin")} />
      {t(it.label)}
    </span>
  );
}

function epVariant(state: EpisodeView["state"]) {
  switch (state) {
    case "completed":
      return "green" as const;
    case "failed":
      return "rose" as const;
    case "deferred":
      return "blue" as const;
    default:
      return "gold" as const;
  }
}

function EpisodeRow({ ep }: { ep: EpisodeView }) {
  const { t } = useI18n();
  const active = ep.state === "running";
  return (
    <div className="rounded-xl border border-white/[0.05] bg-ink-900/40 px-3 py-2.5">
      <div className="flex items-center gap-3">
        <span
          className={clsx(
            "grid h-7 w-7 shrink-0 place-items-center rounded-lg text-[11px] font-semibold",
            ep.state === "completed" && "bg-emerald-500/15 text-emerald-300",
            ep.state === "failed" && "bg-ember-500/15 text-ember-400",
            ep.state === "deferred" && "bg-sky-500/15 text-sky-300",
            (ep.state === "running" || ep.state === "pending") && "bg-white/[0.05] text-slate-400",
          )}
        >
          {ep.state === "completed" ? (
            <CheckCircle2 className="h-4 w-4" />
          ) : ep.state === "failed" ? (
            <XCircle className="h-4 w-4" />
          ) : ep.state === "deferred" ? (
            <Hourglass className="h-3.5 w-3.5" />
          ) : ep.state === "running" ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <span className="font-mono">{ep.key.replace("S", "").replace("E", ".")}</span>
          )}
        </span>

        <div className="min-w-0 flex-1">
          <div className="flex items-center justify-between gap-2">
            <span className="truncate text-sm text-slate-200">
              <span className="font-mono text-xs text-slate-500">{ep.key}</span>{" "}
              {ep.title || ""}
            </span>
            <span className="shrink-0 text-xs tabular-nums text-slate-400">
              {ep.state === "completed" ? "100%" : `${ep.percent}%`}
            </span>
          </div>
          <ProgressBar
            value={ep.state === "completed" ? 100 : ep.percent}
            variant={epVariant(ep.state)}
            active={active}
            className="mt-1.5"
          />
          <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-slate-500">
            {ep.segTotal > 0 && <span>{ep.segDone}/{ep.segTotal} seg</span>}
            {ep.total > 0 && <span>{bytes(ep.bytes)} / {bytes(ep.total)}</span>}
            {active && ep.speedBps > 0 && <span className="text-gold-400/90">{speed(ep.speedBps)}</span>}
            {active && ep.etaSeconds > 0 && <span>{t("ETA")} {eta(ep.etaSeconds, t)}</span>}
            {ep.state === "deferred" && (
              <span className="text-sky-400">{t("retrying (attempt {n})", { n: ep.attempts })}</span>
            )}
            {ep.error && (ep.state === "failed" || ep.state === "deferred") && (
              <span className="truncate text-ember-400/80" title={ep.error}>{ep.error}</span>
            )}
          </div>

          {ep.tracks && ep.tracks.length > 0 && active && (
            <div className="mt-2 space-y-1.5 border-l border-white/[0.06] pl-3">
              {ep.tracks.map((tr, i) => (
                <div key={i}>
                  <div className="flex items-center justify-between text-[11px] text-slate-500">
                    <span className="truncate">{tr.label}</span>
                    <span className="tabular-nums">{tr.percent}%</span>
                  </div>
                  <ProgressBar value={tr.percent} variant="slate" className="mt-0.5 h-1" />
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

export function JobCard({ job }: { job: JobView }) {
  const { toast } = useApp();
  const { t } = useI18n();
  const [showEps, setShowEps] = useState(job.status === "running" || job.status === "resolving");
  const [showLogs, setShowLogs] = useState(false);
  const [busy, setBusy] = useState(false);

  const finished = ["completed", "failed", "canceled"].includes(job.status);
  // The engine plans the full selection up front (job.plan.total) but only adds
  // episode rows to the view as each one starts (concurrency-limited). Use the
  // plan total as the denominator so a multi-episode selection reads "0/2", not
  // "0/1", while the rest are still pending. Fall back to the visible rows only
  // before the plan resolves.
  const visibleEps = job.episodes.length;
  const totalEps = job.plan && job.plan.total > 0 ? job.plan.total : visibleEps;
  const doneEps = job.episodes.filter((e) => e.state === "completed").length;
  const runningPartial = job.episodes
    .filter((e) => e.state === "running")
    .reduce((acc, e) => acc + e.percent / 100, 0);
  const overall =
    totalEps > 0 ? Math.min(100, ((doneEps + runningPartial) / totalEps) * 100) : finished ? 100 : 0;

  const timedOut = looksLikeTimeout(job.error);
  const errorText = timedOut
    ? t("Request timed out — kino.pub may be unreachable without a VPN. Enable a VPN or set a proxy, then retry.")
    : job.error;

  const cancel = async () => {
    setBusy(true);
    try {
      await api.cancelJob(job.id);
      toast(t("Stopping job…"), "info");
    } catch (e: any) {
      toast(e.message || "Error", "error");
    } finally {
      setBusy(false);
    }
  };
  const remove = async () => {
    setBusy(true);
    try {
      await api.deleteJob(job.id);
    } catch (e: any) {
      toast(e.message || "Error", "error");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="card animate-fade-in overflow-hidden">
      <div className="flex gap-4 p-4">
        <PosterImage
          url={job.posterUrl}
          alt={job.title}
          className="hidden aspect-[2/3] w-16 shrink-0 rounded-lg border border-white/[0.08] sm:block"
        />
        <div className="min-w-0 flex-1">
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <h3 className="truncate text-base font-semibold text-slate-100">
                  {job.title || job.url}
                </h3>
                {job.dryRun && (
                  <span className="chip border-sky-500/25 bg-sky-500/10 text-sky-300">{t("dry-run")}</span>
                )}
              </div>
              <p className="mt-0.5 truncate font-mono text-xs text-slate-500">{job.url}</p>
            </div>
            <StatusBadge status={job.status} />
          </div>

          <div className="mt-3">
            <div className="mb-1.5 flex items-center justify-between text-xs text-slate-400">
              <span>
                {job.summary
                  ? t("{ok} ok · {failed} failed · {skipped} skipped", {
                      ok: job.summary.succeeded,
                      failed: job.summary.failed,
                      skipped: job.summary.skipped,
                    })
                  : totalEps > 0
                    ? t("{done}/{total} episodes", { done: doneEps, total: totalEps })
                    : job.status === "resolving"
                      ? t("Resolving source…")
                      : t("Preparing…")}
              </span>
              <span className="tabular-nums">{Math.round(overall)}%</span>
            </div>
            <ProgressBar
              value={overall}
              variant={job.status === "failed" ? "rose" : job.status === "completed" ? "green" : "gold"}
              active={job.status === "running" || job.status === "resolving"}
            />
          </div>

          {errorText && (
            <p className={clsx("mt-2 flex items-start gap-1.5 text-xs", timedOut ? "text-gold-300" : "text-ember-400")}>
              <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              <span className="break-words">{errorText}</span>
            </p>
          )}

          <div className="mt-3 flex flex-wrap items-center gap-2 text-xs">
            <span className="text-slate-500">
              {job.startedAt ? `${t("started")} ${relTime(job.startedAt, t)}` : `${t("created")} ${relTime(job.createdAt, t)}`}
              {job.quality ? ` · ${job.quality}` : ""}
            </span>
            <div className="ml-auto flex items-center gap-2">
              {totalEps > 0 && (
                <button className="btn-ghost px-3 py-1.5" onClick={() => setShowEps((v) => !v)}>
                  {showEps ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
                  {t("Episodes ({n})", { n: totalEps })}
                </button>
              )}
              {job.logs.length > 0 && (
                <button className="btn-ghost px-3 py-1.5" onClick={() => setShowLogs((v) => !v)}>
                  <ScrollText className="h-3.5 w-3.5" /> {t("Log")}
                </button>
              )}
              {!finished ? (
                <button className="btn-danger px-3 py-1.5" onClick={cancel} disabled={busy}>
                  <Ban className="h-3.5 w-3.5" /> {t("Stop")}
                </button>
              ) : (
                <button className="btn-ghost px-3 py-1.5" onClick={remove} disabled={busy}>
                  <Trash2 className="h-3.5 w-3.5" /> {t("Remove")}
                </button>
              )}
            </div>
          </div>
        </div>
      </div>

      {showEps && totalEps > 0 && (
        <div className="grid gap-2 border-t border-white/[0.05] bg-black/20 p-4 md:grid-cols-2">
          {job.episodes.map((ep) => (
            <EpisodeRow key={ep.key} ep={ep} />
          ))}
        </div>
      )}

      {showLogs && job.logs.length > 0 && (
        <div className="max-h-64 overflow-y-auto border-t border-white/[0.05] bg-black/40 p-4 font-mono text-[11px] leading-relaxed">
          {job.logs.map((l, i) => (
            <div key={i} className="flex gap-2">
              <span className="shrink-0 text-slate-600">{clockTime(l.time)}</span>
              <span
                className={clsx(
                  "shrink-0 font-semibold",
                  l.level === "ERROR" && "text-ember-400",
                  l.level === "WARN" && "text-gold-400",
                  l.level === "INFO" && "text-sky-400",
                  l.level === "DEBUG" && "text-slate-600",
                )}
              >
                {l.level}
              </span>
              {l.component && <span className="shrink-0 text-slate-600">[{l.component}]</span>}
              <span className="text-slate-300">{l.message}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
