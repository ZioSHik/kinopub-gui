import { useState } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { expect, it } from "vitest";
import { I18nProvider } from "../i18n";
import type { SeasonAudioSelection } from "../lib/seasonAudio";
import { SeasonAudioPicker } from "./SeasonAudioPicker";

it("applies the suggestion to every eligible season without replacing other choices", () => {
  const groups = [1, 2, 3, 4, 5].map(number => {
    const episodeKeys = [`S${number}E1`];
    const dub = {index: 1, lang: "rus", type: "Дубляж", author: "", label: "Дубляж", filter: "Дубляж", codec: "aac", channels: 2, surround: false, episodeKeys};
    return {number, episodeKeys, audios: [dub, {...dub, index: 2, lang: "eng", type: "Оригинал", label: "Оригинал", filter: "Оригинал"}]};
  });
  function Picker() {
    const [selected, setSelected] = useState<SeasonAudioSelection>({1: new Set(["Дубляж"]), 2: new Set(["Дубляж"]), 4: new Set(["Оригинал"])});
    return <SeasonAudioPicker groups={groups} selected={selected} episodes={new Set(groups.flatMap(group => group.episodeKeys))}
      onToggle={(season, label) => setSelected(current => {
        const next = new Set(current[season]);
        next.has(label) ? next.delete(label) : next.add(label);
        return {...current, [season]: next};
      })} />;
  }
  render(<I18nProvider><Picker /></I18nProvider>);
  fireEvent.click(screen.getByRole("button", {name: "Use “Дубляж” for 2 more seasons"}));
  for (const number of [1, 2, 3, 5]) {
    expect(within(screen.getByLabelText(`Season ${number} voiceovers`)).getByRole("button", {name: "Дубляж"})).toHaveAttribute("aria-pressed", "true");
  }
  const other = within(screen.getByLabelText("Season 4 voiceovers"));
  expect(other.getByRole("button", {name: "Оригинал"})).toHaveAttribute("aria-pressed", "true");
  expect(other.getByRole("button", {name: "Дубляж"})).toHaveAttribute("aria-pressed", "false");
  expect(screen.queryByRole("button", {name: /more seasons/})).not.toBeInTheDocument();
});
