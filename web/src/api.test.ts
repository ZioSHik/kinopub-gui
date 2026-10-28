import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { abortPendingRequests, api, imgURL, isNavigationAbort, NAV_ABORT_MESSAGE } from "./api";

// Captures the last fetch call and lets each test stage a response.
let fetchMock: ReturnType<typeof vi.fn>;

function jsonResponse(body: unknown, init: { ok?: boolean; status?: number } = {}) {
  const ok = init.ok ?? true;
  const status = init.status ?? (ok ? 200 : 500);
  return {
    ok,
    status,
    text: async () => (body === undefined ? "" : JSON.stringify(body)),
  } as Response;
}

function textResponse(text: string, init: { ok?: boolean; status?: number } = {}) {
  const ok = init.ok ?? true;
  const status = init.status ?? (ok ? 200 : 500);
  return { ok, status, text: async () => text } as Response;
}

beforeEach(() => {
  fetchMock = vi.fn();
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => {
  vi.unstubAllGlobals();
});

// Convenience: the [url, init] of the single fetch call.
function lastCall() {
  expect(fetchMock).toHaveBeenCalledTimes(1);
  const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
  return { url, init };
}

describe("req: GET requests", () => {
  it("sends no body / no content-type and parses JSON", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ version: "1.2.3", jobs: [] }));
    const out = await api.state();
    const { url, init } = lastCall();
    expect(url).toBe("/api/state");
    expect(init.method).toBe("GET");
    expect(init.body).toBeUndefined();
    expect(init.headers).toBeUndefined();
    expect(out).toMatchObject({ version: "1.2.3" });
  });
});

describe("req: bodied requests", () => {
  it("PUT serializes the body and sets the JSON content-type", async () => {
    const settings = { quality: "1080p" } as never;
    fetchMock.mockResolvedValue(jsonResponse({ quality: "1080p" }));
    await api.saveSettings(settings);
    const { url, init } = lastCall();
    expect(url).toBe("/api/settings");
    expect(init.method).toBe("PUT");
    expect(init.headers).toEqual({ "Content-Type": "application/json" });
    expect(init.body).toBe(JSON.stringify(settings));
  });

  it("POST with a structured body (answerAudio)", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ ok: true }));
    await api.answerAudio("job1", [0, 2, 5]);
    const { url, init } = lastCall();
    expect(url).toBe("/api/jobs/job1/audio");
    expect(init.method).toBe("POST");
    expect(init.body).toBe(JSON.stringify({ indices: [0, 2, 5] }));
  });

  it("markTime floors and clamps the time, defaults season/episode", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ ok: true }));
    await api.markTime("id9", -3.7);
    expect(JSON.parse(lastCall().init.body as string)).toEqual({
      id: "id9",
      time: 0, // clamped to >= 0
      season: 0,
      episode: 0,
    });

    fetchMock.mockClear();
    fetchMock.mockResolvedValue(jsonResponse({ ok: true }));
    await api.markTime("id9", 123.9, 2, 4);
    expect(JSON.parse(lastCall().init.body as string)).toEqual({
      id: "id9",
      time: 123, // floored
      season: 2,
      episode: 4,
    });
  });
});

describe("req: error mapping", () => {
  it("uses the server-provided error field for non-2xx", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({ error: "boom from server" }, { ok: false, status: 400 }),
    );
    await expect(api.jobs()).rejects.toThrow("boom from server");
  });

  it("falls back to HTTP <status> when no error field", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ nope: true }, { ok: false, status: 503 }));
    await expect(api.jobs()).rejects.toThrow("HTTP 503");
  });

  it("falls back to HTTP <status> on an empty error body", async () => {
    fetchMock.mockResolvedValue(textResponse("", { ok: false, status: 500 }));
    await expect(api.jobs()).rejects.toThrow("HTTP 500");
  });

  it("returns raw text when the body is not JSON and ok", async () => {
    fetchMock.mockResolvedValue(textResponse("plain-pong"));
    const out = await api.jobs();
    expect(out).toBe("plain-pong" as never);
  });
});

describe("req: navigation abort", () => {
  // fetch that stays pending until its signal aborts, as a real in-flight
  // request does.
  function pendingUntilAborted() {
    fetchMock.mockImplementation(
      (_url: string, init: RequestInit) =>
        new Promise((_resolve, reject) => {
          init.signal?.addEventListener("abort", () =>
            reject(new DOMException("aborted", "AbortError")),
          );
        }),
    );
  }

  it("settles the promise instead of leaving the caller suspended forever", async () => {
    pendingUntilAborted();
    const inflight = api.state();
    abortPendingRequests();
    // The point of the assertion is that it resolves at all: this used to return
    // a promise that never settled, pinning the caller's frame — and everything
    // it closed over — for the life of the tab.
    await expect(inflight).rejects.toThrow(NAV_ABORT_MESSAGE);
  });

  it("marks the rejection as a navigation abort", async () => {
    pendingUntilAborted();
    const inflight = api.state();
    abortPendingRequests();
    await inflight.then(
      () => expect.unreachable("should have rejected"),
      (e) => expect(isNavigationAbort(e)).toBe(true),
    );
  });

  it("does not classify a real failure as a navigation abort", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ error: "boom" }, { ok: false, status: 500 }));
    await api.state().then(
      () => expect.unreachable("should have rejected"),
      (e) => expect(isNavigationAbort(e)).toBe(false),
    );
  });

  it("recognises the sentinel as a bare message, as onAppendError passes it", () => {
    expect(isNavigationAbort(NAV_ABORT_MESSAGE)).toBe(true);
    expect(isNavigationAbort("Catalog request failed")).toBe(false);
    expect(isNavigationAbort(undefined)).toBe(false);
    expect(isNavigationAbort(new Error("HTTP 500"))).toBe(false);
  });
});

