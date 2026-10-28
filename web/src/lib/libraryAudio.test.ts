import { describe, expect, it } from "vitest";
import { voiceoverCounts, voiceoverLabels } from "./libraryAudio";

describe("recorded voiceovers", () => {
  it("deduplicates numbered copies without losing the studio", () => {
    expect(voiceoverLabels(["02. Дубляж. Невафильм (RUS)", "02. Дубляж. Невафильм (RUS) (2)"]))
      .toEqual(["Дубляж · Невафильм"]);
  });
  it("reports seven episodes in one dub and one in another", () => {
    const episodes = Array.from({length: 8}, (_, i) => ({exists: true,
      audio: [i < 7 ? "02. Дубляж. Невафильм (RUS)" : "01. Авторский. М. Яроцкий (RUS)"]}));
    expect(voiceoverCounts(episodes)).toEqual([
      {label: "Дубляж · Невафильм", count: 7},
      {label: "Авторский · М. Яроцкий", count: 1},
    ]);
  });
  it("counts versions once and keeps missing metadata explicitly unknown", () => {
    expect(voiceoverCounts([{exists: true, audio: ["Studio"], versions: [{exists: true, audio: ["Studio"]}]},
      {exists: true}, {exists: false, audio: ["Gone"]}]))
      .toEqual([{label: "Studio", count: 1}, {label: "", count: 1}]);
  });
});
