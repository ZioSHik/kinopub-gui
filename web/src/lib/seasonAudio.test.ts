import { beforeEach, expect, it } from "vitest";
import type { DiscoverAudio } from "../api";
import { rememberSeasonAudio, seasonAudioIssue, seedSeasonAudio, sharedSeasonAudioSuggestions } from "./seasonAudio";
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

it("suggests a repeated rendition only for unconfigured selected seasons with complete coverage", () => {
  const many = Array.from({length: 8}, (_, i) => {
    const number = i + 1;
    const episodeKeys = [`S${number}E1`, `S${number}E2`];
    return {number, episodeKeys, audios: [{...dub, episodeKeys}]};
  });
  // Display labels may change, but the source identity must stay the same.
  many[2].audios[0].label = "Дубляж · AAC 2ch";
  many[3].audios[0].codec = "ac3";
  many[4].audios[0].episodeKeys = ["S5E1"];
  many[7].audios[0].lang = "eng";
  const episodes = new Set(many.flatMap(group => group.episodeKeys).filter(key => !key.startsWith("S7")));
  const selected = {1: new Set([dub.label]), 2: new Set([dub.label]), 6: new Set(["Another studio"])};
  const suggestions = () => sharedSeasonAudioSuggestions(many, selected, episodes);
  expect(suggestions()).toHaveLength(1);
  expect(suggestions()[0].targets).toEqual([{season: 3, label: "Дубляж · AAC 2ch"}]);
  episodes.delete("S5E2");
  expect(suggestions()[0].targets.map(target => target.season)).toEqual([3, 5]);
  selected[2].clear();
  expect(suggestions()).toEqual([]);
});

it("does not learn a shared voiceover from an unavailable selection", () => {
  const many = [1, 2, 3].map(number => ({number, episodeKeys: [`S${number}E1`, `S${number}E2`],
    audios: [{...dub, episodeKeys: [`S${number}E1`, `S${number}E2`]}]}));
  many[1].audios[0].episodeKeys = ["S2E1"];
  expect(sharedSeasonAudioSuggestions(many, {1: new Set([dub.label]), 2: new Set([dub.label])}, new Set(many.flatMap(group => group.episodeKeys)))).toEqual([]);
});
