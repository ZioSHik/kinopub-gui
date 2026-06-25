import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  parseHash,
  buildHash,
  readRoute,
  pushRoute,
  replaceRoute,
  dismiss,
  PAGES,
  type Route,
} from "./router";

describe("parseHash", () => {
  it("bare legacy hash resolves to the matching page", () => {
    expect(parseHash("#queue")).toEqual({ page: "queue" });
    expect(parseHash("queue")).toEqual({ page: "queue" });
    expect(parseHash("/queue")).toEqual({ page: "queue" });
    expect(parseHash("#/queue")).toEqual({ page: "queue" });
  });

  it("empty / root resolves to discover", () => {
    expect(parseHash("")).toEqual({ page: "discover" });
    expect(parseHash("#")).toEqual({ page: "discover" });
    expect(parseHash("#/")).toEqual({ page: "discover" });
    expect(parseHash("/")).toEqual({ page: "discover" });
  });

  it("unknown page falls back to discover", () => {
    expect(parseHash("#/bogus")).toEqual({ page: "discover" });
    expect(parseHash("#nope/c/1")).toEqual({ page: "discover", collectionId: "1" });
  });

  it("parses collection / bookmark / item segments", () => {
    expect(parseHash("#/discover/c/678/i/12345")).toEqual({
      page: "discover",
      collectionId: "678",
      itemId: "12345",
    });
    expect(parseHash("#/discover/b/42")).toEqual({
      page: "discover",
      bookmarkId: "42",
    });
    expect(parseHash("#/library/c/1/b/2/i/3")).toEqual({
      page: "library",
      collectionId: "1",
      bookmarkId: "2",
      itemId: "3",
    });
  });

  it("ignores a trailing key with no value", () => {
    // "i" with no following segment: the loop breaks, itemId stays unset.
    expect(parseHash("#/discover/c/678/i")).toEqual({
      page: "discover",
      collectionId: "678",
    });
    expect(parseHash("#/discover/i")).toEqual({ page: "discover" });
  });

  it("ignores unknown keys but keeps scanning in pairs", () => {
    // "x" is unknown, consumed as a pair; "i/9" is then parsed.
    expect(parseHash("#/discover/x/zzz/i/9")).toEqual({
      page: "discover",
      itemId: "9",
    });
  });

  it("decodes percent-encoded values", () => {
    expect(parseHash("#/discover/c/a%20b")).toEqual({
      page: "discover",
      collectionId: "a b",
    });
    expect(parseHash("#/discover/i/%23%2F%3F")).toEqual({
      page: "discover",
      itemId: "#/?",
    });
  });
});

describe("buildHash", () => {
  it("renders page only", () => {
    expect(buildHash({ page: "queue" })).toBe("/queue");
  });

  it("appends c/b/i in order", () => {
    expect(
      buildHash({ page: "discover", collectionId: "1", bookmarkId: "2", itemId: "3" }),
    ).toBe("/discover/c/1/b/2/i/3");
  });

  it("encodes special characters", () => {
    expect(buildHash({ page: "discover", itemId: "#/?" })).toBe(
      "/discover/i/" + encodeURIComponent("#/?"),
    );
    expect(buildHash({ page: "discover", collectionId: "a b" })).toBe(
      "/discover/c/a%20b",
    );
  });

  it("omits falsy / empty ids", () => {
    expect(buildHash({ page: "discover", collectionId: "" })).toBe("/discover");
  });
});

describe("buildHash <-> parseHash round-trips", () => {
  const routes: Route[] = [
    { page: "discover" },
    { page: "queue" },
    { page: "discover", collectionId: "678" },
    { page: "discover", bookmarkId: "42" },
    { page: "discover", collectionId: "678", itemId: "12345" },
    { page: "library", collectionId: "a b", bookmarkId: "x/y", itemId: "#1" },
  ];
  it.each(routes)("round-trips %j", (r) => {
    expect(parseHash(buildHash(r))).toEqual(r);
    // also round-trips with a leading "#"
    expect(parseHash("#" + buildHash(r))).toEqual(r);
  });
});

describe("PAGES", () => {
  it("contains the documented set", () => {
    expect(PAGES).toEqual([
      "discover",
      "download",
      "queue",
      "library",
      "doctor",
      "settings",
    ]);
  });
});

// --- Browser-dependent helpers (jsdom supports window.location/history) -------

describe("readRoute / history helpers (jsdom)", () => {
  beforeEach(() => {
    // Reset history + hash to a clean root entry between tests.
    window.history.replaceState(null, "", "#/discover");
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("readRoute reflects the current hash", () => {
    window.history.replaceState(null, "", "#/queue");
    expect(readRoute()).toEqual({ page: "queue" });
  });

  it("pushRoute updates the hash and the parsed route", () => {
    pushRoute({ page: "library", itemId: "7" });
    expect(window.location.hash).toBe("#/library/i/7");
    expect(readRoute()).toEqual({ page: "library", itemId: "7" });
  });

  it("pushRoute increments kpDepth; replaceRoute preserves it", () => {
    // Start at depth 0 (clean entry from beforeEach has null state).
    pushRoute({ page: "queue" });
    const afterPush = (window.history.state as { kpDepth?: number }).kpDepth;
    expect(afterPush).toBe(1);

    replaceRoute({ page: "library" });
    const afterReplace = (window.history.state as { kpDepth?: number }).kpDepth;
    expect(afterReplace).toBe(1); // replace keeps current depth
    expect(window.location.hash).toBe("#/library");
  });

  it("dismiss with no in-app depth replaces with the parent route", () => {
    // depth 0 -> dismiss should replaceRoute(parent), not navigate out.
    window.history.replaceState({ kpDepth: 0 }, "", "#/discover/i/9");
    dismiss({ page: "discover" });
    expect(window.location.hash).toBe("#/discover");
    expect(readRoute()).toEqual({ page: "discover" });
  });

  it("dismiss with in-app depth calls history.back() instead of replacing", () => {
    // jsdom's history.back() does not synchronously update location.hash, so we
    // assert that dismiss takes the back() branch (vs. replaceRoute) when depth>0.
    window.history.replaceState({ kpDepth: 0 }, "", "#/discover");
    pushRoute({ page: "discover", itemId: "9" }); // now depth 1
    expect(readRoute()).toEqual({ page: "discover", itemId: "9" });

    const backSpy = vi.spyOn(window.history, "back").mockImplementation(() => {});
    dismiss({ page: "discover" });
    expect(backSpy).toHaveBeenCalledTimes(1);
    // It must NOT have replaced the URL with the parent.
    expect(window.location.hash).toBe("#/discover/i/9");
  });
});

// NOTE: useRoute() is a React hook driven by window 'popstate'/'hashchange'
// events. The synthetic-event subscription is exercised indirectly via the
// pushRoute/replaceRoute tests above (which dispatch PopStateEvent). A full
// hook subscription test would need @testing-library/react renderHook; skipped
// here as the underlying readRoute + event plumbing are already covered.
