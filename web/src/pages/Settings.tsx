import { useEffect, useState } from "react";
import { ArrowUpCircle, FolderOpen, FolderPlus, RefreshCw, Save, Server, Trash2 } from "lucide-react";
import { api, type FFmpegStatus, type Settings } from "../api";
import { useApp } from "../store";
import { useI18n } from "../i18n";
import { Field, Spinner, Toggle } from "../components/ui";
import { DirPicker } from "../components/DirPicker";
import { InstallFFmpeg } from "../components/InstallFFmpeg";
import { KinopubLogin } from "../components/KinopubLogin";

export function SettingsPage() {
  const { settings, ffmpeg, setSettingsLocal, toast } = useApp();
  const { t } = useI18n();
  const [form, setForm] = useState<Settings>(settings);
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const [pickOutput, setPickOutput] = useState(false);
  const [pickLib, setPickLib] = useState(false);

  // Only resync from store when the user has no unsaved edits (B4).
  // This prevents an SSE reconnect/blip from clobbering in-progress edits.
  useEffect(() => {
    if (!dirty) setForm(settings);
  }, [settings]);

  const set = <K extends keyof Settings>(k: K, v: Settings[K]) => {
    setDirty(true);
    setForm((f) => ({ ...f, [k]: v }));
  };

  const save = async () => {
    setSaving(true);
    try {
      const saved = await api.saveSettings(form);
      setSettingsLocal(saved);
      setDirty(false);
      toast(t("Settings saved"), "success");
    } catch (e: any) {
      toast(e.message || t("Save failed"), "error");
    } finally {
      setSaving(false);
    }
  };

  const libDirs = form.libraryDirs || [];

  return (
    <div className="mx-auto max-w-3xl space-y-5">
      <header>
        <h1 className="text-2xl font-bold text-slate-100">{t("Settings")}</h1>
        <p className="mt-1 text-sm text-slate-400">{t("Defaults applied to every new download.")}</p>
      </header>

      <KinopubLogin />

      <div className="card space-y-4 p-5">
        <Field label={t("Default output folder")}>
          <button className="input flex items-center gap-2 text-left" onClick={() => setPickOutput(true)} type="button">
            <FolderOpen className="h-4 w-4 shrink-0 text-gold-400" />
            <span className="truncate font-mono text-xs">{form.outputPath || t("Choose…")}</span>
          </button>
        </Field>

        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={t("Default quality")}>
            <select className="input" value={form.quality} onChange={(e) => set("quality", e.target.value)}>
              <option value="">{t("Auto (highest)")}</option>
              <option value="2160p">2160p · 4K</option>
              <option value="1080p">1080p</option>
              <option value="720p">720p</option>
              <option value="480p">480p</option>
              <option value="360p">360p</option>
            </select>
          </Field>
          <Field label={t("Container")}>
            <select className="input" value={form.container} onChange={(e) => set("container", e.target.value)}>
              <option value="mkv">MKV</option>
              <option value="mp4">MP4</option>
            </select>
          </Field>
          <Field label={t("Concurrency")}>
            <input type="number" min={1} max={16} className="input" value={form.concurrency} onChange={(e) => set("concurrency", e.target.value === "" ? 1 : Math.max(1, Number(e.target.value)))} />
          </Field>
          <Field label={t("Retries")}>
            <input type="number" min={0} className="input" value={form.retries} onChange={(e) => set("retries", e.target.value === "" ? 5 : Math.max(0, Number(e.target.value)))} />
          </Field>
          <Field label={t("Min interval (ms)")}>
            <input type="number" min={0} max={60000} className="input" value={form.minIntervalMs} onChange={(e) => set("minIntervalMs", e.target.value === "" ? 0 : Math.max(0, Number(e.target.value)))} />
          </Field>
          <Field label={t("Proxy")}>
            <input className="input" placeholder="socks5://127.0.0.1:1080" value={form.proxy} onChange={(e) => set("proxy", e.target.value)} />
          </Field>
        </div>

        <Toggle label={t("No chunked download by default")} hint={t("Stream everything through ffmpeg")} checked={form.noChunked} onChange={(v) => set("noChunked", v)} />
      </div>

      <div className="card space-y-3 p-5">
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-sm font-semibold text-slate-200">{t("Extra library folders")}</h2>
            <p className="text-xs text-slate-500">{t("Scanned in addition to the output folder.")}</p>
          </div>
          <button className="btn-ghost px-3 py-2" onClick={() => setPickLib(true)}>
            <FolderPlus className="h-4 w-4" /> {t("Add")}
          </button>
        </div>
        {libDirs.length === 0 ? (
          <p className="text-sm text-slate-500">{t("None added.")}</p>
        ) : (
          <div className="space-y-1.5">
            {libDirs.map((d) => (
              <div key={d} className="flex items-center gap-2 rounded-lg border border-white/[0.06] bg-ink-900/40 px-3 py-2">
                <FolderOpen className="h-4 w-4 shrink-0 text-gold-400/80" />
                <span className="min-w-0 flex-1 truncate font-mono text-xs text-slate-300">{d}</span>
                <button
                  className="rounded-md p-1 text-slate-500 hover:bg-white/[0.06] hover:text-ember-400"
                  onClick={() => set("libraryDirs", libDirs.filter((x) => x !== d))}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              </div>
            ))}
          </div>
        )}
      </div>

      <FFmpegInfo ffmpeg={ffmpeg} />

      <UpdateCard />

      <div className="flex justify-end">
        <button className="btn-primary" onClick={save} disabled={saving}>
          {saving ? <Spinner className="h-4 w-4" /> : <Save className="h-4 w-4" />}
          {t("Save settings")}
        </button>
      </div>

      <DirPicker open={pickOutput} initial={form.outputPath} onClose={() => setPickOutput(false)} onSelect={(p) => set("outputPath", p)} />
      <DirPicker
        open={pickLib}
        initial={form.outputPath}
        onClose={() => setPickLib(false)}
        onSelect={(p) => set("libraryDirs", [...new Set([...libDirs, p])])}
      />
    </div>
  );
}

