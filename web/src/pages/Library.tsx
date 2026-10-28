import { Fragment, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import clsx from "clsx";
import {
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Clapperboard,
  Film,
  FolderOpen,
  HardDrive,
  Layers,
  LayoutGrid,
  Library as LibraryIcon,
  Link2,
  List,
  ListVideo,
  MonitorPlay,
  Play,
  RefreshCw,
  Search,
  Trash2,
  Tv,
  X,
  XCircle,
} from "lucide-react";
import {
  api,
  isNavigationAbort,
  type JobView,
  type LibraryEpisode,
  type LibraryResponse,
  type LibrarySeries,
} from "../api";
import { useApp } from "../store";
import { useI18n } from "../i18n";
import { dismiss, pushRoute, replaceRoute, useRoute } from "../router";
import { voiceoverCounts, voiceoverLabels } from "../lib/libraryAudio";
import { bytes, relTime } from "../lib/format";
import { completedSignal, itemIdFromURL } from "../lib/queue";
import { EmptyState, Modal, PosterImage, Spinner } from "../components/ui";
import { JobCard } from "../components/JobCard";
import { TitleDetail } from "../components/TitleDetail";

// Shared empty array so `s.episodes ?? NO_EPISODES` keeps a stable identity and
// the memos below don't recompute on every render.
const NO_EPISODES: LibraryEpisode[] = [];

// useHoverCapable reports whether the pointer can actually hover. On a phone it
// cannot, so anything hidden behind :hover is unreachable there — those tiles
// have to summon their actions with a tap instead.
function useHoverCapable(): boolean {
  const query = () => window.matchMedia?.("(hover: hover)").matches ?? true;
  const [can, setCan] = useState(query);
  useEffect(() => {
    const mq = window.matchMedia?.("(hover: hover)");
    if (!mq) return;
    const on = () => setCan(mq.matches);
    mq.addEventListener("change", on);
    return () => mq.removeEventListener("change", on);
  }, []);
  return can;
}

// Genres beyond this many collapse into a "+N" hint, so a title with a long
// genre list doesn't push the card's actions out of view.
const MAX_GENRES = 3;

// itemIdFromURL now lives next to the queue helpers that also need it; re-exported
// here because this page's tests and callers have always found it under Library.
export { itemIdFromURL };

// itemIdOf extracts the kino.watch item id from a library series (its inputUrl or
// a numeric seriesId), so we can open its catalog card.
function itemIdOf(s: LibrarySeries): string {
  const fromURL = itemIdFromURL(s.inputUrl || "");
  if (fromURL) return fromURL;
  if (s.seriesId && /^\d+$/.test(s.seriesId)) return s.seriesId;
  return "";
}

// supersededByLibrary reports whether a job's work is already sitting on disk —
// every episode it planned exists in the library entry for the same title.
//
// This happens when an attempt fails and a later run finishes the job: the failed
// record survives in the persistent queue with its frozen progress ("831/1418
// seg"), while the finished file is listed below. Those numbers describe an
// attempt whose temp segments are long gone, so the card is not just a duplicate
// — it invites a retry that the engine will immediately skip.
//
// Episodes are matched on season/episode numbers rather than key strings, since
// the queue and the state file don't format keys identically ("S01E01" vs "S1E1").
export function supersededByLibrary(job: JobView, series: LibrarySeries[]): boolean {
  // No planned episodes means the run died before resolving; there is nothing to
  // claim is already downloaded.
  if (job.episodes.length === 0) return false;
  if (job.status !== "completed") return false;
  const id = itemIdFromURL(job.url);
  if (!id) return false;
  const output = job.outputPath.replace(/\\/g, "/").replace(/\/$/, "");
  const entry = series.find(
    (s) => itemIdOf(s) === id && (!output || s.dir.replace(/\\/g, "/").startsWith(output + "/")),
  );
  if (!entry) return false;
  const onDisk = new Set(
    (entry.episodes ?? []).filter((e) => e.exists).map((e) => `${e.season}:${e.episode}`),
  );
  return job.episodes.every((e) => onDisk.has(`${e.season}:${e.episode}`));
}

// matchesQuery is the library search: every whitespace-separated term must occur
// somewhere in the fields a card actually shows (title, original title, genres),
// so narrowing with "драма 1080" behaves the way it reads.
export function matchesQuery(s: LibrarySeries, query: string): boolean {
  const terms = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  if (terms.length === 0) return true;
  const hay = [s.title, s.originalTitle ?? "", ...(s.genres ?? [])].join(" ").toLowerCase();
  return terms.every((term) => hay.includes(term));
}

// useSeriesStats derives everything a card shows about an entry without
// expanding it: how complete it is, what it holds, and its season grouping.
function useSeriesStats(s: LibrarySeries) {
  const episodes = s.episodes ?? NO_EPISODES;
  return useMemo(() => {
    const seen = new Map<string, number>();
    const groups = new Map<number, LibraryEpisode[]>();
    for (const e of episodes) {
      if (e.resolution) seen.set(e.resolution, (seen.get(e.resolution) ?? 0) + 1);
      const g = groups.get(e.season);
      if (g) g.push(e);
      else groups.set(e.season, [e]);
    }
    // The dominant resolution stands in for "what quality is this", which is
    // otherwise only visible per-episode.
    let quality = "";
    let top = 0;
    for (const [res, n] of seen) if (n > top) [quality, top] = [res, n];
    return {
      episodes,
      missing: episodes.filter((e) => !e.exists).length,
      seasons: groups.size,
      quality,
      bySeason: Array.from(groups.entries()).sort((a, b) => a[0] - b[0]),
      // A single-file entry (every movie, and a one-episode series) has nothing
      // to list, so it gets a direct Play action instead of an episode list.
      single: episodes.length === 1 ? episodes[0] : null,
      hasList: episodes.length > 1,
    };
  }, [episodes]);
}

// useSeriesActions bundles the disk operations an entry supports, shared by the
// list card and the grid tile.
function useSeriesActions(s: LibrarySeries, onDeleted: () => void) {
  const { t } = useI18n();
  const { toast } = useApp();
  const [deleting, setDeleting] = useState(false);
  const [removingKey, setRemovingKey] = useState<string | null>(null);

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

  return { openPath, remove, removeEpisode, deleting, removingKey };
}

function VoiceoverSummary({ episodes }: { episodes: LibraryEpisode[] }) {
  const { t } = useI18n();
  const counts = voiceoverCounts(episodes);
  if (!counts.length) return null;
  const text = counts.map(({ label, count }) =>
    `${label || t("Voiceover not recorded")} — ${t("{n} ep", { n: count })}`,
  ).join("; ");
  return <p className="mt-1 text-xs leading-relaxed text-sky-300/70" title={text}>{t("Voiceover")}: {text}</p>;
}

function FileVersions({ episode }: { episode: LibraryEpisode }) {
  const { toast } = useApp();
  const { t } = useI18n();
  if (!episode.versions?.length) return null;
  return (
    <span className="flex flex-wrap gap-1">
      {[episode, ...episode.versions].map((file) => (
        <button
          key={file.path}
          className="rounded bg-white/5 px-2 py-1 text-xs text-slate-400 disabled:opacity-40"
          title={[file.resolution, ...(file.audio || [])].filter(Boolean).join(" · ")}
          disabled={!file.exists}
          onClick={(event) => {
            event.stopPropagation();
            void api.openPath(file.path).catch((e) => toast(e.message, "error"));
          }}
        >
          {t("Play")} · {file.resolution || file.requestedQuality || file.path.split(".").pop()}
        </button>
      ))}
    </span>
  );
}

function episodeFiles(episode: LibraryEpisode): LibraryEpisode[] {
  const pixels = (file: LibraryEpisode) => {
    const [width, height] = (file.resolution || "").split("x").map(Number);
    return (width || 0) * (height || 0);
  };
  return [episode, ...(episode.versions || [])].sort((a, b) =>
    Number(b.exists) - Number(a.exists) || pixels(b) - pixels(a),
  );
}

function EpisodeFile({ file, episodeKey, onOpen }: {
  file: LibraryEpisode;
  episodeKey: string;
  onOpen: (file: LibraryEpisode) => void;
}) {
  const { t } = useI18n();
  const resolution = file.resolution?.replace("x", " × ") ||
    (file.requestedQuality !== "optimal" && file.requestedQuality) || t("Unknown quality");
  const format = file.path.split(".").pop()?.toUpperCase() || "";
  const voices = voiceoverLabels(file.audio);
  return (
    <div role="group" aria-label={`${resolution} · ${format}`}
      className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-2 py-3 first:pt-0 last:pb-0">
      <div className="min-w-0 space-y-2">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px] text-slate-500">
          <span className="inline-flex items-center gap-1.5 font-medium tabular-nums text-slate-300">
            <MonitorPlay className="h-3.5 w-3.5 shrink-0 text-slate-500" /> {resolution}
          </span>
          {format && <span>{format}</span>}
          <span aria-hidden="true">·</span>
          <span className="tabular-nums">{bytes(file.bytes)}</span>
          {!file.exists && <span className="text-ember-400">{t("File missing")}</span>}
        </div>
        <div className="flex flex-wrap gap-1.5 text-xs leading-relaxed">
          {voices.length ? voices.map((voice) => (
            <span key={voice} className="max-w-full break-words rounded-md bg-sky-400/[0.07] px-2 py-0.5 text-sky-200/80">{voice}</span>
          )) : <span className="text-slate-500">{t("Voiceover not recorded")}</span>}
        </div>
      </div>
      <button
        className="inline-flex min-h-10 shrink-0 items-center justify-center gap-2 rounded-lg border border-gold-500/15 bg-gold-500/[0.08] px-3 text-xs font-medium text-gold-200 transition hover:border-gold-500/35 hover:bg-gold-500/[0.15] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-gold-500/60 disabled:cursor-not-allowed disabled:border-white/[0.05] disabled:bg-white/[0.02] disabled:text-slate-600"
        aria-label={t("Play {episode} · {quality}", { episode: episodeKey, quality: `${resolution} · ${format}` })}
        title={file.exists ? t("Play") : t("File missing")}
        disabled={!file.exists}
        onClick={() => onOpen(file)}
      >
        <Play className="h-3.5 w-3.5" />
        <span className="hidden sm:inline">{t("Play")}</span>
      </button>
    </div>
  );
}

