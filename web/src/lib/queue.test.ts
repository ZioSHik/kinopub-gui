import { describe, expect, it } from "vitest";
import type { EpisodeView, JobStatus, JobView } from "../api";
import {
  completedSignal,
  initialEpisodeSelection,
  hasOtherActiveJob,
  isQueued,
  itemIdFromURL,
  nothingLeftToQueue,
  queueCoverage,
} from "./queue";

function ep(season: number, episode: number, state: EpisodeView["state"] = "pending"): EpisodeView {
  return {
    key: `S${season}E${episode}`,
    season,
    episode,
    title: "",
    state,
    percent: 0,
    bytes: 0,
    total: 0,
    speedBps: 0,
    etaSeconds: 0,
    segDone: 0,
    segTotal: 0,
    attempts: 0,
  };
}

function job(over: Partial<JobView> = {}): JobView {
  return {
    id: "j1",
    url: "https://kino.watch/item/view/409",
    status: "running" as JobStatus,
    title: "Breaking Bad",
    outputPath: "/downloads",
    dryRun: false,
    quality: "",
    createdAt: "2026-08-30T10:00:00Z",
    episodes: [],
    logs: [],
    ...over,
  };
}

describe("itemIdFromURL", () => {
  it("pulls the id out of a catalog URL", () => {
    expect(itemIdFromURL("https://kino.watch/item/view/409")).toBe("409");
    expect(itemIdFromURL("https://kino.pub/item/view/77/season/2")).toBe("77");
  });

  it("returns nothing for anything else", () => {
    expect(itemIdFromURL("https://example.com/x")).toBe("");
    expect(itemIdFromURL("")).toBe("");
  });
});

describe("queueCoverage", () => {
  it("covers nothing without an item id", () => {
    expect(queueCoverage([job()], "").refs.size).toBe(0);
  });

  it("lists the episodes an active job will still produce", () => {
    const cov = queueCoverage([job({ episodes: [ep(1, 1), ep(1, 2, "running")] })], "409");
    expect(cov.whole).toBe(false);
    expect(isQueued(cov, 1, 1)).toBe(true);
    expect(isQueued(cov, 1, 2)).toBe(true);
    expect(isQueued(cov, 1, 3)).toBe(false);
  });

  it("leaves a failed episode out — retrying it is the way out of a failure", () => {
    const cov = queueCoverage([job({ episodes: [ep(1, 1, "failed"), ep(1, 2)] })], "409");
    expect(isQueued(cov, 1, 1)).toBe(false);
    expect(isQueued(cov, 1, 2)).toBe(true);
  });

  it("falls back to the explicit selection while a job waits to resolve", () => {
    // A one-episode job queued from the title card carries episodes: [] until
    // it is dispatched; treating that as "the whole title" locked every other
    // episode for as long as the queue was busy, though the server (which keys
    // on SelectedEpisodes) would have accepted them.
    const cov = queueCoverage([job({ status: "queued", episodes: [], selectedEpisodes: ["S1E1"] })], "409");
    expect(cov.whole).toBe(false);
    expect(isQueued(cov, 1, 1)).toBe(true);
    expect(isQueued(cov, 1, 2)).toBe(false);
  });

  it("scopes to the output folder when one is given", () => {
    // The server's duplicate rule keys on URL+folder: a job filling a different
    // folder is a legitimate second download, not a collision.
    const jobs = [job({ episodes: [ep(1, 1)] })];
    expect(isQueued(queueCoverage(jobs, "409", "/downloads"), 1, 1)).toBe(true);
    expect(isQueued(queueCoverage(jobs, "409", "/elsewhere"), 1, 1)).toBe(false);
  });

  it("treats a job that hasn't resolved its plan as owning the whole title", () => {
    const cov = queueCoverage([job({ status: "resolving", episodes: [] })], "409");
    expect(cov.whole).toBe(true);
    expect(isQueued(cov, 7, 7)).toBe(true);
  });

  it("ignores jobs that no longer own their download", () => {
    for (const status of ["completed", "failed", "canceled"] as JobStatus[]) {
      const cov = queueCoverage([job({ status, episodes: [ep(1, 1)] })], "409");
      expect(isQueued(cov, 1, 1)).toBe(false);
    }
  });

  it("counts a paused job — it is still holding its place in the queue", () => {
    const cov = queueCoverage([job({ status: "paused", episodes: [ep(1, 1, "paused")] })], "409");
    expect(isQueued(cov, 1, 1)).toBe(true);
  });

  it("ignores a dry run, which produces no files", () => {
    const cov = queueCoverage([job({ dryRun: true, episodes: [ep(1, 1)] })], "409");
    expect(isQueued(cov, 1, 1)).toBe(false);
  });

  it("ignores jobs for a different title", () => {
    const other = job({ url: "https://kino.watch/item/view/77", episodes: [ep(1, 1)] });
    expect(isQueued(queueCoverage([other], "409"), 1, 1)).toBe(false);
  });

  it("sums several jobs for the same title", () => {
    const cov = queueCoverage(
      [job({ id: "a", episodes: [ep(1, 1)] }), job({ id: "b", episodes: [ep(2, 5)] })],
      "409",
    );
    expect(isQueued(cov, 1, 1)).toBe(true);
    expect(isQueued(cov, 2, 5)).toBe(true);
  });
});