function UpdateCard() {
  const { update, refreshUpdate, version, toast } = useApp();
  const { t } = useI18n();
  const [checking, setChecking] = useState(false);
  const [applying, setApplying] = useState(false);

  const check = async () => {
    setChecking(true);
    await refreshUpdate(true);
    setChecking(false);
  };

  const apply = async () => {
    setApplying(true);
    try {
      const r = await api.applyUpdate();
      toast(
        t("Updating to {v} — the app will restart and this tab will reconnect.", { v: r.version }),
        "success",
      );
      // The server re-execs on the same port; the SSE connection reconnects
      // automatically, so we keep the spinner until that happens.
    } catch (e: any) {
      toast(e.message || t("Update failed"), "error");
      setApplying(false);
    }
  };

  return (
    <div className="card p-5">
      <h2 className="mb-3 flex items-center gap-2 text-sm font-semibold text-slate-200">
        <ArrowUpCircle className="h-4 w-4 text-gold-400" /> {t("Software update")}
      </h2>
      <div className="space-y-3 text-sm">
        <div className="flex items-center justify-between gap-3">
          <span className="text-slate-400">{t("Current version")}</span>
          <span className="font-mono text-xs text-slate-300">{version || "—"}</span>
        </div>

        {update?.updateAvailable && (
          <div className="space-y-3 rounded-lg border border-gold-500/25 bg-gold-500/[0.06] p-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <span className="font-medium text-gold-200">
                {t("New version {v} available", { v: update.latest || "" })}
              </span>
              {update.releaseUrl && (
                <a
                  href={update.releaseUrl}
                  target="_blank"
                  rel="noreferrer"
                  className="text-xs text-slate-400 underline hover:text-slate-200"
                >
                  {t("Release notes")}
                </a>
              )}
            </div>
            <button className="btn-primary" onClick={apply} disabled={applying}>
              {applying ? <Spinner className="h-4 w-4" /> : <ArrowUpCircle className="h-4 w-4" />}
              {applying ? t("Updating…") : t("Update & restart")}
            </button>
          </div>
        )}

        <div className="flex items-center justify-between gap-3">
          <span className="min-w-0 flex-1 truncate text-xs text-slate-500">
            {update?.updateAvailable
              ? ""
              : update?.note
                ? update.note
                : t("You're on the latest version.")}
          </span>
          <button className="btn-ghost shrink-0 px-3 py-1.5 text-xs" onClick={check} disabled={checking}>
            {checking ? <Spinner className="h-3.5 w-3.5" /> : <RefreshCw className="h-3.5 w-3.5" />}{" "}
            {t("Check for updates")}
          </button>
        </div>
      </div>
    </div>
  );
}

function FFmpegInfo({ ffmpeg }: { ffmpeg: FFmpegStatus }) {
  const { t } = useI18n();
  return (
    <div className="card p-5">
      <h2 className="mb-3 flex items-center gap-2 text-sm font-semibold text-slate-200">
        <Server className="h-4 w-4 text-gold-400" /> {t("System")}
      </h2>
      <div className="space-y-2 text-sm">
        <Row label="ffmpeg" ok={ffmpeg.ffmpegFound} detail={ffmpeg.ffmpegFound ? ffmpeg.ffmpegVersion || ffmpeg.ffmpegPath : t("not found on PATH")} />
        <Row label="ffprobe" ok={ffmpeg.ffprobeFound} detail={ffmpeg.ffprobeFound ? ffmpeg.ffprobePath || "" : t("not found on PATH")} />
      </div>
      <InstallFFmpeg className="mt-3" />
    </div>
  );
}

function Row({ label, ok, detail }: { label: string; ok: boolean; detail?: string }) {
  return (
    <div className="flex items-center gap-3">
      <span className={`h-2 w-2 rounded-full ${ok ? "bg-emerald-400" : "bg-ember-500"}`} />
      <span className="w-16 font-medium text-slate-300">{label}</span>
      <span className="min-w-0 flex-1 truncate font-mono text-xs text-slate-500">{detail}</span>
    </div>
  );
}
