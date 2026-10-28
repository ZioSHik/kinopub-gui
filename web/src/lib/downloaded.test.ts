import { describe, it, expect } from "vitest";
import type { DiscoverAudio, DownloadedEpisode } from "../api";
import { matchesDownload } from "./downloaded";

const audios = [
  {label:"Studio A",filter:"Studio A",codec:"aac"},
  {label:"Studio B",filter:"Studio B",codec:"aac"},
] as DiscoverAudio[];
const file: DownloadedEpisode = {key:"S1E1",season:1,episode:1,exists:true,path:"/video.mkv",requestedQuality:"720p",resolution:"1280x534",audio:["Studio A"]};
describe("matchesDownload", () => {
 it("recognizes a cropped rendition with the selected audio", () => {
  expect(matchesDownload(file,"720p","mkv",audios,new Set(["Studio A"]))).toBe(true);
 });
 it("does not block adding another voiceover", () => {
  expect(matchesDownload(file,"720p","mkv",audios,new Set(["Studio B"]))).toBe(false);
  expect(matchesDownload(file,"720p","mkv",audios,new Set(["Studio A","Studio B"]))).toBe(false);
 });
 it("does not block a different quality or container", () => {
  expect(matchesDownload(file,"1080p","mkv",audios,new Set(["Studio A"]))).toBe(false);
  expect(matchesDownload(file,"720p","mp4",audios,new Set(["Studio A"]))).toBe(false);
 });
 it("recognizes the actual tier of a legacy file", () => {
  expect(matchesDownload({...file,requestedQuality:undefined},"720p","mkv",audios,new Set(["Studio A"]))).toBe(true);
 });
 it("does not claim a deleted file or an unselected dub", () => {
  expect(matchesDownload({...file,exists:false},"720p","mkv",audios,new Set(["Studio A"]))).toBe(false);
  expect(matchesDownload(file,"720p","mkv",audios,new Set())).toBe(false);
 });
});
