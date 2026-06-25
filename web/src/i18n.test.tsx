import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { I18nProvider, useI18n, looksLikeTimeout } from "./i18n";

const wrapper = ({ children }: { children: ReactNode }) => (
  <I18nProvider>{children}</I18nProvider>
);

function setup() {
  return renderHook(() => useI18n(), { wrapper });
}

describe("useI18n translator (t)", () => {
  beforeEach(() => {
    localStorage.clear();
    // Force a deterministic default language by clearing the saved pref; the
    // navigator.language fallback in jsdom is typically "en".
  });
  afterEach(() => {
    localStorage.clear();
  });

  it("defaults to English: returns the key verbatim", () => {
    const { result } = setup();
    expect(result.current.lang).toBe("en");
    expect(result.current.t("Settings")).toBe("Settings");
    // Unknown key also returns itself in English.
    expect(result.current.t("Totally unknown key")).toBe("Totally unknown key");
  });

  it("interpolates {placeholder} vars in English", () => {
    const { result } = setup();
    expect(result.current.t("{n} episodes", { n: 12 })).toBe("12 episodes");
    expect(result.current.t("Season {n}", { n: 3 })).toBe("Season 3");
    // Multiple placeholders.
    expect(result.current.t("{done}/{total} episodes", { done: 2, total: 5 })).toBe(
      "2/5 episodes",
    );
  });

  it("repeats a placeholder everywhere it appears", () => {
    const { result } = setup();
    // The interpolation uses split/join, so all occurrences are replaced.
    expect(result.current.t("{x}-{x}", { x: 7 })).toBe("7-7");
  });

  it("switches to Russian and looks up translated strings", () => {
    const { result } = setup();
    act(() => result.current.setLang("ru"));
    expect(result.current.lang).toBe("ru");
    expect(result.current.t("Settings")).toBe("Настройки");
    expect(result.current.t("{n} episodes", { n: 4 })).toBe("эпизодов: 4");
  });

  it("falls back to the English key when a Russian translation is missing", () => {
    const { result } = setup();
    act(() => result.current.setLang("ru"));
    expect(result.current.t("Totally unknown key")).toBe("Totally unknown key");
    // Fallback still interpolates against the key template.
    expect(result.current.t("{n} widgets", { n: 9 })).toBe("9 widgets");
  });

  it("persists the chosen language to localStorage", () => {
    const { result } = setup();
    act(() => result.current.setLang("ru"));
    expect(localStorage.getItem("kinopub.lang")).toBe("ru");
  });

  it("honors a saved language preference on mount", () => {
    localStorage.setItem("kinopub.lang", "ru");
    const { result } = setup();
    expect(result.current.lang).toBe("ru");
    expect(result.current.t("Queue")).toBe("Очередь");
  });
});

describe("useI18n outside provider", () => {
  it("throws a helpful error", () => {
    expect(() => renderHook(() => useI18n())).toThrow(
      /useI18n must be used within I18nProvider/,
    );
  });
});

describe("looksLikeTimeout", () => {
  it.each<[string | undefined, boolean]>([
    [undefined, false],
    ["", false],
    ["something went fine", false],
    ["context deadline exceeded", true],
    ["DEADLINE EXCEEDED", true], // case-insensitive
    ["request timeout", true],
    ["the request timed out", true],
    ["dial tcp: lookup host: no such host", true],
    ["read: i/o timeout", true],
    ["connection refused", false],
  ])("looksLikeTimeout(%j) -> %s", (input, expected) => {
    expect(looksLikeTimeout(input)).toBe(expected);
  });
});
