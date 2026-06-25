import { useCallback, useEffect, useRef, useState } from "react";
import Hls from "hls.js";
import {
  Gauge,
  Loader2,
  Maximize,
  Minimize,
  Pause,
  Play,
  RotateCcw,
  RotateCw,
  SkipBack,
  SkipForward,
  Volume2,
  VolumeX,
  X,
} from "lucide-react";
import { api } from "../api";
import { useI18n } from "../i18n";

interface AudioOpt {
  id: number;
  name: string;
  lang: string;
}

export interface PlayerEpisode {
  season: number;
  episode: number;
  title: string;
}

// Remembered dub across episodes/titles: stored by normalized track name so the
// same voiceover is re-selected on the next episode; titles lacking it fall back
// to the stream default.
const AUDIO_PREF_KEY = "kp.player.audioPref";
const SKIP_SECONDS = 15;
const HIDE_DELAY = 2600;
// How often (in playback seconds) to report progress to kino.pub while playing.
const MARK_INTERVAL = 20;

function audioKey(name: string): string {
  return name.replace(/^\s*\d+\.\s*/, "").trim().toLowerCase();
}

// codecSupported reports whether the browser can decode a level's video codec.
// kino.pub's mixed-playlist 4K master lists each rung twice (HEVC + H.264);
// Chromium without hardware HEVC returns false for hvc1, so we keep the
// universally-playable H.264 variant.
function codecSupported(videoCodec?: string): boolean {
  if (!videoCodec) return true;
  try {
    const MS = (window as any).MediaSource;
    if (MS && typeof MS.isTypeSupported === "function") {
      return MS.isTypeSupported(`video/mp4; codecs="${videoCodec}"`);
    }
  } catch {
    /* fall through */
  }
  return true;
}

// levelLabel names a quality tier from dimensions, keyed off width because
// widescreen content reduces height (1080p film is 1920×800, not ×1080).
function levelLabel(width: number, height: number): string {
  const w = width || 0;
  const h = height || 0;
  if (w >= 3800 || h >= 2000) return "2160p";
  if (w >= 2300 || h >= 1300) return "1440p";
  if (w >= 1800 || h >= 1000) return "1080p";
  if (w >= 1200 || h >= 700) return "720p";
  if (w >= 700 || h >= 460) return "480p";
  if (w >= 480 || h >= 340) return "360p";
  return h ? `${h}p` : `${Math.round(w)}w`;
}

function fmtTime(s: number): string {
  if (!isFinite(s) || s < 0) s = 0;
  const t = Math.floor(s);
  const h = Math.floor(t / 3600);
  const m = Math.floor((t % 3600) / 60);
  const sec = t % 60;
  const mm = h > 0 ? String(m).padStart(2, "0") : String(m);
  return (h > 0 ? `${h}:` : "") + `${mm}:${String(sec).padStart(2, "0")}`;
}

