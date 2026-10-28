// Preserve studio names while removing source numbering and duplicate-track suffixes.
export function voiceoverLabel(name: string): string {
  let label = name.replace(/\s*\[[^\]]+ \d+ch\]$/, "").replace(/^\s*\d+\.\s*/, "").trim();
  if (/^(track|дорожка)\s+\d+/i.test(label)) return label;
  label = label.replace(/\s+\(\d+\)$/, "").replace(/\s*\((rus|eng|jpn|ru|en|ja)\)\s*$/i, "");
  return label.replace(/^(Дубляж|Многоголосый|Двухголосый|Авторский|Одноголосый)\.\s*/i, "$1 · ").trim();
}

export function voiceoverLabels(names?: string[]): string[] {
  return [...new Set((names || []).map(voiceoverLabel).filter(Boolean))];
}

type AudioFile = { exists: boolean; audio?: string[]; versions?: AudioFile[] };

// Count logical episodes per voiceover, even when a file has duplicate tracks
// or an episode exists in several qualities. Empty label means unknown metadata.
export function voiceoverCounts(episodes: AudioFile[]): Array<{ label: string; count: number }> {
  const counts = new Map<string, number>();
  for (const episode of episodes) {
    const files = [episode, ...(episode.versions || [])].filter((file) => file.exists);
    const labels = new Set(files.flatMap((file) => {
      const known = voiceoverLabels(file.audio);
      return known.length ? known : [""];
    }));
    for (const label of labels) counts.set(label, (counts.get(label) || 0) + 1);
  }
  return [...counts].map(([label, count]) => ({ label, count }));
}
