// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { boot, STORAGE, t } from "../i18n";
import type { GraphicsInfo } from "../port/host";

const answer: { now: GraphicsInfo | null } = { now: null };
vi.mock("../port/host", () => ({ host: () => ({ graphics: () => Promise.resolve(answer.now) }) }));
import { GraphicsSection } from "./GraphicsSection";

beforeEach(() => {
  localStorage.clear();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

afterEach(() => {
  cleanup();
  answer.now = null;
});

it("is absent where the shell has no say over graphics", async () => {
  render(<GraphicsSection />);
  await Promise.resolve();
  expect(screen.queryByRole("group", { name: t("图形渲染") })).toBeNull();
});

it.each(["enabled", "enabled_force", "disabled_software", "unavailable_off"])("shows Electron's own compositing status %s and infers nothing from it", async (value) => {
  answer.now = { launchedOff: false, savedOff: false, compositing: value };
  const { container } = render(<GraphicsSection />);
  expect(await screen.findByText(t("当前启动使用：{mode}", { mode: t("图形合成状态：{value}", { value }) }))).toBeTruthy();
  expect(container.querySelector("[data-graphics]")?.getAttribute("data-graphics")).toBe("reported");
  expect(screen.getByRole("button", { name: t("随系统") }).getAttribute("aria-pressed")).toBe("true");
  expect(screen.queryByRole("status")).toBeNull();
});

it("says no status has been reported rather than guessing, when Electron has none", async () => {
  answer.now = { launchedOff: false, savedOff: false, compositing: "" };
  const { container } = render(<GraphicsSection />);
  expect(await screen.findByText(t("当前启动使用：{mode}", { mode: t("尚未报告图形状态") }))).toBeTruthy();
  expect(container.querySelector("[data-graphics]")?.getAttribute("data-graphics")).toBe("unreported");
});

it("saves the choice where the shell reads it and says it applies next launch", async () => {
  answer.now = { launchedOff: false, savedOff: false, compositing: "enabled" };
  render(<GraphicsSection />);
  await userEvent.click(await screen.findByRole("button", { name: t("仅软件渲染") }));
  expect(localStorage.getItem("rx-hw-accel")).toBe("off");
  expect((await screen.findByRole("status")).textContent).toBe(t("已保存，退出并重新打开 Studio 后生效"));
  await userEvent.click(screen.getByRole("button", { name: t("随系统") }));
  expect(localStorage.getItem("rx-hw-accel")).toBe("on");
  expect(screen.queryByRole("status")).toBeNull();
});

it("says a launch that is already software was the saved choice, and keeps the note quiet", async () => {
  answer.now = { launchedOff: true, savedOff: true, compositing: "disabled_software" };
  localStorage.setItem("rx-hw-accel", "off");
  const { container } = render(<GraphicsSection />);
  await screen.findByText(/./, { selector: "[data-graphics]" });
  expect(container.querySelector("[data-graphics]")?.getAttribute("data-graphics")).toBe("software");
  expect(container.textContent).toContain(t("已按你的设置关闭硬件加速"));
  expect(screen.queryByRole("status")).toBeNull();
});

it("does not call a launch-only override a saved choice, and does not ask for a restart over it", async () => {
  answer.now = { launchedOff: true, savedOff: false, compositing: "disabled_software" };
  const { container } = render(<GraphicsSection />);
  await screen.findByText(/./, { selector: "[data-graphics]" });
  expect(container.textContent).toContain(t("本次启动已用启动参数关闭硬件加速"));
  expect(container.textContent).not.toContain(t("已按你的设置关闭硬件加速"));
  expect(screen.getByRole("button", { name: t("随系统") }).getAttribute("aria-pressed")).toBe("true");
  expect(screen.queryByRole("status")).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: t("仅软件渲染") }));
  expect((await screen.findByRole("status")).textContent).toBe(t("已保存，退出并重新打开 Studio 后生效"));
});
