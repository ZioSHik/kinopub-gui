import { useId } from "react";
import clsx from "clsx";
import { Check } from "lucide-react";
import type { DiscoverAudio } from "../api";
import { useI18n } from "../i18n";

type AudioGroup = { type: string; title: string; tracks: DiscoverAudio[] };
const CATEGORIES = [
  ["дубляж", "Dubbing"],
  ["многоголосый", "Multi-voice translation"],
  ["двухголосый", "Two-voice translation"],
  ["авторский", "Author's translation"],
  ["одноголосый", "Single-voice translation"],
  ["оригинал", "Original soundtrack"],
] as const;

function groupAudios(audios: DiscoverAudio[]): AudioGroup[] {
  const groups = new Map<string, AudioGroup>();
  for (const audio of audios) {
    // Old responses may only carry the combined label. Recognize its type
    // prefix without interpreting an unknown studio name as a category.
    const prefix = audio.label.split(" · ")[0].trim();
    const type = audio.type?.trim() || (CATEGORIES.some(([key]) => key === prefix.toLowerCase()) ? prefix : "");
    const key = type.toLowerCase();
    const group = groups.get(key) || { type, title: CATEGORIES.find(([name]) => name === key)?.[1] || type || "Other tracks", tracks: [] };
    group.tracks.push(audio);
    groups.set(key, group);
  }
  const rank = (type: string) => {
    const index = CATEGORIES.findIndex(([key]) => key === type.toLowerCase());
    return index === -1 ? CATEGORIES.length : index;
  };
  return [...groups.values()].sort((a, b) => rank(a.type) - rank(b.type));
}

function trackLabel(audio: DiscoverAudio, type: string): string {
  const parts = audio.label.split(" · ");
  if (type && parts[0].trim().toLowerCase() === type.toLowerCase()) parts.shift();
  // A type-only entry has no studio. Keep language/codec rather than showing
  // a blank button or repeating the category heading for every rendition.
  if (!audio.author && type && audio.lang) parts.unshift(audio.lang.toUpperCase());
  return parts.join(" · ") || audio.label;
}

export function AudioPicker({ audios, selected, onToggle }: {
  audios: DiscoverAudio[];
  selected: Set<string>;
  onToggle: (label: string) => void;
}) {
  const { t } = useI18n();
  const id = useId();
  return (
    <div className="space-y-3">
      {groupAudios(audios).map((group, index) => (
        <div key={group.type} role="group" aria-labelledby={`${id}-${index}`}>
          <h4 id={`${id}-${index}`} className="mb-1.5 flex items-center gap-2 text-xs font-medium text-slate-400">
            {t(group.title)}
            <span className="text-[10px] tabular-nums text-slate-600">{group.tracks.length}</span>
          </h4>
          <div className="flex flex-wrap gap-2">
            {group.tracks.map((audio) => {
              const on = selected.has(audio.label);
              return (
                <button
                  key={audio.label}
                  onClick={() => onToggle(audio.label)}
                  aria-label={audio.label}
                  aria-pressed={on}
                  className={clsx(
                    "inline-flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-xs font-medium transition",
                    on
                      ? "border-gold-500/50 bg-gold-500/[0.14] text-gold-200"
                      : "border-white/[0.08] bg-white/[0.02] text-slate-400 hover:border-white/20 hover:text-slate-200",
                  )}
                >
                  {on && <Check className="h-3 w-3 shrink-0" strokeWidth={3} />}
                  {trackLabel(audio, group.type)}
                </button>
              );
            })}
          </div>
        </div>
      ))}
    </div>
  );
}
