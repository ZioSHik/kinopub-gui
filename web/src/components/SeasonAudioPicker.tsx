import { useI18n } from "../i18n";
import { chosenSeasonAudios, seasonAudioIssue, type SeasonAudioGroup, type SeasonAudioSelection } from "../lib/seasonAudio";
import { AudioPicker } from "./AudioPicker";

export function SeasonAudioPicker({ groups, selected, episodes, onToggle, isSerial = true }: {
  groups: SeasonAudioGroup[];
  selected: SeasonAudioSelection;
  episodes: Set<string>;
  onToggle: (season: number, label: string) => void;
  isSerial?: boolean;
}) {
  const { t } = useI18n();
  const contents = (group: SeasonAudioGroup) => {
    const issue = seasonAudioIssue(group, selected, episodes);
    return <div className="space-y-3 p-3">
      {group.audios.length ? <AudioPicker audios={group.audios} selected={selected[group.number] || new Set()} onToggle={(label) => onToggle(group.number, label)} />
        : <p className="text-xs text-slate-500">{t("Voiceover list appears after sign-in / for available titles.")}</p>}
      {issue && <p className="text-xs text-gold-300" role="status">{t(issue === "missing" ? "Choose a voiceover for this season." : "This voiceover is missing from some selected episodes. Choose another or deselect those episodes.")}</p>}
    </div>;
  };
  if (!isSerial && groups.length === 1) return contents(groups[0]);
  return <div className="space-y-2">
    <p className="text-xs text-slate-400">{t("Voiceover choices apply only to their season.")}</p>
    {groups.map((group, index) => {
      const chosen = chosenSeasonAudios(group, selected);
      const count = group.episodeKeys.filter((key) => episodes.has(key)).length;
      return <details key={group.number} open={index === 0} className="rounded-xl border border-white/[0.08] bg-ink-900/30" aria-label={t("Season {n} voiceovers", { n: group.number })}>
        <summary className="cursor-pointer px-3 py-2.5 text-sm text-slate-200">
          <span className="font-medium">{t("Season {n}", { n: group.number })}</span>
          <span className="ml-2 text-xs text-slate-400">{t("{n} episodes selected", { n: count })}</span>
          <span className="mt-1 block pl-4 text-xs text-gold-200/90">{chosen.length ? chosen.map((audio) => audio.label).join(", ") : t("Choose a voiceover")}</span>
        </summary>
        {contents(group)}
      </details>;
    })}
  </div>;
}
