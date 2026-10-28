import { render, screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import type { JobView } from "../api";
import { I18nProvider } from "../i18n";
import { JobCard } from "./JobCard";

vi.mock("../store", () => ({ useApp: () => ({ jobs: [], toast: vi.fn() }) }));
const job = (over: Partial<JobView> = {}): JobView => ({
  id: "failed", title: "Fixture", url: "https://kino.watch/item/view/42", outputPath: "/downloads",
  status: "failed", quality: "720p", container: "mkv", dryRun: false,
  createdAt: "2026-10-28T12:00:00Z", episodes: [], logs: [], ...over,
});
beforeEach(() => localStorage.clear());

it("does not present a source failure before the plan as 100% completed or a zero success summary", () => {
  render(<I18nProvider><JobCard job={job({
    error: "page scrape: TLS handshake timeout", summary: { total: 0, succeeded: 0, failed: 0, skipped: 0 },
  })} /></I18nProvider>);
  expect(screen.getByText("0%")).toBeInTheDocument();
  expect(screen.queryByText("100%")).not.toBeInTheDocument();
  expect(screen.queryByText("0 ok · 0 failed · 0 skipped")).not.toBeInTheDocument();
  expect(screen.getByText(/Request timed out/)).toBeInTheDocument();
});

it.each(["No space left on device", "There is not enough space on the disk", "write failed: ENOSPC"])(
  "explains disk exhaustion for a finished download: %s", (reason) => {
    render(<I18nProvider><JobCard job={job({
      error: "1 of 1 episodes failed", episodes: [{key: "S1E1", season: 1, episode: 1, title: "", state: "failed", percent: 100, bytes: 1, error: `ffmpeg exited: ${reason}`} as JobView["episodes"][number]],
    })} /></I18nProvider>);
    expect(screen.getByText("Not enough disk space. Free space in the download folder, then retry.")).toBeInTheDocument();
  },
);
