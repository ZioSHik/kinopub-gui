import { act, render, screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import type { JobView } from "../api";
import { I18nProvider } from "../i18n";
import { LibraryPage } from "./Library";

const mocks = vi.hoisted(() => ({ jobs: [] as JobView[], library: vi.fn() }));
vi.mock("../api", async (original) => ({
  ...(await original<typeof import("../api")>()),
  api: { library: mocks.library },
}));
vi.mock("../store", () => ({ useApp: () => ({ jobs: mocks.jobs, toast: vi.fn() }) }));
vi.mock("../components/JobCard", () => ({ JobCard: ({ job }: { job: JobView }) => <div>{job.title}</div> }));

beforeEach(() => {
  localStorage.clear();
  mocks.library.mockResolvedValue({ dirs: [], series: [] });
  mocks.jobs = [
    { id: "failed", title: "Failed attempt", status: "failed", episodes: [] },
    { id: "canceled", title: "Canceled attempt", status: "canceled", episodes: [] },
  ] as unknown as JobView[];
});

it("keeps failed attempts visible without counting them as downloads in progress", async () => {
  render(<I18nProvider><LibraryPage onNew={vi.fn()} /></I18nProvider>);
  await act(async () => {});
  expect(screen.getByText("Failed attempt")).toBeInTheDocument();
  expect(screen.getByText("Canceled attempt")).toBeInTheDocument();
  expect(screen.getByText("Finished downloads")).toBeInTheDocument();
  expect(screen.getByText("Downloads found in your output folders")).toBeInTheDocument();
  expect(screen.queryByText(/in the queue/)).not.toBeInTheDocument();
});

it("counts only pending work when finished attempts remain", async () => {
  mocks.jobs.push({ id: "queued", title: "Pending download", status: "queued", episodes: [] } as unknown as JobView);
  render(<I18nProvider><LibraryPage onNew={vi.fn()} /></I18nProvider>);
  await act(async () => {});
  expect(screen.getByText("1 in the queue · 0 on disk")).toBeInTheDocument();
});
