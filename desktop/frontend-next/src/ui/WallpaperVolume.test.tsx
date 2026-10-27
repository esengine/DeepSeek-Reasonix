// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { t } from "../i18n";
import type { Appearance } from "../port/port";
import type { Wallpaper } from "../port/look";
import { WallpaperVolume } from "./WallpaperVolume";

afterEach(cleanup);

function unit(over: Partial<Wallpaper> = {}): Wallpaper {
  return {
    url: "/bg.jpg",
    kind: "scene",
    title: "花海",
    audio: "/scene.mp3",
    parallax: 0.02,
    muted: false,
    volume: 0.6,
    opacity: 0.5,
    dim: 0.55,
    focusX: 0.5,
    focusY: 0.5,
    ...over,
  };
}

function look(paper: Wallpaper): Appearance {
  return { wallpaper: paper };
}

describe("the scene's main-window volume control", () => {
  it("lives outside the app layout", () => {
    const { container } = render(<WallpaperVolume look={look(unit())} onLook={() => {}} />);
    expect(container.querySelector(".wvol")).toBeNull();
    expect(document.querySelector(".wvol")).not.toBeNull();
  });

  it("mutes from the icon without losing the level", () => {
    const onLook = vi.fn((_next: Appearance) => {});
    render(<WallpaperVolume look={look(unit())} onLook={onLook} />);
    fireEvent.click(screen.getByRole("button", { name: t("关闭声音") }));
    expect(onLook.mock.calls[0][0].wallpaper).toMatchObject({ muted: true, volume: 0.6 });
  });

  it("restores a level when unmuting from silence", () => {
    const onLook = vi.fn((_next: Appearance) => {});
    render(<WallpaperVolume look={look(unit({ muted: true, volume: 0 }))} onLook={onLook} />);
    fireEvent.click(screen.getByRole("button", { name: t("打开声音") }));
    expect(onLook.mock.calls[0][0].wallpaper).toMatchObject({ muted: false, volume: 1 });
  });

  it("sets the level from the vertical slider", () => {
    const onLook = vi.fn((_next: Appearance) => {});
    render(<WallpaperVolume look={look(unit())} onLook={onLook} />);
    fireEvent.change(screen.getByRole("slider", { name: t("音量") }), { target: { value: "0.25" } });
    expect(onLook.mock.calls[0][0].wallpaper).toMatchObject({ volume: 0.25, muted: false });
  });
});
