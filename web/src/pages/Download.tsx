import { useEffect, useRef, useState } from "react";
import {
  ChevronDown,
  ChevronRight,
  Download,
  Eye,
  FolderOpen,
  KeyRound,
  Link2,
  Loader2,
  ShieldAlert,
} from "lucide-react";
import { api, type PreviewResponse, type RunRequest } from "../api";
import { useApp } from "../store";
import { useI18n, looksLikeTimeout } from "../i18n";
import { Field, Toggle } from "../components/ui";
import { SeriesBrowser } from "../components/SeriesBrowser";
import { DirPicker } from "../components/DirPicker";
import { InstallFFmpeg } from "../components/InstallFFmpeg";
import { SeasonAudioPicker } from "../components/SeasonAudioPicker";
import { chosenSeasonAudios, rememberSeasonAudio, seasonAudioIssue, seedSeasonAudio, type SeasonAudioSelection } from "../lib/seasonAudio";
import { audioSelectionSpecs } from "../lib/audio";

const QUALITIES = [
  { v: "", label: "Auto (highest)" },
  { v: "2160p", label: "2160p · 4K" },
  { v: "1080p", label: "1080p" },
  { v: "720p", label: "720p" },
  { v: "480p", label: "480p" },
  { v: "360p", label: "360p" },
];

