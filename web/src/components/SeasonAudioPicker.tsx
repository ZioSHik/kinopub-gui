import { useI18n } from "../i18n";
import { AlertCircle, ChevronDown } from "lucide-react";
import { chosenSeasonAudios, seasonAudioIssue, sharedSeasonAudioSuggestions, type SeasonAudioGroup, type SeasonAudioSelection } from "../lib/seasonAudio";
import { AudioPicker } from "./AudioPicker";

export function SeasonAudioPicker({ groups, selected, episodes, onToggle, isSerial = true }: {
  groups: SeasonAudioGroup[];
  selected: SeasonAudioSelection;
  episodes: Set<string>;
  onToggle: (season: number, label: string) => void;
  isSerial?: boolean;
}) {
  const { t } = useI18n();
  const activeGroups = groups.filter((group) => group.episodeKeys.some((key) => episodes.has(key)));
  if (!activeGroups.length) return null;
  const contents = (group: SeasonAudioGroup) => {
    const issue = seasonAudioIssue(group, selected, episodes);
    return <div className={isSerial ? "space-y-2 border-t border-white/[0.05] p-2" : "space-y-2"}>
      {group.audios.length ? <AudioPicker compact audios={group.audios} selected={selected[group.number] || new Set()} onToggle={(label) => onToggle(group.number, label)} />
        : <p className="text-xs text-slate-500">{t("Voiceover list appears after sign-in / for available titles.")}</p>}
      {issue === "unavailable" && <p className="flex items-start gap-1.5 px-1 text-xs leading-relaxed text-gold-300" role="status"><AlertCircle aria-hidden="true" className="mt-0.5 h-3.5 w-3.5 shrink-0" />{t("This voiceover is missing from some selected episodes. Choose another or deselect those episodes.")}</p>}
    </div>;
  };
  if (!isSerial && activeGroups.length === 1) return contents(activeGroups[0]);
  const suggestions = sharedSeasonAudioSuggestions(activeGroups, selected, episodes);
  return <div className="space-y-1.5">
    {suggestions.map(({ id, audio, count, targets }) => <div key={id} className="flex flex-wrap items-center justify-between gap-2 rounded-xl border border-gold-500/20 bg-gold-500/[0.05] px-3 py-2">
      <div className="min-w-0">
        <p className="text-xs font-medium text-gold-200">{audio.label}</p>
        <p className="mt-0.5 text-[10px] text-slate-400">{t("Seasons with this voiceover: {n}", { n: count })}</p>
      </div>
      <button type="button" className="shrink-0 rounded-lg border border-gold-500/25 px-2.5 py-1.5 text-xs font-medium text-gold-200 transition hover:bg-gold-500/10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-gold-400/60"
        aria-label={t("Use “{voiceover}” for {n} more seasons", { voiceover: audio.label, n: targets.length })}
        onClick={() => targets.forEach(({ season, label }) => onToggle(season, label))}>
        {t("Select for other seasons ({n})", { n: targets.length })}
      </button>
    </div>)}
    {activeGroups.map((group, index) => {
      const chosen = chosenSeasonAudios(group, selected);
      const count = group.episodeKeys.filter((key) => episodes.has(key)).length;
      const issue = seasonAudioIssue(group, selected, episodes);
      const choice = chosen.map((audio) => audio.author || audio.label).join(", ");
      return <details key={group.number} open={index === 0} className="group rounded-xl border border-white/[0.08] bg-ink-900/30" aria-label={t("Season {n} voiceovers", { n: group.number })}>
        <summary className="flex cursor-pointer list-none items-center gap-2 rounded-xl px-3 py-2.5 text-xs text-slate-200 transition hover:bg-white/[0.025] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-gold-400/60 [&::-webkit-details-marker]:hidden">
          <ChevronDown aria-hidden="true" className="h-3.5 w-3.5 shrink-0 -rotate-90 text-slate-500 transition-transform group-open:rotate-0" />
          <span className="shrink-0 font-medium">{t("Season {n}", { n: group.number })}</span>
          <span className="shrink-0 rounded bg-white/[0.05] px-1.5 py-0.5 text-[10px] tabular-nums text-slate-400" title={t("{n} episodes selected", { n: count })}>{t("{n} ep", { n: count })}</span>
          {issue === "unavailable" && <AlertCircle aria-hidden="true" className="ml-auto h-3.5 w-3.5 shrink-0 text-gold-300" />}
          <span className="ml-auto min-w-0 truncate text-right text-[11px] text-gold-200/90" title={choice || undefined}>{choice || t("Choose a voiceover")}</span>
        </summary>
        {contents(group)}
      </details>;
    })}
  </div>;
}
