import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { api } from "../api";
import { I18nProvider } from "../i18n";
import { DirPicker } from "./DirPicker";

vi.mock("../api", async (original) => ({
  ...(await original<typeof import("../api")>()),
  api: { fs: vi.fn() },
}));

beforeEach(() => {
  localStorage.clear();
  vi.mocked(api.fs).mockReset();
});

function picker() {
  const select = vi.fn();
  const close = vi.fn();
  render(<I18nProvider><DirPicker open initial="/downloads" onSelect={select} onClose={close} /></I18nProvider>);
  return { select, close };
}

it("keeps an empty folder with the legacy null response usable and selectable", async () => {
  vi.mocked(api.fs).mockResolvedValue({ path: "/downloads", parent: "/", dirs: null });
  const { select, close } = picker();
  expect(await screen.findByText("No sub-folders here.")).toBeInTheDocument();
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Use this folder" })); });
  expect(select).toHaveBeenCalledWith("/downloads");
  expect(close).toHaveBeenCalledOnce();
});

it("validates a manually entered path and selects the canonical server path", async () => {
  vi.mocked(api.fs).mockResolvedValueOnce({ path: "/downloads", parent: "/", dirs: [] })
    .mockResolvedValueOnce({ path: "/Volumes/Media/Сериалы", parent: "/Volumes/Media", dirs: [] });
  const { select } = picker();
  await screen.findByText("No sub-folders here.");
  fireEvent.change(screen.getByRole("textbox", { name: "Folder path" }), { target: { value: " /Volumes/Media/Сериалы " } });
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Use this folder" })); });
  expect(api.fs).toHaveBeenLastCalledWith("/Volumes/Media/Сериалы");
  expect(select).toHaveBeenCalledWith("/Volumes/Media/Сериалы");
});

it("shows an invalid path error without selecting the previous directory or closing", async () => {
  vi.mocked(api.fs).mockResolvedValueOnce({ path: "/downloads", parent: "/", dirs: [] })
    .mockRejectedValueOnce(new Error("Folder does not exist"));
  const { select, close } = picker();
  await screen.findByText("No sub-folders here.");
  fireEvent.change(screen.getByRole("textbox", { name: "Folder path" }), { target: { value: "/missing" } });
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Use this folder" })); });
  expect(screen.getByText("Folder does not exist")).toBeInTheDocument();
  expect(select).not.toHaveBeenCalled();
  expect(close).not.toHaveBeenCalled();
});

it("opens a typed path with Enter, then browses into an empty subdirectory", async () => {
  vi.mocked(api.fs).mockResolvedValueOnce({ path: "/downloads", parent: "/", dirs: [] })
    .mockResolvedValueOnce({ path: "/media", parent: "/", dirs: [{ name: "TV", path: "/media/TV" }] })
    .mockResolvedValueOnce({ path: "/media/TV", parent: "/media", dirs: null });
  picker();
  await screen.findByText("No sub-folders here.");
  const input = screen.getByRole("textbox", { name: "Folder path" });
  fireEvent.change(input, { target: { value: "/media" } });
  await act(async () => { fireEvent.submit(input.closest("form")!); });
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "TV" })); });
  expect(input).toHaveValue("/media/TV");
  expect(screen.getByText("No sub-folders here.")).toBeInTheDocument();
});
