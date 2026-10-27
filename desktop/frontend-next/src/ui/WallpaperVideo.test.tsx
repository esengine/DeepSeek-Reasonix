// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import { setEffectsMode } from "../state/prefs";
import { WallpaperVideo } from "./WallpaperVideo";

const video = { url: "/appearance/wallpaper/abc.mp4", kind: "video" as const, opacity: 0.5, dim: 0.5, focusX: 0.5, focusY: 0.5 };
let reduced = false;
const play = vi.fn(() => Promise.resolve());
const pause = vi.fn();

beforeEach(() => {
  reduced = false;
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: query.includes("reduce") ? reduced : false,
    addEventListener() {},
    removeEventListener() {},
  }));
  HTMLMediaElement.prototype.play = play;
  HTMLMediaElement.prototype.pause = pause;
  play.mockClear();
  pause.mockClear();
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
  setEffectsMode("full");
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("a video wallpaper", () => {
  it("is drawn as a muted, looping, inline element behind the interface", () => {
    render(<WallpaperVideo wallpaper={video} />);
    const el = document.body.querySelector("video")!;
    expect(el.getAttribute("src")).toBe(video.url);
    expect(el.muted).toBe(true);
    expect(el.loop).toBe(true);
    expect(el.hasAttribute("playsinline")).toBe(true);
    expect(el.getAttribute("aria-hidden")).toBe("true");
    expect(el.tabIndex).toBe(-1);
  });

  it("is not drawn for a picture or for no wallpaper", () => {
    render(<WallpaperVideo wallpaper={undefined} />);
    expect(document.body.querySelector("video")).toBeNull();
    cleanup();
    render(<WallpaperVideo wallpaper={{ ...video, kind: "image" }} />);
    expect(document.body.querySelector("video")).toBeNull();
  });

  it("plays while the window is watched", () => {
    render(<WallpaperVideo wallpaper={video} />);
    expect(play).toHaveBeenCalled();
    expect(pause).not.toHaveBeenCalled();
  });

  it("stops while the window is hidden and goes on when it is back", () => {
    render(<WallpaperVideo wallpaper={video} />);
    play.mockClear();
    Object.defineProperty(document, "hidden", { configurable: true, get: () => true });
    act(() => void document.dispatchEvent(new Event("visibilitychange")));
    expect(pause).toHaveBeenCalled();
    Object.defineProperty(document, "hidden", { configurable: true, get: () => false });
    act(() => void document.dispatchEvent(new Event("visibilitychange")));
    expect(play).toHaveBeenCalled();
  });

  it("never plays under reduced motion, and holds its first frame", () => {
    reduced = true;
    render(<WallpaperVideo wallpaper={video} />);
    expect(play).not.toHaveBeenCalled();
    expect(document.body.querySelector("video")!.getAttribute("preload")).toBe("auto");
  });

  it("never plays when the person chose reduced effects", () => {
    setEffectsMode("reduced");
    render(<WallpaperVideo wallpaper={video} />);
    expect(play).not.toHaveBeenCalled();
  });
});