describe("initialEpisodeSelection", () => {
  const all = ["S1E1", "S1E2", "S1E3", "S1E4"];

  it("leaves already-downloaded episodes unticked", () => {
    const got = initialEpisodeSelection(all, new Map([["S1E2", "1080p"]]), new Set());
    expect([...got].sort()).toEqual(["S1E1", "S1E3", "S1E4"]);
  });

  it("leaves already-queued episodes unticked", () => {
    const got = initialEpisodeSelection(all, new Map(), new Set(["S1E4"]));
    expect([...got].sort()).toEqual(["S1E1", "S1E2", "S1E3"]);
  });

  it("ticks everything when nothing is on disk or queued", () => {
    expect(initialEpisodeSelection(all, new Map(), new Set()).size).toBe(4);
  });

  it("ticks nothing when the whole series is already there", () => {
    const downloaded = new Map(all.map((k) => [k, "1080p"]));
    expect(initialEpisodeSelection(all, downloaded, new Set()).size).toBe(0);
  });
});

describe("nothingLeftToQueue", () => {
  const all = ["S1E1", "S1E2"];

  it("is false while something is still missing", () => {
    expect(nothingLeftToQueue(all, new Map([["S1E1", ""]]), new Set())).toBe(false);
  });

  it("is true when downloaded and queued together cover everything", () => {
    expect(nothingLeftToQueue(all, new Map([["S1E1", ""]]), new Set(["S1E2"]))).toBe(true);
  });

  it("is false for a title with no episodes yet", () => {
    expect(nothingLeftToQueue([], new Map(), new Set())).toBe(false);
  });
});

describe("hasOtherActiveJob", () => {
  it("is false for a lone job — nothing to get ahead of", () => {
    const only = job({ id: "a", status: "queued" });
    expect(hasOtherActiveJob([only], "a")).toBe(false);
  });

  it("is true when another job is waiting or downloading", () => {
    for (const status of ["queued", "resolving", "running"] as JobStatus[]) {
      const jobs = [job({ id: "a", status: "queued" }), job({ id: "b", status })];
      expect(hasOtherActiveJob(jobs, "a")).toBe(true);
    }
  });

  it("ignores jobs that are done or held", () => {
    for (const status of ["completed", "failed", "canceled", "paused"] as JobStatus[]) {
      const jobs = [job({ id: "a", status: "queued" }), job({ id: "b", status })];
      expect(hasOtherActiveJob(jobs, "a")).toBe(false);
    }
  });
});

describe("completedSignal", () => {
  // The reported gap: a season downloading one episode at a time. The job is
  // still running, so watching job status alone never fired a rescan and the
  // finished file stayed missing from the list on disk.
  it("grows when an episode finishes inside a still-running job", () => {
    const before = job({ id: "a", status: "running", episodes: [ep(1, 1, "running"), ep(1, 2, "pending")] });
    const after = job({ id: "a", status: "running", episodes: [ep(1, 1, "completed"), ep(1, 2, "running")] });
    expect(completedSignal([after])).toBeGreaterThan(completedSignal([before]));
  });

  it("grows when a whole job finishes", () => {
    const running = job({ id: "a", status: "running", episodes: [ep(1, 1, "completed")] });
    const done = job({ id: "a", status: "completed", episodes: [ep(1, 1, "completed")] });
    expect(completedSignal([done])).toBeGreaterThan(completedSignal([running]));
  });

  it("does not move while nothing has landed", () => {
    const jobs = [job({ id: "a", status: "running", episodes: [ep(1, 1, "running"), ep(1, 2, "paused")] })];
    expect(completedSignal(jobs)).toBe(0);
  });

  it("counts across several jobs", () => {
    const jobs = [
      job({ id: "a", status: "running", episodes: [ep(1, 1, "completed")] }),
      job({ id: "b", status: "completed", episodes: [ep(1, 1, "completed")] }),
    ];
    expect(completedSignal(jobs)).toBe(3); // one episode + (job + its episode)
  });
});

it("only locks the rendition currently selected, independent of audio token order", () => {
  const selection = {quality: "1080p",container: "mkv",audioSpecs: [{require: ["Studio", "ac3"],forbid: ["commentar", "комментар"]}]};
  const queued = job({...selection,audioSpecs: [{require: ["ac3", "Studio"],forbid: ["комментар", "commentar"]}]});
  expect(queueCoverage([queued], "409", "/downloads", selection).whole).toBe(true);
  expect(queueCoverage([queued], "409", "/downloads", {...selection,quality: "720p"}).whole).toBe(false);
  expect(queueCoverage([queued], "409", "/downloads", {...selection,container: "mp4"}).whole).toBe(false);
  expect(queueCoverage([queued], "409", "/downloads", {...selection,audioSpecs: [{require: ["Other studio"],forbid: []}]}).whole).toBe(false);
});