// Keep the episode title separate from the files and tracks it contains.
function EpisodeRow({
  e,
  busy,
  onOpen,
  onDelete,
}: {
  e: LibraryEpisode;
  busy: boolean;
  onOpen: (file: LibraryEpisode) => void;
  onDelete: () => void;
}) {
  const { t } = useI18n();
  const files = episodeFiles(e);
  const available = files.some((file) => file.exists);
  const title = e.title || t("Episode {n}", { n: e.episode });
  return (
    <article aria-label={`${e.key} · ${title}`} className="rounded-xl border border-white/[0.07] bg-white/[0.015] px-3 py-3 sm:px-4">
      <div className="mb-2 flex items-start gap-2.5">
        <span className="inline-flex min-h-7 shrink-0 items-center gap-1.5 rounded-md bg-white/[0.04] px-2 font-mono text-[11px] text-slate-400">
          {available ? <CheckCircle2 className="h-3.5 w-3.5 text-emerald-400/80" /> : <XCircle className="h-3.5 w-3.5 text-ember-400/80" />}
          {e.key}
        </span>
        <h4 className="min-w-0 flex-1 break-words pt-0.5 text-sm font-medium leading-relaxed text-slate-200">{title}</h4>
        <button
          className="shrink-0 rounded-md p-1.5 text-slate-600 transition hover:bg-ember-500/10 hover:text-ember-300 focus-visible:text-ember-300 disabled:opacity-40"
          aria-label={t("Delete {episode} from disk", { episode: e.key })}
          title={files.length > 1 ? t("Delete all versions of this episode") : t("Delete this episode from disk")}
          onClick={onDelete}
          disabled={busy}
        >
          {busy ? <Spinner className="h-4 w-4" /> : <Trash2 className="h-4 w-4" />}
        </button>
      </div>
      <div className="divide-y divide-white/[0.06] sm:ml-[5.5rem]">
        {files.map((file) => <EpisodeFile key={file.path} file={file} episodeKey={e.key} onOpen={onOpen} />)}
      </div>
    </article>
  );
}