export function DownloadPage({ onSignIn }: { onSignIn: () => void }) {
  const { settings, settingsLoaded, ffmpeg, kpauth, toast } = useApp();
  const { t } = useI18n();

  const [form, setForm] = useState<RunRequest>(() => ({
    url: "",
    outputPath: settings.outputPath,
    quality: settings.quality,
    container: settings.container,
    proxy: settings.proxy,
    seasons: "",
    episodes: "",
    audio: "",
    audioMenu: false,
    force: false,
    dryRun: false,
    ffmpegArgs: "",
    ffmpegPath: "",
    userAgent: "",
    verbosity: settings.verbosity,
  }));

  const [advanced, setAdvanced] = useState(false);
  const [preview, setPreview] = useState<PreviewResponse | null>(null);
  const [seasonAudioSel, setSeasonAudioSel] = useState<SeasonAudioSelection>({});
  const previewGeneration = useRef(0);
  useEffect(() => () => { ++previewGeneration.current; }, []);
  // Per-episode selection (episode keys "S{n}E{n}"). null until a preview loads;
  // a preview seeds it with all not-yet-downloaded episodes.
  const [selectedKeys, setSelectedKeys] = useState<Set<string> | null>(null);

  const selectableKeysOf = (p: PreviewResponse): string[] =>
    p.seasons.flatMap((s) => s.episodes.filter((e) => !e.completed).map((e) => e.key));
  const [previewing, setPreviewing] = useState(false);
  const [starting, setStarting] = useState(false);
  const [pickDir, setPickDir] = useState(false);

  // Seed form defaults from settings once the SSE snapshot has loaded (B3).
  // The seeded flag prevents re-seeding on subsequent SSE reconnects.
  const seeded = useRef(false);
  useEffect(() => {
    if (!settingsLoaded || seeded.current) return;
    seeded.current = true;
    setForm((f) => ({
      ...f,
      outputPath: f.outputPath || settings.outputPath,
      quality: settings.quality,
      container: settings.container,
      proxy: settings.proxy,
      verbosity: settings.verbosity,
    }));
  }, [settingsLoaded, settings]);

  const set = <K extends keyof RunRequest>(k: K, v: RunRequest[K]) => {
    if (k === "url" || k === "outputPath" || k === "proxy" || k === "force") {
      ++previewGeneration.current;
      setPreview(null);
      setSelectedKeys(null);
      setSeasonAudioSel({});
      setPreviewing(false);
    }
    setForm((f) => ({ ...f, [k]: v }));
  };
  const seasonGroups = (preview?.seasons || []).map((season) => ({
    number: season.number, audios: season.audios || [], episodeKeys: season.episodes.map((episode) => episode.key),
  }));
  const needsAudio = seasonGroups.some((group) => seasonAudioIssue(group, seasonAudioSel, selectedKeys || new Set()));
  const toggleSeasonAudio = (season: number, label: string) => setSeasonAudioSel((cur) => {
    const next = new Set(cur[season] || []);
    next.has(label) ? next.delete(label) : next.add(label);
    return { ...cur, [season]: next };
  });

  const toggleEpisode = (key: string) =>
    setSelectedKeys((cur) => {
      const base = new Set(cur ?? []);
      base.has(key) ? base.delete(key) : base.add(key);
      return base;
    });

  const toggleSeason = (season: number) =>
    setSelectedKeys((cur) => {
      const base = new Set(cur ?? []);
      const eps = preview?.seasons.find((s) => s.number === season)?.episodes.filter((e) => !e.completed) ?? [];
      const allOn = eps.length > 0 && eps.every((e) => base.has(e.key));
      eps.forEach((e) => (allOn ? base.delete(e.key) : base.add(e.key)));
      return base;
    });

  const selectAllEpisodes = () => setSelectedKeys(new Set(preview ? selectableKeysOf(preview) : []));
  const deselectAllEpisodes = () => setSelectedKeys(new Set());

  const errorToast = (msg: string, fallback: string) => {
    if (looksLikeTimeout(msg)) {
      toast(t("Request timed out — kino.watch may be unreachable without a VPN. Enable a VPN or set a proxy, then retry."), "error");
    } else {
      // t() passes an unknown string straight through, so a server message that
      // happens to have a translation (the duplicate guard's "already in the
      // queue") gets one, and everything else reads as the server wrote it.
      toast(t(msg) || fallback, "error");
    }
  };

  const doPreview = async () => {
    if (!form.url.trim()) {
      toast(t("Enter a kino.watch URL first"), "error");
      return;
    }
    const generation = ++previewGeneration.current;
    setPreviewing(true);
    setPreview(null);
    setSelectedKeys(null);
    try {
      const r = await api.preview({ ...form, dryRun: true });
      if (generation !== previewGeneration.current) return;
      setPreview(r);
      setSelectedKeys(new Set(selectableKeysOf(r)));
      setSeasonAudioSel(seedSeasonAudio(r.seriesId, r.seasons.map((season) => ({
        number: season.number, audios: season.audios || [], episodeKeys: season.episodes.map((episode) => episode.key),
      }))));
      toast(t('Resolved “{title}” · {n} episodes', { title: r.title, n: r.total }), "success");
    } catch (e: any) {
      if (generation !== previewGeneration.current) return;
      errorToast(e.message, t("Preview failed"));
      setPreview(null);
    } finally {
      if (generation === previewGeneration.current) setPreviewing(false);
    }
  };

  const start = async () => {
    if (!form.url.trim()) {
      toast(t("Enter a kino.watch URL first"), "error");
      return;
    }
    if (!ffmpeg.ffmpegFound) {
      toast(t("ffmpeg not found — install it to download"), "error");
      return;
    }
    if (!preview || !selectedKeys?.size) {
      toast(t("Preview this link before downloading."), "error");
      return;
    }
    if (needsAudio) {
      toast(t("Select at least one voiceover"), "error");
      return;
    }
    setStarting(true);
    try {
      const seedTitles = preview
        ? Object.fromEntries(
            preview.seasons.flatMap((s) => s.episodes.map((e) => [e.key, e.title])),
          )
        : null;
      const request = {
        ...form,
        dryRun: false,
        // The preview owns the explicit episode selection.
        episodeKeys: preview && selectedKeys ? [...selectedKeys] : undefined,
        seedTitle: preview?.title || "",
        seedPoster: preview?.posterUrl || "",
        seedTitles,
      };
      for (const group of seasonGroups) {
        const keys = group.episodeKeys.filter((key) => selectedKeys.has(key));
        if (!keys.length) continue;
        await api.startJob({ ...request, seasons: String(group.number), episodeKeys: keys, audio: "", audioMenu: false,
          audioSpecs: audioSelectionSpecs(chosenSeasonAudios(group, seasonAudioSel)) });
        rememberSeasonAudio(preview.seriesId, group, seasonAudioSel);
        setSelectedKeys((cur) => cur ? new Set([...cur].filter((key) => !keys.includes(key))) : cur);
      }
      toast(t("Added to the queue"), "success");
    } catch (e: any) {
      errorToast(e.message, t("Failed to start"));
    } finally {
      setStarting(false);
    }
  };

  return (
    <div className="mx-auto max-w-3xl space-y-5">
      <header>
        <h1 className="text-2xl font-bold text-slate-100">{t("Advanced download")}</h1>
        <p className="mt-1 text-sm text-slate-400">
          {t("Paste a kino.watch link to download it directly. The Catalog is the main way to find titles.")}
        </p>
      </header>

      <div className="card flex items-start gap-3 border-gold-500/20 bg-gold-500/[0.06] p-4 text-sm text-gold-200">
        <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" />
        <span>
          {t("kino.watch is often unavailable without a VPN. If requests hang or time out, enable a VPN or set a proxy below.")}
        </span>
      </div>

      {!kpauth.loggedIn && (
        <div className="card flex flex-wrap items-center gap-3 border-white/[0.08] p-4 text-sm text-slate-300">
          <KeyRound className="h-4 w-4 shrink-0 text-gold-400" />
          <span className="min-w-0 flex-1">
            {t("Sign in to kino.watch (Profile) to resolve and download titles.")}
          </span>
          <button className="btn-primary px-3 py-2" onClick={onSignIn}>
            {t("Sign in")}
          </button>
        </div>
      )}

      <div className="card space-y-4 p-5">
        <Field label={t("kino.watch link")}>
          <div className="flex gap-2">
            <div className="relative flex-1">
              <Link2 className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" />
              <input
                className="input pl-9"
                placeholder="https://kino.watch/item/view/38290"
                value={form.url}
                onChange={(e) => set("url", e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && doPreview()}
              />
            </div>
            <button className="btn-ghost" onClick={doPreview} disabled={previewing}>
              {previewing ? <Loader2 className="h-4 w-4 animate-spin" /> : <Eye className="h-4 w-4" />}
              {t("Preview")}
            </button>
          </div>
        </Field>

        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={t("Quality")}>
            <select className="input" value={form.quality} onChange={(e) => set("quality", e.target.value)}>
              {QUALITIES.map((q) => (
                <option key={q.v} value={q.v}>
                  {q.v === "" ? t("Auto (highest)") : q.label}
                </option>
              ))}
            </select>
          </Field>
          <Field label={t("Output folder")}>
            <button
              className="input flex items-center gap-2 text-left"
              onClick={() => setPickDir(true)}
              type="button"
            >
              <FolderOpen className="h-4 w-4 shrink-0 text-gold-400" />
              <span className="truncate font-mono text-xs">{form.outputPath || t("Choose…")}</span>
            </button>
          </Field>
        </div>

        <button
          className="flex items-center gap-1.5 text-sm font-medium text-slate-400 hover:text-slate-200"
          onClick={() => setAdvanced((v) => !v)}
          type="button"
        >
          {advanced ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
          {t("Advanced options")}
        </button>

        {advanced && (
          <div className="animate-fade-in space-y-4 border-t border-white/[0.05] pt-4">
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label={t("Container")}>
                <select className="input" value={form.container} onChange={(e) => set("container", e.target.value)}>
                  <option value="mkv">{t("MKV (best multi-audio)")}</option>
                  <option value="mp4">MP4</option>
                </select>
              </Field>
              <Field label={t("Proxy")} hint={t("http / https / socks5")}>
                <input className="input" placeholder="socks5://127.0.0.1:1080" value={form.proxy} onChange={(e) => set("proxy", e.target.value)} />
              </Field>
            </div>

            <div className="grid gap-2 sm:grid-cols-2">
              <Toggle label={t("Force re-download")} hint={t("Ignore completed state")} checked={form.force} onChange={(v) => set("force", v)} />
            </div>

            <Field label={t("Extra ffmpeg args")} hint={t('advanced — e.g. "-c:v libx265 -crf 28"')}>
              <input className="input font-mono text-xs" value={form.ffmpegArgs} onChange={(e) => set("ffmpegArgs", e.target.value)} />
            </Field>
          </div>
        )}

        <div className="flex flex-wrap items-center gap-3 border-t border-white/[0.05] pt-4">
          <button
            className="btn-primary"
            onClick={start}
            disabled={starting || previewing || !preview || !selectedKeys?.size || needsAudio}
          >
            {starting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Download className="h-4 w-4" />}
            {preview && selectedKeys ? t("Start download ({n})", { n: selectedKeys.size }) : t("Start download")}
          </button>
          {!ffmpeg.ffmpegFound && (
            <span className="text-xs text-ember-400">{t("ffmpeg not detected — required to download")}</span>
          )}
        </div>
        {!ffmpeg.ffmpegFound && <InstallFFmpeg className="border-t border-white/[0.05] pt-4" />}
        {!preview && <p className="text-xs text-slate-400">{t("Preview to choose episodes and season voiceovers.")}</p>}
      </div>

      {preview && (
        <div className="card space-y-3 p-5">
          <h2 className="font-semibold text-slate-200">{t("Voiceover")}</h2>
          <SeasonAudioPicker groups={seasonGroups} selected={seasonAudioSel} episodes={selectedKeys || new Set()} onToggle={toggleSeasonAudio} isSerial={preview.isSerial !== false} />
        </div>
      )}
      {preview && (
        <div className="card p-5">
          <SeriesBrowser
            preview={preview}
            selectedKeys={selectedKeys ?? new Set()}
            onToggleEpisode={toggleEpisode}
            onToggleSeason={toggleSeason}
            onSelectAll={selectAllEpisodes}
            onDeselectAll={deselectAllEpisodes}
          />
        </div>
      )}

      <DirPicker
        open={pickDir}
        initial={form.outputPath}
        onClose={() => setPickDir(false)}
        onSelect={(p) => set("outputPath", p)}
      />
    </div>
  );
}