// Player streams a catalog title in-app via hls.js (fed by the backend HLS
// proxy). It uses a fully custom control overlay inside one fullscreen-able
// wrapper, so every control — seek, skip, episode nav, quality, audio,
// fullscreen — stays available in fullscreen (native <video> fullscreen would
// hide the surrounding modal chrome).
export function Player({
  id,
  season,
  episode,
  title,
  episodes,
  onChangeEpisode,
  onClose,
}: {
  id: string;
  season?: number;
  episode?: number;
  title?: string;
  episodes?: PlayerEpisode[];
  onChangeEpisode?: (season: number, episode: number) => void;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const tRef = useRef(t);
  tRef.current = t;

  const wrapRef = useRef<HTMLDivElement>(null);
  const videoRef = useRef<HTMLVideoElement>(null);
  const hlsRef = useRef<Hls | null>(null);
  const hideTimer = useRef<number | undefined>(undefined);

  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [audioTracks, setAudioTracks] = useState<AudioOpt[]>([]);
  const [activeAudio, setActiveAudio] = useState(-1);
  const [levels, setLevels] = useState<{ id: number; label: string }[]>([]);
  const [activeLevel, setActiveLevel] = useState(-1);
  const [heading, setHeading] = useState(title || "");

  const [paused, setPaused] = useState(false);
  const [current, setCurrent] = useState(0);
  const [duration, setDuration] = useState(0);
  const [muted, setMuted] = useState(false);
  const [volume, setVolume] = useState(1);
  const [isFs, setIsFs] = useState(false);
  const [chrome, setChrome] = useState(true); // controls visible

  // Resume: saved position (seconds) for this episode and whether to ask before
  // playing. resumeRef mirrors resumeAt for use inside the load effect's hls
  // callbacks (which capture a stale render).
  const [resumeAt, setResumeAt] = useState(0);
  const [askResume, setAskResume] = useState(false);
  const resumeRef = useRef(0);

  // Progress reporting. markRef is reassigned every render so it always sees the
  // current id/season/episode; lastMark throttles to MARK_INTERVAL playback secs.
  const markRef = useRef<(force: boolean) => void>(() => {});
  const lastMark = useRef(-1);
  markRef.current = (force: boolean) => {
    const v = videoRef.current;
    if (!v) return;
    const time = Math.floor(v.currentTime);
    if (time <= 0) return;
    if (!force && lastMark.current >= 0 && Math.abs(time - lastMark.current) < MARK_INTERVAL) return;
    lastMark.current = time;
    void api.markTime(id, time, season, episode).catch(() => {});
  };

  // Episode navigation (serials).
  const hasList = !!episodes && episodes.length > 1;
  const idx =
    episodes && season != null && episode != null
      ? episodes.findIndex((e) => e.season === season && e.episode === episode)
      : -1;
  const prevEp = idx > 0 ? episodes![idx - 1] : null;
  const nextEp = idx >= 0 && episodes && idx < episodes.length - 1 ? episodes[idx + 1] : null;
  const nextRef = useRef<PlayerEpisode | null>(null);
  nextRef.current = nextEp;
  const onChangeRef = useRef(onChangeEpisode);
  onChangeRef.current = onChangeEpisode;

  // ---- chrome (controls) auto-hide ------------------------------------------
  const showChrome = useCallback(() => {
    setChrome(true);
    window.clearTimeout(hideTimer.current);
    hideTimer.current = window.setTimeout(() => {
      const v = videoRef.current;
      if (v && !v.paused) setChrome(false);
    }, HIDE_DELAY);
  }, []);

  // ---- keyboard shortcuts ---------------------------------------------------
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const v = videoRef.current;
      switch (e.key) {
        case "Escape":
          if (!document.fullscreenElement) onClose();
          break;
        case " ":
        case "k":
          e.preventDefault();
          if (v) (v.paused ? v.play() : v.pause());
          showChrome();
          break;
        case "ArrowRight":
          if (v) v.currentTime = Math.min(v.duration || 1e9, v.currentTime + SKIP_SECONDS);
          showChrome();
          break;
        case "ArrowLeft":
          if (v) v.currentTime = Math.max(0, v.currentTime - SKIP_SECONDS);
          showChrome();
          break;
        case "f":
          toggleFs();
          break;
        case "m":
          if (v) v.muted = !v.muted;
          break;
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [onClose, showChrome]);

  // ---- fullscreen state -----------------------------------------------------
  useEffect(() => {
    const onFs = () => setIsFs(document.fullscreenElement === wrapRef.current);
    document.addEventListener("fullscreenchange", onFs);
    return () => document.removeEventListener("fullscreenchange", onFs);
  }, []);

  const toggleFs = useCallback(() => {
    if (document.fullscreenElement) {
      void document.exitFullscreen().catch(() => {});
    } else {
      void wrapRef.current?.requestFullscreen().catch(() => {});
    }
  }, []);

  // ---- <video> element events ----------------------------------------------
  useEffect(() => {
    const v = videoRef.current;
    if (!v) return;
    const onPlay = () => setPaused(false);
    const onPause = () => setPaused(true);
    const onTime = () => {
      setCurrent(v.currentTime);
      markRef.current(false);
    };
    const onDur = () => setDuration(isFinite(v.duration) ? v.duration : 0);
    const onVol = () => {
      setMuted(v.muted);
      setVolume(v.volume);
    };
    const onEnded = () => {
      markRef.current(true);
      const n = nextRef.current;
      if (n) onChangeRef.current?.(n.season, n.episode);
    };
    v.addEventListener("play", onPlay);
    v.addEventListener("pause", onPause);
    v.addEventListener("timeupdate", onTime);
    v.addEventListener("durationchange", onDur);
    v.addEventListener("loadedmetadata", onDur);
    v.addEventListener("volumechange", onVol);
    v.addEventListener("ended", onEnded);
    return () => {
      v.removeEventListener("play", onPlay);
      v.removeEventListener("pause", onPause);
      v.removeEventListener("timeupdate", onTime);
      v.removeEventListener("durationchange", onDur);
      v.removeEventListener("loadedmetadata", onDur);
      v.removeEventListener("volumechange", onVol);
      v.removeEventListener("ended", onEnded);
    };
  }, []);

  // Final progress report when the player closes, so the last position sticks.
  useEffect(() => () => markRef.current(true), []);

  // ---- load the episode (fresh hls per episode) -----------------------------
  useEffect(() => {
    let alive = true;
    let retries = 0;
    let hls: Hls | null = null;
    const video = videoRef.current;
    if (!video) return;

    setLoading(true);
    setError(null);
    setAudioTracks([]);
    setLevels([]);
    setAskResume(false);
    lastMark.current = -1;
    resumeRef.current = 0;

    // Start playback, unless there's a saved position worth resuming — then ask
    // first (the <video> has no autoPlay, so it stays paused until the choice).
    const maybeStart = () => {
      if (resumeRef.current > 10) setAskResume(true);
      else void video.play().catch(() => {});
    };

    const applyRememberedAudio = (tracks: AudioOpt[], cur: number, select: (i: number) => void) => {
      const pref = localStorage.getItem(AUDIO_PREF_KEY);
      const match = pref ? tracks.find((tk) => audioKey(tk.name) === pref) : undefined;
      if (match && match.id !== cur) select(match.id);
      else setActiveAudio(cur);
    };
    const readAudio = (h: Hls) => {
      const tracks = h.audioTracks.map((a, i) => ({ id: i, name: a.name, lang: (a as any).lang || "" }));
      setAudioTracks(tracks);
      applyRememberedAudio(tracks, h.audioTrack, (i) => {
        h.audioTrack = i;
        setActiveAudio(i);
      });
    };

    const start = (src: string) => {
      if (!alive) return;
      if (Hls.isSupported()) {
        // A high default bandwidth estimate so adaptive mode starts at a high
        // rung (reaching 4K quickly on a fast line) instead of ramping up from
        // the bottom; real throughput measurements take over within seconds.
        hls = new Hls({ maxBufferLength: 30, abrEwmaDefaultEstimate: 25_000_000 });
        hlsRef.current = hls;
        hls.attachMedia(video);
        hls.on(Hls.Events.MANIFEST_PARSED, () => {
          if (!alive || !hls) return;
          retries = 0;
          setLoading(false);
          readAudio(hls);
          // kino.pub's mixed 4K master lists every rung twice — HEVC + H.264.
          // Drop the HEVC twins so BOTH adaptive (Auto) and manual selection only
          // ever pick decodable H.264 levels: isTypeSupported lies about HEVC on
          // machines that advertise but can't actually decode it (a hard stall).
          const isAVC = (c?: string) => /avc1|h264/i.test(c || "");
          if (hls.levels.some((l) => isAVC((l as any).videoCodec))) {
            for (let i = hls.levels.length - 1; i >= 0; i--) {
              if (!isAVC((hls.levels[i] as any).videoCodec)) {
                try {
                  hls.removeLevel(i);
                } catch {
                  /* older hls.js — codec filter below still de-dups the menu */
                }
              }
            }
          }
          // Build the quality menu (one entry per resolution) from what remains.
          const seen = new Map<string, { id: number; label: string; px: number }>();
          hls.levels.forEach((l, i) => {
            if (!codecSupported((l as any).videoCodec)) return;
            const label = levelLabel(l.width, l.height);
            if (!seen.has(label)) seen.set(label, { id: i, label, px: (l.width || 0) * (l.height || 0) || l.bitrate || 0 });
          });
          setLevels([...seen.values()].sort((a, b) => b.px - a.px).map(({ id, label }) => ({ id, label })));
          // Default to adaptive ("Auto"): hls.js picks by measured bandwidth,
          // downgrades when the connection drops and climbs back when it recovers.
          hls.currentLevel = -1;
          setActiveLevel(-1);
          maybeStart();
        });
        hls.on(Hls.Events.AUDIO_TRACKS_UPDATED, () => alive && hls && readAudio(hls));
        hls.on(Hls.Events.AUDIO_TRACK_SWITCHED, () => alive && hls && setActiveAudio(hls.audioTrack));
        hls.on(Hls.Events.LEVEL_SWITCHED, () => alive && hls && setActiveLevel(hls.autoLevelEnabled ? -1 : hls.currentLevel));
        hls.on(Hls.Events.ERROR, (_evt, data) => {
          if (!data.fatal || !hls) return;
          if (data.type === Hls.ErrorTypes.NETWORK_ERROR && retries < 3) {
            retries++;
            hls.startLoad();
          } else if (data.type === Hls.ErrorTypes.MEDIA_ERROR && retries < 3) {
            retries++;
            hls.recoverMediaError();
          } else if (alive) {
            setError(tRef.current("Playback error — try reopening."));
          }
        });
        hls.loadSource(src);
      } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
        const onMeta = () => {
          if (!alive) return;
          setLoading(false);
          maybeStart();
        };
        video.addEventListener("loadedmetadata", onMeta, { once: true });
        video.src = src;
        video.load();
      } else {
        setError(tRef.current("Your browser can’t play HLS video."));
      }
    };

    api
      .stream(id, season, episode)
      .then((s) => {
        if (!alive) return;
        setHeading(s.title || title || "");
        const rt = Math.max(0, Math.floor(s.resumeTime || 0));
        resumeRef.current = rt;
        setResumeAt(rt);
        start(s.playUrl);
      })
      .catch((e) => alive && setError(e.message || tRef.current("Failed to load stream")));

    return () => {
      alive = false;
      if (hls) hls.destroy();
      hlsRef.current = null;
    };
  }, [id, season, episode, title]);

  useEffect(() => {
    showChrome();
    return () => window.clearTimeout(hideTimer.current);
  }, [showChrome]);

  // ---- control actions ------------------------------------------------------
  const togglePlay = () => {
    const v = videoRef.current;
    if (!v) return;
    if (v.paused) void v.play().catch(() => {});
    else v.pause();
    showChrome();
  };
  const skip = (delta: number) => {
    const v = videoRef.current;
    if (!v) return;
    const dur = isFinite(v.duration) ? v.duration : Number.MAX_SAFE_INTEGER;
    v.currentTime = Math.max(0, Math.min(dur, v.currentTime + delta));
    showChrome();
  };
  const seek = (val: number) => {
    const v = videoRef.current;
    if (v) v.currentTime = val;
  };
  const toggleMute = () => {
    const v = videoRef.current;
    if (v) v.muted = !v.muted;
  };
  const setVol = (val: number) => {
    const v = videoRef.current;
    if (v) {
      v.volume = val;
      v.muted = val === 0;
    }
  };
  const goTo = (ep: PlayerEpisode | null) => ep && onChangeEpisode?.(ep.season, ep.episode);

  const startPlaybackAt = (seconds: number) => {
    setAskResume(false);
    const v = videoRef.current;
    if (!v) return;
    try {
      v.currentTime = seconds;
    } catch {
      /* metadata not ready yet — play from wherever we are */
    }
    void v.play().catch(() => {});
  };

  const pickAudio = (i: number) => {
    const name = audioTracks.find((a) => a.id === i)?.name;
    if (name) {
      try {
        localStorage.setItem(AUDIO_PREF_KEY, audioKey(name));
      } catch {
        /* ignore */
      }
    }
    const hls = hlsRef.current;
    if (hls) {
      hls.audioTrack = i;
      setActiveAudio(i);
    }
  };
  const pickLevel = (i: number) => {
    const hls = hlsRef.current;
    if (hls) {
      hls.currentLevel = i;
      setActiveLevel(i);
    }
  };

  const btn = "rounded-lg p-1.5 text-white/90 transition hover:bg-white/15 hover:text-white disabled:opacity-30 disabled:hover:bg-transparent";

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/85 p-0 sm:p-4"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        ref={wrapRef}
        className="group relative aspect-video w-full max-w-6xl overflow-hidden bg-black sm:rounded-2xl"
        onMouseMove={showChrome}
        onMouseLeave={() => {
          const v = videoRef.current;
          if (v && !v.paused) setChrome(false);
        }}
        style={{ cursor: chrome ? "default" : "none" }}
      >
        {/* No autoPlay: playback is started explicitly so a saved position can be
            offered first. eslint-disable-next-line jsx-a11y/media-has-caption */}
        <video ref={videoRef} className="h-full w-full bg-black" onClick={togglePlay} />

        {loading && !error && (
          <div className="pointer-events-none absolute inset-0 grid place-items-center bg-black/30 text-white">
            <Loader2 className="h-10 w-10 animate-spin" />
          </div>
        )}
        {error && (
          <div className="absolute inset-0 grid place-items-center bg-black/70 p-6 text-center text-sm text-ember-300">{error}</div>
        )}

        {/* Resume prompt: shown when the title has a saved position. */}
        {askResume && !error && (
          <div className="absolute inset-0 z-20 grid place-items-center bg-black/75 p-6 backdrop-blur-sm">
            <div className="w-full max-w-sm rounded-2xl border border-white/10 bg-ink-900/95 p-6 text-center shadow-2xl">
              <p className="text-base font-semibold text-slate-100">{t("Continue watching?")}</p>
              <p className="mt-1 text-sm text-slate-400">{t("You stopped at {time}", { time: fmtTime(resumeAt) })}</p>
              <div className="mt-5 flex flex-col gap-2 sm:flex-row sm:justify-center">
                <button className="btn-primary justify-center" onClick={() => startPlaybackAt(resumeRef.current)}>
                  <Play className="h-4 w-4" /> {t("Continue from {time}", { time: fmtTime(resumeAt) })}
                </button>
                <button className="btn-ghost justify-center" onClick={() => startPlaybackAt(0)}>
                  <RotateCcw className="h-4 w-4" /> {t("Start over")}
                </button>
              </div>
            </div>
          </div>
        )}

        {/* Top gradient: title + close */}
        <div
          className={`pointer-events-none absolute inset-x-0 top-0 flex items-start justify-between gap-3 bg-gradient-to-b from-black/70 to-transparent p-3 transition-opacity duration-200 ${
            chrome ? "opacity-100" : "opacity-0"
          }`}
        >
          <h3 className="pointer-events-auto min-w-0 truncate pt-1 text-sm font-semibold text-white drop-shadow">{heading || t("Player")}</h3>
          <button className={`pointer-events-auto ${btn}`} onClick={onClose} title={t("Close")}>
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Bottom gradient: full control bar */}
        <div
          className={`absolute inset-x-0 bottom-0 flex flex-col gap-1 bg-gradient-to-t from-black/80 to-transparent px-3 pb-2 pt-6 transition-opacity duration-200 ${
            chrome ? "opacity-100" : "pointer-events-none opacity-0"
          }`}
        >
          {/* Seek bar */}
          <input
            type="range"
            className="player-range w-full"
            min={0}
            max={duration || 0}
            step="any"
            value={Math.min(current, duration || 0)}
            onChange={(e) => seek(Number(e.target.value))}
            style={{ ["--val" as any]: `${duration ? (Math.min(current, duration) / duration) * 100 : 0}%` }}
            aria-label={t("Seek")}
          />

          <div className="flex flex-wrap items-center gap-x-1 gap-y-1 text-white">
            <button className={btn} onClick={togglePlay} title={paused ? t("Play") : t("Pause")}>
              {paused ? <Play className="h-5 w-5" /> : <Pause className="h-5 w-5" />}
            </button>
            {hasList && (
              <button className={btn} onClick={() => goTo(prevEp)} disabled={!prevEp} title={t("Previous episode")}>
                <SkipBack className="h-4 w-4" />
              </button>
            )}
            <button className={`${btn} flex items-center gap-0.5`} onClick={() => skip(-SKIP_SECONDS)} title={t("Back {n}s", { n: SKIP_SECONDS })}>
              <RotateCcw className="h-4 w-4" />
              <span className="text-[11px] font-semibold tabular-nums">{SKIP_SECONDS}</span>
            </button>
            <button className={`${btn} flex items-center gap-0.5`} onClick={() => skip(SKIP_SECONDS)} title={t("Forward {n}s", { n: SKIP_SECONDS })}>
              <span className="text-[11px] font-semibold tabular-nums">{SKIP_SECONDS}</span>
              <RotateCw className="h-4 w-4" />
            </button>
            {hasList && (
              <button className={btn} onClick={() => goTo(nextEp)} disabled={!nextEp} title={t("Next episode")}>
                <SkipForward className="h-4 w-4" />
              </button>
            )}

            <div className="flex items-center gap-1 pl-1">
              <button className={btn} onClick={toggleMute} title={muted ? t("Unmute") : t("Mute")}>
                {muted || volume === 0 ? <VolumeX className="h-4 w-4" /> : <Volume2 className="h-4 w-4" />}
              </button>
              <input
                type="range"
                className="player-range hidden w-16 sm:block"
                min={0}
                max={1}
                step={0.05}
                value={muted ? 0 : volume}
                onChange={(e) => setVol(Number(e.target.value))}
                style={{ ["--val" as any]: `${(muted ? 0 : volume) * 100}%` }}
                aria-label={t("Volume")}
              />
            </div>

            <span className="px-1 text-xs tabular-nums text-white/90">
              {fmtTime(current)} / {fmtTime(duration)}
            </span>

            <div className="ml-auto flex items-center gap-1.5">
              {levels.length > 1 && (
                <span className="flex items-center gap-1 text-white/90" title={t("Quality")}>
                  <Gauge className="h-4 w-4" />
                  <select className="player-select" value={activeLevel} onChange={(e) => pickLevel(Number(e.target.value))}>
                    <option value={-1}>{t("Auto")}</option>
                    {levels.map((l) => (
                      <option key={l.id} value={l.id}>
                        {l.label}
                      </option>
                    ))}
                  </select>
                </span>
              )}
              {audioTracks.length > 1 && (
                <span className="flex items-center gap-1 text-white/90" title={t("Audio track")}>
                  <Volume2 className="h-4 w-4" />
                  <select className="player-select max-w-[200px]" value={activeAudio} onChange={(e) => pickAudio(Number(e.target.value))}>
                    {audioTracks.map((a) => (
                      <option key={a.id} value={a.id}>
                        {a.name}
                        {a.lang ? ` · ${a.lang}` : ""}
                      </option>
                    ))}
                  </select>
                </span>
              )}
              <button className={btn} onClick={toggleFs} title={isFs ? t("Exit fullscreen") : t("Fullscreen")}>
                {isFs ? <Minimize className="h-4 w-4" /> : <Maximize className="h-4 w-4" />}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