// Group episodes by season, keeping every file and its voiceovers together.
function EpisodeList({
  bySeason,
  removingKey,
  onOpen,
  onDelete,
}: {
  bySeason: [number, LibraryEpisode[]][];
  removingKey: string | null;
  onOpen: (e: LibraryEpisode) => void;
  onDelete: (e: LibraryEpisode) => void;
}) {
  const { t } = useI18n();
  return (
    <div className="space-y-3">
      {bySeason.map(([season, eps]) => (
        <div key={season}>
          <div className="mb-2.5 flex items-center gap-2 px-1">
            <span className="text-xs font-semibold uppercase tracking-wide text-slate-400">
              {t("Season {n}", { n: season })}
            </span>
            <span className="text-[11px] text-slate-600">{t("{n} ep", { n: eps.length })}</span>
            <span className="h-px flex-1 bg-white/[0.06]" />
          </div>
          <div className="space-y-2">
            {eps.map((e) => (
              <EpisodeRow
                key={e.key}
                e={e}
                busy={removingKey === e.key}
                onOpen={onOpen}
                onDelete={() => onDelete(e)}
              />
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

// GenreChips lists the genres of an entry; clicking one filters the library down
// to it.
function GenreChips({ genres, onPick }: { genres: string[]; onPick: (genre: string) => void }) {
  const { t } = useI18n();
  if (genres.length === 0) return null;
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {genres.slice(0, MAX_GENRES).map((g) => (
        <button
          key={g}
          className="rounded-md border border-white/[0.07] bg-white/[0.03] px-2 py-0.5 text-[11px] text-slate-400 transition hover:border-gold-500/30 hover:bg-gold-500/[0.08] hover:text-gold-200"
          onClick={() => onPick(g)}
          title={t("Show only this genre")}
        >
          {g}
        </button>
      ))}
      {genres.length > MAX_GENRES && (
        <span className="text-[11px] text-slate-600" title={genres.join(", ")}>
          +{genres.length - MAX_GENRES}
        </span>
      )}
    </div>
  );
}

function TypeBadge({ isMovie }: { isMovie: boolean }) {
  const { t } = useI18n();
  return (
    <span
      className={clsx(
        "chip shrink-0",
        isMovie
          ? "border-sky-500/25 bg-sky-500/10 text-sky-300"
          : "border-violet-500/25 bg-violet-500/10 text-violet-300",
      )}
    >
      {isMovie ? <Film className="h-3 w-3" /> : <Tv className="h-3 w-3" />}
      {isMovie ? t("Movie") : t("Serial")}
    </span>
  );
}

type CardProps = {
  s: LibrarySeries;
  onDeleted: () => void;
  onOpenCard: (id: string) => void;
  onPickGenre: (genre: string) => void;
};

function SeriesCard({ s, onDeleted, onOpenCard, onPickGenre }: CardProps) {
  const { t } = useI18n();
  const { kpauth } = useApp();
  const itemId = itemIdOf(s);
  const [open, setOpen] = useState(false);
  const { episodes, missing, seasons, quality, bySeason, single, hasList } = useSeriesStats(s);
  const { openPath, remove, removeEpisode, deleting, removingKey } = useSeriesActions(s, onDeleted);
  const genres = s.genres ?? [];
  const canOpenCard = kpauth.loggedIn && !!itemId;

  // Facts about the entry, dot-separated on one quiet line under the title.
  const facts: ReactNode[] = [];
  if (!s.isMovie && seasons > 1) {
    facts.push(
      <span key="seasons" className="inline-flex items-center gap-1.5">
        <Layers className="h-3.5 w-3.5 text-slate-500" /> {t("{n} seasons", { n: seasons })}
      </span>,
    );
  }
  if (hasList) {
    facts.push(
      <span key="eps" className="inline-flex items-center gap-1.5">
        <ListVideo className="h-3.5 w-3.5 text-slate-500" /> {t("{n} episodes", { n: episodes.length })}
      </span>,
    );
  }
  facts.push(
    <span key="size" className="inline-flex items-center gap-1.5">
      <HardDrive className="h-3.5 w-3.5 text-slate-500" /> {bytes(s.totalBytes)}
    </span>,
  );
  if (quality) {
    facts.push(
      <span key="quality" className="inline-flex items-center gap-1.5">
        <MonitorPlay className="h-3.5 w-3.5 text-slate-500" /> {quality}
      </span>,
    );
  }
  if (s.updatedAt) facts.push(<span key="when">{relTime(s.updatedAt, t)}</span>);

  return (
    <div className="card animate-fade-in overflow-hidden transition hover:border-white/[0.12]">
      <div className="flex gap-4 p-4">
        {/* The poster is a shortcut to the catalog card when we can open one; a
            plain image otherwise, so it isn't announced as a dead button.
            self-start keeps it out of the flex row's default stretch: a stretched
            item gets a definite height, which overrides aspect-ratio and squeezes
            the art into a tall strip once the card grows. */}
        {canOpenCard ? (
          <button
            className="shrink-0 self-start rounded-lg transition hover:ring-2 hover:ring-gold-500/40"
            onClick={() => onOpenCard(itemId)}
            title={t("Open card")}
          >
            <PosterImage url={s.posterUrl} alt={s.title} className="aspect-[2/3] w-20 rounded-lg border border-white/[0.08]" />
          </button>
        ) : (
          <PosterImage
            url={s.posterUrl}
            alt={s.title}
            className="aspect-[2/3] w-20 shrink-0 self-start rounded-lg border border-white/[0.08]"
          />
        )}

        <div className="min-w-0 flex-1">
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              <h3 className="truncate text-base font-semibold text-slate-100">{s.title}</h3>
              {s.originalTitle && <p className="truncate text-sm text-slate-500">{s.originalTitle}</p>}
            </div>
            <TypeBadge isMovie={s.isMovie} />
          </div>

          <div className="mt-2 flex flex-wrap items-center gap-x-2.5 gap-y-1 text-xs text-slate-400">
            {facts.map((f, i) => (
              <Fragment key={i}>
                {i > 0 && <span className="text-slate-700">·</span>}
                {f}
              </Fragment>
            ))}
          </div>

          <VoiceoverSummary episodes={episodes} />

          {genres.length > 0 && (
            <div className="mt-2">
              <GenreChips genres={genres} onPick={onPickGenre} />
            </div>
          )}

          {(missing > 0 || (single && !single.exists)) && (
            <p className="mt-2 inline-flex items-center gap-1.5 rounded-lg bg-ember-500/[0.08] px-2 py-1 text-xs text-ember-400">
              <XCircle className="h-3.5 w-3.5 shrink-0" />
              {single ? t("File missing") : t("{n} missing", { n: missing })}
            </p>
          )}

          <button
            className="mt-2 flex w-full items-center gap-1.5 text-left font-mono text-[11px] text-slate-600 transition hover:text-slate-400"
            onClick={() => openPath(s.dir)}
            title={t("Open folder")}
          >
            <FolderOpen className="h-3 w-3 shrink-0" />
            <span className="truncate">{s.dir}</span>
          </button>

          {single && <FileVersions episode={single} />}

          {/* Actions live on the card itself — only the episode list is behind a cut. */}
          <div className="mt-3 flex flex-wrap items-center gap-2 text-xs">
            {hasList && (
              <button className="btn-ghost px-3 py-1.5 text-xs" onClick={() => setOpen((v) => !v)}>
                {open ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
                {t("Episodes ({n})", { n: episodes.length })}
              </button>
            )}
            {single && single.exists && (
              <button className="btn-ghost px-3 py-1.5 text-xs text-gold-300" onClick={() => openPath(single.path)}>
                <Play className="h-3.5 w-3.5" /> {t("Play")}
              </button>
            )}
            <div className="ml-auto flex flex-wrap items-center gap-2">
              {canOpenCard && (
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
          </div>
        </div>
      </div>

      {open && hasList && (
        <div className="border-t border-white/[0.05] bg-black/20 p-3">
          <EpisodeList
            bySeason={bySeason}
            removingKey={removingKey}
            onOpen={(e) => openPath(e.path)}
            onDelete={removeEpisode}
          />
        </div>
      )}
    </div>
  );
}

// SeriesTile is the poster-grid form of a library entry. Its primary action is
// spelled out twice — as a permanent badge on the poster and as a labelled
// button underneath — so it never depends on hover to be discoverable.
function SeriesTile({ s, onDeleted, onOpenCard, onPickGenre }: CardProps) {
  const { t } = useI18n();
  const { kpauth } = useApp();
  const itemId = itemIdOf(s);
  const [showEpisodes, setShowEpisodes] = useState(false);
  const { episodes, missing, seasons, quality, bySeason, single, hasList } = useSeriesStats(s);
  const { openPath, remove, removeEpisode, deleting, removingKey } = useSeriesActions(s, onDeleted);
  const genres = s.genres ?? [];
  const canOpenCard = kpauth.loggedIn && !!itemId;
  const playable = single !== null && single.exists;

  const primary = () => (playable ? openPath(single.path) : hasList ? setShowEpisodes(true) : undefined);
  const primaryLabel = playable
    ? t("Play")
    : hasList
      ? t("Episodes ({n})", { n: episodes.length })
      : s.title;

  // Without hover the overlay would never appear and a tap would go straight to
  // playing the file, leaving no way to pick an episode or delete the entry. So
  // on touch the poster tap reveals the actions instead of firing the primary.
  const hoverable = useHoverCapable();
  const [revealed, setRevealed] = useState(false);
  const overlayShown = !hoverable && revealed;

  // What the entry holds, in words: "Сериал · 3 сезона · 30 эп." / "Фильм".
  const holds = [s.isMovie ? t("Movie") : t("Serial")];
  if (seasons > 1) holds.push(t("{n} seasons", { n: seasons }));
  if (hasList) holds.push(t("{n} ep", { n: episodes.length }));

  return (
    <div className="group animate-fade-in">
      <div className="relative overflow-hidden rounded-xl border border-white/[0.08] transition group-hover:border-white/[0.16]">
        <PosterImage url={s.posterUrl} alt={s.title} className="aspect-[2/3] w-full" />

        {/* The whole poster is the primary action; the overlay below sits on top
            of it and takes over once it appears. */}
        <button
          className="absolute inset-0 z-10"
          onClick={() => (hoverable ? primary() : setRevealed((v) => !v))}
          title={hoverable ? primaryLabel : t("Show actions")}
          aria-label={hoverable ? primaryLabel : t("Show actions")}
        />

        {/* Markers don't take pointer events, so they never eat the poster click. */}
        <span
          className={clsx(
            "pointer-events-none absolute left-1.5 top-1.5 z-20 grid h-6 w-6 place-items-center rounded-md bg-black/70 backdrop-blur-sm",
            s.isMovie ? "text-sky-300" : "text-violet-300",
          )}
          title={s.isMovie ? t("Movie") : t("Serial")}
        >
          {s.isMovie ? <Film className="h-3.5 w-3.5" /> : <Tv className="h-3.5 w-3.5" />}
        </span>
        {(missing > 0 || (single && !single.exists)) && (
          <span
            className="pointer-events-none absolute right-1.5 top-1.5 z-20 grid h-6 w-6 place-items-center rounded-md bg-black/70 text-ember-400 backdrop-blur-sm"
            title={single ? t("File missing") : t("{n} missing", { n: missing })}
          >
            <XCircle className="h-3.5 w-3.5" />
          </span>
        )}
        {quality && (
          <span className="pointer-events-none absolute bottom-1.5 left-1.5 z-20 rounded-md bg-black/75 px-1.5 py-0.5 text-[10px] font-semibold text-slate-200 backdrop-blur-sm">
            {quality}
          </span>
        )}

        {/* A small permanent badge advertises the primary action; it fades out
            as the overlay takes over. */}
        {(playable || hasList) && (
          <span
            className={clsx(
              "pointer-events-none absolute bottom-1.5 right-1.5 z-20 grid h-9 w-9 place-items-center rounded-full bg-gold-500 text-ink-950 shadow-glow transition group-hover:opacity-0",
              overlayShown && "opacity-0",
            )}
          >
            {playable ? <Play className="h-4 w-4" /> : <ListVideo className="h-4 w-4" />}
          </span>
        )}

        {/* The actions live over the poster rather than as a button row under
            every tile. Hover reveals them on a mouse; a tap does on touch. */}
        <div
          className={clsx(
            "absolute inset-0 z-30 flex flex-col items-center justify-center gap-2 bg-black/65 p-2 backdrop-blur-[2px] transition",
            hoverable
              ? "pointer-events-none opacity-0 group-hover:pointer-events-auto group-hover:opacity-100"
              : overlayShown
                ? "pointer-events-auto opacity-100"
                : "pointer-events-none opacity-0",
          )}
          onClick={() => !hoverable && setRevealed(false)}
        >
          {playable ? (
            <button className="btn-primary px-3 py-1.5 text-xs" onClick={() => openPath(single.path)}>
              <Play className="h-4 w-4" /> {t("Play")}
            </button>
          ) : hasList ? (
            <button className="btn-primary px-3 py-1.5 text-xs" onClick={() => setShowEpisodes(true)}>
              <ListVideo className="h-4 w-4" /> {t("Episodes ({n})", { n: episodes.length })}
            </button>
          ) : (
            <span className="chip border-ember-500/30 bg-ember-500/15 text-ember-300">
              <XCircle className="h-3.5 w-3.5" /> {t("File missing")}
            </span>
          )}
          <div className="flex items-center gap-1.5">
            {canOpenCard && (
              <button
                className="rounded-lg bg-white/10 p-2 text-slate-100 transition hover:bg-white/20"
                onClick={() => onOpenCard(itemId)}
                title={t("Open card")}
              >
                <Clapperboard className="h-4 w-4" />
              </button>
            )}
            <button
              className="rounded-lg bg-white/10 p-2 text-slate-100 transition hover:bg-white/20"
              onClick={() => openPath(s.dir)}
              title={t("Open folder")}
            >
              <FolderOpen className="h-4 w-4" />
            </button>
            <button
              className="rounded-lg bg-white/10 p-2 text-ember-300 transition hover:bg-ember-500/25 hover:text-ember-200"
              onClick={remove}
              disabled={deleting}
              title={t("Delete")}
            >
              {deleting ? <Spinner className="h-4 w-4" /> : <Trash2 className="h-4 w-4" />}
            </button>
          </div>
        </div>
      </div>

      <p className="mt-2 truncate text-sm font-semibold text-slate-100" title={s.title}>
        {s.title}
      </p>
      {single && <FileVersions episode={single} />}
      {/* Empty meta lines are omitted rather than filled: a blank filler row read
          as a hole under the title. Grid rows size to their tallest tile, so the
          posters in a row still line up. */}
      {s.originalTitle && (
        <p className="truncate text-[11px] text-slate-500" title={s.originalTitle}>
          {s.originalTitle}
        </p>
      )}
      <p className="mt-0.5 truncate text-[11px] text-slate-400">{holds.join(" · ")}</p>
      <p className="truncate text-[11px] text-slate-500">
        {bytes(s.totalBytes)}
        {s.updatedAt && ` · ${relTime(s.updatedAt, t)}`}
        {missing > 0 && <span className="text-ember-400"> · {t("{n} missing", { n: missing })}</span>}
      </p>
      <VoiceoverSummary episodes={episodes} />
      {genres.length > 0 && (
        <p className="truncate text-[11px] text-slate-600" title={genres.join(", ")}>
          {genres.slice(0, 2).join(" · ")}
        </p>
      )}

      {/* A tile has no room for an inline list, so its episodes open in a modal. */}
      <Modal
        open={showEpisodes}
        onClose={() => setShowEpisodes(false)}
        wide
        scrollable
        title={
          <span className="flex min-w-0 flex-col gap-1 pr-2">
            <span className="break-words">{s.title}</span>
            <span className="text-xs font-normal text-slate-500">{t("{n} episodes", { n: episodes.length })} · {t("{n} files", { n: episodes.reduce((count, e) => count + episodeFiles(e).filter((file) => file.exists).length, 0) })}</span>
          </span>
        }
      >
        <EpisodeList
          bySeason={bySeason}
          removingKey={removingKey}
          onOpen={(e) => openPath(e.path)}
          onDelete={removeEpisode}
        />
      </Modal>
    </div>
  );
}

type TypeTab = "all" | "movies" | "series";
type SortKey = "recent" | "title" | "size";
type ViewMode = "list" | "grid";

const VIEW_KEY = "kinopub.library.view";

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

export function LibraryPage({ onNew }: { onNew: () => void }) {
  const { toast, jobs } = useApp();
  const { t } = useI18n();
  const [data, setData] = useState<LibraryResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [typeTab, setTypeTab] = useState<TypeTab>("all");
  const [sortKey, setSortKey] = useState<SortKey>("recent");
  const [genre, setGenre] = useState("");
  const [query, setQuery] = useState("");
  // Tiles are the default: this page is a media library first, and a poster wall
  // reads faster than a stack of rows.
  const [view, setView] = useState<ViewMode>(() => {
    try { return localStorage.getItem(VIEW_KEY) === "list" ? "list" : "grid"; } catch { return "grid"; }
  });
  // The open card lives in the URL hash ("#/library/i/<id>") so it survives a
  // reload and browser back closes it.
  const cardId = useRoute().itemId ?? null;

  const [scanError, setScanError] = useState(false);
  const load = () => {
    setLoading(true);
    api
      .library()
      .then((d) => {
        setData(d);
        setScanError(false);
      })
      .catch((e) => {
        // A failed scan must land somewhere visible: leaving `data` null forever
        // rendered a permanent "Scanning your folders…" (this page replaced the
        // Queue, so the active downloads became invisible with it). A navigation
        // abort is not a failure — the page is leaving anyway.
        if (!isNavigationAbort(e)) setScanError(true);
        toast(e.message || t("Scan failed"), "error");
      })
      .finally(() => setLoading(false));
  };

  useEffect(load, []); // eslint-disable-line react-hooks/exhaustive-deps

  // A finished download is represented by its entry on disk, not by its job
  // card — but that entry only appears after a rescan. This watches finished
  // EPISODES as well as finished jobs: a season lands one file at a time, and
  // waiting for the whole job left every episode but the last missing from the
  // list below until the user pressed Rescan by hand.
  const doneCount = completedSignal(jobs);
  const prevDone = useRef<number | null>(null);
  useEffect(() => {
    if (prevDone.current !== null && doneCount > prevDone.current) load();
    prevDone.current = doneCount;
  }, [doneCount]); // eslint-disable-line react-hooks/exhaustive-deps

  const setViewMode = (v: ViewMode) => {
    setView(v);
    try { localStorage.setItem(VIEW_KEY, v); } catch { /* The selected view still applies to this session. */ }
  };

  const series = data?.series ?? [];
  const dirs = data?.dirs ?? [];

  // Nothing about the downloads can be judged before the scan lands: deciding
  // whether a failed job is stale needs to know what is on disk. Rendering the
  // section early flashed cards that then vanished, and made the header claim
  // "0 on disk" while it simply did not know yet.
  const scanned = data !== null;

  // Everything still in flight or needing attention (failed/canceled) stays
  // visible; a completed job is dropped so it isn't listed twice. A failed one
  // whose files a later run already put on disk is dropped for the same reason —
  // its entry below is the truth, and its frozen progress only misleads.
  // Declared after `series`: the filter reads it, and hoisting this above the
  // declaration threw a TDZ ReferenceError that only surfaced at render.
  //
  // In-flight jobs do NOT wait for the scan: this page is the only place with
  // pause/cancel controls, and hiding the cards behind a slow or failing
  // library walk left live downloads invisible and uncontrollable. Only the
  // failed/canceled ones wait — judging them stale needs to know what is on
  // disk, and rendering early flashed cards that then vanished.
  const liveJobs = jobs.filter((j) => {
    if (j.status === "completed") return false;
    if (j.status !== "failed" && j.status !== "canceled") return true;
    if (!scanned) return false;
    return !supersededByLibrary(j, series);
  });
  const dismissable = jobs.filter((j) => ["completed", "failed", "canceled"].includes(j.status)).length;
  const queuedCount = liveJobs.filter((j) => j.status !== "failed" && j.status !== "canceled").length;

  const clearFinished = async () => {
    try {
      const r = await api.clearJobs();
      toast(t("Cleared {n} finished jobs", { n: r.removed }), "info");
    } catch (e: any) {
      toast(e.message || "Error", "error");
    }
  };

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
      return matchesQuery(s, query);
    });
    list.sort((a, b) => {
      if (sortKey === "title") return a.title.localeCompare(b.title, "ru");
      if (sortKey === "size") return b.totalBytes - a.totalBytes;
      return (b.updatedAt || "").localeCompare(a.updatedAt || ""); // recent first (ISO dates sort lexically)
    });
    return list;
  }, [series, typeTab, genre, query, sortKey]);

  const filtered = typeTab !== "all" || !!genre || query.trim() !== "";
  const resetFilters = () => {
    setTypeTab("all");
    setGenre("");
    setQuery("");
  };

  const cardProps = {
    onDeleted: load,
    onOpenCard: (id: string) => pushRoute({ page: "library", itemId: id }),
    onPickGenre: setGenre,
  };

  return (
    <div className={clsx("mx-auto space-y-5", view === "grid" ? "max-w-6xl" : "max-w-4xl")}>
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold text-slate-100">{t("Offline library")}</h1>
          <p className="mt-1 text-sm text-slate-400">
            {!scanned
              ? scanError
                ? t("Couldn't scan your folders")
                : t("Scanning your folders…")
              : queuedCount > 0
                ? t("{n} in the queue · {m} on disk", { n: queuedCount, m: series.length })
                : t("Downloads found in your output folders")}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button className="btn-ghost" onClick={onNew} title={t("Download by a kino.watch link")}>
            <Link2 className="h-4 w-4" /> {t("Advanced download")}
          </button>
          <button className="btn-ghost" onClick={load} disabled={loading}>
            {loading ? <Spinner className="h-4 w-4" /> : <RefreshCw className="h-4 w-4" />}
            {t("Rescan")}
          </button>
        </div>
      </header>

      {/* Downloads in flight sit above the library rather than on a page of their
          own: a finished one just becomes an entry below, so keeping them apart
          meant the same title was listed twice. The section also renders when
          only FINISHED jobs remain: "Clear finished" lived inside it, and once
          the queue drained there was no way left to clear those records. */}
      {(liveJobs.length > 0 || (scanned && dismissable > 0)) && (
        <section className="space-y-3">
          <div className="flex items-center gap-3">
            <h2 className="text-xs font-semibold uppercase tracking-wide text-slate-400">
              {queuedCount > 0 ? t("Downloading") : t("Finished downloads")}
            </h2>
            {liveJobs.length > 0 && (
              <span className="chip border-gold-500/25 bg-gold-500/10 text-gold-300">{liveJobs.length}</span>
            )}
            <span className="h-px flex-1 bg-white/[0.06]" />
            {dismissable > 0 && (
              <button className="btn-ghost px-3 py-1.5 text-xs" onClick={clearFinished}>
                <Trash2 className="h-3.5 w-3.5" /> {t("Clear finished")}
              </button>
            )}
          </div>
          {liveJobs.length > 0 && (
            <div className="space-y-4">
              {liveJobs.map((j) => (
                <JobCard key={j.id} job={j} />
              ))}
            </div>
          )}
        </section>
      )}

      {scanned && liveJobs.length > 0 && series.length > 0 && (
        <div className="flex items-center gap-3 pt-1">
          <h2 className="text-xs font-semibold uppercase tracking-wide text-slate-400">{t("On disk")}</h2>
          <span className="chip border-white/10 bg-white/[0.04] text-slate-400">{series.length}</span>
          <span className="h-px flex-1 bg-white/[0.06]" />
        </div>
      )}

      {series.length > 0 && (
        <div className="space-y-3">
          <div className="relative">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" />
            <input
              className="input pl-9 pr-9"
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t("Search by title or genre…")}
            />
            {query && (
              <button
                className="absolute right-2.5 top-1/2 -translate-y-1/2 rounded-md p-1 text-slate-500 transition hover:bg-white/[0.06] hover:text-slate-200"
                onClick={() => setQuery("")}
                title={t("Clear search")}
              >
                <X className="h-4 w-4" />
              </button>
            )}
          </div>

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
            {filtered && <span className="text-xs text-slate-500">{t("{n} found", { n: shown.length })}</span>}
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
              <div className="flex items-center gap-1 rounded-xl border border-white/[0.06] bg-white/[0.02] p-1">
                <button
                  className={clsx(
                    "rounded-lg p-1.5 transition",
                    view === "list" ? "bg-gold-500/[0.14] text-gold-200" : "text-slate-400 hover:bg-white/[0.05] hover:text-slate-200",
                  )}
                  onClick={() => setViewMode("list")}
                  title={t("List view")}
                >
                  <List className="h-4 w-4" />
                </button>
                <button
                  className={clsx(
                    "rounded-lg p-1.5 transition",
                    view === "grid" ? "bg-gold-500/[0.14] text-gold-200" : "text-slate-400 hover:bg-white/[0.05] hover:text-slate-200",
                  )}
                  onClick={() => setViewMode("grid")}
                  title={t("Tiles view")}
                >
                  <LayoutGrid className="h-4 w-4" />
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Until the first scan lands `shown` is empty for want of data, not for
          want of matches — so say we are looking rather than render a void. A
          FAILED scan says so with a retry, instead of spinning forever. */}
      {!scanned ? (
        scanError ? (
          <EmptyState
            icon={<LibraryIcon className="h-7 w-7" />}
            title={t("Couldn't scan your folders")}
            action={
              <button className="btn-primary" onClick={load} disabled={loading}>
                {loading ? <Spinner className="h-4 w-4" /> : <RefreshCw className="h-4 w-4" />} {t("Rescan")}
              </button>
            }
          />
        ) : (
          <div className="flex items-center justify-center gap-2 py-16 text-sm text-slate-500">
            <Spinner className="h-4 w-4" /> {t("Scanning your folders…")}
          </div>
        )
      ) : series.length === 0 ? (
        liveJobs.length > 0 ? null : (
          <EmptyState
            icon={<LibraryIcon className="h-7 w-7" />}
            title={t("Nothing downloaded yet")}
            hint={dirs.filter(Boolean).join(", ")}
            action={
              <button className="btn-primary" onClick={onNew}>
                <Link2 className="h-4 w-4" /> {t("Advanced download")}
              </button>
            }
          />
        )
      ) : shown.length === 0 ? (
        <EmptyState
          icon={<LibraryIcon className="h-7 w-7" />}
          title={t("Nothing matches the filters")}
          action={
            <button className="btn-ghost" onClick={resetFilters}>
              <X className="h-4 w-4" /> {t("Reset filters")}
            </button>
          }
        />
      ) : view === "grid" ? (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
          {shown.map((s) => (
            <SeriesTile key={s.stateFile} s={s} {...cardProps} />
          ))}
        </div>
      ) : (
        <div className="space-y-4">
          {shown.map((s) => (
            <SeriesCard key={s.stateFile} s={s} {...cardProps} />
          ))}
        </div>
      )}

      {cardId && (
        <TitleDetail
          id={cardId}
          onClose={() => dismiss({ page: "library" })}
          onPick={(it) => replaceRoute({ page: "library", itemId: it.id })}
        />
      )}
    </div>
  );
}
