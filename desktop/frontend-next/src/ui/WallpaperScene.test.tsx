// @vitest-environment jsdom
import { cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Wallpaper } from "../port/look";
import { WallpaperScene } from "./WallpaperScene";

const scene: Wallpaper = {
  url: "/bg.jpg",
  kind: "scene",
  title: "花海",
  audio: "/scene.mp3",
  parallax: 0.04,
  muted: false,
  volume: 0.5,
  opacity: 0.5,
  dim: 0.55,
  focusX: 0.5,
  focusY: 0.5,
};

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  document.documentElement.style.removeProperty("--bg-x");
  document.documentElement.style.removeProperty("--bg-y");
});

describe("a Wallpaper Engine backdrop", () => {
  it("renders a looping soundtrack and starts it", async () => {
    const play = vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
    render(<WallpaperScene wallpaper={scene} />);
    const audio = document.querySelector("audio");
    expect(audio?.getAttribute("src")).toBe("/scene.mp3");
    expect(audio?.loop).toBe(true);
    await waitFor(() => expect(audio?.volume).toBeCloseTo(0.5));
    await waitFor(() => expect(play).toHaveBeenCalled());
  });

  it("does not start at zero volume", () => {
    const play = vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
    render(<WallpaperScene wallpaper={{ ...scene, volume: 0 }} />);
    expect(play).not.toHaveBeenCalled();
  });

  it("moves the backdrop with the pointer", () => {
    vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
    render(<WallpaperScene wallpaper={scene} />);
    Object.defineProperty(window, "innerWidth", { configurable: true, value: 1000 });
    Object.defineProperty(window, "innerHeight", { configurable: true, value: 500 });
    window.dispatchEvent(new MouseEvent("pointermove", { clientX: 100, clientY: 100 }));
    expect(document.documentElement.style.getPropertyValue("--bg-x")).not.toBe("50%");
    expect(document.documentElement.style.getPropertyValue("--bg-y")).not.toBe("50%");
  });

  it("applies each track's volume and loop mode", async () => {
    const play = vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
    render(
      <WallpaperScene
        wallpaper={{
          ...scene,
          audio: undefined,
          audios: undefined,
          audioTracks: [
            { url: "/a.mp3", volume: 0.4, loop: true },
            { url: "/b.mp3", volume: 0.8, loop: false },
          ],
        }}
      />,
    );
    const audio = Array.from(document.querySelectorAll<HTMLAudioElement>("audio"));
    expect(audio).toHaveLength(2);
    expect(audio[0].loop).toBe(true);
    expect(audio[1].loop).toBe(false);
    await waitFor(() => expect(audio[0].volume).toBeCloseTo(0.2));
    expect(audio[1].volume).toBeCloseTo(0.4);
    expect(play).toHaveBeenCalledTimes(2);
  });

  it("does not install the soundtrack when muted", () => {
    const play = vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
    render(<WallpaperScene wallpaper={{ ...scene, muted: true }} />);
    const audio = document.querySelector("audio");
    expect(audio?.muted).toBe(true);
    expect(play).not.toHaveBeenCalled();
  });
});
