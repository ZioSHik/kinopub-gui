import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { I18nProvider } from "../i18n";
import { DownloadPage } from "./Download";

const mocks = vi.hoisted(() => ({ start: vi.fn(), preview: vi.fn() }));
vi.mock("../api", async (original) => ({ ...(await original<typeof import("../api")>()), api: { startJob: mocks.start, preview: mocks.preview } }));
vi.mock("../store", () => ({ useApp: () => ({
  settings: { outputPath: "/downloads", quality: "480p", container: "mp4", proxy: "", verbosity: "info" },
  settingsLoaded: true, ffmpeg: { ffmpegFound: true }, kpauth: { loggedIn: true }, toast: vi.fn(),
}) }));

const dub = { index: 1, lang: "rus", type: "Дубляж", author: "", label: "Дубляж", filter: "Дубляж", codec: "aac", channels: 2, surround: false, episodeKeys: ["S1E1"] };
const rentv = { ...dub, author: "РенТВ", label: "Дубляж · РенТВ", filter: "РенТВ", episodeKeys: ["S6E1"] };
const catalog = () => ({ seriesId: "8867", title: "Supernatural", isSerial: true, total: 2, selected: 2, seasons: [
  { number: 1, audios: [dub], episodes: [{ key: "S1E1", season: 1, episode: 1, title: "Pilot", completed: false }] },
  { number: 6, audios: [rentv], episodes: [{ key: "S6E1", season: 6, episode: 1, title: "Family", completed: false }] },
] });
beforeEach(() => {
  localStorage.clear(); mocks.start.mockReset(); mocks.preview.mockReset();
  mocks.start.mockResolvedValue({ id: "job-test" }); mocks.preview.mockResolvedValue(catalog());
});
function open() {
  render(<I18nProvider><DownloadPage onSignIn={vi.fn()} /></I18nProvider>);
  fireEvent.change(screen.getByPlaceholderText("https://kino.watch/item/view/38290"), { target: { value: "https://kino.watch/item/view/8867" } });
}

it("loads season pickers after Preview and submits independent choices", async () => {
  open();
  expect(screen.getByRole("button", { name: "Start download" })).toBeDisabled();
  expect(screen.queryByRole("textbox", { name: "Audio tracks" })).not.toBeInTheDocument();
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Preview" })); });
  const first = screen.getByLabelText("Season 1 voiceovers");
  const sixth = screen.getByLabelText("Season 6 voiceovers");
  expect(within(first).queryByRole("button", { name: rentv.label, hidden: true })).not.toBeInTheDocument();
  fireEvent.click(within(first).getByRole("button", { name: dub.label }));
  expect(screen.getByRole("button", { name: "Start download (2)" })).toBeDisabled();
  fireEvent.click(within(sixth).getByRole("button", { name: rentv.label, hidden: true }));
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Start download (2)" })); });
  expect(mocks.start).toHaveBeenCalledTimes(2);
  expect(mocks.start.mock.calls[0][0]).toMatchObject({ seasons: "1", episodeKeys: ["S1E1"], audio: "", audioMenu: false, audioSpecs: [{ source: { author: "" } }] });
  expect(mocks.start.mock.calls[1][0]).toMatchObject({ seasons: "6", episodeKeys: ["S6E1"], audio: "", audioMenu: false, audioSpecs: [{ source: { author: "РенТВ" } }] });
});

it("invalidates the old preview and choices when the URL changes", async () => {
  open();
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Preview" })); });
  fireEvent.change(screen.getByPlaceholderText("https://kino.watch/item/view/38290"), { target: { value: "https://kino.watch/item/view/123" } });
  expect(screen.queryByLabelText("Season 6 voiceovers")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Start download" })).toBeDisabled();
});

it("ignores an old preview response after the input changes", async () => {
  let resolve!: (value: ReturnType<typeof catalog>) => void;
  mocks.preview.mockReturnValue(new Promise((done) => { resolve = done; }));
  open();
  fireEvent.click(screen.getByRole("button", { name: "Preview" }));
  fireEvent.change(screen.getByPlaceholderText("https://kino.watch/item/view/38290"), { target: { value: "https://kino.watch/item/view/123" } });
  await act(async () => { resolve(catalog()); });
  expect(screen.queryByLabelText("Season 6 voiceovers")).not.toBeInTheDocument();
});

it("keeps only the unsubmitted season selected after a partial queue error", async () => {
  mocks.start.mockResolvedValueOnce({ id: "first" }).mockRejectedValueOnce(new Error("queue unavailable"));
  open();
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Preview" })); });
  fireEvent.click(within(screen.getByLabelText("Season 1 voiceovers")).getByRole("button", { name: dub.label }));
  fireEvent.click(within(screen.getByLabelText("Season 6 voiceovers")).getByRole("button", { name: rentv.label, hidden: true }));
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Start download (2)" })); });
  expect(screen.getByRole("button", { name: "Start download (1)" })).toBeEnabled();
  mocks.start.mockResolvedValue({ id: "sixth" });
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Start download (1)" })); });
  expect(mocks.start.mock.calls[2][0]).toMatchObject({ seasons: "6", episodeKeys: ["S6E1"] });
});

it("refreshes the preview when force re-download changes and sends that choice", async () => {
  open();
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Preview" })); });
  fireEvent.click(screen.getByRole("button", { name: "Advanced options" }));
  fireEvent.click(screen.getByRole("button", { name: /Force re-download/ }));
  expect(screen.queryByLabelText("Season 1 voiceovers")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Start download" })).toBeDisabled();
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Preview" })); });
  expect(mocks.preview).toHaveBeenLastCalledWith(expect.objectContaining({ force: true }));
  fireEvent.click(within(screen.getByLabelText("Season 1 voiceovers")).getByRole("button", { name: dub.label }));
  fireEvent.click(within(screen.getByLabelText("Season 6 voiceovers")).getByRole("button", { name: rentv.label, hidden: true }));
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Start download (2)" })); });
  expect(mocks.start.mock.calls[0][0]).toMatchObject({ force: true, seasons: "1" });
});
