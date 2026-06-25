import { afterEach, describe, expect, it, vi } from "vitest";
import { bytes, speed, eta, duration, relTime, clockTime, parseSeasons } from "./format";

// A translator stub that records calls and returns a recognizable rendering, so
// we can assert both the chosen key and the interpolated vars independently of
// the English templates.
const tSpy = (key: string, vars?: Record<string, string | number>) => {
  if (!vars) return `K(${key})`;
  const parts = Object.keys(vars)
    .map((k) => `${k}=${vars[k]}`)
    .join(",");
  return `K(${key}|${parts})`;
};

describe("bytes", () => {
  it.each<[number | undefined, string]>([
    [undefined, "0 B"],
    [0, "0 B"],
    [-5, "0 B"],
    [1, "1 B"],
    [512, "512 B"],
    [1023, "1023 B"],
    [1024, "1.0 KB"],
    [1536, "1.5 KB"],
    [102400, "100 KB"], // >=100 in a non-byte unit rounds to 0 decimals
    [1024 * 1024, "1.0 MB"],
    [1024 * 1024 * 1024, "1.0 GB"],
    [1024 ** 4, "1.0 TB"],
    [1024 ** 5, "1024 TB"], // capped at TB (no PB); v=1024 >= 100 so 0 decimals
  ])("bytes(%s) -> %s", (input, expected) => {
    expect(bytes(input)).toBe(expected);
  });

  it("uses 0 decimals for raw bytes (i===0) even under 100", () => {
    expect(bytes(99)).toBe("99 B");
  });

  it("switches to 0 decimals exactly at 100 in a scaled unit", () => {
    // 100 KB exactly -> v=100 -> toFixed(0)
    expect(bytes(100 * 1024)).toBe("100 KB");
    // just under 100 KB -> 1 decimal
    expect(bytes(99 * 1024 + 512)).toBe("99.5 KB");
  });
});

describe("speed", () => {
  it.each<[number | undefined, string]>([
    [undefined, "—"],
    [0, "—"],
    [-1, "—"],
    [1024, "1.0 KB/s"],
    [1048576, "1.0 MB/s"],
  ])("speed(%s) -> %s", (input, expected) => {
    expect(speed(input)).toBe(expected);
  });
});

describe("eta", () => {
  it("returns em-dash for falsy / non-positive", () => {
    expect(eta(undefined)).toBe("—");
    expect(eta(0)).toBe("—");
    expect(eta(-3)).toBe("—");
  });

  it("seconds bucket rounds to nearest second", () => {
    expect(eta(1, tSpy)).toBe("K({n}s|n=1)");
    expect(eta(59.4, tSpy)).toBe("K({n}s|n=59)");
  });

  it("minutes bucket below 60m", () => {
    // 90s -> 1m 30s
    expect(eta(90, tSpy)).toBe("K({m}m {s}s|m=1,s=30)");
    // 3599s -> 59m 59s
    expect(eta(3599, tSpy)).toBe("K({m}m {s}s|m=59,s=59)");
  });

  it("hours bucket at/above 60m", () => {
    // 3600s -> 1h 0m
    expect(eta(3600, tSpy)).toBe("K({h}h {m}m|h=1,m=0)");
    // 3660s -> 1h 1m
    expect(eta(3660, tSpy)).toBe("K({h}h {m}m|h=1,m=1)");
  });

  it("default (no translator) renders the English template with interpolation", () => {
    expect(eta(90)).toBe("1m 30s");
    expect(eta(30)).toBe("30s");
    expect(eta(3660)).toBe("1h 1m");
  });

  // KNOWN QUIRK (flagged): the seconds remainder is rounded independently of the
  // minute floor, so a value whose remainder rounds up to 60 renders "Xm 60s"
  // instead of carrying into "(X+1)m 0s". Pinning current behavior, NOT asserting
  // it is desirable. e.g. 119.7s -> m=floor(1.995)=1, s=round(59.7)=60.
  it("[quirk] renders 60s instead of carrying into the next minute", () => {
    expect(eta(119.7)).toBe("1m 60s");
  });
});

