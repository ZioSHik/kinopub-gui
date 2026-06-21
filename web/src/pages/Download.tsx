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
import { parseSeasons } from "../lib/format";
import { Field, Toggle } from "../components/ui";
import { SeriesBrowser } from "../components/SeriesBrowser";
import { DirPicker } from "../components/DirPicker";

const QUALITIES = [
  { v: "", label: "Auto (highest)" },
  { v: "2160p", label: "2160p · 4K" },
  { v: "1080p", label: "1080p" },
  { v: "720p", label: "720p" },
  { v: "480p", label: "480p" },
  { v: "360p", label: "360p" },
];

export function DownloadPage({ onStarted, onSignIn }: { onStarted: () => void; onSignIn: () => void }) {
  const { settings, settingsLoaded, ffmpeg, auth, toast } = useApp();
  const { t } = useI18n();

  const [form, setForm] = useState<RunRequest>(() => ({
    url: "",
    outputPath: settings.outputPath,
    quality: settings.quality,
    container: settings.container,
    concurrency: settings.concurrency,
    retries: settings.retries,
    minIntervalMs: settings.minIntervalMs,
    proxy: settings.proxy,
    seasons: "",
    episodes: "",
    audio: "",
    audioMenu: true,
    force: false,
    noChunked: settings.noChunked,
    dryRun: false,
    ffmpegArgs: "",
    ffmpegPath: "",
    userAgent: "",
    cookie: "",
    browser: "",
    headers: null,
    feedFile: "",
    verbosity: settings.verbosity,
  }));

  const [advanced, setAdvanced] = useState(false);
  const [preview, setPreview] = useState<PreviewResponse | null>(null);
  const [selectedSeasons, setSelectedSeasons] = useState<Set<number> | null>(null);
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
      concurrency: settings.concurrency,
      retries: settings.retries,
      minIntervalMs: settings.minIntervalMs,
      proxy: settings.proxy,
      noChunked: settings.noChunked,
      verbosity: settings.verbosity,
    }));
  }, [settingsLoaded, settings]);

  const set = <K extends keyof RunRequest>(k: K, v: RunRequest[K]) =>
    setForm((f) => ({ ...f, [k]: v }));

  const seasonsString = (): string => {
    if (!preview || selectedSeasons === null) return "";
    return parseSeasons([...selectedSeasons]);
  };

  const toggleSeason = (n: number) => {
    setSelectedSeasons((cur) => {
      const all = preview ? preview.seasons.map((s) => s.number) : [];
      const base = cur === null ? new Set(all) : new Set(cur);
      base.has(n) ? base.delete(n) : base.add(n);
      return base;
    });
  };

  const errorToast = (msg: string, fallback: string) => {
    if (looksLikeTimeout(msg)) {
      toast(t("Request timed out — kino.pub may be unreachable without a VPN. Enable a VPN or set a proxy, then retry."), "error");
    } else {
      toast(msg || fallback, "error");
    }
  };

  const doPreview = async () => {
    if (!form.url.trim() && !form.feedFile.trim()) {
      toast(t("Enter a kino.pub URL first"), "error");
      return;
    }
    setPreviewing(true);
    try {
      const r = await api.preview({ ...form, dryRun: true });
      setPreview(r);
      setSelectedSeasons(null);
      toast(t('Resolved “{title}” · {n} episodes', { title: r.title, n: r.total }), "success");
    } catch (e: any) {
      errorToast(e.message, t("Preview failed"));
      setPreview(null);
    } finally {
      setPreviewing(false);
    }
  };

  const start = async () => {
    if (!form.url.trim() && !form.feedFile.trim()) {
      toast(t("Enter a kino.pub URL first"), "error");
      return;
    }
    if (!ffmpeg.ffmpegFound) {
      toast(t("ffmpeg not found — install it to download"), "error");
      return;
    }
    setStarting(true);
    try {
      const seedTitles = preview
        ? Object.fromEntries(
            preview.seasons.flatMap((s) => s.episodes.map((e) => [e.key, e.title])),
          )
        : null;
      await api.startJob({
        ...form,
        dryRun: false,
        seasons: seasonsString(),
        seedTitle: preview?.title || "",
        seedPoster: preview?.posterUrl || "",
        seedTitles,
      });
      toast(t("Download started"), "success");
      onStarted();
    } catch (e: any) {
      errorToast(e.message, t("Failed to start"));
    } finally {
      setStarting(false);
    }
  };

  return (
    <div className="mx-auto max-w-3xl space-y-5">
      <header>
        <h1 className="text-2xl font-bold text-slate-100">{t("New download")}</h1>
        <p className="mt-1 text-sm text-slate-400">
          {t("Paste a kino.pub page link, a podcast feed link, or a local feed file.")}
        </p>
      </header>

      <div className="card flex items-start gap-3 border-gold-500/20 bg-gold-500/[0.06] p-4 text-sm text-gold-200">
        <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" />
        <span>
          {t("kino.pub is often unavailable without a VPN. If requests hang or time out, enable a VPN or set a proxy below.")}
        </span>
      </div>

      {!auth.loggedIn && (
        <div className="card flex flex-wrap items-center gap-3 border-white/[0.08] p-4 text-sm text-slate-300">
          <KeyRound className="h-4 w-4 shrink-0 text-gold-400" />
          <span className="min-w-0 flex-1">
            {t(
              "You're not signed in. Page links (/item/view/…) need kino.pub cookies. Direct podcast feeds, local feed files and the Library work without signing in.",
            )}
          </span>
          <button className="btn-primary px-3 py-2" onClick={onSignIn}>
            {t("Sign in")}
          </button>
        </div>
      )}

      <div className="card space-y-4 p-5">
        <Field label={t("kino.pub URL or feed")}>
          <div className="flex gap-2">
            <div className="relative flex-1">
              <Link2 className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" />
              <input
                className="input pl-9"
                placeholder="https://kino.pub/item/view/38290"
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
              <Field label={t("Audio tracks")} hint={t('e.g. "anilibria,!jpn" — patterns; "!"=exclude')}>
                <input className="input" placeholder={t("all")} value={form.audio} onChange={(e) => set("audio", e.target.value)} />
              </Field>
              <Field label={t("Seasons")} hint={t("e.g. 1,3-5 — or use the browser below")}>
                <input
                  className="input"
                  placeholder={t("all")}
                  value={preview && selectedSeasons !== null ? seasonsString() : form.seasons}
                  onChange={(e) => {
                    set("seasons", e.target.value);
                    setSelectedSeasons(null);
                  }}
                />
              </Field>
              <Field label={t("Episodes")} hint={t("e.g. 1,3-5")}>
                <input className="input" placeholder={t("all")} value={form.episodes} onChange={(e) => set("episodes", e.target.value)} />
              </Field>
              <Field label={t("Concurrency")} hint={t("parallel downloads (1–16)")}>
                <input
                  type="number"
                  min={1}
                  max={16}
                  className="input"
                  value={form.concurrency}
                  onChange={(e) => set("concurrency", e.target.value === "" ? 1 : Math.max(1, Number(e.target.value)))}
                />
              </Field>
              <Field label={t("Retries")}>
                <input
                  type="number"
                  min={0}
                  className="input"
                  value={form.retries}
                  onChange={(e) => set("retries", e.target.value === "" ? 5 : Math.max(0, Number(e.target.value)))}
                />
              </Field>
              <Field label={t("Min interval (ms)")} hint={t("throttle requests (0–60000)")}>
                <input
                  type="number"
                  min={0}
                  max={60000}
                  className="input"
                  value={form.minIntervalMs}
                  onChange={(e) => set("minIntervalMs", e.target.value === "" ? 0 : Math.max(0, Number(e.target.value)))}
                />
              </Field>
              <Field label={t("Proxy")} hint={t("http / https / socks5")}>
                <input className="input" placeholder="socks5://127.0.0.1:1080" value={form.proxy} onChange={(e) => set("proxy", e.target.value)} />
              </Field>
            </div>

            <div className="grid gap-2 sm:grid-cols-2">
              <Toggle label={t("Interactive audio menu")} hint={t("Pick tracks before downloading")} checked={form.audioMenu} onChange={(v) => set("audioMenu", v)} />
              <Toggle label={t("Force re-download")} hint={t("Ignore completed state")} checked={form.force} onChange={(v) => set("force", v)} />
              <Toggle label={t("No chunked download")} hint={t("Stream everything via ffmpeg")} checked={form.noChunked} onChange={(v) => set("noChunked", v)} />
              <Toggle label={t("Verbose logs")} hint={t("Show debug-level log lines")} checked={form.verbosity === "verbose"} onChange={(v) => set("verbosity", v ? "verbose" : "normal")} />
            </div>

            <div className="grid gap-4 sm:grid-cols-2">
              <Field label={t("Extra ffmpeg args")} hint={t('advanced — e.g. "-c:v libx265 -crf 28"')}>
                <input className="input font-mono text-xs" value={form.ffmpegArgs} onChange={(e) => set("ffmpegArgs", e.target.value)} />
              </Field>
              <Field label={t("Local feed file")} hint={t("path to a saved RSS/XML feed (optional)")}>
                <input className="input font-mono text-xs" value={form.feedFile} onChange={(e) => set("feedFile", e.target.value)} />
              </Field>
            </div>

            <Field label={t("One-off cookie override")} hint={t("Leave empty to use saved credentials")}>
              <textarea
                className="input min-h-[60px] font-mono text-xs"
                placeholder="cf_clearance=...; _identity=..."
                value={form.cookie}
                onChange={(e) => set("cookie", e.target.value)}
              />
            </Field>
          </div>
        )}

        <div className="flex flex-wrap items-center gap-3 border-t border-white/[0.05] pt-4">
          <button className="btn-primary" onClick={start} disabled={starting}>
            {starting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Download className="h-4 w-4" />}
            {t("Start download")}
          </button>
          {!ffmpeg.ffmpegFound && (
            <span className="text-xs text-ember-400">{t("ffmpeg not detected — required to download")}</span>
          )}
        </div>
      </div>

      {preview && (
        <div className="card p-5">
          <SeriesBrowser preview={preview} selectedSeasons={selectedSeasons} onToggleSeason={toggleSeason} />
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
