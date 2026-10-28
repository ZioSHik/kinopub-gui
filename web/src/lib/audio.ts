import type { AudioSpec, DiscoverAudio } from "../api";

// Remembering "the voiceover I picked last time" needs an identity that survives
// the move to another title or season. The card's label is not it: it reads
// "<тип> · <автор>" (see audioLabel in internal/gui/discover.go), and kino.pub
// files the same studio under a different type from title to title — "Многоголосый
// · AniLibria" here, "Дубляж · AniLibria" there. Comparing whole labels called
// that a different voiceover and told the user theirs was unavailable, while the
// download itself matched on the studio alone and would have found it.
//
// So identity starts from the studio: the API's own `filter` token, which is
// exactly what the download sends as its Require spec. Codec and surround ride
// along because the picker lists each rendition of one studio as its own entry —
// "Авторский · М. Яроцкий · AAC 5.1" and "· AC3 5.1" are two of them. Keying on
// the studio alone pre-selected both and would have downloaded two audio tracks
// where one was asked for.
export function audioIdentity(
  a: Pick<DiscoverAudio, "filter" | "label" | "surround" | "codec"> & Partial<Pick<DiscoverAudio, "lang" | "channels">>,
): string {
  const studio = (a.filter || a.label || "").trim().toLowerCase();
  return [studio, (a.codec || "").trim().toLowerCase(), a.surround ? "5.1" : "", a.lang || "", a.channels || ""].join("|");
}

// Codecs that appear verbatim in an HLS track name. This is the only thing
// telling a studio's surround rendition from its plain sibling, so the download
// spec keys on it (a tagged entry REQUIREs its token, a plain one FORBIDs them
// all — see the Specs docs in internal/domain/audio.go) and so does the on-disk
// match below.
export const TAGGED_CODECS = ["ac3", "eac3", "e-ac3", "dts", "dts-hd", "truehd", "true-hd"];

export function isTaggedCodec(a: Pick<DiscoverAudio, "codec">): boolean {
  return TAGGED_CODECS.includes((a.codec || "").toLowerCase());
}

// legacyAudioKey is the identity written by versions that stored whole labels.
// Kept so a preference saved before the fix still pre-selects something instead
// of greeting the user with "your last voiceover isn't available here"; the next
// download rewrites the preference in the current form.
export function legacyAudioKey(label: string): string {
  return label
    .replace(/^\s*\d+\.\s*/, "")
    .trim()
    .toLowerCase();
}

// matchRememberedAudio returns the tracks that are the remembered voiceover.
// Empty means it genuinely is not offered here.
export function matchRememberedAudio<T extends Pick<DiscoverAudio, "filter" | "label" | "surround">>(
  audios: T[],
  prefs: string[],
): T[] {
  if (!prefs.length) return [];
  const want = new Set(prefs);
  const exact = audios.filter((a) => want.has(audioIdentity(a)));
  for (const pref of prefs) {
    if (exact.some((a) => audioIdentity(a) === pref)) continue;
    const legacy = audios.find((a) => audioIdentity(a).split("|").slice(0, 3).join("|") === pref || legacyAudioKey(a.label) === pref);
    if (legacy && !exact.includes(legacy)) exact.push(legacy);
  }
  return exact;
}

// canonCodec folds the spelling variants of one codec together ("e-ac3" and
// "eac3", "true-hd" and "truehd") so identity never depends on punctuation.
function canonCodec(c: string): string {
  return c.replace(/-/g, "");
}

// codecOfName returns the most specific tagged codec appearing in an HLS track
// name, or "" when it carries none. Longest match wins: "eac3"/"e-ac3" contain
// "ac3" as a substring, and a plain includes() test let a studio's AC3 picker
// entry claim its E-AC3 recording — both renditions came out pre-selected and
// the next download fetched two audio tracks where one was on disk.
function codecOfName(name: string): string {
  let best = "";
  for (const c of TAGGED_CODECS) {
    if (name.includes(c) && c.length > best.length) best = c;
  }
  return best;
}

// matchDownloadedAudio returns the picker entries corresponding to voiceovers
// already on disk. The names recorded with an episode are HLS rendition names
// ("01. Многоголосый. AniLibria (RUS)"), not picker labels, so they are matched
// the way the downloader itself matches them: the studio token appears somewhere
// in the track name (see audioMatches in internal/domain/audio.go).
export function matchDownloadedAudio<T extends Pick<DiscoverAudio, "filter" | "codec"> & Partial<DiscoverAudio>>(
  audios: T[],
  downloadedNames: string[],
): T[] {
  const names = downloadedNames.map((n) => n.toLowerCase());
  if (!names.length) return [];
  return audios.filter((a) => {
    const studio = (a.filter || "").trim().toLowerCase();
    if (!studio) return false;
    const tagged = isTaggedCodec(a);
    const codec = canonCodec((a.codec || "").toLowerCase());
    return names.some((n) => {
      const source = sourceOfAudio(a);
      if (source && /\[[^\]]+ \d+ch\]$/.test(n)) return matchesRecordedSource(source, n);
      if (!n.includes(studio)) return false;
      if (isCommentaryName(n) && !isCommentaryAudio(a)) return false;
      // One studio can offer several renditions; only the codec tells them
      // apart, so a plain entry must not claim a recording that is AC3 and vice
      // versa — and an AC3 entry must not claim an E-AC3 recording just because
      // "ac3" appears inside "eac3".
      const recorded = codecOfName(n);
      return tagged ? canonCodec(recorded) === codec : recorded === "";
    });
  });
}

// Where the voiceover choice is remembered. The per-title map is the honest
// answer to "what did I pick for THIS show"; the single last-choice key is only
// a starting guess for a title never downloaded before.
const AUDIO_PREF_KEY = "kp.download.audioPref";
const AUDIO_PREF_BY_ITEM_KEY = "kp.download.audioPrefByItem";

