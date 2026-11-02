// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { t } from "../i18n";
import { MockPort } from "../port/mock";
import type { Appearance as Look } from "../port/port";
import { Appearance } from "./Appearance";

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

const selected: Look = {
  wallpaper: { kind: "image", url: "/selected.jpg", opacity: 0.5, dim: 0.55, focusX: 0.25, focusY: 0.75 },
};

function draw(initial: Look = selected) {
  const port = new MockPort();
  const upload = vi.spyOn(port, "uploadWallpaper").mockResolvedValue(selected);
  const clear = vi.spyOn(port, "clearWallpaper").mockResolvedValue();
  const save = vi.fn();
  function Settings() {
    const [look, setLook] = useState(initial);
    return <Appearance port={port} theme="light" contrast="" weight="" onTheme={() => {}} onContrast={() => {}}
      onWeight={() => {}} reloadThemes={() => {}} look={look} onLook={(next) => { save(next); setLook(next); }} />;
  }
  return { ...render(<Settings />), upload, clear, save };
}

it("exposes the wallpaper section and its single selected preview without opening a disclosure", async () => {
  const { container } = draw();
  const section = container.querySelector('#set-wallpaper[data-setting="wallpaper"]')!;
  expect(section.closest("details")).toBeNull();
  const preview = screen.getByRole("img", { name: t("壁纸") });
  expect(section.contains(preview)).toBe(true);
  expect(container.querySelectorAll(".paperview")).toHaveLength(1);
  expect(preview.style.backgroundImage).toContain("/selected.jpg");
  expect(preview.style.backgroundPosition).toBe("25% 75%");
  expect((preview.querySelector("i") as HTMLElement).style.opacity).toBe("0.5");
  expect((preview.querySelector("b") as HTMLElement).style.opacity).toBe("0.55");
  await waitFor(() => expect(container.querySelector(".palettes")).not.toBeNull());
});

it("keeps upload, replacement, adjustment and removal on the same appearance state", async () => {
  const { container, upload, clear, save } = draw({});
  const input = container.querySelector('input[data-action="wallpaper.change"]') as HTMLInputElement;
  const first = new File(["image"], "wallpaper.png", { type: "image/png" });
  expect(container.querySelector(".paperview")).toBeNull();
  fireEvent.change(input, { target: { files: [first] } });
  await waitFor(() => expect(upload).toHaveBeenCalledWith(first));
  await screen.findByRole("img", { name: t("壁纸") });
  expect(input.value).toBe("");
  const second = new File(["replacement"], "replacement.jpg", { type: "image/jpeg" });
  fireEvent.change(input, { target: { files: [second] } });
  await waitFor(() => expect(upload).toHaveBeenCalledWith(second));
  for (const [name, field, value] of [["浓度", "opacity", "0.8"], ["压暗", "dim", "0.2"], ["横向焦点", "focusX", "0.6"], ["纵向焦点", "focusY", "0.4"]]) {
    fireEvent.change(screen.getByRole("slider", { name: new RegExp(t(name)) }), { target: { value } });
    expect(save.mock.lastCall?.[0].wallpaper[field]).toBe(Number(value));
  }
  expect(screen.getByRole("img", { name: t("壁纸") }).style.backgroundPosition).toBe("60% 40%");
  fireEvent.click(container.querySelector('[data-action="wallpaper.remove"]')!);
  await waitFor(() => expect(clear).toHaveBeenCalledOnce());
  await waitFor(() => expect(container.querySelector(".paperview")).toBeNull());
  expect(save.mock.lastCall?.[0].wallpaper).toBeUndefined();
  expect(screen.getByRole("button", { name: t("选择图片或视频…") })).toBeTruthy();
});
