// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Appearance } from "./Appearance";
import { apply } from "./look";
import { effectsMode, setEffectsMode } from "../state/prefs";
import { MockPort } from "../port/mock";
import { boot, STORAGE } from "../i18n";
import type { AgentPort } from "../port/port";

beforeEach(() => {
  localStorage.clear();
  localStorage.setItem(STORAGE, "zh");
  boot();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  apply(null);
  setEffectsMode("full");
});

function draw() {
  render(<Appearance port={new MockPort() as unknown as AgentPort} theme="light" onTheme={() => {}} contrast="" onContrast={() => {}}
    weight="" onWeight={() => {}} look={{}} onLook={() => {}} reloadThemes={() => {}} />);
}

describe("the visual effects row", () => {
  it("is one switch, on for the full effects", () => {
    draw();
    const sw = screen.getByRole("switch", { name: "视觉效果" });
    expect(sw.getAttribute("aria-checked")).toBe("true");
  });

  it("turns the effects down and back up", async () => {
    draw();
    const sw = screen.getByRole("switch", { name: "视觉效果" });
    await userEvent.click(sw);
    expect(effectsMode()).toBe("reduced");
    expect(sw.getAttribute("aria-checked")).toBe("false");
    await userEvent.click(sw);
    expect(effectsMode()).toBe("full");
  });
});
