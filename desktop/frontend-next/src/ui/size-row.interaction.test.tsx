// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Appearance } from "./Appearance";
import { SettingDeepContext } from "./SettingDeep";
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

function draw(look: Look = {}, go = vi.fn()) {
  const onLook = vi.fn();
  render(
    <SettingDeepContext.Provider value={{ pages: true, open: null, section: "外观", go }}>
      <Appearance port={new MockPort() as unknown as AgentPort} theme="light" onTheme={() => {}} contrast="" onContrast={() => {}}
        weight="" onWeight={() => {}} look={look} onLook={onLook} reloadThemes={() => {}} />
    </SettingDeepContext.Provider>,
  );
  return { onLook, go };
}

const group = () => screen.getByRole("radiogroup", { name: "界面大小" });
const checked = () => [...group().querySelectorAll("button")].filter((b) => b.getAttribute("aria-checked") === "true").map((b) => b.textContent);

describe("the size row on the appearance page", () => {
  it("offers small, standard and large in place, standard until one is chosen", () => {
    draw();
    expect([...group().querySelectorAll("button")].map((b) => b.textContent)).toEqual(["小", "标准", "大"]);
    expect(checked()).toEqual(["标准"]);
  });

  it("shows the rung the saved scale sits on, and none for one between rungs", () => {
    draw({ zoom: 1.15 });
    expect(checked()).toEqual(["大"]);
    cleanup();
    draw({ zoom: 1.3 });
    expect(checked()).toEqual([]);
  });

  it("saves the scale and touches nothing else", async () => {
    const { onLook } = draw({ textSize: 1.15 });
    await userEvent.click(screen.getByRole("radio", { name: "小" }));
    expect(onLook).toHaveBeenCalledExactlyOnceWith({ textSize: 1.15, zoom: 0.9 });
  });

  it("keeps the page with the slider and the other size controls one click away", async () => {
    const { go } = draw();
    await userEvent.click(screen.getByRole("button", { name: "调整" }));
    expect(go).toHaveBeenCalledWith("size");
  });
});

describe("the advanced appearance row", () => {
  it("opens the page that holds theme import and custom colours", async () => {
    const { go } = draw();
    expect(screen.getByText("导入主题、自定义颜色")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "高级外观" }));
    expect(go).toHaveBeenCalledWith("scheme");
  });
});
