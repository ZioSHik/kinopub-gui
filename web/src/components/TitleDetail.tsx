import { useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";
import {
  CalendarDays,
  Check,
  ChevronDown,
  ChevronRight,
  Clock,
  Download,
  Eye,
  HardDrive,
  Layers,
  ListVideo,
  Loader2,
  Mic2,
  MonitorPlay,
  Play,
  Sparkles,
  X,
} from "lucide-react";
import {
  api,
  isNavigationAbort,
  type DiscoverDetail,
  type DiscoverItem,
  type DownloadedEpisode,
  imgURL,
} from "../api";
import { useApp } from "../store";
import { useI18n } from "../i18n";
import { pushRoute } from "../router";
import {
  audioIdentity,
  isTaggedCodec,
  matchRememberedAudio,
  readAudioPref,
  TAGGED_CODECS,
  writeAudioPref,
} from "../lib/audio";
import { voiceoverLabels } from "../lib/libraryAudio";
import { existingDownloads, matchesDownload, preferredRecordedAudio } from "../lib/downloaded";
import { initialEpisodeSelection, isQueued, nothingLeftToQueue, queueCoverage } from "../lib/queue";
import { Modal, PosterImage } from "./ui";
import { Ratings } from "./Ratings";
import { Player } from "./Player";
import { AudioPicker } from "./AudioPicker";

const QUALITIES = ["", "2160p", "1080p", "720p", "480p", "360p"];

// Seasons are expanded on open only while the whole list stays scannable; past
// this many episodes everything but the first season starts collapsed so the
// card doesn't open onto a wall of rows.
const AUTO_EXPAND_LIMIT = 60;

// Longer plots are clamped behind a "show more" toggle so the hero and the
// download controls stay close together.
const PLOT_CLAMP_CHARS = 320;

function epKey(season: number, episode: number) {
  return `S${season}E${episode}`;
}

// downloadedHint describes what an episode on disk actually came out in —
// resolution and voiceover — and flags the voiceover as a substitute when the
// requested one wasn't offered for that episode.
function downloadedHint(ep: DownloadedEpisode, t: (k: string, v?: Record<string, string | number>) => string): string {
  const bits = [ep.resolution, voiceoverLabels(ep.audio).join(", ")].filter(Boolean) as string[];
  const base = bits.length ? t("Downloaded · {details}", { details: bits.join(" · ") }) : t("Downloaded");
  return ep.audioFallback ? `${base} — ${t("voiceover substituted")}` : base;
}

export function TitleDetail({
  id,
  onClose,
  onPick,
}: {
  id: string;
  onClose: () => void;
  onPick: (item: DiscoverItem) => void;
}) {
  const { settings, ffmpeg, toast, jobs } = useApp();
  const { t } = useI18n();

  const [detail, setDetail] = useState<DiscoverDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [similar, setSimilar] = useState<DiscoverItem[]>([]);

  const [quality, setQuality] = useState(settings.quality);
  // Selected voiceover labels. An empty selection waits for an explicit choice.
  const [audioSel, setAudioSel] = useState<Set<string>>(new Set());
  // True when a remembered voiceover existed but isn't available here, so the
  // user is prompted to pick another.
  const [audioPrefMissing, setAudioPrefMissing] = useState(false);
  // Episodes of this title already on disk, keyed by episode key. Carries what
  // each one was downloaded in — resolution and voiceover.
  const [downloadedFiles, setDownloadedFiles] = useState<DownloadedEpisode[]>([]);
  const matchingDownloads = useMemo(() => {
    // Until a voiceover is chosen, only the video tier determines what is
    // already on disk. Empty audio must not mark existing episodes as missing.
    const audios = audioSel.size ? detail?.audios || [] : [];
    return new Map(downloadedFiles
      .filter((file) => matchesDownload(file, quality, settings.container, audios, audioSel))
      .map((file) => [file.key, file]));
  }, [downloadedFiles, quality, settings.container, detail, audioSel]);
  const downloaded = useMemo(() => existingDownloads(downloadedFiles), [downloadedFiles]);
  const selectionVersion = useRef("");
  // Whether the on-disk scan has answered. The episode selection waits for it:
  // seeding before it lands would tick episodes that are already downloaded.
  const [downloadedReady, setDownloadedReady] = useState(false);
  // Selected episode keys (serials). null until detail loads.
  const [epSel, setEpSel] = useState<Set<string> | null>(null);
  // Whether the episode and voiceover selections have been seeded for this
  // title. Both are the user's to edit afterwards, so seeding happens once.
  const [seeded, setSeeded] = useState(false);
  const [openSeasons, setOpenSeasons] = useState<Set<number>>(new Set());
  const [plotOpen, setPlotOpen] = useState(false);
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
    setSeeded(false);
    setAudioSel(new Set());
    setAudioPrefMissing(false);
    setDownloadedFiles([]);
    selectionVersion.current = "";
    setDownloadedReady(false);
    setPlotOpen(false);
    api
      .discoverItem(id)
      .then((d) => {
        if (!alive) return;
        setDetail(d);
        setQuality(settings.quality);
        const keys = (d.seasons || []).flatMap((s) => s.episodes.map((e) => epKey(e.season, e.episode)));
        const seasons = d.seasons || [];
        setOpenSeasons(
          new Set(
            keys.length > AUTO_EXPAND_LIMIT
              ? seasons.slice(0, 1).map((s) => s.number)
              : seasons.map((s) => s.number),
          ),
        );
      })
      .catch((e) => alive && !isNavigationAbort(e) && setError(e.message || "Failed to load"))
      .finally(() => alive && setLoading(false));
    api
      .discoverSimilar(id)
      .then((r) => alive && setSimilar(r.items || []))
      .catch(() => {});
    api
      .libraryDownloaded(id, settings.outputPath)
      .then(
        (r) =>
          alive &&
          // Only files actually on disk count. The state file keeps a record for
          // an episode deleted in Finder (the Library shows it as "File
          // missing"), and counting it as downloaded removed the only way to
          // re-download it: the button read "Already downloaded — open" and the
          // episode dropped out of both the seeded selection and "Only missing".
          setDownloadedFiles((r.episodes || []).filter((e) => e.exists)),
      )
      .catch(() => {}) // a failed scan just means nothing is known to be on disk
      .finally(() => alive && setDownloadedReady(true));
    return () => {
      alive = false;
    };
  }, [id, settings.outputPath]);

  // What the queue already holds for this title. Read straight off the live job
  // list, so it is true the moment a job is submitted and stays true for as long
  // as that job has work left — no timers, no local "I just clicked it" flag that
  // could disagree with reality.
  // Scoped to the folder this card would download into: a job filling a
  // different folder is a different download by the server's own rule.
  const coverage = useMemo(
    () => queueCoverage(jobs, id, settings.outputPath),
    [jobs, id, settings.outputPath],
  );

  // The same thing in this card's own key format, so it compares directly
  // against the episode selection.
  const queuedKeys = useMemo(() => {
    const out = new Set<string>();
    for (const s of detail?.seasons || []) {
      for (const e of s.episodes) {
        if (isQueued(coverage, e.season, e.episode)) out.add(epKey(e.season, e.episode));
      }
    }
    return out;
  }, [detail, coverage]);

  // An episode already in the queue is not selectable, so it can never be sent
  // twice: it drops out of the selection as soon as it lands in the queue —
  // including the episodes just queued by this very card. Returning the previous
  // set unchanged when there is nothing to prune keeps this from looping.
  useEffect(() => {
    if (queuedKeys.size === 0) return;
    setEpSel((cur) => {
      if (!cur) return cur;
      const next = new Set([...cur].filter((k) => !queuedKeys.has(k)));
      return next.size === cur.size ? cur : next;
    });
  }, [queuedKeys]);

  const allEpKeys = useMemo(
    () => (detail?.seasons || []).flatMap((s) => s.episodes.map((e) => epKey(e.season, e.episode))),
    [detail],
  );

  // Seed the episode and voiceover selections once BOTH the episode list and the
  // on-disk scan have answered — they arrive on separate requests, and seeding on
  // the list alone would tick every episode, including what is already
  // downloaded (the engine skips those, so the count promised work that would
  // never happen) and would decide the voiceover without knowing what the
  // existing episodes are actually in. Runs once per title; afterwards the user
  // owns both selections.
  useEffect(() => {
    if (!detail || !downloadedReady || seeded) return;
    setSeeded(true);
    setEpSel(initialEpisodeSelection(allEpKeys, matchingDownloads, queuedKeys));

    if (!detail.audios.length) return;
    // What to pre-tick, most truthful source first: the voiceover the episodes on
    // disk are actually in, then this title's own remembered choice, then the
    // last choice made anywhere as a mere starting guess.
    const onDisk = downloadedFiles.flatMap((e) => e.audio || []);
    const fromDisk = preferredRecordedAudio(detail.audios, downloadedFiles);
    const remembered = readAudioPref(id);
    const matched = fromDisk.length ? fromDisk : matchRememberedAudio(detail.audios, remembered.prefs);

    if (matched.length) {
      setAudioSel(new Set(matched.map((a) => a.label)));
      return;
    }
    // Nothing to carry over: leave every voiceover unticked and let the user
    // choose. Pre-ticking all of them meant a click on Download quietly fetched
    // every dub the title had, multiplying the size of each episode.
    //
    // Warn only when the expectation belonged to THIS title — episodes on disk in
    // a dub that is gone, or a choice made here before. A choice carried over
    // from an unrelated title not being offered is ordinary, and warning about it
    // was what put the message on titles the user had never touched.
    setAudioSel(new Set());
    setAudioPrefMissing(onDisk.length > 0 || (remembered.scoped && remembered.prefs.length > 0));
  }, [detail, downloadedReady, seeded, allEpKeys, downloaded, matchingDownloads, downloadedFiles, queuedKeys, id]);

  // A changed quality or voiceover needs a fresh missing-episode selection.
  // Keep manual episode choices while this selection remains unchanged.
  useEffect(() => {
    if (!seeded || !downloadedReady) return;
    const signature = JSON.stringify([id, quality, settings.container, [...audioSel].sort()]);
    if (selectionVersion.current === signature) return;
    selectionVersion.current = signature;
    setEpSel(initialEpisodeSelection(allEpKeys, matchingDownloads, queuedKeys));
  }, [seeded, downloadedReady, id, quality, settings.container, audioSel, allEpKeys, matchingDownloads, queuedKeys]);

  const toggleAudio = (label: string) =>
    setAudioSel((cur) => {
      const next = new Set(cur);
      next.has(label) ? next.delete(label) : next.add(label);
      return next;
    });

  const needsAudio = !!detail?.audios.length && audioSel.size === 0;
  const allAudioOn = !!detail && detail.audios.length > 0 && audioSel.size === detail.audios.length;
  const toggleAllAudio = () =>
    setAudioSel(() => (allAudioOn ? new Set() : new Set((detail?.audios || []).map((a) => a.label))));

  const toggleEpisode = (key: string) => {
    if (queuedKeys.has(key)) return; // already lined up — nothing to select
    setEpSel((cur) => {
      const next = new Set(cur ?? []);
      next.has(key) ? next.delete(key) : next.add(key);
      return next;
    });
  };

  const toggleSeason = (season: number) =>
    setEpSel((cur) => {
      const next = new Set(cur ?? []);
      const eps = (detail?.seasons?.find((s) => s.number === season)?.episodes ?? []).filter(
        (e) => !queuedKeys.has(epKey(e.season, e.episode)),
      );
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
  const queuedCount = useMemo(() => allEpKeys.filter((k) => queuedKeys.has(k)).length, [allEpKeys, queuedKeys]);
  // Everything this card could add is already lined up — in the queue, or on
  // disk. The engine skips what the state file records as complete, so a
  // "Download" armed over already-downloaded files would start a job that
  // finishes instantly having done nothing. The button stops offering to start
  // anything and points at the page holding the answer to "where is it then?".
  const nothingLeft = isSerial
    ? nothingLeftToQueue(allEpKeys, matchingDownloads, queuedKeys)
    : coverage.whole || coverage.refs.size > 0 || matchingDownloads.size > 0;
  // Which of the two it is, so the button can say the true one.
  const queuedSomething = isSerial ? queuedCount > 0 : coverage.whole || coverage.refs.size > 0;

  // The queue shares a page with the files it produces.
  const openQueue = () => pushRoute({ page: "library" });

  // Where the poster's play button goes: the first unwatched episode of a serial
  // (falling back to the very first), or the movie itself.
  const resumeAt = useMemo(() => {
    if (!detail?.seasons?.length) return {};
    const eps = detail.seasons.flatMap((s) => s.episodes);
    const next = eps.find((e) => !e.watched) || eps[0];
    return next ? { season: next.season, episode: next.episode } : {};
  }, [detail]);

  const start = async () => {
    if (!detail) return;
    if (!ffmpeg.ffmpegFound) {
      toast(t("ffmpeg not found — install it to download"), "error");
      return;
    }
    // Nothing here that isn't already downloading. The button shows the queue in
    // that state, so this only catches a click that raced the job list.
    if (nothingLeft) {
      openQueue();
      return;
    }
    if (isSerial && (!epSel || epSel.size === 0)) {
      toast(t("Select at least one episode"), "error");
      return;
    }
    const chosenAudios = detail.audios.filter((a) => audioSel.has(a.label));
    if (detail.audios.length > 0 && chosenAudios.length === 0) {
      toast(t("Select at least one voiceover"), "error");
      return;
    }
    // When every track is selected, send no filter (keep all). Otherwise build an
    // exact per-track spec so the chosen variant is matched precisely — separating
    // a plain dub from its AC3 sibling (same studio name). The discriminator is
    // the CODEC: tagged codecs (AC3/DTS/…) appear verbatim in the HLS track name,
    // so the entry REQUIRES that token; a plain (AAC/MP3) entry instead FORBIDS the
    // tagged tokens so it doesn't also match its AC3 sibling.
    // The token list lives in lib/audio.ts, shared with the rules that decide
    // which entries are pre-ticked — keeping a second copy here is how the two
    // drifted apart and one studio's AAC and AC3 renditions both got selected.
    const isTagged = isTaggedCodec;
    const allSelected = chosenAudios.length === detail.audios.length;
    const audioSpecs =
      allSelected || chosenAudios.length === 0
        ? undefined
        : chosenAudios.map((a) => {
            const require = [a.filter].filter(Boolean);
            if (isTagged(a) && a.codec) require.push(a.codec);
            return { require, forbid: isTagged(a) ? [] : TAGGED_CODECS };
          });
    // Remember this voiceover choice for the next title/season. Stored by studio
    // identity, the same thing the specs above match on — remembering the whole
    // label made the next title claim the voiceover was unavailable whenever
    // kino.pub filed the same studio under a different type.
    writeAudioPref(id, chosenAudios.map(audioIdentity));
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
        proxy: settings.proxy,
        seasons: "",
        episodes: "",
        // Belt and braces: the selection is already free of queued episodes, but
        // never let one through even if the pruning above lost a race.
        episodeKeys: isSerial && epSel ? [...epSel].filter((k) => !queuedKeys.has(k)) : undefined,
        audio: "",
        audioSpecs,
        audioMenu: false,
        force: false,
        dryRun: false,
        ffmpegArgs: "",
        ffmpegPath: "",
        userAgent: "",
        verbosity: settings.verbosity,
        seedTitle: detail.title,
        seedPoster: detail.poster,
        seedTitles,
      });
      // No local "just queued" flag: the job list is the truth, and it flips the
      // button to "In the queue" as soon as the new job lands in it.
      toast(t("Added to the queue"), "success");
    } catch (e: any) {
      toast(t(e.message) || t("Failed to start"), "error");
    } finally {
      setStarting(false);
    }
  };

  const plot = detail?.plot || "";
  const plotLong = plot.length > PLOT_CLAMP_CHARS;
  const topQuality = detail?.qualities?.[0];

  return (
    <>
    <Modal open onClose={onClose} wide bare title={detail?.title || (loading ? t("Loading…") : t("Title"))}>
      {/* The card is a fixed-height column: the hero and the download bar stay
          put while only the middle section scrolls. Combined with the modal's
          body-scroll lock, nothing behind the card moves while it's open. */}
      <div className="relative flex max-h-[calc(100vh-2rem)] flex-col sm:max-h-[calc(100vh-4rem)]">
        <button
          onClick={onClose}
          title={t("Close")}
          className="absolute right-3 top-3 z-20 grid h-9 w-9 place-items-center rounded-full bg-ink-950/60 text-slate-300 backdrop-blur transition hover:bg-ink-950/90 hover:text-white"
        >
          <X className="h-4 w-4" />
        </button>

        {loading ? (
          <div className="flex h-64 items-center justify-center text-slate-400">
            <Loader2 className="h-6 w-6 animate-spin" />
          </div>
        ) : error ? (
          <p className="px-6 py-16 text-center text-sm text-ember-400">{error}</p>
        ) : detail ? (
          <>
            {/* ── Hero ───────────────────────────────────────────────── */}
            <div className="relative shrink-0 overflow-hidden">
              {/* The poster, blown up and blurred, tints the header with the
                  title's own palette; the gradient keeps text legible on top. */}
              <div aria-hidden className="pointer-events-none absolute inset-0">
                {detail.poster && (
                  <img
                    src={imgURL(detail.poster)}
                    alt=""
                    className="h-full w-full scale-110 object-cover opacity-30 blur-2xl"
                    onError={(e) => ((e.currentTarget as HTMLImageElement).style.display = "none")}
                  />
                )}
                <div className="absolute inset-0 bg-gradient-to-br from-ink-950/60 via-ink-850/80 to-ink-850" />
              </div>

              <div className="relative flex gap-4 p-5 sm:gap-6 sm:p-6">
                <div className="group relative shrink-0">
                  <PosterImage
                    url={detail.poster}
                    alt={detail.title}
                    className="h-[10.5rem] w-28 rounded-xl shadow-2xl ring-1 ring-white/10 sm:h-56 sm:w-[9.5rem]"
                  />
                  <button
                    onClick={() => setPlaying(resumeAt)}
                    title={t("Watch")}
                    className="absolute inset-0 grid place-items-center rounded-xl bg-ink-950/55 opacity-0 transition group-hover:opacity-100 focus-visible:opacity-100"
                  >
                    <span className="grid h-12 w-12 place-items-center rounded-full bg-gold-500 text-ink-950 shadow-glow">
                      <Play className="h-5 w-5 fill-current" />
                    </span>
                  </button>
                </div>

                <div className="min-w-0 flex-1 space-y-2.5 pr-8">
                  <div>
                    <h2 className="text-xl font-bold leading-tight text-white sm:text-2xl">{detail.title}</h2>
                    {detail.originalTitle && (
                      <p className="mt-1 truncate text-sm text-slate-400" title={detail.originalTitle}>
                        {detail.originalTitle}
                      </p>
                    )}
                  </div>

                  <div className="flex flex-wrap items-center gap-1.5">
                    {detail.year > 0 && (
                      <Tag icon={<CalendarDays className="h-3 w-3" />}>{detail.year}</Tag>
                    )}
                    <Tag icon={<MonitorPlay className="h-3 w-3" />}>
                      {detail.isSerial ? t("TV series") : t("Movie")}
                    </Tag>
                    {isSerial && detail.episodeCount > 0 && (
                      <Tag icon={<Layers className="h-3 w-3" />}>
                        {t("Episodes ({n})", { n: detail.episodeCount })}
                      </Tag>
                    )}
                    {detail.durationMin ? (
                      <Tag icon={<Clock className="h-3 w-3" />}>
                        {detail.durationMin} {t("min")}
                      </Tag>
                    ) : null}
                    {topQuality && <Tag accent>{t("up to {q}", { q: topQuality })}</Tag>}
                  </div>

                  <Ratings item={detail} />

                  {detail.genres && detail.genres.length > 0 && (
                    <p className="text-xs font-medium text-emerald-300/90">{detail.genres.join(" · ")}</p>
                  )}

                  {downloaded.size > 0 && (
                    <p className="inline-flex items-center gap-1.5 rounded-full bg-sky-500/10 px-2.5 py-1 text-xs font-medium text-sky-300">
                      <HardDrive className="h-3 w-3" />
                      {isSerial
                        ? t("{n} of {m} downloaded", { n: downloaded.size, m: allEpKeys.length })
                        : t("Downloaded")}
                    </p>
                  )}
                </div>
              </div>
            </div>

            {/* ── Scrolling body ─────────────────────────────────────── */}
            <div className="min-h-0 flex-1 space-y-6 overflow-y-auto overscroll-contain border-t border-white/[0.05] px-5 py-5 sm:px-6">
              {(plot || detail.director || detail.cast || detail.countries?.length) && (
                <section className="space-y-3">
                  {plot && (
                    <div>
                      <p
                        className={clsx(
                          "text-sm leading-relaxed text-slate-300",
                          plotLong && !plotOpen && "line-clamp-4",
                        )}
                      >
                        {plot}
                      </p>
                      {plotLong && (
                        <button
                          onClick={() => setPlotOpen((v) => !v)}
                          className="mt-1 text-xs font-semibold text-gold-300 transition hover:text-gold-200"
                        >
                          {plotOpen ? t("Show less") : t("Show more")}
                        </button>
                      )}
                    </div>
                  )}
                  {(detail.director || detail.cast || detail.countries?.length) && (
                    <div className="space-y-1.5">
                      {detail.director && <MetaRow label={t("Director")} value={detail.director} />}
                      {detail.countries && detail.countries.length > 0 && (
                        <MetaRow label={t("Country")} value={detail.countries.join(", ")} />
                      )}
                      {detail.cast && <MetaRow label={t("Cast")} value={detail.cast} />}
                    </div>
                  )}
                </section>
              )}

              {/* Озвучки */}
              <Section
                icon={<Mic2 className="h-4 w-4" />}
                title={t("Voiceover")}
                hint={
                  allAudioOn
                    ? t("(all selected)")
                    : audioSel.size === 0
                      ? t("(none)")
                      : t("({n} selected)", { n: audioSel.size })
                }
                action={
                  detail.audios.length > 1 && (
                    <button onClick={toggleAllAudio} className="text-gold-300 transition hover:text-gold-200">
                      {allAudioOn ? t("Deselect all") : t("Select all")}
                    </button>
                  )
                }
              >
                {audioPrefMissing && detail.audios.length > 0 && (
                  <p className="mb-2 text-xs text-gold-300/90">
                    {t("Your last voiceover isn't available here — pick another.")}
                  </p>
                )}
                {detail.audios.length === 0 ? (
                  <p className="text-xs text-slate-500">
                    {t("Voiceover list appears after sign-in / for available titles.")}
                  </p>
                ) : (
                  <AudioPicker audios={detail.audios} selected={audioSel} onToggle={toggleAudio} />
                )}
              </Section>

              {/* Episodes (serials) */}
              {isSerial && epSel && (
                <Section
                  icon={<Layers className="h-4 w-4" />}
                  title={t("Episodes")}
                  hint={t("{n} of {m} selected", { n: epSel.size, m: allEpKeys.length - queuedCount })}
                  action={
                    <>
                      {downloaded.size > 0 && (
                        <button
                          className="text-slate-400 transition hover:text-sky-300"
                          title={t("Select episodes missing the selected quality or voiceover")}
                          onClick={() =>
                            setEpSel(new Set(allEpKeys.filter((k) => !matchingDownloads.has(k) && !queuedKeys.has(k))))
                          }
                        >
                          {t("Only missing")}
                        </button>
                      )}
                      <button
                        className="text-slate-400 transition hover:text-gold-300"
                        onClick={() => setEpSel(new Set(allEpKeys.filter((k) => !queuedKeys.has(k))))}
                      >
                        {t("Select all")}
                      </button>
                      <button
                        className="text-slate-400 transition hover:text-gold-300"
                        onClick={() => setEpSel(new Set())}
                      >
                        {t("Deselect all")}
                      </button>
                    </>
                  }
                >
                  {!needsAudio && downloaded.size > 0 && matchingDownloads.size !== downloaded.size && (
                    <p className="mb-2 text-xs text-slate-400">
                      {t("{n} of {m} downloaded episodes match the selected quality and voiceover", {
                        n: matchingDownloads.size, m: downloaded.size,
                      })}
                    </p>
                  )}
                  <div className="space-y-2">
                    {detail.seasons!.map((s) => {
                      const open = openSeasons.has(s.number);
                      const total = s.episodes.length;
                      const watched = s.episodes.filter((e) => e.watched).length;
                      const dled = s.episodes.filter((e) => downloaded.has(epKey(e.season, e.episode))).length;
                      const qd = s.episodes.filter((e) => queuedKeys.has(epKey(e.season, e.episode))).length;
                      // Episodes already in the queue can't be picked, so they are
                      // not part of "all of this season" either.
                      const selectable = total - qd;
                      const sel = s.episodes.filter((e) => epSel.has(epKey(e.season, e.episode))).length;
                      const allSel = selectable > 0 && sel === selectable;
                      const someSel = sel > 0 && !allSel;
                      return (
                        <div
                          key={s.number}
                          className={clsx(
                            "overflow-hidden rounded-xl border bg-ink-900/50 transition",
                            allSel || someSel ? "border-gold-500/20" : "border-white/[0.06]",
                          )}
                        >
                          <div className="flex items-center gap-2.5 px-3 py-2.5">
                            <button
                              onClick={() => toggleSeason(s.number)}
                              title={t("Toggle season")}
                              className={clsx(
                                "grid h-[18px] w-[18px] shrink-0 place-items-center rounded-md border transition",
                                allSel
                                  ? "border-gold-500 bg-gold-500"
                                  : someSel
                                    ? "border-gold-500 bg-gold-500/30"
                                    : "border-white/25 hover:border-white/45",
                              )}
                            >
                              {allSel && <Check className="h-3 w-3 text-ink-950" strokeWidth={3} />}
                              {someSel && <span className="h-[2px] w-2 rounded bg-gold-300" />}
                            </button>
                            <button
                              onClick={() => toggleSeasonOpen(s.number)}
                              className="flex flex-1 items-center gap-1.5 text-left text-sm font-semibold text-slate-100"
                            >
                              {open ? (
                                <ChevronDown className="h-4 w-4 text-slate-500" />
                              ) : (
                                <ChevronRight className="h-4 w-4 text-slate-500" />
                              )}
                              {t("Season {n}", { n: s.number })}
                            </button>
                            <span className="flex items-center gap-2 text-xs">
                              {qd > 0 && (
                                <span
                                  className="inline-flex items-center gap-1 rounded-full bg-emerald-500/10 px-2 py-0.5 text-emerald-300/90"
                                  title={t("In the queue")}
                                >
                                  <ListVideo className="h-3 w-3" /> {qd}
                                </span>
                              )}
                              {dled > 0 && (
                                <span
                                  className="inline-flex items-center gap-1 rounded-full bg-sky-500/10 px-2 py-0.5 text-sky-300/90"
                                  title={t("Downloaded")}
                                >
                                  <HardDrive className="h-3 w-3" /> {dled}
                                </span>
                              )}
                              {watched > 0 && (
                                <span
                                  className="inline-flex items-center gap-1 rounded-full bg-emerald-500/10 px-2 py-0.5 text-emerald-300/90"
                                  title={t("Watched")}
                                >
                                  <Eye className="h-3 w-3" /> {watched}
                                </span>
                              )}
                              <span
                                className={clsx(
                                  "rounded-full px-2 py-0.5 font-medium",
                                  allSel ? "bg-gold-500/15 text-gold-300" : "bg-white/[0.04] text-slate-400",
                                )}
                              >
                                {sel}/{selectable}
                              </span>
                            </span>
                          </div>
                          {open && (
                            <div className="border-t border-white/[0.05]">
                              {s.episodes.map((e) => {
                                const key = epKey(e.season, e.episode);
                                const on = epSel.has(key);
                                const have = matchingDownloads.get(key) || downloaded.get(key);
                                const dl = !!have;
                                const qed = queuedKeys.has(key);
                                return (
                                  <div
                                    key={key}
                                    className={clsx(
                                      "group flex items-center border-l-2 text-sm transition",
                                      qed
                                        ? "border-l-emerald-500/50 bg-emerald-500/[0.05]"
                                        : on
                                          ? "border-l-gold-500/60 bg-gold-500/[0.07]"
                                          : "border-l-transparent hover:bg-white/[0.03]",
                                    )}
                                  >
                                    <button
                                      onClick={() => toggleEpisode(key)}
                                      disabled={qed}
                                      aria-pressed={on}
                                      title={qed ? t("In the queue") : e.watched ? t("Watched") : undefined}
                                      className="flex flex-1 items-center gap-2.5 px-3 py-2 text-left disabled:cursor-default"
                                    >
                                      <span
                                        className={clsx(
                                          "grid h-6 w-7 shrink-0 place-items-center rounded-md text-xs font-bold tabular-nums",
                                          on ? "bg-gold-500/25 text-gold-200" : "bg-white/[0.05] text-slate-400",
                                        )}
                                      >
                                        {e.episode}
                                      </span>
                                      <span
                                        className={clsx(
                                          "flex-1 truncate",
                                          e.watched ? "text-slate-500" : on ? "text-slate-100" : "text-slate-400",
                                        )}
                                      >
                                        <span className="block truncate">{e.title}</span>
                                        {have && (
                                          <span className="mt-0.5 block truncate text-[11px] font-normal text-sky-300/70"
                                            title={downloadedHint(have, t)}>
                                            {[have.resolution, voiceoverLabels(have.audio).join(", ") || t("Voiceover not recorded")].filter(Boolean).join(" · ")}
                                          </span>
                                        )}
                                      </span>
                                      {qed && (
                                        <span className="shrink-0 text-emerald-400/90" title={t("In the queue")}>
                                          <ListVideo className="h-3.5 w-3.5" />
                                        </span>
                                      )}
                                      {have && (
                                        <span
                                          // Amber rather than blue when the episode came out in a
                                          // different dub than the one asked for: it IS downloaded,
                                          // just not in what the picker above says.
                                          className={clsx(
                                            "shrink-0",
                                            have.audioFallback ? "text-amber-400/90" : "text-sky-400/80",
                                          )}
                                          title={downloadedHint(have, t)}
                                        >
                                          <HardDrive className="h-3.5 w-3.5" />
                                        </span>
                                      )}
                                      {e.watched && <Eye className="h-3.5 w-3.5 shrink-0 text-emerald-500/70" />}
                                      {on && <Check className="h-3.5 w-3.5 shrink-0 text-gold-300" strokeWidth={3} />}
                                    </button>
                                    <button
                                      onClick={() => setPlaying({ season: e.season, episode: e.episode })}
                                      title={t("Watch")}
                                      className="mr-2 shrink-0 rounded-lg p-1.5 text-slate-500 transition hover:bg-gold-500/15 hover:text-gold-200 group-hover:text-gold-400/90"
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
                </Section>
              )}

              {/* Similar */}
              {similar.length > 0 && (
                <Section icon={<Sparkles className="h-4 w-4" />} title={t("Similar")}>
                  <div className="-mx-1 flex gap-3 overflow-x-auto px-1 pb-2">
                    {similar.slice(0, 12).map((it) => (
                      <button
                        key={it.id}
                        onClick={() => onPick(it)}
                        className="group w-24 shrink-0 text-left"
                        title={it.originalTitle || it.title}
                      >
                        <div className="overflow-hidden rounded-xl ring-1 ring-white/[0.06] transition group-hover:ring-gold-500/40">
                          <img
                            src={imgURL(it.poster)}
                            alt={it.title}
                            loading="lazy"
                            className="h-36 w-24 object-cover transition duration-200 group-hover:scale-[1.04]"
                            onError={(e) => ((e.currentTarget as HTMLImageElement).style.visibility = "hidden")}
                          />
                        </div>
                        <p className="mt-1.5 truncate text-xs font-medium text-slate-300 transition group-hover:text-slate-100">
                          {it.title}
                        </p>
                        {it.year > 0 && <p className="truncate text-[11px] text-slate-500">{it.year}</p>}
                      </button>
                    ))}
                  </div>
                </Section>
              )}
            </div>

            {/* ── Download bar ───────────────────────────────────────── */}
            <div className="shrink-0 border-t border-white/[0.06] bg-ink-900/70 px-5 py-3.5 backdrop-blur sm:px-6">
              <div className="flex flex-wrap items-center gap-3">
                <select
                  className="input w-auto"
                  title={t("Quality")}
                  value={quality}
                  onChange={(e) => setQuality(e.target.value)}
                >
                  {["", ...(detail.qualities?.length ? detail.qualities : QUALITIES.filter(Boolean))].map((q) => (
                    <option key={q} value={q}>
                      {q === "" ? t("Auto (highest)") : q}
                    </option>
                  ))}
                </select>
                {needsAudio ? (
                  <button className="btn-primary" disabled>
                    <Mic2 className="h-4 w-4" />
                    {t("Choose a voiceover")}
                  </button>
                ) : nothingLeft ? (
                  <button
                    className="btn border border-emerald-500/40 bg-emerald-500/[0.16] text-emerald-200 hover:bg-emerald-500/[0.24]"
                    onClick={openQueue}
                    title={t("Open the queue")}
                  >
                    <Check className="h-4 w-4" />
                    {queuedSomething ? t("In the queue — open") : t("Already downloaded — open")}
                  </button>
                ) : (
                  <button
                    className="btn-primary"
                    onClick={start}
                    // Nothing is pre-ticked when the voiceover can't be carried
                    // over, so the button waits for a choice instead of taking
                    // the click and answering with a toast.
                    disabled={
                      starting ||
                      (isSerial && selectedCount === 0) ||
                      (detail.audios.length > 0 && audioSel.size === 0)
                    }
                  >
                    {starting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Download className="h-4 w-4" />}
                    {isSerial ? t("Download ({n})", { n: selectedCount }) : t("Download")}
                  </button>
                )}
                {/* Part of the title is already downloading and part isn't: the
                    button above takes the rest, this says where the first half
                    went. */}
                {!nothingLeft && queuedCount > 0 && (
                  <button
                    className="text-xs font-medium text-emerald-300/90 underline-offset-2 hover:underline"
                    onClick={openQueue}
                  >
                    {t("{n} already in the queue", { n: queuedCount })}
                  </button>
                )}
                <button className="btn-ghost" onClick={() => setPlaying(resumeAt)}>
                  <Play className="h-4 w-4" /> {t("Watch")}
                </button>
                {!ffmpeg.ffmpegFound && (
                  <span className="text-xs text-ember-400">{t("ffmpeg not detected — required to download")}</span>
                )}
              </div>
            </div>
          </>
        ) : null}
      </div>
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

// Tag is a compact hero pill (year, type, runtime, top quality).
function Tag({
  icon,
  accent,
  children,
}: {
  icon?: React.ReactNode;
  accent?: boolean;
  children: React.ReactNode;
}) {
  return (
    <span
      className={clsx(
        "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium",
        accent
          ? "border-gold-500/30 bg-gold-500/10 text-gold-200"
          : "border-white/[0.08] bg-white/[0.04] text-slate-300",
      )}
    >
      {icon}
      {children}
    </span>
  );
}

// Section is one labelled block of the card body (voiceover, episodes, similar),
// with an optional counter next to the heading and controls pinned right.
function Section({
  icon,
  title,
  hint,
  action,
  children,
}: {
  icon: React.ReactNode;
  title: string;
  hint?: string;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section>
      <div className="mb-2.5 flex items-center gap-2">
        <span className="text-gold-400">{icon}</span>
        <h3 className="text-sm font-semibold text-slate-100">{title}</h3>
        {hint && <span className="text-xs font-normal text-slate-500">{hint}</span>}
        {action && <div className="ml-auto flex items-center gap-3 text-xs">{action}</div>}
      </div>
      {children}
    </section>
  );
}

// MetaRow renders a labelled metadata line (Director / Country / Cast). The
// label column has a fixed width so the values line up in one column instead of
// each row starting at its own offset.
function MetaRow({ label, value }: { label: string; value: string }) {
  return (
    <p className="flex gap-3 text-xs leading-relaxed">
      <span className="w-20 shrink-0 font-medium text-slate-500">{label}</span>
      <span className="min-w-0 flex-1 text-slate-300" title={value}>
        {value}
      </span>
    </p>
  );
}
