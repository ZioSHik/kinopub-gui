import { Inbox, Trash2 } from "lucide-react";
import { api } from "../api";
import { useApp } from "../store";
import { useI18n } from "../i18n";
import { JobCard } from "../components/JobCard";
import { EmptyState } from "../components/ui";

export function QueuePage({ onNew }: { onNew: () => void }) {
  const { jobs, toast } = useApp();
  const { t } = useI18n();

  const active = jobs.filter((j) => !["completed", "failed", "canceled"].includes(j.status));
  const finished = jobs.filter((j) => ["completed", "failed", "canceled"].includes(j.status));

  const clear = async () => {
    try {
      const r = await api.clearJobs();
      toast(t("Cleared {n} finished jobs", { n: r.removed }), "info");
    } catch (e: any) {
      toast(e.message || "Error", "error");
    }
  };

  return (
    <div className="mx-auto max-w-4xl space-y-5">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-slate-100">{t("Queue")}</h1>
          <p className="mt-1 text-sm text-slate-400">
            {t("{n} active · {m} finished", { n: active.length, m: finished.length })}
          </p>
        </div>
        {finished.length > 0 && (
          <button className="btn-ghost" onClick={clear}>
            <Trash2 className="h-4 w-4" /> {t("Clear finished")}
          </button>
        )}
      </header>

      {jobs.length === 0 ? (
        <EmptyState
          icon={<Inbox className="h-7 w-7" />}
          title={t("No downloads yet")}
          hint={t("Start a download and live progress for every episode shows up here.")}
          action={
            <button className="btn-primary" onClick={onNew}>
              {t("New download ")}
            </button>
          }
        />
      ) : (
        <div className="space-y-4">
          {active.map((j) => (
            <JobCard key={j.id} job={j} />
          ))}
          {finished.length > 0 && active.length > 0 && (
            <div className="flex items-center gap-3 pt-2 text-xs uppercase tracking-wide text-slate-500">
              <span>{t("Finished")}</span>
              <span className="h-px flex-1 bg-white/[0.06]" />
            </div>
          )}
          {finished.map((j) => (
            <JobCard key={j.id} job={j} />
          ))}
        </div>
      )}
    </div>
  );
}
