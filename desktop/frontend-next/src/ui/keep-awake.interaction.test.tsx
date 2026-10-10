// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Appearance } from "./Appearance";
import { MockPort } from "../port/mock";
import { boot, STORAGE, t } from "../i18n";
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
});

async function open() {
  const port = new MockPort() as unknown as AgentPort;
  render(
    <Appearance port={port} theme="light" onTheme={() => {}} contrast="" onContrast={() => {}}
      weight="" onWeight={() => {}} look={{}} onLook={() => {}} reloadThemes={() => {}} />,
  );
  return screen.findByRole("switch", { name: t("任务运行时阻止系统休眠") });
}

it("keeps the computer awake by default and says so", async () => {
  const sw = await open();
  expect(sw.getAttribute("aria-checked")).toBe("true");
  expect(localStorage.getItem("rx-keep-awake")).toBeNull();
});

it("writes the choice where the desktop shell reads it, both ways", async () => {
  const sw = await open();
  await userEvent.click(sw);
  expect(localStorage.getItem("rx-keep-awake")).toBe("off");
  expect(sw.getAttribute("aria-checked")).toBe("false");
  await userEvent.click(sw);
  expect(localStorage.getItem("rx-keep-awake")).toBe("on");
  expect(sw.getAttribute("aria-checked")).toBe("true");
});

it("reads a saved off back as off after a reload", async () => {
  localStorage.setItem("rx-keep-awake", "off");
  const sw = await open();
  expect(sw.getAttribute("aria-checked")).toBe("false");
});

it("leaves the tray switches where they were", async () => {
  await open();
  expect(screen.getByRole("switch", { name: t("在托盘显示图标") })).toBeTruthy();
});
