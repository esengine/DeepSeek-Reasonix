// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import type { Wallpaper } from "../port/look";
import { WallpaperLayers } from "./WallpaperLayers";

afterEach(cleanup);

const scene: Wallpaper = {
  url: "/fallback.png",
  kind: "scene",
  opacity: 0.8,
  dim: 0.2,
  focusX: 0.5,
  focusY: 0.5,
  sceneWidth: 100,
  sceneHeight: 50,
  layers: [
    { url: "/background.png", x: 0, y: 0, w: 1, h: 1, alpha: 1, effects: ["effects/glow/effect.json"] },
    { url: "/foreground.png", x: 0.05, y: 0.3, w: 0.4, h: 0.4, alpha: 0.5 },
  ],
};

describe("a scene's image layers", () => {
  it("renders every layer with its transform and opacity", () => {
    const { container } = render(<WallpaperLayers wallpaper={scene} />);
    expect(container.querySelector(".wlayers")).toBeNull();
    const images = Array.from(document.querySelectorAll<HTMLImageElement>(".wlayers img"));
    expect(images).toHaveLength(2);
    expect(images[0].getAttribute("src")).toBe("/background.png");
    expect(images[1].style.left).toBe("5%");
    expect(images[1].style.top).toBe("30%");
    expect(images[1].style.width).toBe("40%");
    expect(images[1].style.height).toBe("40%");
    expect(images[1].style.opacity).toBe("0.5");
    expect(images[0].dataset.effects).toBe("effects/glow/effect.json");
  });

  it("does not render a layer plane for a picture without layers", () => {
    render(<WallpaperLayers wallpaper={{ ...scene, layers: undefined }} />);
    expect(document.querySelector(".wlayers")).toBeNull();
  });
});
