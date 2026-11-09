// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Appearance } from "./Appearance";
import { apply } from "./look";
import { MockPort } from "../port/mock";
import { boot, STORAGE } from "../i18n";
import type { AgentPort, Appearance as Look } from "../port/port";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  apply(null);
});

function draw(look: Look = {}) {
  const onLook = vi.fn();
  render(
    <>
      <Appearance port={new MockPort() as unknown as AgentPort} theme="light" onTheme={() => {}} contrast="" onContrast={() => {}}
        weight="" onWeight={() => {}} look={look} onLook={onLook} reloadThemes={() => {}} />
    </>,
  );
  return { onLook };
}

const group = () => screen.getByRole("group", { name: "界面大小" });
const checked = () => [...group().querySelectorAll("button")].filter((b) => b.getAttribute("aria-pressed") === "true").map((b) => b.textContent);

describe("the size row on the appearance page", () => {
  it("offers all size presets inline, standard until one is chosen", () => {
    draw();
    expect([...group().querySelectorAll("button")].map((b) => b.textContent)).toEqual(["紧凑", "标准", "宽松", "更大"]);
    expect(checked()).toEqual(["标准"]);
  });

  it("shows the rung the saved scale sits on, and none for one between rungs", () => {
    draw({ zoom: 1.15 });
    expect(checked()).toEqual(["宽松"]);
    cleanup();
    draw({ zoom: 1.22 });
    expect(checked()).toEqual([]);
  });

  it("saves the scale and touches nothing else", async () => {
    const { onLook } = draw({ textSize: 1.15 });
    await userEvent.click(screen.getByRole("button", { name: "紧凑" }));
    expect(onLook).toHaveBeenCalledExactlyOnceWith({ textSize: 1.15, zoom: 0.9 });
  });

  it("keeps the slider and text size controls visible alongside the presets", () => {
    draw();
    expect(screen.getByRole("slider", { name: "界面大小微调" })).toBeTruthy();
    expect(screen.getByRole("group", { name: "界面文字大小" })).toBeTruthy();
    expect(document.querySelector('[data-action="theme.import"]')).toBeTruthy();
  });
});
