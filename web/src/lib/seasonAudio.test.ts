import { beforeEach, expect, it } from "vitest";
import type { DiscoverAudio } from "../api";
import { rememberSeasonAudio, seasonAudioIssue, seedSeasonAudio } from "./seasonAudio";
const dub: DiscoverAudio = { index: 1,lang: "rus",type: "Дубляж",author: "",label: "Дубляж",filter: "Дубляж",codec: "aac",channels: 2,surround: false,episodeKeys: ["S1E1"] };
const rentv = { ...dub,author: "РенТВ",label: "Дубляж · РенТВ",filter: "РенТВ",episodeKeys: ["S6E1"] };
const groups = [{number: 1,audios: [dub],episodeKeys: ["S1E1"]},{number: 6,audios: [rentv],episodeKeys: ["S6E1"]}];
beforeEach(() => localStorage.clear());
it("remembers each season independently", () => {
  const selection = {1: new Set([dub.label]),6: new Set([rentv.label])};
  for (const group of groups) rememberSeasonAudio("8867", group, selection);
  const seeded = seedSeasonAudio("8867", groups);
  expect([...seeded[1]]).toEqual([dub.label]);
  expect([...seeded[6]]).toEqual([rentv.label]);
});
it("requires choices only for selected seasons and rejects missing metadata", () => {
  expect(seasonAudioIssue(groups[0], {}, new Set(["S6E1"])) ).toBeNull();
  expect(seasonAudioIssue(groups[1], {}, new Set(["S6E1"])) ).toBe("missing");
  expect(seasonAudioIssue({...groups[1],audios: []}, {}, new Set(["S6E1"])) ).toBe("missing");
});
it("checks availability against selected episodes rather than all episodes", () => {
  const group = {...groups[1],episodeKeys: ["S6E1","S6E2"]};
  const selected = {6: new Set([rentv.label])};
  expect(seasonAudioIssue(group, selected, new Set(["S6E1"])) ).toBeNull();
  expect(seasonAudioIssue(group, selected, new Set(["S6E1","S6E2"])) ).toBe("unavailable");
});