describe("duration", () => {
  it("returns empty string for falsy / non-positive", () => {
    expect(duration(undefined)).toBe("");
    expect(duration(0)).toBe("");
    expect(duration(-10)).toBe("");
  });

  it("minutes only below 60m", () => {
    // 59s -> 0m (floored)
    expect(duration(59, tSpy)).toBe("K({m}m|m=0)");
    expect(duration(125, tSpy)).toBe("K({m}m|m=2)");
  });

  it("hours and minutes at/above 60m", () => {
    expect(duration(3600, tSpy)).toBe("K({h}h {m}m|h=1,m=0)");
    expect(duration(3660, tSpy)).toBe("K({h}h {m}m|h=1,m=1)");
  });

  it("default renders English template", () => {
    expect(duration(125)).toBe("2m");
    expect(duration(3660)).toBe("1h 1m");
  });
});

describe("relTime", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("returns empty for missing / invalid ISO", () => {
    expect(relTime(undefined)).toBe("");
    expect(relTime("")).toBe("");
    expect(relTime("not-a-date")).toBe("");
  });

  it("buckets relative to a fixed system time", () => {
    vi.useFakeTimers();
    const now = new Date("2026-06-25T12:00:00.000Z");
    vi.setSystemTime(now);

    const iso = (msAgo: number) => new Date(now.getTime() - msAgo).toISOString();

    expect(relTime(iso(0), tSpy)).toBe("K(just now)");
    expect(relTime(iso(30_000), tSpy)).toBe("K(just now)"); // 30s < 60
    expect(relTime(iso(5 * 60_000), tSpy)).toBe("K({n}m ago|n=5)");
    expect(relTime(iso(59 * 60_000), tSpy)).toBe("K({n}m ago|n=59)");
    expect(relTime(iso(2 * 3600_000), tSpy)).toBe("K({n}h ago|n=2)");
    expect(relTime(iso(23 * 3600_000), tSpy)).toBe("K({n}h ago|n=23)");
    expect(relTime(iso(3 * 86400_000), tSpy)).toBe("K({n}d ago|n=3)");
  });

  it("default renders English template", () => {
    vi.useFakeTimers();
    const now = new Date("2026-06-25T12:00:00.000Z");
    vi.setSystemTime(now);
    const fiveMinAgo = new Date(now.getTime() - 5 * 60_000).toISOString();
    expect(relTime(fiveMinAgo)).toBe("5m ago");
  });
});

describe("clockTime", () => {
  it("returns empty for missing / invalid date", () => {
    expect(clockTime(undefined)).toBe("");
    expect(clockTime("")).toBe("");
    expect(clockTime("nonsense")).toBe("");
  });

  it("returns a non-empty time string for a valid date", () => {
    // Locale/timezone-dependent formatting; assert it produced something.
    const out = clockTime("2026-06-25T12:34:56.000Z");
    expect(out).not.toBe("");
    expect(typeof out).toBe("string");
  });
});

describe("parseSeasons", () => {
  it.each<[number[], string]>([
    [[], ""],
    [[1], "1"],
    [[1, 2, 3], "1-3"],
    [[1, 3, 4, 5], "1,3-5"],
    [[5, 3, 1, 4], "1,3-5"], // unsorted input
    [[1, 1, 2, 2, 3], "1-3"], // duplicates collapsed
    [[2, 4, 6], "2,4,6"], // no runs
    [[1, 2, 4, 5, 7], "1-2,4-5,7"],
    [[10, 11, 13], "10-11,13"],
  ])("parseSeasons(%j) -> %s", (input, expected) => {
    expect(parseSeasons(input)).toBe(expected);
  });

  it("does not mutate its input array", () => {
    const input = [3, 1, 2];
    parseSeasons(input);
    expect(input).toEqual([3, 1, 2]);
  });
});