describe("query/URL building", () => {
  it("checkUpdate appends ?force=1 only when forced", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ current: "1" }));
    await api.checkUpdate();
    expect(lastCall().url).toBe("/api/update");

    fetchMock.mockClear();
    fetchMock.mockResolvedValue(jsonResponse({ current: "1" }));
    await api.checkUpdate(true);
    expect(lastCall().url).toBe("/api/update?force=1");
  });

  it("discoverSearch encodes the query and includes the page", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ items: [] }));
    await api.discoverSearch("a b&c", 3);
    expect(lastCall().url).toBe("/api/discover/search?q=a%20b%26c&page=3");
  });

  it("discoverSearch defaults page to 1", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ items: [] }));
    await api.discoverSearch("x");
    expect(lastCall().url).toBe("/api/discover/search?q=x&page=1");
  });

  it("stream builds query params and omits null season/episode", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ manifestUrl: "", playUrl: "" }));
    await api.stream("777");
    expect(lastCall().url).toBe("/api/discover/stream?id=777");

    fetchMock.mockClear();
    fetchMock.mockResolvedValue(jsonResponse({ manifestUrl: "", playUrl: "" }));
    await api.stream("777", 1, 2);
    expect(lastCall().url).toBe("/api/discover/stream?id=777&season=1&episode=2");

    // season 0 is included (0 != null), episode omitted.
    fetchMock.mockClear();
    fetchMock.mockResolvedValue(jsonResponse({ manifestUrl: "", playUrl: "" }));
    await api.stream("777", 0);
    expect(lastCall().url).toBe("/api/discover/stream?id=777&season=0");
  });

  it("discoverItems only sets provided (truthy) params", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ items: [] }));
    await api.discoverItems({
      type: "movie",
      sort: "year",
      genre: "25",
      yearFrom: 2000,
      ac3: true,
      subtitles: false, // falsy -> omitted
      page: 2,
    });
    const url = lastCall().url;
    expect(url.startsWith("/api/discover/items?")).toBe(true);
    const qs = new URLSearchParams(url.split("?")[1]);
    expect(qs.get("type")).toBe("movie");
    expect(qs.get("sort")).toBe("year");
    expect(qs.get("genre")).toBe("25");
    expect(qs.get("yearFrom")).toBe("2000");
    expect(qs.get("ac3")).toBe("1");
    expect(qs.has("subtitles")).toBe(false);
    expect(qs.get("page")).toBe("2");
  });

  it("discoverItems with an empty query produces a bare URL", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ items: [] }));
    await api.discoverItems({});
    expect(lastCall().url).toBe("/api/discover/items?");
  });

  it("discoverWatching maps booleans to 0/1 and uses defaults", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ items: [] }));
    await api.discoverWatching();
    expect(lastCall().url).toBe("/api/discover/watching?type=serials&subscribed=0&page=1");

    fetchMock.mockClear();
    fetchMock.mockResolvedValue(jsonResponse({ items: [] }));
    await api.discoverWatching(true, "movies", 4);
    expect(lastCall().url).toBe("/api/discover/watching?type=movies&subscribed=1&page=4");
  });

  it("discoverGenres only adds the type param when provided", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ items: [] }));
    await api.discoverGenres();
    expect(lastCall().url).toBe("/api/discover/genres");

    fetchMock.mockClear();
    fetchMock.mockResolvedValue(jsonResponse({ items: [] }));
    await api.discoverGenres("movie");
    expect(lastCall().url).toBe("/api/discover/genres?type=movie");
  });

  it("libraryDownloaded / fs encode the id/path", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ id: "a b", episodes: [] }));
    await api.libraryDownloaded("a b");
    expect(lastCall().url).toBe("/api/library/downloaded?id=a%20b");

    fetchMock.mockClear();
    fetchMock.mockResolvedValue(jsonResponse({ path: "", parent: "", dirs: [] }));
    await api.fs("/some path/x");
    expect(lastCall().url).toBe("/api/fs?path=%2Fsome%20path%2Fx");
  });

  it("path-param endpoints interpolate the id without extra encoding", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ canceling: true }));
    await api.cancelJob("job-42");
    expect(lastCall().url).toBe("/api/jobs/job-42/cancel");
  });
});

describe("imgURL", () => {
  it("returns empty string for falsy input", () => {
    expect(imgURL()).toBe("");
    expect(imgURL("")).toBe("");
  });

  it("wraps and encodes the upstream URL", () => {
    expect(imgURL("https://x.test/a b.jpg")).toBe(
      "/api/img?u=" + encodeURIComponent("https://x.test/a b.jpg"),
    );
  });
});

describe("settings autosave ordering", () => {
  it("does not send the newer edit while the older one is still being saved", async () => {
    let finish!: (value: Response) => void;
    fetchMock.mockImplementationOnce(() => new Promise<Response>((resolve) => { finish = resolve; }));
    fetchMock.mockResolvedValue(jsonResponse({quality: "720p"}));
    const first = api.saveSettings({quality: "1080p"} as never);
    const second = api.saveSettings({quality: "720p"} as never);
    await Promise.resolve();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    finish(jsonResponse({quality: "1080p"}));
    await first;
    await second;
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(JSON.parse(fetchMock.mock.calls[1][1].body).quality).toBe("720p");
  });
  it("continues saving newer edits after a failed request", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({error: "disk full"}, {ok: false}));
    fetchMock.mockResolvedValueOnce(jsonResponse({quality: "720p"}));
    const first = api.saveSettings({quality: "1080p"} as never);
    const second = api.saveSettings({quality: "720p"} as never);
    await expect(first).rejects.toThrow("disk full");
    await expect(second).resolves.toEqual({quality: "720p"});
  });
});
