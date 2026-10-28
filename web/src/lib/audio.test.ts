import { beforeEach, describe, expect, it } from "vitest";
import type { DiscoverAudio } from "../api";
import {
  audioIdentity,
  legacyAudioKey,
  matchDownloadedAudio,
  matchRememberedAudio,
  readAudioPref,
  writeAudioPref,
} from "./audio";

function audio(over: Partial<DiscoverAudio> = {}): DiscoverAudio {
  return {
    index: 0,
    lang: "rus",
    type: "Многоголосый",
    author: "AniLibria",
    label: "Многоголосый · AniLibria",
    filter: "AniLibria",
    surround: false,
    ...over,
  };
}

describe("matchRememberedAudio", () => {
  // The reported bug: the studio IS offered, under a different type, and the
  // card announced "your last voiceover isn't available here".
  it("finds the same studio filed under another type", () => {
    const remembered = [audioIdentity(audio())]; // picked as "Многоголосый · AniLibria"
    const here = [
      audio({ type: "Дубляж", label: "Дубляж · AniLibria" }),
      audio({ type: "Дубляж", author: "Невафильм", label: "Дубляж · Невафильм", filter: "Невафильм" }),
    ];
    expect(matchRememberedAudio(here, remembered).map((a) => a.label)).toEqual(["Дубляж · AniLibria"]);
  });

  it("reports nothing when the studio really is absent", () => {
    const remembered = [audioIdentity(audio())];
    const here = [audio({ author: "Невафильм", label: "Дубляж · Невафильм", filter: "Невафильм" })];
    expect(matchRememberedAudio(here, remembered)).toEqual([]);
  });

  // The picker lists a 5.1 variant as its own entry, so remembering one must not
  // tick both — that would quietly download two audio tracks instead of one.
  it("keeps the surround variant distinct from the plain dub", () => {
    const plain = audio();
    const surround = audio({ label: "Многоголосый · AniLibria · AC3 5.1", surround: true, codec: "ac3" });

    const remPlain = matchRememberedAudio([plain, surround], [audioIdentity(plain)]);
    expect(remPlain.map((a) => a.surround)).toEqual([false]);

    const remSurround = matchRememberedAudio([plain, surround], [audioIdentity(surround)]);
    expect(remSurround.map((a) => a.surround)).toEqual([true]);
  });

  it("still honours a preference saved in the old whole-label form", () => {
    const here = [audio()];
    expect(matchRememberedAudio(here, ["многоголосый · anilibria"])).toHaveLength(1);
  });

  it("matches nothing when nothing was remembered", () => {
    expect(matchRememberedAudio([audio()], [])).toEqual([]);
  });

  it("falls back to the label when the API gives no filter token", () => {
    const noFilter = audio({ filter: "", label: "Дорожка 2" });
    expect(matchRememberedAudio([noFilter], [audioIdentity(noFilter)])).toHaveLength(1);
  });
});

describe("legacyAudioKey", () => {
  it("strips the leading ordinal kino.pub prepends", () => {
    expect(legacyAudioKey("02. Дубляж. Невафильм (RUS)")).toBe("дубляж. невафильм (rus)");
  });
});

describe("matchDownloadedAudio", () => {
  // What is recorded with an episode is the HLS rendition name, which carries the
  // studio inside a longer string — so matching is by containment, the same rule
  // the downloader applies.
  it("finds the picker entry inside a recorded track name", () => {
    const here = [
      audio(),
      audio({ author: "Невафильм", label: "Дубляж · Невафильм", filter: "Невафильм" }),
    ];
    const onDisk = ["01. Многоголосый. AniLibria (RUS)"];
    expect(matchDownloadedAudio(here, onDisk).map((a) => a.filter)).toEqual(["AniLibria"]);
  });

  it("matches nothing when the disk holds a studio this title no longer lists", () => {
    expect(matchDownloadedAudio([audio()], ["03. Какая-то другая студия (RUS)"])).toEqual([]);
  });

  it("matches nothing when no episode recorded its voiceover", () => {
    expect(matchDownloadedAudio([audio()], [])).toEqual([]);
  });

  it("ignores entries the API gave no studio token for", () => {
    const noFilter = audio({ filter: "", label: "Дорожка 2" });
    expect(matchDownloadedAudio([noFilter], ["01. Что угодно"])).toEqual([]);
  });
});

