import { useState } from "react";
import { Check, ChevronDown, ChevronRight, Clock3, Download } from "lucide-react";
import type { PreviewResponse } from "../api";
import { duration } from "../lib/format";
import { useI18n } from "../i18n";
import { PosterImage } from "./ui";

export function SeriesBrowser({
  preview,
  selectedSeasons,
  onToggleSeason,
}: {
  preview: PreviewResponse;
  selectedSeasons: Set<number> | null; // null => all seasons
  onToggleSeason: (n: number) => void;
}) {
  const { t } = useI18n();
  const [openSeason, setOpenSeason] = useState<number | null>(
    preview.seasons.length === 1 ? preview.seasons[0].number : null,
  );

  const isSeasonSelected = (n: number) => selectedSeasons === null || selectedSeasons.has(n);

  return (
    <div className="animate-fade-in space-y-4">
      <div className="flex gap-4">
        <PosterImage
          url={preview.posterUrl}
          alt={preview.title}
          className="aspect-[2/3] w-24 shrink-0 rounded-xl border border-white/[0.08]"
        />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-lg font-semibold text-slate-100">{preview.title}</h3>
            <span className="chip border-white/10 bg-white/[0.04] text-slate-400 uppercase">
              {preview.source}
            </span>
          </div>
          {preview.originalTitle && (
            <p className="text-sm text-slate-500">{preview.originalTitle}</p>
          )}
          <div className="mt-2 flex flex-wrap gap-2 text-xs">
            <span className="chip border-white/10 bg-white/[0.04] text-slate-300">
              {t("{n} episodes", { n: preview.total })}
            </span>
            <span className="chip border-gold-500/25 bg-gold-500/10 text-gold-300">
              <Download className="h-3 w-3" /> {t("{n} to download", { n: preview.selected })}
            </span>
            {preview.alreadyCompleted > 0 && (
              <span className="chip border-emerald-500/25 bg-emerald-500/10 text-emerald-300">
                <Check className="h-3 w-3" /> {t("{n} done", { n: preview.alreadyCompleted })}
              </span>
            )}
          </div>
          {preview.description && (
            <p className="mt-2 line-clamp-3 text-sm text-slate-400">{preview.description}</p>
          )}
        </div>
      </div>

      <div className="space-y-2">
        {preview.seasons.map((s) => {
          const open = openSeason === s.number;
          const done = s.episodes.filter((e) => e.completed).length;
          const sel = isSeasonSelected(s.number);
          return (
            <div key={s.number} className="overflow-hidden rounded-xl border border-white/[0.06] bg-ink-900/40">
              <div className="flex items-center gap-3 px-3 py-2.5">
                <button
                  type="button"
                  onClick={() => onToggleSeason(s.number)}
                  className={`grid h-5 w-5 shrink-0 place-items-center rounded-md border transition ${
                    sel ? "border-gold-500 bg-gold-500 text-ink-950" : "border-white/20"
                  }`}
                >
                  {sel && <Check className="h-3.5 w-3.5" />}
                </button>
                <button
                  type="button"
                  onClick={() => setOpenSeason(open ? null : s.number)}
                  className="flex flex-1 items-center justify-between gap-2"
                >
                  <span className="flex items-center gap-2 text-sm font-medium text-slate-200">
                    {open ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
                    {t("Season {n}", { n: s.number })}
                  </span>
                  <span className="flex items-center gap-2 text-xs text-slate-500">
                    <span>{t("{n} ep", { n: s.episodes.length })}</span>
                    {done > 0 && <span className="text-emerald-400">{t("{n} done", { n: done })}</span>}
                  </span>
                </button>
              </div>
              {open && (
                <div className="border-t border-white/[0.05] px-2 py-1.5">
                  {s.episodes.map((e) => (
                    <div
                      key={e.key}
                      className="flex items-center gap-3 rounded-lg px-2 py-1.5 text-sm hover:bg-white/[0.03]"
                    >
                      <span className="w-12 shrink-0 font-mono text-xs text-slate-500">{e.key}</span>
                      <span className="min-w-0 flex-1 truncate text-slate-300">
                        {e.title || t("Episode {n}", { n: e.episode })}
                      </span>
                      {e.durationSeconds > 0 && (
                        <span className="flex items-center gap-1 text-xs text-slate-500">
                          <Clock3 className="h-3 w-3" />
                          {duration(e.durationSeconds, t)}
                        </span>
                      )}
                      {e.completed ? (
                        <span className="chip border-emerald-500/25 bg-emerald-500/10 text-emerald-300">
                          <Check className="h-3 w-3" /> {t("done")}
                        </span>
                      ) : sel ? (
                        <span className="chip border-gold-500/25 bg-gold-500/10 text-gold-300">{t("queued")}</span>
                      ) : (
                        <span className="chip border-white/10 bg-white/[0.03] text-slate-500">{t("skip")}</span>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
