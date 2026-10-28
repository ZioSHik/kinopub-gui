import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { I18nProvider } from "../i18n";
import { TitleDetail } from "./TitleDetail";

const mocks = vi.hoisted(() => ({detail: vi.fn(), downloaded: vi.fn()}));
vi.mock("../api", async (original) => ({
  ...(await original<typeof import("../api")>()),
  api: { discoverItem: mocks.detail, libraryDownloaded: mocks.downloaded, discoverSimilar: async () => ({items: []}) },
}));
vi.mock("../store", () => ({useApp: () => ({
  settings: {quality: "1080p", container: "mkv", outputPath: "/downloads"},
  ffmpeg: {ffmpegFound: true}, jobs: [], toast: vi.fn(),
})}));

beforeEach(() => {
  localStorage.clear();
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
