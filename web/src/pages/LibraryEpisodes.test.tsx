import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import type { LibraryEpisode, LibrarySeries } from "../api";
import { I18nProvider } from "../i18n";
import { LibraryPage } from "./Library";

const mocks = vi.hoisted(() => ({ library: vi.fn(), openPath: vi.fn(), remove: vi.fn(), toast: vi.fn() }));
vi.mock("../api", async (original) => ({
  ...(await original<typeof import("../api")>()),
  api: { library: mocks.library, openPath: mocks.openPath, deleteLibraryEpisode: mocks.remove },
}));
vi.mock("../store", () => ({ useApp: () => ({ jobs: [], kpauth: { loggedIn: false }, toast: mocks.toast }) }));

const title = "Не стоит нервничать из-за длинного названия серии";
const file = (over: Partial<LibraryEpisode> = {}): LibraryEpisode => ({
  key: "S1E8", season: 1, episode: 8, title: "Что на ужин?", exists: true,
  path: "/downloads/S01E08-480p.mkv", resolution: "720x300", bytes: 256 * 1024 * 1024,
  audio: ["Дубляж. Невафильм (RUS) [AAC 6ch]"], completedAt: "2026-10-28T12:00:00Z", ...over,
});

function fixture(): LibrarySeries {
  return {
    dir: "/downloads", stateFile: "/downloads/.kinopub-state.json", seriesId: "77890",
    title: "Разделение / Severance", isMovie: false, count: 2, totalBytes: 1024,
    updatedAt: "2026-10-28T12:00:00Z", genres: ["Драма"], episodes: [
      file({ key: "S1E1", episode: 1, title, path: "/downloads/S01E01.mkv", resolution: "1920x800", audio: undefined }),
      file({ versions: [file({ path: "/downloads/S01E08.mkv", resolution: "1920x800", bytes: 768 * 1024 * 1024,
        audio: ["01. Авторский. М. Яроцкий (RUS)", "Дубляж. Невафильм (RUS) [AAC 6ch]", "Оригинал (ENG) [AAC 6ch]"] })] }),
    ],
  };
}

beforeEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
  mocks.library.mockResolvedValue({ dirs: ["/downloads"], series: [fixture()] });
  mocks.openPath.mockReset().mockResolvedValue({ ok: true });
  mocks.remove.mockReset().mockResolvedValue({ ok: true });
});

async function openEpisodes() {
  render(<I18nProvider><LibraryPage onNew={vi.fn()} /></I18nProvider>);
  await act(async () => {});
  fireEvent.click(screen.getAllByRole("button", { name: "Episodes (2)" })[0]);
}

it("keeps full episode titles and groups every file with its own voiceovers, resolution and size", async () => {
  await openEpisodes();
  expect(screen.getByText(title)).toBeInTheDocument();
  expect(screen.getByText("2 episodes · 3 files")).toBeInTheDocument();
  expect(screen.getByText("Season 1")).toBeInTheDocument();
  expect(screen.getByText("Voiceover not recorded")).toBeInTheDocument();
  const episode = screen.getByRole("article", { name: "S1E8 · Что на ужин?" });
  const files = within(episode).getAllByRole("group");
  expect(files.map((group) => group.getAttribute("aria-label"))).toEqual(["1920 × 800 · MKV", "720 × 300 · MKV"]);
  expect(within(files[0]).getByText("Авторский · М. Яроцкий")).toBeInTheDocument();
  expect(within(files[0]).getByText("768 MB")).toBeInTheDocument();
  expect(within(files[1]).queryByText("Авторский · М. Яроцкий")).not.toBeInTheDocument();
  expect(within(files[1]).getByText("256 MB")).toBeInTheDocument();
});

it("plays the exact chosen file rather than the episode's primary version", async () => {
  await openEpisodes();
  const episode = screen.getByRole("article", { name: "S1E8 · Что на ужин?" });
  fireEvent.click(within(episode).getByRole("button", { name: "Play S1E8 · 1920 × 800 · MKV" }));
  expect(mocks.openPath).toHaveBeenLastCalledWith("/downloads/S01E08.mkv", false);
  fireEvent.click(within(episode).getByRole("button", { name: "Play S1E8 · 720 × 300 · MKV" }));
  expect(mocks.openPath).toHaveBeenLastCalledWith("/downloads/S01E08-480p.mkv", false);
});

it("disables a missing version while keeping another version playable", async () => {
  const series = fixture();
  series.episodes[1].versions![0].exists = false;
  mocks.library.mockResolvedValue({ dirs: ["/downloads"], series: [series] });
  await openEpisodes();
  const episode = screen.getByRole("article", { name: "S1E8 · Что на ужин?" });
  expect(within(episode).getByRole("button", { name: "Play S1E8 · 1920 × 800 · MKV" })).toBeDisabled();
  expect(within(episode).getByRole("button", { name: "Play S1E8 · 720 × 300 · MKV" })).toBeEnabled();
  expect(within(episode).getByText("File missing")).toBeInTheDocument();
  expect(screen.getByText("2 episodes · 2 files")).toBeInTheDocument();
});

it("keeps deletion scoped to the logical episode, with an explicit all-versions action", async () => {
  vi.spyOn(window, "confirm").mockReturnValue(true);
  await openEpisodes();
  const episode = screen.getByRole("article", { name: "S1E8 · Что на ужин?" });
  const remove = within(episode).getByRole("button", { name: "Delete S1E8 from disk" });
  expect(remove).toHaveAttribute("title", "Delete all versions of this episode");
  await act(async () => { fireEvent.click(remove); });
  expect(mocks.remove).toHaveBeenCalledExactlyOnceWith("/downloads", "S1E8");
});
