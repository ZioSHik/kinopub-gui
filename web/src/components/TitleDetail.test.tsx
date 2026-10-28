import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import type { JobView } from "../api";
import { audioSelectionSpecs } from "../lib/audio";
import { I18nProvider } from "../i18n";
import { TitleDetail } from "./TitleDetail";

const mocks = vi.hoisted(() => ({detail: vi.fn(), downloaded: vi.fn(), start: vi.fn(), jobs: [] as JobView[]}));
vi.mock("../api", async (original) => ({
  ...(await original<typeof import("../api")>()),
  api: { startJob: mocks.start, discoverItem: mocks.detail, libraryDownloaded: mocks.downloaded, discoverSimilar: async () => ({items: []}) },
}));
vi.mock("../store", () => ({useApp: () => ({
  settings: {quality: "1080p", container: "mkv", outputPath: "/downloads"},
  ffmpeg: {ffmpegFound: true}, jobs: mocks.jobs, toast: vi.fn(),
})}));

beforeEach(() => {
  localStorage.clear();
  mocks.jobs = [];
  mocks.start.mockClear();
  mocks.detail.mockResolvedValue({id: "77890",title: "Severance",year: 2022,poster: "",isSerial: true,
    rating: 0,imdbRating: 0,kinopoiskRating: 0,episodeCount: 8,itemUrl: "https://kino.watch/item/view/77890",
    audios: [{label: "Studio A",filter: "Studio A",codec: "aac"}, {label: "Studio B",filter: "Studio B",codec: "aac"}],
    seasons: [{number: 1,episodes: Array.from({length: 8}, (_, i) => ({season: 1,episode: i+1,title: `Episode ${i+1}`,watched: false}))}],
  });
  mocks.downloaded.mockResolvedValue({episodes: Array.from({length: 8},(_, i) => ({key: `S1E${i+1}`,season: 1,episode: i+1,
    exists: true,path: `/downloads/${i}.mkv`,resolution: "1920x800",audio: i===7 ? ["Studio B"] : []}))});
});

it("counts all eight files when only one has recorded audio, regardless of the picker", async () => {
  render(<I18nProvider><TitleDetail id="77890" onClose={vi.fn()} onPick={vi.fn()} /></I18nProvider>);
  await act(async () => {});
  expect(screen.getByText("8 of 8 downloaded")).toBeInTheDocument();
  expect(screen.getByText("1 of 8 downloaded episodes match the selected quality and voiceover")).toBeInTheDocument();
  expect(screen.getAllByText(/Voiceover not recorded/)).toHaveLength(7);
  fireEvent.click(screen.getByRole("button",{name: "Studio A"}));
  expect(screen.getByText("8 of 8 downloaded")).toBeInTheDocument();
  expect(screen.getByText("0 of 8 downloaded episodes match the selected quality and voiceover")).toBeInTheDocument();
});

it("starts with the most common existing voiceover rather than requesting a union of different dubs", async () => {
  mocks.downloaded.mockResolvedValue({episodes: Array.from({length: 8},(_, i) => ({key: `S1E${i+1}`,season: 1,episode: i+1,
    exists: true,path: `/downloads/${i}.mkv`,resolution: "1920x800",audio: [i<7 ? "Studio A" : "Studio B"]}))});
  render(<I18nProvider><TitleDetail id="77890" onClose={vi.fn()} onPick={vi.fn()} /></I18nProvider>);
  await act(async () => {});
  expect(screen.getByText("8 of 8 downloaded")).toBeInTheDocument();
  expect(screen.getByRole("button",{name: "Studio A"})).toHaveAttribute("aria-pressed","true");
  expect(screen.getByRole("button",{name: "Studio B"})).toHaveAttribute("aria-pressed","false");
  expect(screen.getByText("7 of 8 downloaded episodes match the selected quality and voiceover")).toBeInTheDocument();
});

it("clearing all voiceovers keeps existing video unchecked and explains why downloading is disabled", async () => {
  const detail = await mocks.detail();
  detail.seasons[0].episodes.push({season: 1,episode: 9,title: "Episode 9",watched: false});
  detail.episodeCount = 9;
  mocks.detail.mockResolvedValue(detail);
  render(<I18nProvider><TitleDetail id="77890" onClose={vi.fn()} onPick={vi.fn()} /></I18nProvider>);
  await act(async () => {});
  // Studio B was seeded from the one recording with audio metadata.
  fireEvent.click(screen.getByRole("button",{name: "Studio B"}));
  expect(screen.getByRole("button",{name: "Choose a voiceover"})).toBeDisabled();
  expect(screen.getByText("8 of 9 downloaded")).toBeInTheDocument();
  for (let number = 1; number <= 8; number++) {
    expect(screen.getByRole("button",{name: new RegExp(`^${number} Episode ${number} `)}))
      .toHaveAttribute("aria-pressed","false");
  }
  expect(screen.getByRole("button",{name: /^9 Episode 9/})).toHaveAttribute("aria-pressed","true");
  expect(screen.queryByText(/downloaded episodes match the selected quality and voiceover/)).not.toBeInTheDocument();
});


