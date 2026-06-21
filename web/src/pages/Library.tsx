import { useEffect, useState } from "react";
import { CheckCircle2, FolderOpen, HardDrive, Library as LibraryIcon, Play, RefreshCw, Trash2, XCircle } from "lucide-react";
import { api, type LibraryResponse, type LibrarySeries } from "../api";
import { useApp } from "../store";
import { useI18n } from "../i18n";
import { bytes, relTime } from "../lib/format";
import { EmptyState, PosterImage, Spinner } from "../components/ui";

function SeriesCard({ s, onDeleted }: { s: LibrarySeries; onDeleted: () => void }) {
  const { t } = useI18n();
  const { toast } = useApp();
  const [open, setOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const episodes = s.episodes ?? [];
  const missing = episodes.filter((e) => !e.exists).length;

  const openPath = async (path: string, reveal = false) => {
    try {
      await api.openPath(path, reveal);
    } catch (e: any) {
      toast(e.message || t("Could not open"), "error");
    }
  };

  const remove = async () => {
    if (!window.confirm(t("Delete “{title}” and all its files from disk? This cannot be undone.", { title: s.title }))) {
      return;
    }
    setDeleting(true);
    try {
      await api.deleteLibrary(s.dir);
      toast(t("Deleted “{title}”", { title: s.title }), "success");
      onDeleted();
    } catch (e: any) {
      toast(e.message || t("Delete failed"), "error");
    } finally {
      setDeleting(false);
    }
  };
  return (
    <div className="card overflow-hidden">
      <button className="flex w-full gap-4 p-4 text-left" onClick={() => setOpen((v) => !v)}>
        <PosterImage
          url={s.posterUrl}
          alt={s.title}
          className="aspect-[2/3] w-16 shrink-0 rounded-lg border border-white/[0.08]"
        />
        <div className="min-w-0 flex-1">
          <h3 className="truncate text-base font-semibold text-slate-100">{s.title}</h3>
          {s.originalTitle && <p className="truncate text-sm text-slate-500">{s.originalTitle}</p>}
          <div className="mt-2 flex flex-wrap gap-2 text-xs">
            <span className="chip border-white/10 bg-white/[0.04] text-slate-300">{t("{n} episodes", { n: s.count })}</span>
            <span className="chip border-white/10 bg-white/[0.04] text-slate-300">
              <HardDrive className="h-3 w-3" /> {bytes(s.totalBytes)}
            </span>
            {missing > 0 && (
              <span className="chip border-ember-500/25 bg-ember-500/10 text-ember-400">
                <XCircle className="h-3 w-3" /> {t("{n} missing", { n: missing })}
              </span>
            )}
            {s.updatedAt && <span className="chip border-white/10 bg-white/[0.04] text-slate-500">{relTime(s.updatedAt, t)}</span>}
          </div>
          <p className="mt-1.5 flex items-center gap-1.5 truncate font-mono text-[11px] text-slate-600">
            <FolderOpen className="h-3 w-3 shrink-0" /> {s.dir}
          </p>
        </div>
      </button>
      {open && (
        <div className="border-t border-white/[0.05] bg-black/20 p-3">
          <div className="mb-2 flex justify-end gap-2">
            <button className="btn-ghost px-3 py-1.5 text-xs" onClick={() => openPath(s.dir)}>
              <FolderOpen className="h-3.5 w-3.5" /> {t("Open folder")}
            </button>
            <button
              className="btn-ghost px-3 py-1.5 text-xs text-ember-300 hover:bg-ember-500/10 hover:text-ember-200"
              onClick={remove}
              disabled={deleting}
            >
              {deleting ? <Spinner className="h-3.5 w-3.5" /> : <Trash2 className="h-3.5 w-3.5" />} {t("Delete")}
            </button>
          </div>
          <div className="grid gap-1 sm:grid-cols-2">
            {episodes.map((e) => (
              <div key={e.key} className="group flex items-center gap-2 rounded-lg px-2 py-1.5 text-sm hover:bg-white/[0.03]">
                {e.exists ? (
                  <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-400/80" />
                ) : (
                  <XCircle className="h-4 w-4 shrink-0 text-ember-400/80" />
                )}
                <span className="w-12 shrink-0 font-mono text-xs text-slate-500">{e.key}</span>
                <span className="min-w-0 flex-1 truncate text-slate-300">{e.title || t("Episode {n}", { n: e.episode })}</span>
                {e.resolution && <span className="hidden text-[11px] text-slate-500 sm:inline">{e.resolution}</span>}
                <span className="shrink-0 text-[11px] tabular-nums text-slate-500">{bytes(e.bytes)}</span>
                {e.exists && (
                  <button
                    className="shrink-0 rounded-md p-1 text-slate-500 opacity-0 transition hover:bg-white/[0.08] hover:text-gold-300 group-hover:opacity-100"
                    title={t("Open")}
                    onClick={() => openPath(e.path)}
                  >
                    <Play className="h-3.5 w-3.5" />
                  </button>
                )}
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

export function LibraryPage() {
  const { toast } = useApp();
  const { t } = useI18n();
  const [data, setData] = useState<LibraryResponse | null>(null);
  const [loading, setLoading] = useState(false);

  const load = () => {
    setLoading(true);
    api
      .library()
      .then(setData)
      .catch((e) => toast(e.message || t("Scan failed"), "error"))
      .finally(() => setLoading(false));
  };

  useEffect(load, []); // eslint-disable-line react-hooks/exhaustive-deps

  const series = data?.series ?? [];
  const dirs = data?.dirs ?? [];

  return (
    <div className="mx-auto max-w-4xl space-y-5">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-slate-100">{t("Library")}</h1>
          <p className="mt-1 text-sm text-slate-400">
            {t("Downloads found in your output folders")}
          </p>
        </div>
        <button className="btn-ghost" onClick={load} disabled={loading}>
          {loading ? <Spinner className="h-4 w-4" /> : <RefreshCw className="h-4 w-4" />}
          {t("Rescan")}
        </button>
      </header>

      {data && series.length === 0 ? (
        <EmptyState
          icon={<LibraryIcon className="h-7 w-7" />}
          title={t("Nothing downloaded yet")}
          hint={dirs.filter(Boolean).join(", ")}
        />
      ) : (
        <div className="space-y-4">
          {series.map((s) => (
            <SeriesCard key={s.stateFile} s={s} onDeleted={load} />
          ))}
        </div>
      )}
    </div>
  );
}
