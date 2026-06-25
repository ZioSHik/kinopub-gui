import { describe, expect, it } from "vitest";
import { CATEGORIES, categoryByKey } from "./categories";

describe("CATEGORIES data", () => {
  it("has unique keys", () => {
    const keys = CATEGORIES.map((c) => c.key);
    expect(new Set(keys).size).toBe(keys.length);
  });

  it("type categories carry a content type and no fixed genre", () => {
    for (const c of CATEGORIES) {
      if (c.type) {
        expect(c.genre).toBe("");
      }
    }
  });

  it("genre-based categories carry a genre and no content type", () => {
    const anime = categoryByKey("anime")!;
    const sport = categoryByKey("sport")!;
    expect(anime.type).toBe("");
    expect(anime.genre).toBe("25");
    expect(anime.genreType).toBe("");
    expect(sport.type).toBe("");
    expect(sport.genre).toBe("20,71");
    expect(sport.genreType).toBe("");
  });

  it("type categories use their own type as the sub-genre source", () => {
    for (const c of CATEGORIES) {
      if (c.type) expect(c.genreType).toBe(c.type);
    }
  });

  it("every category exposes a label and an icon", () => {
    for (const c of CATEGORIES) {
      expect(c.label.length).toBeGreaterThan(0);
      expect(c.icon).toBeTruthy();
    }
  });
});

describe("categoryByKey", () => {
  it.each(CATEGORIES.map((c) => c.key))("resolves %s to its category", (key) => {
    expect(categoryByKey(key)?.key).toBe(key);
  });

  it("returns undefined for an unknown key", () => {
    expect(categoryByKey("does-not-exist")).toBeUndefined();
    expect(categoryByKey("")).toBeUndefined();
  });

  it("returns the exact object reference from CATEGORIES", () => {
    expect(categoryByKey("movie")).toBe(CATEGORIES[0]);
  });
});
