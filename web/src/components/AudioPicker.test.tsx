import { fireEvent, render, screen, within } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { I18nProvider } from "../i18n";
import type { DiscoverAudio } from "../api";
import { AudioPicker } from "./AudioPicker";

it("groups voiceovers by type while preserving exact codec variants and selection labels", () => {
  const audios = [
    {type: "Авторский",author: "М. Яроцкий",label: "Авторский · М. Яроцкий · AAC 5.1",codec: "aac"},
    {type: "Дубляж",author: "Невафильм",label: "Дубляж · Невафильм · AAC 5.1",codec: "aac"},
    {type: "Авторский",author: "М. Яроцкий",label: "Авторский · М. Яроцкий · AC3 5.1",codec: "ac3"},
    {type: "Оригинал",author: "",lang: "eng",label: "Оригинал · AAC 5.1",codec: "aac"},
  ] as DiscoverAudio[];
  const toggle = vi.fn();
  render(<I18nProvider><AudioPicker audios={audios} selected={new Set([audios[1].label])} onToggle={toggle} /></I18nProvider>);
  const groups = screen.getAllByRole("group");
  expect(groups.map((group) => group.querySelector("h4")?.textContent)).toEqual([
    "Dubbing1", "Author's translation2", "Original soundtrack1",
  ]);
  const author = screen.getByRole("group", {name: "Author's translation 2"});
  expect(within(author).getByText("М. Яроцкий · AAC 5.1")).toBeInTheDocument();
  expect(within(author).getByText("М. Яроцкий · AC3 5.1")).toBeInTheDocument();
  expect(screen.getByText("ENG · AAC 5.1")).toBeInTheDocument();
  expect(screen.getByRole("button",{name: audios[1].label})).toHaveAttribute("aria-pressed","true");
  fireEvent.click(within(author).getByRole("button",{name: audios[2].label}));
  expect(toggle).toHaveBeenCalledWith(audios[2].label);
});


it("puts original commentary in its own category", () => {
  const audios = [
    {type: "Оригинал",lang: "eng",author: "",label: "Оригинал · AAC 5.1",filter: "Оригинал"},
    {type: "Оригинал",lang: "eng",author: "Комментарии",label: "Оригинал · Комментарии",filter: "Комментарии"},
  ] as DiscoverAudio[];
  render(<I18nProvider><AudioPicker audios={audios} selected={new Set()} onToggle={vi.fn()} /></I18nProvider>);
  const original = screen.getByRole("group", {name: "Original soundtrack 1"});
  const commentary = screen.getByRole("group", {name: "Commentary 1"});
  expect(within(original).getAllByRole("button")).toHaveLength(1);
  expect(within(commentary).getByRole("button",{name: "Оригинал · Комментарии"})).toBeInTheDocument();
});