// Bound on remembered titles, so a long browsing history cannot grow the entry
// without limit. Entries are held most-recent-first in an ordered array — NOT
// an object: item ids are integer-like strings, which JS objects enumerate in
// ascending numeric order regardless of insertion, so the old delete/re-insert
// recency trick was a no-op and eviction removed the smallest ids (including,
// for a low-id title, the preference just saved) instead of the oldest.
const MAX_REMEMBERED_TITLES = 300;

// RememberedAudio is a voiceover preference plus where it came from. Scoped
// means "this title's own choice" — only then is it worth telling the user the
// voiceover is missing; a carried-over choice from an unrelated title going
// unmatched is normal and not worth a warning.
export type RememberedAudio = { prefs: string[]; scoped: boolean };

function readJSON<T>(key: string, fallback: T): T {
  try {
    const v = JSON.parse(localStorage.getItem(key) || "null");
    return v === null ? fallback : (v as T);
  } catch {
    return fallback;
  }
}

type ItemPref = [id: string, prefs: string[]];

function readByItem(): ItemPref[] {
  const raw = readJSON<unknown>(AUDIO_PREF_BY_ITEM_KEY, null);
  const out: ItemPref[] = [];
  if (Array.isArray(raw)) {
    for (const e of raw) {
      if (Array.isArray(e) && typeof e[0] === "string" && Array.isArray(e[1])) {
        out.push([e[0], (e[1] as unknown[]).filter((x): x is string => typeof x === "string")]);
      }
    }
    return out;
  }
  // Older builds stored a plain id → prefs object; carry those entries over
  // (their relative order is lost — it never really existed, see above).
  if (raw && typeof raw === "object") {
    for (const [k, v] of Object.entries(raw as Record<string, unknown>)) {
      if (Array.isArray(v)) out.push([k, v.filter((x): x is string => typeof x === "string")]);
    }
  }
  return out;
}

// readAudioPref returns this title's own remembered voiceover when there is one,
// otherwise the last choice made anywhere.
export function readAudioPref(itemId: string): RememberedAudio {
  const mine = readByItem().find(([k]) => k === itemId)?.[1];
  if (mine?.length) return { prefs: mine, scoped: true };
  const last = readJSON<unknown[]>(AUDIO_PREF_KEY, []);
  const prefs = Array.isArray(last) ? last.filter((x): x is string => typeof x === "string") : [];
  return { prefs, scoped: false };
}

// writeAudioPref records the choice against this title and as the global
// starting guess for the next unknown one.
export function writeAudioPref(itemId: string, identities: string[]) {
  try {
    if (identities.length) localStorage.setItem(AUDIO_PREF_KEY, JSON.stringify(identities));
    else localStorage.removeItem(AUDIO_PREF_KEY);

    if (!itemId) return;
    // Re-inserting at the front doubles as the recency update, so a title in
    // regular use is never the one evicted.
    let entries = readByItem().filter(([k]) => k !== itemId);
    if (identities.length) entries = [[itemId, identities], ...entries];
    localStorage.setItem(AUDIO_PREF_BY_ITEM_KEY, JSON.stringify(entries.slice(0, MAX_REMEMBERED_TITLES)));
  } catch {
    /* storage unavailable — the preference just won't persist */
  }
}

// Commentary is deliberately separate from an original soundtrack: providers
// often name both "Original", with a commentary suffix on the extra track.
export const COMMENTARY_MARKERS = ["комментар", "commentar"];
export function isCommentaryName(name: string): boolean {
  const lower = name.toLowerCase();
  return COMMENTARY_MARKERS.some((marker) => lower.includes(marker));
}
export function isCommentaryAudio(audio: { filter?: string; label?: string; author?: string }): boolean {
  return isCommentaryName([audio.filter, audio.label, audio.author].filter(Boolean).join(" "));
}

export function audioSelectionSpecs(audios: DiscoverAudio[]): AudioSpec[] {
  return audios.map((audio) => {
    const require = [audio.filter].filter(Boolean);
    if (isTaggedCodec(audio) && audio.codec) require.push(audio.codec);
    const forbid = isTaggedCodec(audio) ? [] : [...TAGGED_CODECS];
    if (!isCommentaryAudio(audio)) forbid.push(...COMMENTARY_MARKERS);
    const source = sourceOfAudio(audio);
    return source ? { require, forbid, source } : { require, forbid };
  });
}

function sourceOfAudio(audio: Partial<DiscoverAudio>): AudioSpec["source"] {
  if (audio.index === undefined && !audio.lang && !audio.type && !audio.author) return undefined;
  return { language: audio.lang || "", type: audio.type || "", author: audio.author || "", codec: audio.codec || "", channels: audio.channels || 0 };
}

function matchesRecordedSource(source: NonNullable<AudioSpec["source"]>, name: string): boolean {
  const details = /\s*\[([^\]]+) (\d+)ch\]$/.exec(name);
  if (!details || (source.codec && canonCodec(details[1]) !== canonCodec(source.codec.toLowerCase())) || (source.channels && Number(details[2]) !== source.channels)) return false;
  const body = name.replace(/\s*\[[^\]]+ \d+ch\]$/, "").replace(/^\s*\d+\.\s*/, "").replace(/\s+\(\d+\)$/, "")
    .replace(/(\))\s+(?:e-?ac3|ac3|dts(?:-hd)?|true-?hd)(?:\s+[257]\.1)?$/i, "$1");
  const expected = [source.type, source.author].filter(Boolean).join(". ") + ` (${source.language})`;
  return body.trim().toLowerCase() === expected.trim().toLowerCase();
}
