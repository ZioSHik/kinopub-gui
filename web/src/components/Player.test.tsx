import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, afterEach, expect, it, vi } from "vitest";
import { I18nProvider } from "../i18n";
import { Player } from "./Player";

const mocks = vi.hoisted(() => ({stream: vi.fn(),mark: vi.fn(),lease: vi.fn(),instances: [] as any[]}));
vi.mock("../api", async (original) => ({...(await original<typeof import("../api")>()),api: {stream: mocks.stream,markTime: mocks.mark,playbackLease: mocks.lease}}));
vi.mock("hls.js", () => {
  class MockHls {
    static isSupported = () => true;
    static Events = {MANIFEST_PARSED: "manifest",AUDIO_TRACKS_UPDATED: "audio",AUDIO_TRACK_SWITCHED: "switch",LEVEL_SWITCHED: "level",ERROR: "error"};
    static ErrorTypes = {NETWORK_ERROR: "network",MEDIA_ERROR: "media"};
    handlers = new Map<string, (...args: any[]) => void>();
    audioTracks = [];
    levels = [];
    audioTrack = -1;
    currentLevel = -1;
    autoLevelEnabled = true;
    destroy = vi.fn();
    startLoad = vi.fn();
    recoverMediaError = vi.fn();
    attachMedia() {}
    loadSource() {}
    on(event: string, callback: (...args: any[]) => void) { this.handlers.set(event, callback); }
    constructor() { mocks.instances.push(this); }
  }
  return {default: MockHls};
});

beforeEach(() => {
  mocks.instances = [];
  mocks.stream.mockReset().mockResolvedValue({title: "Fixture episode",playUrl: "/fixture.m3u8",resumeTime: 0});
  mocks.mark.mockReset().mockResolvedValue({ok: true});
  mocks.lease.mockReset().mockResolvedValue({ok: true});
  vi.spyOn(HTMLMediaElement.prototype,"play").mockResolvedValue();
  vi.spyOn(HTMLMediaElement.prototype,"pause").mockImplementation(() => {});
  vi.spyOn(HTMLMediaElement.prototype,"load").mockImplementation(() => {});
});
afterEach(async () => { cleanup(); await act(async () => {}); vi.useRealTimers(); vi.restoreAllMocks(); });

it("saves the old episode on navigation and does not assign its playback position to the new one", async () => {
  const props = {id: "42",season: 1,episode: 1,onClose: vi.fn()};
  const rendered = render(<I18nProvider><Player {...props} /></I18nProvider>);
  await act(async () => {});
  act(() => mocks.instances[0].handlers.get("manifest")());
  const video = rendered.container.querySelector("video")!;
  video.currentTime = 75.8;
  rendered.rerender(<I18nProvider><Player {...props} episode={2} /></I18nProvider>);
  await act(async () => {});
  expect(mocks.mark).toHaveBeenCalledWith("42",75,1,1);
  expect(mocks.mark).not.toHaveBeenCalledWith("42",75,1,2);
  fireEvent.timeUpdate(video); // stale media events while the next stream loads
  expect(mocks.mark).not.toHaveBeenCalledWith("42",75,1,2);
  video.currentTime = 4;
  act(() => mocks.instances[1].handlers.get("manifest")());
  fireEvent.timeUpdate(video);
  expect(mocks.mark).toHaveBeenCalledWith("42",4,1,2);
  rendered.unmount();
  expect(mocks.instances.every((hls) => hls.destroy.mock.calls.length === 1)).toBe(true);
});

it("shows an unavailable stream error without leaving an infinite loader", async () => {
  mocks.stream.mockRejectedValue(new Error("upstream unavailable"));
  render(<I18nProvider><Player id="42" onClose={vi.fn()} /></I18nProvider>);
  await act(async () => {});
  expect(screen.getByText("upstream unavailable")).toBeInTheDocument();
});

it("keeps playback working when browser storage is blocked", async () => {
  vi.spyOn(Storage.prototype,"getItem").mockImplementation(() => {throw new Error("blocked");});
  render(<I18nProvider><Player id="42" onClose={vi.fn()} /></I18nProvider>);
  await act(async () => {});
  expect(() => act(() => mocks.instances[0].handlers.get("manifest")())).not.toThrow();
});

it("renews playback priority and releases it on pause, end, and close", async () => {
  vi.useFakeTimers();
  const rendered = render(<I18nProvider><Player id="42" onClose={vi.fn()} /></I18nProvider>);
  await act(async () => {});
  const video = rendered.container.querySelector("video")!;
  await act(async () => {
    mocks.instances[0].handlers.get("manifest")();
    fireEvent.play(video);
  });
  const lease = mocks.lease.mock.calls[0][0];
  expect(mocks.lease).toHaveBeenCalledWith(lease,true);
  await act(async () => { await vi.advanceTimersByTimeAsync(10_000); });
  expect(mocks.lease.mock.calls.filter(([id, active]) => id===lease && active)).toHaveLength(2);
  await act(async () => { fireEvent.pause(video); });
  expect(mocks.lease).toHaveBeenLastCalledWith(lease,false);
  const calls = mocks.lease.mock.calls.length;
  await act(async () => { await vi.advanceTimersByTimeAsync(20_000); });
  expect(mocks.lease).toHaveBeenCalledTimes(calls);
  await act(async () => { fireEvent.play(video); });
  const nextLease = mocks.lease.mock.calls[mocks.lease.mock.calls.length-1][0];
  expect(nextLease).not.toBe(lease);
  await act(async () => { fireEvent.ended(video); });
  expect(mocks.lease).toHaveBeenLastCalledWith(nextLease,false);
  await act(async () => { fireEvent.play(video); });
  const closingLease = mocks.lease.mock.calls[mocks.lease.mock.calls.length-1][0];
  await act(async () => { rendered.unmount(); });
  expect(mocks.lease).toHaveBeenLastCalledWith(closingLease,false);
});

it("waits for a pending heartbeat before releasing a closed player", async () => {
  let finish!: (value: unknown) => void;
  mocks.lease.mockImplementationOnce(() => new Promise(resolve => {finish=resolve;}));
  const rendered = render(<I18nProvider><Player id="42" onClose={vi.fn()} /></I18nProvider>);
  await act(async () => {});
  const lease=mocks.lease.mock.calls[0][0];
  rendered.unmount();
  await act(async () => {});
  expect(mocks.lease).toHaveBeenCalledTimes(1);
  await act(async () => { finish({ok:true}); });
  expect(mocks.lease).toHaveBeenLastCalledWith(lease,false);
});

it("releases playback priority when stream loading fails", async () => {
  mocks.stream.mockRejectedValue(new Error("unavailable"));
  render(<I18nProvider><Player id="42" onClose={vi.fn()} /></I18nProvider>);
  await act(async () => {});
  const lease=mocks.lease.mock.calls[0][0];
  expect(mocks.lease).toHaveBeenLastCalledWith(lease,false);
});
