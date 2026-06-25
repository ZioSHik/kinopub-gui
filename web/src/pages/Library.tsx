import { useEffect, useMemo, useState } from "react";
import { CheckCircle2, Clapperboard, Film, FolderOpen, HardDrive, Library as LibraryIcon, Play, RefreshCw, Trash2, Tv, XCircle } from "lucide-react";
import { api, type LibraryEpisode, type LibraryResponse, type LibrarySeries } from "../api";
import { useApp } from "../store";
import { useI18n } from "../i18n";
import { dismiss, pushRoute, replaceRoute, useRoute } from "../router";
import { bytes, relTime } from "../lib/format";
import { EmptyState, PosterImage, Spinner } from "../components/ui";
import { TitleDetail } from "../components/TitleDetail";

// itemIdOf extracts the kino.pub item id from a library series (its inputUrl or
// a numeric seriesId), so we can open its catalog card.
function itemIdOf(s: LibrarySeries): string {
  const m = (s.inputUrl || "").match(/\/item\/view\/(\d+)/);
  if (m) return m[1];
  if (s.seriesId && /^\d+$/.test(s.seriesId)) return s.seriesId;
  return "";
}

function SeriesCard({ s, onDeleted, onOpenCard }: { s: LibrarySeries; onDeleted: () => void; onOpenCard: (id: string) => void }) {
  const { t } = useI18n();
  const { toast, kpauth } = useApp();
  const itemId = itemIdOf(s);
  const [open, setOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [removingKey, setRemovingKey] = useState<string | null>(null);
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

  const removeEpisode = async (e: LibraryEpisode) => {
    const label = `${e.key}${e.title ? ` · ${e.title}` : ""}`;
    if (!window.confirm(t("Delete episode {label} from disk? This frees its space and cannot be undone.", { label }))) {
      return;
    }
    setRemovingKey(e.key);
    try {
      await api.deleteLibraryEpisode(s.dir, e.key);
      toast(t("Deleted {label}", { label }), "success");
      onDeleted();
    } catch (err: any) {
      toast(err.message || t("Delete failed"), "error");
    } finally {
      setRemovingKey(null);
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
            <span className="chip border-white/10 bg-white/[0.04] text-slate-400">
              {s.isMovie ? <Film className="h-3 w-3" /> : <Tv className="h-3 w-3" />}
              {s.isMovie ? t("Movie") : t("Serial")}
            </span>
            {(!s.isMovie || s.count > 1) && (
              <span className="chip border-white/10 bg-white/[0.04] text-slate-300">{t("{n} episodes", { n: s.count })}</span>
            )}
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
            {kpauth.loggedIn && itemId && (
              <button className="btn-ghost px-3 py-1.5 text-xs" onClick={() => onOpenCard(itemId)}>
                <Clapperboard className="h-3.5 w-3.5" /> {t("Open card")}
              </button>
            )}
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
                <button
                  className="shrink-0 rounded-md p-1 text-slate-500 opacity-0 transition hover:bg-ember-500/10 hover:text-ember-300 group-hover:opacity-100 disabled:opacity-100"
                  title={t("Delete this episode from disk")}
                  onClick={() => removeEpisode(e)}
                  disabled={removingKey === e.key}
                >
                  {removingKey === e.key ? (
                    <Spinner className="h-3.5 w-3.5" />
                  ) : (
                    <Trash2 className="h-3.5 w-3.5" />
                  )}
                </button>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

type TypeTab = "all" | "movies" | "series";
type SortKey = "recent" | "title" | "size";

function TypeChip({ active, count, onClick, children }: { active: boolean; count: number; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      onClick={onClick}
      className={`flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm transition ${
        active ? "bg-gold-500/[0.14] text-gold-200" : "text-slate-400 hover:bg-white/[0.05] hover:text-slate-200"
      }`}
    >
      {children}
      <span className={`text-xs tabular-nums ${active ? "text-gold-300/70" : "text-slate-600"}`}>{count}</span>
    </button>
  );
}

export function LibraryPage() {
  const { toast } = useApp();
  const { t } = useI18n();
  const [data, setData] = useState<LibraryResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [typeTab, setTypeTab] = useState<TypeTab>("all");
  const [sortKey, setSortKey] = useState<SortKey>("recent");
  const [genre, setGenre] = useState("");
  // The open card lives in the URL hash ("#/library/i/<id>") so it survives a
  // reload and browser back closes it.
  const cardId = useRoute().itemId ?? null;

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

  const counts = useMemo(() => {
    const movies = series.filter((s) => s.isMovie).length;
    return { all: series.length, movies, series: series.length - movies };
  }, [series]);

  // Genres present across the whole library, for the filter dropdown.
  const allGenres = useMemo(() => {
    const set = new Set<string>();
    for (const s of series) for (const g of s.genres ?? []) set.add(g);
    return Array.from(set).sort((a, b) => a.localeCompare(b, "ru"));
  }, [series]);

  const shown = useMemo(() => {
    const list = series.filter((s) => {
      if (typeTab === "movies" && !s.isMovie) return false;
      if (typeTab === "series" && s.isMovie) return false;
      if (genre && !(s.genres ?? []).includes(genre)) return false;
      return true;
    });
    list.sort((a, b) => {
      if (sortKey === "title") return a.title.localeCompare(b.title, "ru");
      if (sortKey === "size") return b.totalBytes - a.totalBytes;
      return (b.updatedAt || "").localeCompare(a.updatedAt || ""); // recent first (ISO dates sort lexically)
    });
    return list;
  }, [series, typeTab, genre, sortKey]);

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

      {series.length > 0 && (
        <div className="flex flex-wrap items-center gap-3">
          <div className="flex items-center gap-1 rounded-xl border border-white/[0.06] bg-white/[0.02] p-1">
            <TypeChip active={typeTab === "all"} count={counts.all} onClick={() => setTypeTab("all")}>{t("All")}</TypeChip>
            <TypeChip active={typeTab === "movies"} count={counts.movies} onClick={() => setTypeTab("movies")}>
              <Film className="h-3.5 w-3.5" /> {t("Movies")}
            </TypeChip>
            <TypeChip active={typeTab === "series"} count={counts.series} onClick={() => setTypeTab("series")}>
              <Tv className="h-3.5 w-3.5" /> {t("Series")}
            </TypeChip>
          </div>
          <div className="ml-auto flex items-center gap-2">
            {allGenres.length > 0 && (
              <select className="input w-auto py-1.5" value={genre} onChange={(e) => setGenre(e.target.value)} title={t("Genre")}>
                <option value="">{t("All genres")}</option>
                {allGenres.map((g) => (
                  <option key={g} value={g}>{g}</option>
                ))}
              </select>
            )}
            <select className="input w-auto py-1.5" value={sortKey} onChange={(e) => setSortKey(e.target.value as SortKey)} title={t("Sort")}>
              <option value="recent">{t("Recently added")}</option>
              <option value="title">{t("Name (A–Z)")}</option>
              <option value="size">{t("Largest first")}</option>
            </select>
          </div>
        </div>
      )}

      {data && series.length === 0 ? (
        <EmptyState
          icon={<LibraryIcon className="h-7 w-7" />}
          title={t("Nothing downloaded yet")}
          hint={dirs.filter(Boolean).join(", ")}
        />
      ) : shown.length === 0 ? (
        <EmptyState icon={<LibraryIcon className="h-7 w-7" />} title={t("Nothing matches the filters")} />
      ) : (
        <div className="space-y-4">
          {shown.map((s) => (
            <SeriesCard
              key={s.stateFile}
              s={s}
              onDeleted={load}
              onOpenCard={(id) => pushRoute({ page: "library", itemId: id })}
            />
          ))}
        </div>
      )}

      {cardId && (
        <TitleDetail
          id={cardId}
          onClose={() => dismiss({ page: "library" })}
          onPick={(it) => replaceRoute({ page: "library", itemId: it.id })}
          onStarted={() => pushRoute({ page: "queue" })}
        />
      )}
    </div>
  );
}
