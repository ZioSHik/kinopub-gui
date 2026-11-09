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

export function seedSeasonAudio(itemId: string, groups: SeasonAudioGroup[], files: DownloadedEpisode[] = []): SeasonAudioSelection {
  return Object.fromEntries(groups.map((group) => {
    const onDisk = preferredRecordedAudio(group.audios, files.filter((file) => file.season === group.number));
    const scoped = readAudioPref(`${itemId}:season:${group.number}`);
    const remembered = scoped.scoped ? scoped : readAudioPref(itemId);
    const chosen = onDisk.length ? onDisk : matchRememberedAudio(group.audios, remembered.prefs);
    return [group.number, new Set(chosen.map((audio) => audio.label))];
  }));
}

export function rememberSeasonAudio(itemId: string, group: SeasonAudioGroup, selection: SeasonAudioSelection) {
  writeAudioPref(`${itemId}:season:${group.number}`, chosenSeasonAudios(group, selection).map(audioIdentity));
}