it.each([false, true])("sends an original-only selection even when commentary is not listed (%s)", async (listCommentary) => {
  const detail = await mocks.detail();
  detail.audios = [
    {type: "Оригинал",lang: "eng",author: "",label: "Оригинал · AAC 5.1",filter: "Оригинал",codec: "aac"},
    {type: "Оригинал",lang: "eng",author: "Комментарии",label: "Оригинал · Комментарии",filter: "Комментарии",codec: "aac"},
  ];
  if (!listCommentary) detail.audios = detail.audios.slice(0,1);
  mocks.detail.mockResolvedValue(detail);
  mocks.downloaded.mockResolvedValue({episodes: []});
  mocks.start.mockResolvedValue({id: "test-job"});
  render(<I18nProvider><TitleDetail id="409" onClose={vi.fn()} onPick={vi.fn()} /></I18nProvider>);
  await act(async () => {});
  fireEvent.click(screen.getByRole("button",{name: "Оригинал · AAC 5.1"}));
  await act(async () => {fireEvent.click(screen.getByRole("button",{name: "Download (8)"}));});
  const request = mocks.start.mock.calls[0][0];
  expect(request.audioSpecs).toHaveLength(1);
  expect(request.audioSpecs[0].require).toEqual(["Оригинал"]);
  expect(request.audioSpecs[0].forbid).toContain("комментар");
  expect(request.audioSpecs[0].forbid).toContain("commentar");
});


it.each([false, true])("allows adding another voiceover or quality while the same title is queued (serial=%s)", async (serial) => {
  const detail = await mocks.detail();
  if (!serial) { detail.seasons = []; detail.isSerial = false; }
  mocks.detail.mockResolvedValue(detail);
  mocks.downloaded.mockResolvedValue({episodes: []});
  localStorage.setItem("kp.download.audioPrefByItem", JSON.stringify([["77890", ["studio a|aac|"]]]));
  mocks.jobs = [{id: "existing",url: detail.itemUrl,status: "running",title: detail.title,outputPath: "/downloads",
    quality: "1080p",container: "mkv",dryRun: false,createdAt: "",episodes: [],logs: [],
    audioSpecs: audioSelectionSpecs([detail.audios[0]])}];
  render(<I18nProvider><TitleDetail id="77890" onClose={vi.fn()} onPick={vi.fn()} /></I18nProvider>);
  await act(async () => {});
  expect(screen.getByRole("button", {name: "In the queue — open"})).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", {name: "Studio A"}));
  fireEvent.click(screen.getByRole("button", {name: "Studio B"}));
  const button = () => screen.getByRole("button", {name: serial ? "Download (8)" : "Download"});
  expect(button()).toBeEnabled();
  await act(async () => {fireEvent.click(button());});
  expect(mocks.start.mock.calls[0][0].audioSpecs[0].require).toContain("Studio B");
  fireEvent.click(screen.getByRole("button", {name: "Studio B"}));
  fireEvent.click(screen.getByRole("button", {name: "Studio A"}));
  expect(screen.getByRole("button", {name: "In the queue — open"})).toBeInTheDocument();
  fireEvent.change(screen.getByTitle("Quality"), {target: {value: "720p"}});
  expect(button()).toBeEnabled();
  await act(async () => {fireEvent.click(button());});
  expect(mocks.start.mock.calls[1][0].quality).toBe("720p");
});

it("refreshes the inventory while the open title finishes downloading", async () => {
  mocks.downloaded.mockResolvedValue({episodes: []});
  const props = {id: "77890",onClose: vi.fn(),onPick: vi.fn()};
  const rendered = render(<I18nProvider><TitleDetail {...props} /></I18nProvider>);
  await act(async () => {});
  expect(screen.queryByText("8 of 8 downloaded")).not.toBeInTheDocument();
  mocks.downloaded.mockResolvedValue({episodes: Array.from({length: 8}, (_, i) => ({key: `S1E${i+1}`,season: 1,episode: i+1,
    exists: true,path: `/downloads/${i}.mkv`,resolution: "1920x800",audio: ["Studio A"]}))});
  mocks.jobs = [{id: "fresh",url: "https://kino.watch/item/view/77890",status: "completed",episodes: [],quality: "1080p",logs: [],createdAt: "",title: "Test",outputPath: "/downloads",dryRun: false}];
  rendered.rerender(<I18nProvider><TitleDetail {...props} /></I18nProvider>);
  await act(async () => {});
  expect(screen.getByText("8 of 8 downloaded")).toBeInTheDocument();
});
