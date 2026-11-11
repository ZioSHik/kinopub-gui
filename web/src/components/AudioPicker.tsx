import { useId } from "react";
import clsx from "clsx";
import { Check } from "lucide-react";
import type { DiscoverAudio } from "../api";
import { isCommentaryAudio } from "../lib/audio";
import { useI18n } from "../i18n";

type AudioGroup = { type: string; title: string; tracks: DiscoverAudio[] };
const CATEGORIES = [
  ["дубляж", "Dubbing"],
  ["многоголосый", "Multi-voice translation"],
  ["двухголосый", "Two-voice translation"],
  ["авторский", "Author's translation"],
  ["одноголосый", "Single-voice translation"],
  ["оригинал", "Original soundtrack"],
  ["комментарии", "Commentary"],
] as const;

function groupAudios(audios: DiscoverAudio[]): AudioGroup[] {
  const groups = new Map<string, AudioGroup>();
  for (const audio of audios) {
    // Old responses may only carry the combined label. Recognize its type
    // prefix without interpreting an unknown studio name as a category.
    const prefix = audio.label.split(" · ")[0].trim();
    const type = isCommentaryAudio(audio) ? "Комментарии"
      : audio.type?.trim() || (CATEGORIES.some(([key]) => key === prefix.toLowerCase()) ? prefix : "");
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
  const sourceType = audio.type?.trim() || type;
  if (sourceType && parts[0].trim().toLowerCase() === sourceType.toLowerCase()) parts.shift();
  if (isCommentaryAudio(audio)) {
    const label = parts.join(" · ");
    return [audio.lang?.toUpperCase(), label].filter(Boolean).join(" · ") || audio.label;
  }
  // A type-only entry has no studio. Keep language/codec rather than showing
  // a blank button or repeating the category heading for every rendition.
  if (!audio.author && type && audio.lang) parts.unshift(audio.lang.toUpperCase());
  return parts.join(" · ") || audio.label;
}

export function AudioPicker({ audios, selected, onToggle, compact = false }: {
  audios: DiscoverAudio[];
  selected: Set<string>;
  onToggle: (label: string) => void;
  compact?: boolean;
}) {
  const { t } = useI18n();
  const id = useId();
  if (compact) return (
    <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
      {groupAudios(audios).flatMap((group) => group.tracks.map((audio) => {
        const on = selected.has(audio.label);
        const channels = audio.channels === 6 ? "5.1" : audio.channels === 8 ? "7.1" : audio.channels ? `${audio.channels}ch` : "";
        const format = [audio.codec?.toUpperCase(), channels].filter(Boolean).join(" ");
        const name = audio.author || (group.type ? t(group.title) : audio.label);
        const meta = [audio.author && t(group.title), audio.lang?.toUpperCase(), format].filter(Boolean).join(" · ");
        return <button
          key={audio.label}
          type="button"
          onClick={() => onToggle(audio.label)}
          aria-label={audio.label}
          aria-pressed={on}
          title={audio.label}
          className={clsx(
            "flex min-w-0 items-center gap-2 rounded-lg border px-2.5 py-2 text-left transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-gold-400/60",
            on ? "border-gold-500/35 bg-gold-500/[0.08] text-gold-200" : "border-transparent bg-white/[0.025] text-slate-300 hover:bg-white/[0.06]",
          )}
        >
          <span aria-hidden="true" className={clsx("flex h-4 w-4 shrink-0 items-center justify-center rounded border", on ? "border-gold-400 bg-gold-400 text-ink-950" : "border-slate-600")}>
            {on && <Check className="h-3 w-3" strokeWidth={3} />}
          </span>
          <span className="min-w-0">
            <span className="block truncate text-xs font-medium">{name}</span>
            {meta && <span className="mt-0.5 block truncate text-[10px] leading-tight text-slate-400">{meta}</span>}
          </span>
        </button>;
      }))}
    </div>
  );
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
