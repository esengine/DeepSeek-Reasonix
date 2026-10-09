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

it("says hardware acceleration is in use and offers the switch", async () => {
  answer.now = { launchedOff: false, compositing: "enabled" };
  render(<GraphicsSection />);
  expect(await screen.findByText(t("当前启动使用：{mode}", { mode: t("硬件加速") }))).toBeTruthy();
  expect(screen.getByRole("button", { name: t("随系统") }).getAttribute("aria-pressed")).toBe("true");
  expect(screen.queryByRole("status")).toBeNull();
});

it("saves the choice where the shell reads it and says it applies next launch", async () => {
  answer.now = { launchedOff: false, compositing: "enabled" };
  render(<GraphicsSection />);
  await userEvent.click(await screen.findByRole("button", { name: t("仅软件渲染") }));
  expect(localStorage.getItem("rx-hw-accel")).toBe("off");
  expect((await screen.findByRole("status")).textContent).toBe(t("已保存，退出并重新打开 Studio 后生效"));
  await userEvent.click(screen.getByRole("button", { name: t("随系统") }));
  expect(localStorage.getItem("rx-hw-accel")).toBe("on");
  expect(screen.queryByRole("status")).toBeNull();
});

it("tells a launch that is already software from one the driver forced", async () => {
  answer.now = { launchedOff: true, compositing: "disabled_software" };
  localStorage.setItem("rx-hw-accel", "off");
  const { container } = render(<GraphicsSection />);
  await screen.findByText(/./, { selector: "[data-graphics]" });
  expect(container.querySelector("[data-graphics]")?.getAttribute("data-graphics")).toBe("software");
  expect(screen.queryByRole("status")).toBeNull();
  cleanup();
  answer.now = { launchedOff: false, compositing: "disabled_software" };
  const again = render(<GraphicsSection />);
  await screen.findByText(/./, { selector: "[data-graphics]" });
  expect(again.container.querySelector("[data-graphics]")?.getAttribute("data-graphics")).toBe("fallback");
});