describe("readAudioPref / writeAudioPref", () => {
  beforeEach(() => localStorage.clear());

  it("prefers this title's own choice and marks it as scoped", () => {
    writeAudioPref("100", ["anilibria"]);
    writeAudioPref("200", ["невафильм"]);
    expect(readAudioPref("100")).toEqual({ prefs: ["anilibria"], scoped: true });
  });

  // A title never downloaded still gets a sensible starting guess — but it is
  // NOT scoped, so a mismatch there must not be reported as a missing voiceover.
  it("falls back to the last choice made anywhere, unscoped", () => {
    writeAudioPref("100", ["anilibria"]);
    expect(readAudioPref("999")).toEqual({ prefs: ["anilibria"], scoped: false });
  });

  it("returns nothing when the user has never chosen", () => {
    expect(readAudioPref("100")).toEqual({ prefs: [], scoped: false });
  });

  it("survives a corrupted entry", () => {
    localStorage.setItem("kp.download.audioPrefByItem", "{not json");
    expect(readAudioPref("100")).toEqual({ prefs: [], scoped: false });
  });

  it("keeps the remembered titles bounded", () => {
    for (let i = 0; i < 320; i++) writeAudioPref(String(i), ["dub" + i]);
    const stored = JSON.parse(localStorage.getItem("kp.download.audioPrefByItem") || "[]");
    expect(Object.keys(stored).length).toBeLessThanOrEqual(300);
    // The newest survives, the oldest is dropped.
    expect(readAudioPref("319").scoped).toBe(true);
    expect(readAudioPref("0").scoped).toBe(false);
  });

  it("evicts by recency, not by numeric id", () => {
    // Ids inserted high-to-low, so the OLDEST entries carry the LARGEST ids. An
    // object-keyed store enumerates integer-like keys in ascending numeric
    // order regardless of insertion, so the old eviction removed the smallest
    // ids — including, for a low-id title, the preference saved a second ago.
    for (let i = 320; i >= 1; i--) writeAudioPref(String(i), ["dub" + i]);
    expect(readAudioPref("1").scoped).toBe(true); // just saved
    expect(readAudioPref("320").scoped).toBe(false); // genuinely the oldest
  });

  it("migrates the legacy object form", () => {
    localStorage.setItem("kp.download.audioPrefByItem", JSON.stringify({ "500": ["anilibria"] }));
    expect(readAudioPref("500")).toEqual({ prefs: ["anilibria"], scoped: true });
  });
});

// Reported: one studio offering both AAC 5.1 and AC3 5.1 had BOTH entries
// pre-ticked, so a click on Download would have fetched two audio tracks where
// one was picked. Studio and surround are identical there — only the codec
// separates them.
describe("codec variants of one studio", () => {
  const aac = audio({
    author: "М. Яроцкий",
    filter: "М. Яроцкий",
    label: "Авторский · М. Яроцкий · AAC 5.1",
    codec: "aac",
    surround: true,
  });
  const ac3 = audio({
    author: "М. Яроцкий",
    filter: "М. Яроцкий",
    label: "Авторский · М. Яроцкий · AC3 5.1",
    codec: "ac3",
    surround: true,
  });

  it("gives them different identities", () => {
    expect(audioIdentity(aac)).not.toBe(audioIdentity(ac3));
  });

  it("remembering one does not pre-select the other", () => {
    expect(matchRememberedAudio([aac, ac3], [audioIdentity(ac3)]).map((a) => a.codec)).toEqual(["ac3"]);
    expect(matchRememberedAudio([aac, ac3], [audioIdentity(aac)]).map((a) => a.codec)).toEqual(["aac"]);
  });

  // The recorded HLS name carries the tagged codec verbatim; a plain rendition
  // carries none — the same rule the download spec is built on.
  it("matches an AC3 recording to the AC3 entry only", () => {
    const onDisk = ["04. Авторский. М. Яроцкий (RUS) AC3 5.1"];
    expect(matchDownloadedAudio([aac, ac3], onDisk).map((a) => a.codec)).toEqual(["ac3"]);
  });

  it("matches a plain recording to the plain entry only", () => {
    const onDisk = ["03. Авторский. М. Яроцкий (RUS)"];
    expect(matchDownloadedAudio([aac, ac3], onDisk).map((a) => a.codec)).toEqual(["aac"]);
  });

  // "ac3" is a substring of "eac3"/"e-ac3": a plain includes() test let the AC3
  // entry claim an E-AC3 recording, pre-ticking both renditions and downloading
  // a duplicate audio track.
  it("does not let the AC3 entry claim an E-AC3 recording", () => {
    const eac3 = audio({
      author: "М. Яроцкий",
      filter: "М. Яроцкий",
      label: "Авторский · М. Яроцкий · E-AC3 5.1",
      codec: "eac3",
      surround: true,
    });
    expect(
      matchDownloadedAudio([ac3, eac3], ["05. Авторский. М. Яроцкий (RUS) EAC3 5.1"]).map((a) => a.codec),
    ).toEqual(["eac3"]);
    // The dashed spelling on disk names the same codec.
    expect(
      matchDownloadedAudio([ac3, eac3], ["05. Авторский. М. Яроцкий (RUS) E-AC3 5.1"]).map((a) => a.codec),
    ).toEqual(["eac3"]);
  });
});


describe("original soundtrack and commentary", () => {
  const original = audio({filter: "Оригинал", label: "Оригинал · AAC 5.1", codec: "aac"});
  const commentary = audio({filter: "Комментарии", label: "Оригинал · Комментарии", codec: "aac"});
  it("does not mark commentary as a downloaded original", () => {
    expect(matchDownloadedAudio([original, commentary], ["19. Оригинал. Комментарии (ENG)"])).toEqual([commentary]);
    expect(matchDownloadedAudio([original, commentary], ["18. Оригинал (ENG)"])).toEqual([original]);
  });
});
