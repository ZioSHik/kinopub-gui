import { Fragment, useState, type ReactNode } from "react";
import clsx from "clsx";
import {
  AlertTriangle,
  ArrowUp,
  Ban,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Clock,
  FolderOpen,
  HardDrive,
  Hourglass,
  ListVideo,
  Loader2,
  MonitorPlay,
  Pause,
  Play,
  RotateCw,
  Trash2,
  XCircle,
} from "lucide-react";
import { api, type EpisodeView, type JobView, type Leftovers } from "../api";
import { useApp } from "../store";
import { useI18n, looksLikeCanceled, looksLikeDiskFull, looksLikeTimeout } from "../i18n";
import { bytes, eta, relTime, speed } from "../lib/format";
import { hasOtherActiveJob } from "../lib/queue";
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
    paused: { label: "Paused", cls: "border-amber-500/30 bg-amber-500/10 text-amber-300", icon: Pause },
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
    case "paused":
      return "slate" as const;
    default:
      return "gold" as const;
  }
}

function EpisodeRow({
  ep,
  jobId,
  jobStatus,
}: {
  ep: EpisodeView;
  jobId: string;
  jobStatus: JobView["status"];
}) {
  const { t } = useI18n();
  const { toast } = useApp();
  const [busy, setBusy] = useState(false);
  const jobFinished = jobStatus === "completed" || jobStatus === "failed" || jobStatus === "canceled";
  const jobLive = jobStatus === "running" || jobStatus === "resolving";
  const active = ep.state === "running" && !jobFinished;
  // "Next" belongs to an episode that is waiting, and goes when it stops
  // waiting. It used to be gated on a count of other waiting episodes too, but
  // that count changes the instant a run starts — every episode is briefly
  // pending before the workers pick two up — so the button flashed on and off at
  // exactly the moment the user was looking at it. Tying it to this row's own
  // state costs a button that does nothing when it is the only one queued, and
  // buys one that never moves on its own.
  const canPrioritize = jobLive && (ep.state === "pending" || ep.state === "deferred");
  // Retry shows for an episode that failed mid-run, or any non-completed episode
  // left behind on a finished job. Not on a paused job — there the right action
  // is Resume (which re-attempts the failed episode), so Retry would contradict
  // the pause and start a download while the job is paused.
  const canRetryEp =
    (ep.state === "failed" && jobStatus !== "paused") || (jobFinished && ep.state !== "completed");
  // Per-episode pause holds an episode aside — including one that is actively
  // downloading (its download stops and partial segments are kept). Resume
  // releases it. Only meaningful while the job itself is downloading.
  const canPauseEp =
    jobLive && (ep.state === "pending" || ep.state === "deferred" || ep.state === "running");
  // Resume also has to work on a PAUSED job: pausing the last active episode
  // pauses the job itself, and this button is then the only way to release one
  // episode without restarting the whole thing (the server starts a fresh run
  // scoped to it).
  const canResumeEp = (jobLive || jobStatus === "paused") && ep.state === "paused";
  // Per-episode cancel drops just this episode (siblings keep downloading);
  // unlike a pause it doesn't keep the run alive. Retry can bring it back.
  const canCancelEp =
    jobLive &&
    (ep.state === "pending" || ep.state === "deferred" || ep.state === "running" || ep.state === "paused");

  const act = (fn: () => Promise<unknown>, ok: string) => async () => {
    setBusy(true);
    try {
      await fn();
      if (ok) toast(ok, "info");
    } catch (e: any) {
      toast(e.message || "Error", "error");
    } finally {
      setBusy(false);
    }
  };
  const retryEp = act(() => api.retryEpisode(jobId, ep.season, ep.episode), t("Retrying {ep} — re-downloading…", { ep: ep.key }));
  const prioritizeEp = act(() => api.prioritizeEpisode(jobId, ep.season, ep.episode), t("{ep} moved to the front — downloading next", { ep: ep.key }));
  const pauseEp = act(() => api.pauseEpisode(jobId, ep.season, ep.episode), t("{ep} paused", { ep: ep.key }));
  const resumeEp = act(() => api.resumeEpisode(jobId, ep.season, ep.episode), t("{ep} resumed", { ep: ep.key }));
  const cancelEp = act(() => api.cancelEpisode(jobId, ep.season, ep.episode), t("{ep} canceled — the rest keep downloading", { ep: ep.key }));

  return (
    <div className="rounded-xl border border-white/[0.05] bg-ink-900/40 px-3 py-2.5">
      <div className="flex items-center gap-3">
        <span
          className={clsx(
            "grid h-7 w-7 shrink-0 place-items-center rounded-lg text-[11px] font-semibold",
            ep.state === "completed" && "bg-emerald-500/15 text-emerald-300",
            ep.state === "failed" && "bg-ember-500/15 text-ember-400",
            ep.state === "deferred" && "bg-sky-500/15 text-sky-300",
            ep.state === "paused" && "bg-amber-500/15 text-amber-300",
            (ep.state === "running" || ep.state === "pending") && "bg-white/[0.05] text-slate-400",
          )}
        >
          {ep.state === "completed" ? (
            <CheckCircle2 className="h-4 w-4" />
          ) : ep.state === "failed" ? (
            <XCircle className="h-4 w-4" />
          ) : ep.state === "deferred" ? (
            <Hourglass className="h-3.5 w-3.5" />
          ) : ep.state === "paused" ? (
            <Pause className="h-3.5 w-3.5" />
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
            <span className="flex shrink-0 items-center gap-2">
              {canResumeEp && (
                <button
                  className="btn-ghost px-2 py-0.5 text-amber-300"
                  onClick={resumeEp}
                  disabled={busy}
                  title={t("Resume this episode")}
                >
                  <Play className="h-3.5 w-3.5" /> {t("Resume")}
                </button>
              )}
              {canPauseEp && (
                <button
                  className="btn-ghost px-2 py-0.5 text-amber-300"
                  onClick={pauseEp}
                  disabled={busy}
                  title={t("Pause this episode — hold it in the queue")}
                >
                  <Pause className="h-3.5 w-3.5" /> {t("Pause")}
                </button>
              )}
              {canPrioritize && (
                <button
                  className="btn-ghost px-2 py-0.5 text-gold-300"
                  onClick={prioritizeEp}
                  disabled={busy}
                  title={t("Download this episode next")}
                >
                  <ArrowUp className="h-3.5 w-3.5" /> {t("Next")}
                </button>
              )}
              {canRetryEp && (
                <button
                  className="btn-ghost px-2 py-0.5 text-gold-300"
                  onClick={retryEp}
                  disabled={busy}
                  title={t("Retry this episode now — without waiting for the rest")}
                >
                  <RotateCw className="h-3.5 w-3.5" /> {t("Retry")}
                </button>
              )}
              {canCancelEp && (
                <button
                  className="btn-ghost px-2 py-0.5 text-ember-400"
                  onClick={cancelEp}
                  disabled={busy}
                  title={t("Cancel this episode — the rest keep downloading")}
                >
                  <Ban className="h-3.5 w-3.5" /> {t("Cancel")}
                </button>
              )}
              <span className="text-xs tabular-nums text-slate-400">
                {ep.state === "completed" ? "100%" : `${ep.percent}%`}
              </span>
            </span>
          </div>
          <ProgressBar
            value={ep.state === "completed" ? 100 : ep.percent}
            variant={epVariant(ep.state)}
            active={active}
            className="mt-1.5"
          />
          <EpisodeMeta ep={ep} active={active} />
        </div>
      </div>
    </div>
  );
}

// EpisodeMeta is the live detail of one episode: how much has arrived, how fast,
// what went wrong, and per-track muxing progress. A single-file job (a movie)
// shows it directly on the job card, where it would otherwise be buried behind an
// "Episodes (1)" toggle.
function EpisodeMeta({
  ep,
  active,
  className,
  // A one-file job hoists the episode's error up into the card's own error block,
  // so it must not be printed twice.
  showError = true,
}: {
  ep: EpisodeView;
  active: boolean;
  className?: string;
  showError?: boolean;
}) {
  const { t } = useI18n();
  return (
    <>
      <div className={clsx("mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-slate-500", className)}>
        {ep.segTotal > 0 && <span>{ep.segDone}/{ep.segTotal} seg</span>}
        {ep.total > 0 && (
          <span title={ep.totalApprox ? t("Estimated size — refines as it downloads (HLS has no fixed total)") : undefined}>
            {bytes(ep.bytes)} / {ep.totalApprox ? "~" : ""}{bytes(ep.total)}
          </span>
        )}
        {active && ep.speedBps > 0 && <span className="text-gold-400/90">{speed(ep.speedBps)}</span>}
        {active && ep.etaSeconds > 0 && <span>{t("ETA")} {eta(ep.etaSeconds, t)}</span>}
        {ep.state === "deferred" && (
          <span className="text-sky-400">{t("retrying (attempt {n})", { n: ep.attempts })}</span>
        )}
        {ep.state === "paused" && <span className="text-amber-400">{t("paused")}</span>}
        {showError && ep.error && (ep.state === "failed" || ep.state === "deferred") && (
          // A stop the user asked for is an outcome, not a fault: it reads in the
          // muted meta colour like "paused", never as red error text.
          looksLikeCanceled(ep.error) ? (
            <span className="text-slate-400">{t("Canceled")}</span>
          ) : looksLikeDiskFull(ep.error) ? (
            <span className="text-ember-400/80" title={ep.error}>{t("Not enough disk space. Free space in the download folder, then retry.")}</span>
          ) : (
            <span className="truncate text-ember-400/80" title={ep.error}>{ep.error}</span>
          )
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
    </>
  );
}

export function JobCard({ job }: { job: JobView }) {
  const { toast, jobs } = useApp();
  const { t } = useI18n();
  const [showEps, setShowEps] = useState(job.status === "running" || job.status === "resolving");
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
    .filter((e) => e.state === "running" || e.state === "paused")
    .reduce((acc, e) => acc + e.percent / 100, 0);
  // A one-file job is a movie (or a lone selected episode): its overall bar and
  // that episode's bar are the same number, so an "Episodes (1)" collapsible
  // holding one row is pure noise. Fold the episode's detail into the card and
  // drop the list entirely.
  const single = totalEps === 1 && visibleEps === 1 ? job.episodes[0] : null;
  // For that one file, progress is the episode's own. Counting completed episodes
  // instead reports 0% for a file that stopped halfway, which flatly contradicts
  // the byte counter printed right underneath it.
  const overall = single
    ? single.state === "completed"
      ? 100
      : single.percent
    : totalEps > 0
      ? Math.min(100, ((doneEps + runningPartial) / totalEps) * 100)
      : job.status === "completed"
        ? 100
        : 0;
  // The key only carries information when it isn't the implicit first episode —
  // a movie is always S01E01, but a lone S02E05 says which episode is running.
  const singleKey = single && !(single.season <= 1 && single.episode <= 1) ? single.key : "";

  // For a one-file job the counters read "0/1 episodes" and "1 ok · 0 failed ·
  // 0 skipped" — true, and useless. Say which episode it is when that carries
  // information, and otherwise let the status badge and the bar speak.
  const progressLabel = single
    ? singleKey
    : job.summary && job.summary.total > 0
      ? t("{ok} ok · {failed} failed · {skipped} skipped", {
          ok: job.summary.succeeded,
          failed: job.summary.failed,
          skipped: job.summary.skipped,
        })
      : totalEps > 0
        ? t("{done}/{total} episodes", { done: doneEps, total: totalEps })
        : finished
          ? ""
        : job.status === "resolving"
          ? t("Resolving source…")
          : t("Preparing…");

  // A one-file job reports its failure twice: the episode carries what actually
  // broke, and the job level only tallies it ("1 of 1 episodes failed"). Show a
  // single error, and prefer the specific one.
  const episodeError =
    single && single.error && (single.state === "failed" || single.state === "deferred") ? single.error : "";
  // The backend hands over a pre-formatted English tally ("3 of 8 episodes
  // failed"), which no translator can reach. Recognise exactly that string and
  // rebuild it from the structured summary the UI already has.
  const tally = job.summary ? `${job.summary.failed} of ${job.summary.total} episodes failed` : "";
  const jobError =
    tally && job.error === tally
      ? t("{n} of {m} episodes failed", { n: job.summary!.failed, m: job.summary!.total })
      : job.error;
  const rawError = episodeError || jobError;
  const timedOut = looksLikeTimeout(rawError);
  // A stop the user asked for is not a failure. On a canceled card the status
  // badge already says "Canceled", so the block would only repeat it; when the
  // job ended some other way (one episode was canceled out of many) the reason
  // is still worth a line, just not a red one.
  const canceledStop = looksLikeCanceled(rawError);
  const errorText = canceledStop
    ? job.status === "canceled"
      ? ""
      : t("Canceled")
    : looksLikeDiskFull(rawError)
      ? t("Not enough disk space. Free space in the download folder, then retry.")
    : timedOut
      ? t("Request timed out — kino.watch may be unreachable without a VPN. Enable a VPN or set a proxy, then retry.")
      : rawError;

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
  // Removing the card is also the last moment anything points at the partial
  // segments a paused (or per-episode canceled) download kept for its resume.
  // Ask before taking them along; silently deleting gigabytes behind a button
  // that reads "Remove" would be a nasty surprise.
  const remove = async () => {
    setBusy(true);
    try {
      let purge = false;
      let left: Leftovers = { bytes: 0, items: 0, conflict: false };
      try {
        left = await api.jobLeftovers(job.id);
      } catch {
        // Best-effort: a failed scan must not block removing the card.
      }
      if (left.bytes > 0) {
        const size = bytes(left.bytes);
        const ok = window.confirm(
          left.conflict
            ? t(
                "This job holds {size} of partly downloaded data, but another job is still using it — the files stay on disk. Remove the card?",
                { size },
              )
            : t("Remove this job and delete the {size} it already downloaded? This cannot be undone.", { size }),
        );
        if (!ok) {
          setBusy(false);
          return;
        }
        purge = !left.conflict;
      }
      await api.deleteJob(job.id, purge);
    } catch (e: any) {
      toast(e.message || "Error", "error");
    } finally {
      setBusy(false);
    }
  };
  const retry = async () => {
    setBusy(true);
    try {
      await api.retryJob(job.id);
      toast(t("Retrying — re-downloading what failed…"), "info");
    } catch (e: any) {
      toast(e.message || "Error", "error");
    } finally {
      setBusy(false);
    }
  };
  const prioritize = async () => {
    setBusy(true);
    try {
      await api.prioritizeJob(job.id);
      toast(t("Moved to the front of the queue"), "info");
    } catch (e: any) {
      toast(e.message || "Error", "error");
    } finally {
      setBusy(false);
    }
  };
  const pause = async () => {
    setBusy(true);
    try {
      await api.pauseJob(job.id);
      toast(t("Paused — progress is kept"), "info");
    } catch (e: any) {
      toast(e.message || "Error", "error");
    } finally {
      setBusy(false);
    }
  };
  const resume = async () => {
    setBusy(true);
    try {
      await api.resumeJob(job.id);
      toast(t("Resuming — continuing where it stopped…"), "info");
    } catch (e: any) {
      toast(e.message || "Error", "error");
    } finally {
      setBusy(false);
    }
  };

  // Offer a retry whenever the run ended with something left to download: a hard
  // failure, a cancellation, or a partial success. Completed-clean jobs don't.
  const canRetry =
    finished && (job.status === "failed" || job.status === "canceled" || (job.summary?.failed ?? 0) > 0);

  // Is there anything for this job to get ahead of? Without competition
  // "Prioritize" does nothing, and a job dispatched the instant it is queued —
  // a per-episode resume, say — would flash the button for a single frame.
  const otherActiveJob = hasOtherActiveJob(jobs, job.id);

  const openOutput = async () => {
    try {
      await api.openPath(job.outputPath);
    } catch (e: any) {
      toast(e.message || t("Could not open"), "error");
    }
  };

  // The same dot-separated fact line the library cards use, so a download and the
  // file it becomes read as one family of card.
  const fetched = job.episodes.reduce((acc, e) => acc + e.bytes, 0);
  const facts: ReactNode[] = [];
  if (job.quality) {
    facts.push(
      <span key="quality" className="inline-flex items-center gap-1.5">
        <MonitorPlay className="h-3.5 w-3.5 text-slate-500" /> {job.quality}
      </span>,
    );
  }
  // While running, the progress label already reads "3/8 episodes" — repeating
  // the total here would say the same thing twice on adjacent lines. Once the run
  // ends that label becomes the ok/failed summary, so the count earns its place.
  if (totalEps > 1 && job.summary) {
    facts.push(
      <span key="eps" className="inline-flex items-center gap-1.5">
        <ListVideo className="h-3.5 w-3.5 text-slate-500" /> {t("{n} episodes", { n: totalEps })}
      </span>,
    );
  }
  // A one-file job already reports its bytes in the EpisodeMeta line below.
  if (!single && fetched > 0) {
    facts.push(
      <span key="size" className="inline-flex items-center gap-1.5">
        <HardDrive className="h-3.5 w-3.5 text-slate-500" /> {bytes(fetched)}
      </span>,
    );
  }
  facts.push(
    <span key="when">
      {job.startedAt ? `${t("started")} ${relTime(job.startedAt, t)}` : `${t("created")} ${relTime(job.createdAt, t)}`}
    </span>,
  );

  return (
    <div className="card animate-fade-in overflow-hidden transition hover:border-white/[0.12]">
      <div className="flex gap-4 p-4">
        {/* self-start matters: as a stretched flex item the poster would get a
            definite height, which overrides aspect-ratio and squeezes the art
            into a tall strip whenever the card grows (a long error, say). */}
        <PosterImage
          url={job.posterUrl}
          alt={job.title}
          className="aspect-[2/3] w-20 shrink-0 self-start rounded-lg border border-white/[0.08]"
        />
        <div className="min-w-0 flex-1">
          <div className="flex items-center justify-between gap-3">
            {/* The source URL moves to the tooltip: it duplicated the title and
                the fact line carries what is actually worth reading. The dry-run
                chip shares this row instead of claiming one of its own. */}
            <div className="flex min-w-0 items-center gap-2">
              <h3 className="truncate text-base font-semibold text-slate-100" title={job.url}>
                {job.title || job.url}
              </h3>
              {job.dryRun && (
                <span className="chip shrink-0 border-sky-500/25 bg-sky-500/10 text-sky-300">{t("dry-run")}</span>
              )}
            </div>
            <StatusBadge status={job.status} />
          </div>

          <div className="mt-1.5 flex flex-wrap items-center gap-x-2.5 gap-y-1 text-xs text-slate-400">
            {facts.map((f, i) => (
              <Fragment key={i}>
                {i > 0 && <span className="text-slate-700">·</span>}
                {f}
              </Fragment>
            ))}
          </div>

          <div className="mt-2.5">
            {/* Label, bar and percent share one row. Stacking the label above the
                bar spent a whole line on a handful of words. */}
            <div className="flex items-center gap-3">
              {progressLabel && (
                <span
                  className={clsx("max-w-[45%] truncate text-xs text-slate-400", singleKey && "font-mono")}
                  title={progressLabel}
                >
                  {progressLabel}
                </span>
              )}
              <ProgressBar
                className="flex-1"
                value={overall}
                variant={
                  job.status === "failed"
                    ? "rose"
                    : job.status === "completed"
                      ? "green"
                      : job.status === "paused"
                        ? "slate"
                        : "gold"
                }
                active={job.status === "running" || job.status === "resolving"}
              />
              <span className="w-9 shrink-0 text-right text-xs font-medium tabular-nums text-slate-300">
                {Math.round(overall)}%
              </span>
            </div>
            {/* The one file's live detail — size, speed, ETA, muxing — sits right
                here instead of behind a toggle over a single row. */}
            {single && (
              <EpisodeMeta
                ep={single}
                active={single.state === "running" && !finished}
                showError={!errorText}
              />
            )}
          </div>

          {/* Tinted rather than loose red text: the card stacks several small
              lines, and a block keeps the failure reading as one thing. */}
          {errorText && (
            <div
              className={clsx(
                "mt-2 flex items-start gap-1.5 rounded-lg px-2 py-1.5 text-xs",
                canceledStop
                  ? "bg-white/[0.04] text-slate-400"
                  : timedOut
                    ? "bg-gold-500/[0.08] text-gold-300"
                    : "bg-ember-500/[0.08] text-ember-400",
              )}
            >
              {canceledStop ? (
                <Ban className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              ) : (
                <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              )}
              <span className="min-w-0 break-words">{errorText}</span>
            </div>
          )}

          <div className="mt-2.5 flex flex-wrap items-center gap-2 text-xs">
            {totalEps > 0 && !single && (
              <button className="btn-ghost px-3 py-1.5 text-xs" onClick={() => setShowEps((v) => !v)}>
                {showEps ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
                {t("Episodes ({n})", { n: totalEps })}
              </button>
            )}
            <div className="ml-auto flex flex-wrap items-center gap-2">
              {/* The output folder is the same root for every job, so printing the
                  path on each card was a repeated row of noise. Keep the action,
                  drop the line — the path lives in the tooltip. */}
              {job.outputPath && (
                <button
                  className="btn-ghost px-3 py-1.5 text-xs"
                  onClick={openOutput}
                  title={`${t("Open folder")} — ${job.outputPath}`}
                >
                  <FolderOpen className="h-3.5 w-3.5" /> {t("Folder")}
                </button>
              )}
              {job.status === "paused" ? (
                <>
                  <button className="btn-ghost px-3 py-1.5 text-xs text-amber-300" onClick={resume} disabled={busy}>
                    <Play className="h-3.5 w-3.5" /> {t("Resume")}
                  </button>
                  <button className="btn-ghost px-3 py-1.5 text-xs" onClick={remove} disabled={busy}>
                    <Trash2 className="h-3.5 w-3.5" /> {t("Remove")}
                  </button>
                </>
              ) : !finished ? (
                <>
                  {job.status === "queued" && otherActiveJob && (
                    <button className="btn-ghost px-3 py-1.5 text-xs text-gold-300" onClick={prioritize} disabled={busy}>
                      <ArrowUp className="h-3.5 w-3.5" /> {t("Prioritize")}
                    </button>
                  )}
                  <button className="btn-ghost px-3 py-1.5 text-xs text-amber-300" onClick={pause} disabled={busy}>
                    <Pause className="h-3.5 w-3.5" /> {t("Pause")}
                  </button>
                  <button className="btn-danger px-3 py-1.5 text-xs" onClick={cancel} disabled={busy}>
                    <Ban className="h-3.5 w-3.5" /> {t("Cancel")}
                  </button>
                </>
              ) : (
                <>
                  {canRetry && (
                    <button className="btn-ghost px-3 py-1.5 text-xs text-gold-300" onClick={retry} disabled={busy}>
                      <RotateCw className="h-3.5 w-3.5" /> {t("Retry")}
                    </button>
                  )}
                  <button className="btn-ghost px-3 py-1.5 text-xs" onClick={remove} disabled={busy}>
                    <Trash2 className="h-3.5 w-3.5" /> {t("Remove")}
                  </button>
                </>
              )}
            </div>
          </div>
        </div>
      </div>

      {showEps && totalEps > 0 && !single && (
        <div className="grid gap-2 border-t border-white/[0.05] bg-black/20 p-4 md:grid-cols-2">
          {job.episodes.map((ep) => (
            <EpisodeRow
              key={ep.key}
              ep={ep}
              jobId={job.id}
              jobStatus={job.status}
              // "Next" needs something to get ahead of: other not-yet-done
              // episodes here (plan total counts unstarted ones without rows),
              // or another active download in the queue.
            />
          ))}
        </div>
      )}

    </div>
  );
}
