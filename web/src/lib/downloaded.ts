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
