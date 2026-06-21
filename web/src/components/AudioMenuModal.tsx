import { useEffect, useMemo, useState } from "react";
import { AudioLines, Check, Clock } from "lucide-react";
import { api, type JobView } from "../api";
import { useApp } from "../store";
import { useI18n } from "../i18n";
import { Modal } from "./ui";

export function AudioMenuModal({ job }: { job: JobView }) {
  const { toast } = useApp();
  const { t } = useI18n();
  const req = job.pendingAudio!;
  const [selected, setSelected] = useState<Set<number>>(
    () => new Set(req.tracks.map((t) => t.Index)),
  );
  const [busy, setBusy] = useState(false);
  const [remaining, setRemaining] = useState(() =>
    Math.max(0, req.deadlineUnix - Math.floor(Date.now() / 1000)),
  );

  useEffect(() => {
    const t = setInterval(() => {
      setRemaining(Math.max(0, req.deadlineUnix - Math.floor(Date.now() / 1000)));
    }, 1000);
    return () => clearInterval(t);
  }, [req.deadlineUnix]);

  const toggle = (idx: number) => {
    setSelected((s) => {
      const next = new Set(s);
      next.has(idx) ? next.delete(idx) : next.add(idx);
      return next;
    });
  };

  const submit = async (indices: number[]) => {
    setBusy(true);
    try {
      await api.answerAudio(job.id, indices);
    } catch (e: any) {
      toast(e.message || t("Failed to submit selection"), "error");
    } finally {
      setBusy(false);
    }
  };

  const ordered = useMemo(
    () => [...req.tracks].sort((a, b) => a.Index - b.Index),
    [req.tracks],
  );

  return (
    <Modal
      open
      onClose={() => submit([])}
      title={
        <span className="flex items-center gap-2">
          <AudioLines className="h-5 w-5 text-gold-400" /> {t("Choose audio tracks")}
        </span>
      }
      wide
    >
      <div className="space-y-4">
        <div className="flex items-center justify-between gap-3 text-sm">
          <span className="truncate text-slate-400">{job.title || job.url}</span>
          <span className="chip border-gold-500/30 bg-gold-500/10 text-gold-300">
            <Clock className="h-3.5 w-3.5" /> {remaining}s
          </span>
        </div>

        <p className="text-sm text-slate-400">
          {t(
            "Pick which dubs/languages to keep. Your choice is generalized across episodes, so a dub missing from some episode falls back to the same language. No choice within the timer keeps every track.",
          )}
        </p>

        <div className="max-h-72 space-y-2 overflow-y-auto pr-1">
          {ordered.map((tr) => {
            const on = selected.has(tr.Index);
            return (
              <button
                key={tr.Index}
                onClick={() => toggle(tr.Index)}
                className={`flex w-full items-center gap-3 rounded-xl border px-3.5 py-3 text-left transition ${
                  on
                    ? "border-gold-500/40 bg-gold-500/[0.08]"
                    : "border-white/[0.06] bg-ink-900/40 hover:border-white/[0.12]"
                }`}
              >
                <span
                  className={`grid h-5 w-5 shrink-0 place-items-center rounded-md border ${
                    on ? "border-gold-500 bg-gold-500 text-ink-950" : "border-white/20"
                  }`}
                >
                  {on && <Check className="h-3.5 w-3.5" />}
                </span>
                <span className="min-w-0">
                  <span className="block truncate text-sm font-medium text-slate-200">{tr.Name}</span>
                  {tr.Language && (
                    <span className="text-xs uppercase tracking-wide text-slate-500">{tr.Language}</span>
                  )}
                </span>
              </button>
            );
          })}
        </div>

        <div className="flex flex-wrap items-center justify-end gap-2 pt-1">
          <button className="btn-ghost" onClick={() => submit([])} disabled={busy}>
            {t("Keep all")}
          </button>
          <button
            className="btn-primary"
            onClick={() => submit([...selected])}
            disabled={busy || selected.size === 0}
          >
            <Check className="h-4 w-4" />
            {t("Download selected ({n})", { n: selected.size })}
          </button>
        </div>
      </div>
    </Modal>
  );
}
