import type { DiscoverAudio, DownloadedEpisode } from "../api";
import { matchDownloadedAudio } from "./audio";

// Match the selected rendition rather than treating any file as a completed episode.
export function matchesDownload(ep: DownloadedEpisode, quality: string, container: string,
  audios: DiscoverAudio[], selected: Set<string>): boolean {
  if (!ep.exists) return false;
  if (ep.path && !ep.path.toLowerCase().endsWith("." + container)) return false;
  const q = quality || "optimal";
  if (ep.requestedQuality !== q) {
    let [w, h] = (ep.resolution || "").split("x").map(Number);
    const tiers: Record<number, number> = {3840:2160, 1920:1080, 1280:720, 854:480, 720:480, 640:360};
    h = Math.max(h || 0, tiers[w] || 0);
    const [tier, codec] = q.split("-");
    if (tier !== `${h}p` || (codec && !ep.quality?.endsWith("/" + codec))) return false;
  }
  if (!audios.length) return true;
  const wanted = audios.filter((a) => selected.has(a.label));
  if (!wanted.length) return false;
  const found = new Set(matchDownloadedAudio(wanted, ep.audio || []));
  return wanted.every((a) => found.has(a));
}

// The disk inventory is independent of the next download's selected tracks.
export function existingDownloads(files: DownloadedEpisode[]): Map<string, DownloadedEpisode> {
  const episodes = new Map<string, DownloadedEpisode>();
  for (const file of files) {
    if (!file.exists) continue;
    const current = episodes.get(file.key);
    if (!current || (!current.audio?.length && file.audio?.length)) episodes.set(file.key, file);
  }
  return episodes;
}

// Seed the picker from the most common recorded track combination. Taking the
// union across episodes would silently request both dubs in a mixed season.
export function preferredRecordedAudio(audios: DiscoverAudio[], files: DownloadedEpisode[]): DiscoverAudio[] {
  const profiles = new Map<string, { audios: DiscoverAudio[]; episodes: Set<string> }>();
  for (const file of files) {
    if (!file.exists) continue;
    const matched = matchDownloadedAudio(audios, file.audio || []);
    if (!matched.length) continue;
    const key = JSON.stringify(matched.map((audio) => audio.label).sort());
    const profile = profiles.get(key) || { audios: matched, episodes: new Set<string>() };
    profile.episodes.add(file.key);
    profiles.set(key, profile);
  }
  return [...profiles.values()].sort((a, b) => b.episodes.size - a.episodes.size)[0]?.audios || [];
}
