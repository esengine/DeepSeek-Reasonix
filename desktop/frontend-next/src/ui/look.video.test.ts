// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { apply } from "./look";

afterEach(() => {
  apply(null);
  document.documentElement.removeAttribute("style");
  document.documentElement.removeAttribute("data-bg");
});

const paper = { url: "/appearance/wallpaper/a.mp4", opacity: 0.6, dim: 0.4, focusX: 0.25, focusY: 0.75 };

describe("applying a wallpaper", () => {
  it("sets a picture as the background image", () => {
    apply({ wallpaper: { ...paper, kind: "image", url: "/appearance/wallpaper/a.png" } });
    const style = document.documentElement.style;
    expect(style.getPropertyValue("--bg-image")).toBe('url("/appearance/wallpaper/a.png")');
    expect(document.documentElement.dataset.bg).toBe("on");
  });

  it("leaves the image variable alone for a video, which is an element, but keeps the shared ones", () => {
    apply({ wallpaper: { ...paper, kind: "video" } });
    const style = document.documentElement.style;
    expect(style.getPropertyValue("--bg-image")).toBe("");
    expect(style.getPropertyValue("--bg-x")).toBe("25%");
    expect(style.getPropertyValue("--bg-y")).toBe("75%");
    expect(style.getPropertyValue("--bg-alpha")).toBe("0.6");
    expect(style.getPropertyValue("--bg-overlay")).toBe("0.4");
    expect(document.documentElement.dataset.bg).toBe("on");
  });
});
