import type { DiscoverAudio, DownloadedEpisode } from "../api";
import { audioIdentity, matchRememberedAudio, readAudioPref, writeAudioPref } from "./audio";
import { preferredRecordedAudio } from "./downloaded";

export type SeasonAudioGroup = { number: number; audios: DiscoverAudio[]; episodeKeys: string[] };
export type SeasonAudioSelection = Record<number, Set<string>>;

export function chosenSeasonAudios(group: SeasonAudioGroup, selection: SeasonAudioSelection) {
  return group.audios.filter((audio) => selection[group.number]?.has(audio.label));
}

export function seasonAudioIssue(group: SeasonAudioGroup, selection: SeasonAudioSelection, episodes: Set<string>): "missing" | "unavailable" | null {
  const keys = group.episodeKeys.filter((key) => episodes.has(key));
  if (!keys.length) return null;
  const chosen = chosenSeasonAudios(group, selection);
  if (!chosen.length) return "missing";
  return chosen.some((audio) => audio.episodeKeys && keys.some((key) => !audio.episodeKeys!.includes(key))) ? "unavailable" : null;
}

// Suggest a repeated single-track choice only where the exact rendition covers
// every selected episode. Applying it must not replace another season's choice.
export function sharedSeasonAudioSuggestions(groups: SeasonAudioGroup[], selection: SeasonAudioSelection, episodes: Set<string>) {
  const active = groups.map((group) => ({ group, keys: group.episodeKeys.filter((key) => episodes.has(key)) }))
    .filter(({ keys }) => keys.length);
  const available = (audio: DiscoverAudio, keys: string[]) => audio.episodeKeys && keys.every((key) => audio.episodeKeys!.includes(key));
  const repeated = new Map<string, { audio: DiscoverAudio; count: number }>();
  for (const { group, keys } of active) {
    const chosen = chosenSeasonAudios(group, selection);
    if (chosen.length !== 1 || !available(chosen[0], keys)) continue;
    const id = audioIdentity(chosen[0]);
    const entry = repeated.get(id) || { audio: chosen[0], count: 0 };
    entry.count++;
    repeated.set(id, entry);
  }
  return [...repeated].flatMap(([id, { audio, count }]) => {
    if (count < 2) return [];
    const targets = active.flatMap(({ group, keys }) => {
      if (selection[group.number]?.size) return [];
      const match = group.audios.find((candidate) => audioIdentity(candidate) === id && available(candidate, keys));
      return match ? [{ season: group.number, label: match.label }] : [];
    });
    return targets.length ? [{ id, audio, count, targets }] : [];
  });
}

export function seedSeasonAudio(itemId: string, groups: SeasonAudioGroup[], files: DownloadedEpisode[] = [], isSerial = true): SeasonAudioSelection {
  return Object.fromEntries(groups.map((group) => {
    const onDisk = preferredRecordedAudio(group.audios, files.filter((file) => file.season === group.number));
    const scoped = readAudioPref(isSerial ? `${itemId}:season:${group.number}` : itemId);
    const remembered = scoped.scoped ? scoped : readAudioPref(itemId);
    const chosen = onDisk.length ? onDisk : matchRememberedAudio(group.audios, remembered.prefs);
    return [group.number, new Set(chosen.map((audio) => audio.label))];
  }));
}

export function rememberSeasonAudio(itemId: string, group: SeasonAudioGroup, selection: SeasonAudioSelection, isSerial = true) {
  writeAudioPref(isSerial ? `${itemId}:season:${group.number}` : itemId, chosenSeasonAudios(group, selection).map(audioIdentity));
}
