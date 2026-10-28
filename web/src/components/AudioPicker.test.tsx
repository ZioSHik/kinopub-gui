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
