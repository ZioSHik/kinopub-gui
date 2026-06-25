import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import type { JobView } from "./api";

// The store opens an EventSource and calls api.* on mount. Mock both so the
// provider mounts cleanly and we can drive SSE events to exercise the pure
// derivation logic (job sorting, upsert, removal) and the toast lifecycle.

// --- mock the api module ----------------------------------------------------
vi.mock("./api", () => ({
  api: {
    checkUpdate: vi.fn().mockResolvedValue({ current: "1.0.0", updateAvailable: false }),
    deps: vi.fn().mockResolvedValue({ installSupported: false }),
    kpUser: vi.fn().mockResolvedValue({ username: "u", subscriptionActive: true, subscriptionDays: 5 }),
    state: vi.fn().mockResolvedValue({ version: "", jobs: [], settings: {} }),
  },
}));

// --- mock EventSource -------------------------------------------------------
class FakeEventSource {
  static instances: FakeEventSource[] = [];
  url: string;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  closed = false;
  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }
  close() {
    this.closed = true;
  }
  emit(type: string, data: unknown) {
    this.onmessage?.({ data: JSON.stringify({ type, data }) });
  }
}

import { AppProvider, useApp } from "./store";

const wrapper = ({ children }: { children: ReactNode }) => <AppProvider>{children}</AppProvider>;

beforeEach(() => {
  FakeEventSource.instances = [];
  vi.stubGlobal("EventSource", FakeEventSource as unknown as typeof EventSource);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllTimers();
  vi.useRealTimers();
});

function job(id: string, createdAt: string) {
  return {
    id,
    url: "",
    status: "queued",
    title: id,
    outputPath: "",
    dryRun: false,
    quality: "",
    createdAt,
    episodes: [],
    logs: [],
  } as unknown as JobView;
}

function mountStore() {
  const r = renderHook(() => useApp(), { wrapper });
  const es = FakeEventSource.instances[0];
  return { ...r, es };
}

describe("AppProvider job derivation", () => {
  it("connects and flips `connected` on EventSource open", () => {
    const { result, es } = mountStore();
    expect(es).toBeTruthy();
    expect(result.current.connected).toBe(false);
    act(() => es.onopen?.());
    expect(result.current.connected).toBe(true);
  });

  it("sorts snapshot jobs newest-first by createdAt", () => {
    const { result, es } = mountStore();
    act(() => {
      es.emit("snapshot", {
        version: "9",
        jobs: [
          job("old", "2026-01-01T00:00:00Z"),
          job("new", "2026-06-01T00:00:00Z"),
          job("mid", "2026-03-01T00:00:00Z"),
        ],
        settings: {},
      });
    });
    expect(result.current.jobs.map((j) => j.id)).toEqual(["new", "mid", "old"]);
    expect(result.current.version).toBe("9");
    expect(result.current.settingsLoaded).toBe(true);
  });

  it("upserts a job: new job is inserted and re-sorted", () => {
    const { result, es } = mountStore();
    act(() => {
      es.emit("snapshot", { version: "", jobs: [job("a", "2026-01-01T00:00:00Z")], settings: {} });
    });
    act(() => es.emit("job", job("b", "2026-06-01T00:00:00Z")));
    expect(result.current.jobs.map((j) => j.id)).toEqual(["b", "a"]);
  });

  it("upserts a job: existing job is replaced in place", () => {
    const { result, es } = mountStore();
    act(() => {
      es.emit("snapshot", { version: "", jobs: [job("a", "2026-01-01T00:00:00Z")], settings: {} });
    });
    const updated = { ...job("a", "2026-01-01T00:00:00Z"), status: "completed" };
    act(() => es.emit("job", updated));
    expect(result.current.jobs).toHaveLength(1);
    expect(result.current.jobs[0].status).toBe("completed");
  });

  it("removes a job on job_removed", () => {
    const { result, es } = mountStore();
    act(() => {
      es.emit("snapshot", {
        version: "",
        jobs: [job("a", "2026-01-01T00:00:00Z"), job("b", "2026-02-01T00:00:00Z")],
        settings: {},
      });
    });
    act(() => es.emit("job_removed", { id: "a" }));
    expect(result.current.jobs.map((j) => j.id)).toEqual(["b"]);
  });

  it("ignores malformed SSE payloads without throwing", () => {
    const { result, es } = mountStore();
    act(() => es.onmessage?.({ data: "not json" }));
    act(() => es.onmessage?.({ data: "" }));
    expect(result.current.jobs).toEqual([]);
  });

  it("updates kpauth / ffmpeg / settings from their events", () => {
    const { result, es } = mountStore();
    act(() => es.emit("kpauth", { loggedIn: true, pending: false }));
    expect(result.current.kpauth.loggedIn).toBe(true);
    act(() => es.emit("ffmpeg", { ffmpegFound: true, ffprobeFound: true }));
    expect(result.current.ffmpeg.ffmpegFound).toBe(true);
    act(() => es.emit("settings", { quality: "720p" }));
    expect(result.current.settings.quality).toBe("720p");
  });
});

describe("AppProvider toasts", () => {
  it("adds a toast and auto-dismisses after the timeout", () => {
    vi.useFakeTimers();
    const { result } = mountStore();
    act(() => result.current.toast("hello"));
    expect(result.current.toasts).toHaveLength(1);
    expect(result.current.toasts[0]).toMatchObject({ kind: "info", message: "hello" });

    act(() => vi.advanceTimersByTime(4000));
    expect(result.current.toasts).toHaveLength(0);
  });

  it("error toasts live longer (6500ms)", () => {
    vi.useFakeTimers();
    const { result } = mountStore();
    act(() => result.current.toast("bad", "error"));
    act(() => vi.advanceTimersByTime(4000));
    expect(result.current.toasts).toHaveLength(1); // still alive at 4s
    act(() => vi.advanceTimersByTime(2500));
    expect(result.current.toasts).toHaveLength(0); // gone by 6.5s
  });

  it("assigns increasing ids and dismissToast removes a specific toast", () => {
    const { result } = mountStore();
    act(() => result.current.toast("one"));
    act(() => result.current.toast("two"));
    const ids = result.current.toasts.map((t) => t.id);
    expect(ids[1]).toBeGreaterThan(ids[0]);
    act(() => result.current.dismissToast(ids[0]));
    expect(result.current.toasts.map((t) => t.id)).toEqual([ids[1]]);
  });
});

describe("useApp outside provider", () => {
  it("throws", () => {
    expect(() => renderHook(() => useApp())).toThrow(/useApp must be used within AppProvider/);
  });
});

describe("AppProvider kpUser loading", () => {
  it("loads the profile when logged in and clears it on logout", async () => {
    const { result, es } = mountStore();
    act(() => es.emit("kpauth", { loggedIn: true, pending: false }));
    await waitFor(() => expect(result.current.kpUser?.username).toBe("u"));
    act(() => es.emit("kpauth", { loggedIn: false, pending: false }));
    await waitFor(() => expect(result.current.kpUser).toBeNull());
  });
});
