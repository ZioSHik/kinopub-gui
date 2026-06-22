import { useEffect, useMemo, useState } from "react";
import {
  Check,
  ChevronDown,
  ChevronRight,
  Download,
  Eye,
  Loader2,
  Mic2,
  Play,
  Star,
} from "lucide-react";
import {
  api,
  type DiscoverDetail,
  type DiscoverItem,
  imgURL,
} from "../api";
import { useApp } from "../store";
import { useI18n } from "../i18n";
import { Modal, PosterImage } from "./ui";
import { Ratings } from "./Ratings";
import { Player } from "./Player";

const QUALITIES = ["", "2160p", "1080p", "720p", "480p", "360p"];

function epKey(season: number, episode: number) {
  return `S${season}E${episode}`;
}

export function TitleDetail({
  id,
  onClose,
  onPick,
  onStarted,
}: {
  id: string;
  onClose: () => void;
  onPick: (item: DiscoverItem) => void;
  onStarted: () => void;
}) {
  const { settings, ffmpeg, toast } = useApp();
  const { t } = useI18n();

  const [detail, setDetail] = useState<DiscoverDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [similar, setSimilar] = useState<DiscoverItem[]>([]);

  const [quality, setQuality] = useState(settings.quality);
  // Selected озвучка labels. Empty set → keep every track.
  const [audioSel, setAudioSel] = useState<Set<string>>(new Set());
  // Selected episode keys (serials). null until detail loads.
  const [epSel, setEpSel] = useState<Set<string> | null>(null);
  const [openSeasons, setOpenSeasons] = useState<Set<number>>(new Set());
  const [starting, setStarting] = useState(false);
  // When set, the in-app player is open for this title (a serial episode, or the
  // whole title for a movie when season/episode are undefined).
  const [playing, setPlaying] = useState<{ season?: number; episode?: number } | null>(null);

  useEffect(() => {
    let alive = true;
    setLoading(true);
    setError("");
    setDetail(null);
    setEpSel(null);
    setAudioSel(new Set());
    api
      .discoverItem(id)
      .then((d) => {
        if (!alive) return;
        setDetail(d);
        setQuality(settings.quality);
        const keys = (d.seasons || []).flatMap((s) => s.episodes.map((e) => epKey(e.season, e.episode)));
        setEpSel(new Set(keys));
        setOpenSeasons(new Set((d.seasons || []).map((s) => s.number)));
      })
      .catch((e) => alive && setError(e.message || "Failed to load"))
      .finally(() => alive && setLoading(false));
    api
      .discoverSimilar(id)
      .then((r) => alive && setSimilar(r.items || []))
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [id]);

  const allEpKeys = useMemo(
    () => (detail?.seasons || []).flatMap((s) => s.episodes.map((e) => epKey(e.season, e.episode))),
    [detail],
  );

  const toggleAudio = (label: string) =>
    setAudioSel((cur) => {
      const next = new Set(cur);
      next.has(label) ? next.delete(label) : next.add(label);
      return next;
    });

  const toggleEpisode = (key: string) =>
    setEpSel((cur) => {
      const next = new Set(cur ?? []);
      next.has(key) ? next.delete(key) : next.add(key);
      return next;
    });

  const toggleSeason = (season: number) =>
    setEpSel((cur) => {
      const next = new Set(cur ?? []);
      const eps = detail?.seasons?.find((s) => s.number === season)?.episodes ?? [];
      const allOn = eps.length > 0 && eps.every((e) => next.has(epKey(e.season, e.episode)));
      eps.forEach((e) => (allOn ? next.delete(epKey(e.season, e.episode)) : next.add(epKey(e.season, e.episode))));
      return next;
    });

  const toggleSeasonOpen = (season: number) =>
    setOpenSeasons((cur) => {
      const next = new Set(cur);
      next.has(season) ? next.delete(season) : next.add(season);
      return next;
    });

  const isSerial = !!detail?.seasons && detail.seasons.length > 0;
  const selectedCount = isSerial ? epSel?.size ?? 0 : 1;

  const start = async () => {
    if (!detail) return;
    if (!ffmpeg.ffmpegFound) {
      toast(t("ffmpeg not found — install it to download"), "error");
      return;
    }
    if (isSerial && (!epSel || epSel.size === 0)) {
      toast(t("Select at least one episode"), "error");
      return;
    }
    const audioFilter = detail.audios
      .filter((a) => audioSel.has(a.label))
      .map((a) => a.filter)
      .filter(Boolean)
      .join(",");
    const seedTitles = Object.fromEntries(
      (detail.seasons || []).flatMap((s) => s.episodes.map((e) => [epKey(e.season, e.episode), e.title])),
    );
    setStarting(true);
    try {
      await api.startJob({
        url: detail.itemUrl,
        outputPath: settings.outputPath,
        quality,
        container: settings.container,
        concurrency: settings.concurrency,
        retries: settings.retries,
        minIntervalMs: settings.minIntervalMs,
        proxy: settings.proxy,
        seasons: "",
        episodes: "",
        episodeKeys: isSerial && epSel ? [...epSel] : undefined,
        audio: audioFilter,
        audioMenu: false,
        force: false,
        noChunked: settings.noChunked,
        dryRun: false,
        ffmpegArgs: "",
        ffmpegPath: "",
        userAgent: "",
        verbosity: settings.verbosity,
        seedTitle: detail.title,
        seedPoster: detail.poster,
        seedTitles,
      });
      toast(t("Download started"), "success");
      onStarted();
    } catch (e: any) {
      toast(e.message || t("Failed to start"), "error");
    } finally {
      setStarting(false);
    }
  };

  return (
    <>
    <Modal open onClose={onClose} wide title={detail?.title || (loading ? t("Loading…") : t("Title"))}>
      {loading ? (
        <div className="flex items-center justify-center py-16 text-slate-400">
          <Loader2 className="h-6 w-6 animate-spin" />
        </div>
      ) : error ? (
        <p className="py-8 text-center text-sm text-ember-400">{error}</p>
      ) : detail ? (
        <div className="space-y-5">
          <div className="flex flex-col gap-4 sm:flex-row">
            <PosterImage url={detail.poster} alt={detail.title} className="h-48 w-32 shrink-0 rounded-xl" />
            <div className="min-w-0 flex-1 space-y-2">
              {detail.originalTitle && <p className="-mt-1 text-sm text-slate-400">{detail.originalTitle}</p>}
              <div className="flex flex-wrap items-center gap-2 text-xs text-slate-400">
                {detail.year > 0 && <span className="chip">{detail.year}</span>}
                {detail.isSerial && <span className="chip">{t("Series")}</span>}
                {detail.durationMin ? <span className="chip">{detail.durationMin} {t("min")}</span> : null}
                <Ratings item={detail} className="ml-1" />
              </div>
              {detail.genres && detail.genres.length > 0 && (
                <p className="text-xs font-medium text-emerald-300/90">{detail.genres.join(", ")}</p>
              )}
              {detail.director && <MetaRow label={t("Director")} value={detail.director} />}
              {detail.cast && <MetaRow label={t("Cast")} value={detail.cast} />}
              {detail.countries && detail.countries.length > 0 && (
                <MetaRow label={t("Country")} value={detail.countries.join(", ")} />
              )}
              {detail.plot && <p className="max-h-32 overflow-y-auto text-sm leading-relaxed text-slate-300">{detail.plot}</p>}
            </div>
          </div>

          {/* Озвучки */}
          <div>
            <h3 className="mb-2 flex items-center gap-2 text-sm font-semibold text-slate-200">
              <Mic2 className="h-4 w-4 text-gold-400" /> {t("Voiceover")}
              <span className="text-xs font-normal text-slate-500">
                {audioSel.size === 0 ? t("(all tracks)") : t("({n} selected)", { n: audioSel.size })}
              </span>
            </h3>
            {detail.audios.length === 0 ? (
              <p className="text-xs text-slate-500">{t("Voiceover list appears after sign-in / for available titles.")}</p>
            ) : (
              <div className="flex flex-wrap gap-2">
                {detail.audios.map((a) => {
                  const on = audioSel.has(a.label);
                  return (
                    <button
                      key={a.label}
                      onClick={() => toggleAudio(a.label)}
                      className={`chip transition ${
                        on
                          ? "border-gold-500/40 bg-gold-500/[0.14] text-gold-200"
                          : "text-slate-300 hover:bg-white/[0.06]"
                      }`}
                    >
                      {on && <Check className="h-3 w-3" />}
                      {a.label}
                    </button>
                  );
                })}
              </div>
            )}
          </div>

          {/* Episodes (serials) */}
          {isSerial && epSel && (
            <div>
              <div className="mb-2 flex items-center justify-between">
                <h3 className="text-sm font-semibold text-slate-200">
                  {t("Episodes")}{" "}
                  <span className="text-xs font-normal text-slate-500">
                    {t("{n} of {m} selected", { n: epSel.size, m: allEpKeys.length })}
                  </span>
                </h3>
                <div className="flex gap-2 text-xs">
                  <button className="text-slate-400 hover:text-gold-300" onClick={() => setEpSel(new Set(allEpKeys))}>
                    {t("Select all")}
                  </button>
                  <button className="text-slate-400 hover:text-gold-300" onClick={() => setEpSel(new Set())}>
                    {t("Deselect all")}
                  </button>
                </div>
              </div>
              <div className="max-h-64 space-y-1.5 overflow-y-auto pr-1">
                {detail.seasons!.map((s) => {
                  const open = openSeasons.has(s.number);
                  const total = s.episodes.length;
                  const watched = s.episodes.filter((e) => e.watched).length;
                  const sel = s.episodes.filter((e) => epSel.has(epKey(e.season, e.episode))).length;
                  const allSel = total > 0 && sel === total;
                  const someSel = sel > 0 && !allSel;
                  return (
                    <div key={s.number} className="rounded-lg border border-white/[0.06] bg-ink-900/40">
                      <div className="flex items-center gap-2.5 px-3 py-2">
                        <button
                          onClick={() => toggleSeason(s.number)}
                          title={t("Toggle season")}
                          className={`grid h-4 w-4 shrink-0 place-items-center rounded border transition ${
                            allSel
                              ? "border-gold-500 bg-gold-500"
                              : someSel
                                ? "border-gold-500 bg-gold-500/30"
                                : "border-white/25 hover:border-white/40"
                          }`}
                        >
                          {allSel && <Check className="h-3 w-3 text-ink-950" strokeWidth={3} />}
                          {someSel && <span className="h-[2px] w-2 rounded bg-gold-300" />}
                        </button>
                        <button
                          onClick={() => toggleSeasonOpen(s.number)}
                          className="flex flex-1 items-center gap-1.5 text-left text-sm font-medium text-slate-200"
                        >
                          {open ? <ChevronDown className="h-4 w-4 text-slate-400" /> : <ChevronRight className="h-4 w-4 text-slate-400" />}
                          {t("Season {n}", { n: s.number })}
                        </button>
                        <span className="flex items-center gap-2 text-xs text-slate-500">
                          {watched > 0 && (
                            <span className="inline-flex items-center gap-0.5 text-emerald-500/70" title={t("Watched")}>
                              <Eye className="h-3 w-3" /> {watched}
                            </span>
                          )}
                          <span className={allSel ? "text-gold-300" : ""}>
                            {sel}/{total}
                          </span>
                        </span>
                      </div>
                      {open && (
                        <div className="border-t border-white/[0.05]">
                          {s.episodes.map((e) => {
                            const key = epKey(e.season, e.episode);
                            const on = epSel.has(key);
                            return (
                              <div
                                key={key}
                                className={`group flex items-center text-sm transition ${
                                  on ? "bg-gold-500/[0.10]" : "hover:bg-white/[0.03]"
                                }`}
                              >
                                <button
                                  onClick={() => toggleEpisode(key)}
                                  title={e.watched ? t("Watched") : undefined}
                                  className="flex flex-1 items-center gap-2.5 px-3 py-1.5 text-left"
                                >
                                  <span
                                    className={`grid h-6 w-7 shrink-0 place-items-center rounded text-xs font-semibold ${
                                      on ? "bg-gold-500/25 text-gold-200" : "bg-white/[0.05] text-slate-400"
                                    }`}
                                  >
                                    {e.episode}
                                  </span>
                                  <span className={`flex-1 truncate ${e.watched ? "text-slate-500" : on ? "text-slate-100" : "text-slate-400"}`}>
                                    {e.title}
                                  </span>
                                  {e.watched && <Eye className="h-3.5 w-3.5 shrink-0 text-emerald-500/70" />}
                                  {on && <Check className="h-3.5 w-3.5 shrink-0 text-gold-300" />}
                                </button>
                                <button
                                  onClick={() => setPlaying({ season: e.season, episode: e.episode })}
                                  title={t("Watch")}
                                  className="mr-2 shrink-0 rounded-md p-1.5 text-gold-400/80 transition hover:bg-gold-500/15 hover:text-gold-200"
                                >
                                  <Play className="h-4 w-4" />
                                </button>
                              </div>
                            );
                          })}
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            </div>
          )}

          {/* Download bar */}
          <div className="flex flex-wrap items-center gap-3 border-t border-white/[0.05] pt-4">
            <select className="input w-auto" value={quality} onChange={(e) => setQuality(e.target.value)}>
              {QUALITIES.map((q) => (
                <option key={q} value={q}>
                  {q === "" ? t("Auto (highest)") : q}
                </option>
              ))}
            </select>
            <button className="btn-primary" onClick={start} disabled={starting || (isSerial && selectedCount === 0)}>
              {starting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Download className="h-4 w-4" />}
              {isSerial ? t("Download ({n})", { n: selectedCount }) : t("Download")}
            </button>
            {!isSerial && (
              <button
                className="inline-flex items-center gap-2 rounded-xl bg-emerald-500/90 px-4 py-2 text-sm font-semibold text-ink-950 transition hover:bg-emerald-400"
                onClick={() => setPlaying({})}
              >
                <Play className="h-4 w-4" /> {t("Watch")}
              </button>
            )}
            {!ffmpeg.ffmpegFound && (
              <span className="text-xs text-ember-400">{t("ffmpeg not detected — required to download")}</span>
            )}
          </div>

          {/* Similar */}
          {similar.length > 0 && (
            <div className="border-t border-white/[0.05] pt-4">
              <h3 className="mb-2 text-sm font-semibold text-slate-200">{t("Similar")}</h3>
              <div className="flex gap-3 overflow-x-auto pb-1">
                {similar.slice(0, 12).map((it) => (
                  <button
                    key={it.id}
                    onClick={() => onPick(it)}
                    className="w-24 shrink-0 text-left"
                    title={it.title}
                  >
                    <img
                      src={imgURL(it.poster)}
                      alt={it.title}
                      loading="lazy"
                      className="h-32 w-24 rounded-lg object-cover"
                      onError={(e) => ((e.currentTarget as HTMLImageElement).style.visibility = "hidden")}
                    />
                    <p className="mt-1 truncate text-xs text-slate-400">{it.title}</p>
                  </button>
                ))}
              </div>
            </div>
          )}
        </div>
      ) : null}
    </Modal>
    {playing && detail && (
      <Player
        key={`${detail.id}-${playing.season ?? ""}-${playing.episode ?? ""}`}
        id={detail.id}
        season={playing.season}
        episode={playing.episode}
        title={detail.title}
        episodes={(detail.seasons || []).flatMap((s) =>
          s.episodes.map((e) => ({ season: e.season, episode: e.episode, title: e.title })),
        )}
        onChangeEpisode={(season, episode) => setPlaying({ season, episode })}
        onClose={() => setPlaying(null)}
      />
    )}
    </>
  );
}

// MetaRow renders a labelled metadata line (Director / Cast / Country).
function MetaRow({ label, value }: { label: string; value: string }) {
  return (
    <p className="text-xs leading-relaxed text-slate-400">
      <span className="font-semibold text-slate-300">{label}: </span>
      {value}
    </p>
  );
}
