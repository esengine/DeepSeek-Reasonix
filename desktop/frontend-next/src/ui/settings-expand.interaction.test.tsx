// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { AgentPort, SessionStatus } from "../port/port";

beforeEach(() => localStorage.clear());
afterEach(cleanup);

function draw(onClose = vi.fn()) {
  render(<Settings
    hub={new MockHub() as never} port={new MockPort() as unknown as AgentPort}
    status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{} as never} onLook={() => {}} reloadThemes={() => {}}
    onClose={onClose} onChanged={vi.fn()} onError={() => {}} at="appearance"
    account={null} accountUnread="" reloadAccount={() => {}}
  />);
  return document.querySelector<HTMLElement>(".prefs-sheet")!;
}

it("offers a control that fills the window, and says whether it is on", async () => {
  const sheet = draw();
  const expand = screen.getByRole("button", { name: "放大设置窗口" });
  expect(expand.getAttribute("aria-pressed")).toBe("false");
  expect(sheet.hasAttribute("data-wide")).toBe(false);
  await userEvent.click(expand);
  expect(expand.getAttribute("aria-pressed")).toBe("true");
  expect(sheet.hasAttribute("data-wide")).toBe(true);
  await userEvent.click(expand);
  expect(sheet.hasAttribute("data-wide")).toBe(false);
});

it("opens at the size it was last left at", async () => {
  draw();
  await userEvent.click(screen.getByRole("button", { name: "放大设置窗口" }));
  cleanup();
  const sheet = draw();
  expect(sheet.hasAttribute("data-wide")).toBe(true);
  expect(screen.getByRole("button", { name: "放大设置窗口" }).getAttribute("aria-pressed")).toBe("true");
});

it("draws the arrows outward while the window is small and inward once it is enlarged", async () => {
  draw();
  const expand = screen.getByRole("button", { name: "放大设置窗口" });
  const path = () => expand.querySelector("path")?.getAttribute("d");
  const outward = path();
  expect(expand.getAttribute("title")).toBe("放大设置窗口");
  await userEvent.click(expand);
  expect(path()).not.toBe(outward);
  expect(expand.getAttribute("title")).toBe("缩小设置窗口");
  await userEvent.click(expand);
  expect(path()).toBe(outward);
});

it("keeps search Escape and dismissal available after expanding and restoring", async () => {
  const onClose = vi.fn();
  const sheet = draw(onClose);
  const expand = screen.getByRole("button", { name: "放大设置窗口" });
  await userEvent.click(expand);
  const search = screen.getByRole<HTMLInputElement>("textbox", { name: "搜索设置" });
  await userEvent.type(search, "模型");
  await userEvent.keyboard("{Escape}");
  expect(search.value).toBe("");
  expect(onClose).not.toHaveBeenCalled();
  expect(sheet.hasAttribute("data-wide")).toBe(true);
  await userEvent.click(expand);
  expect(sheet.hasAttribute("data-wide")).toBe(false);
  await userEvent.click(screen.getByRole("button", { name: /关闭.*Esc/ }));
  expect(onClose).toHaveBeenCalledTimes(1);
  await userEvent.keyboard("{Escape}");
  expect(onClose).toHaveBeenCalledTimes(2);
});
