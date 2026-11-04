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
  expect(screen.queryByRole("group", { name: t("look.graphics") })).toBeNull();
});

it.each(["enabled", "enabled_force", "disabled_software", "unavailable_off"])("shows Electron's own compositing status %s and infers nothing from it", async (value) => {
  answer.now = { launchedOff: false, savedOff: false, compositing: value };
  const { container } = render(<GraphicsSection />);
  expect(await screen.findByText(t("look.this_launch_uses_mode", { mode: t("look.graphics_compositing_status_value", { value }) }))).toBeTruthy();
  expect(container.querySelector("[data-graphics]")?.getAttribute("data-graphics")).toBe("reported");
  expect(screen.getByRole("button", { name: t("look.system_default") }).getAttribute("aria-pressed")).toBe("true");
  expect(screen.queryByRole("status")).toBeNull();
});

it("says no status has been reported rather than guessing, when Electron has none", async () => {
  answer.now = { launchedOff: false, savedOff: false, compositing: "" };
  const { container } = render(<GraphicsSection />);
  expect(await screen.findByText(t("look.this_launch_uses_mode", { mode: t("look.no_graphics_status_reported_yet") }))).toBeTruthy();
  expect(container.querySelector("[data-graphics]")?.getAttribute("data-graphics")).toBe("unreported");
});

it("saves the choice where the shell reads it and says it applies next launch", async () => {
  answer.now = { launchedOff: false, savedOff: false, compositing: "enabled" };
  render(<GraphicsSection />);
  await userEvent.click(await screen.findByRole("button", { name: t("look.software_only") }));
  expect(localStorage.getItem("rx-hw-accel")).toBe("off");
  expect((await screen.findByRole("status")).textContent).toBe(t("look.saved_quit_and_reopen_studio"));
  await userEvent.click(screen.getByRole("button", { name: t("look.system_default") }));
  expect(localStorage.getItem("rx-hw-accel")).toBe("on");
  expect(screen.queryByRole("status")).toBeNull();
});

it("says a launch that is already software was the saved choice, and keeps the note quiet", async () => {
  answer.now = { launchedOff: true, savedOff: true, compositing: "disabled_software" };
  localStorage.setItem("rx-hw-accel", "off");
  const { container } = render(<GraphicsSection />);
  await screen.findByText(/./, { selector: "[data-graphics]" });
  expect(container.querySelector("[data-graphics]")?.getAttribute("data-graphics")).toBe("software");
  expect(container.textContent).toContain(t("look.hardware_acceleration_is_switched_off"));
  expect(screen.queryByRole("status")).toBeNull();
});

it("does not call a launch-only override a saved choice, and does not ask for a restart over it", async () => {
  answer.now = { launchedOff: true, savedOff: false, compositing: "disabled_software" };
  const { container } = render(<GraphicsSection />);
  await screen.findByText(/./, { selector: "[data-graphics]" });
  expect(container.textContent).toContain(t("look.hardware_acceleration_was_switched_off"));
  expect(container.textContent).not.toContain(t("look.hardware_acceleration_is_switched_off"));
  expect(screen.getByRole("button", { name: t("look.system_default") }).getAttribute("aria-pressed")).toBe("true");
  expect(screen.queryByRole("status")).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: t("look.software_only") }));
  expect((await screen.findByRole("status")).textContent).toBe(t("look.saved_quit_and_reopen_studio"));
});
