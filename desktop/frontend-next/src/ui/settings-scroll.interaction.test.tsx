// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.useRealTimers(); });

function draw(at: string, port = new MockPort()) {
  render(<Settings
    hub={new MockHub()} port={port} status={null}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}}
    weight="" onWeight={() => {}} look={{}} onLook={() => {}}
    reloadThemes={() => {}} onClose={() => {}} onChanged={() => {}}
    onError={() => {}} at={at} account={null} accountUnread="" reloadAccount={() => {}}
  />);
}

it.each([
  ["appearance", "账号与关于"],
  ["tools", "Agent 行为"],
])("starts the next category at the top after scrolling %s", async (from, to) => {
  draw(from);
  const panel = screen.getByRole("tabpanel");
  panel.scrollTop = 1800;
  panel.scrollLeft = 120;
  await userEvent.click(screen.getByRole("tab", { name: new RegExp(`^${to}`) }));
  expect(screen.getByRole("tab", { name: new RegExp(`^${to}`), selected: true })).toBeTruthy();
  expect(screen.getByRole("tabpanel").scrollTop).toBe(0);
  expect(screen.getByRole("tabpanel").scrollLeft).toBe(0);
});

it("starts a keyboard-selected category at the top and keeps focus on its tab", async () => {
  draw("tools");
  const current = screen.getByRole("tab", { name: /^权限与安全/ });
  current.focus();
  screen.getByRole("tabpanel").scrollTop = 1600;
  await userEvent.keyboard("{ArrowDown}");
  const next = screen.getByRole("tab", { name: /^扩展/ });
  expect(next.getAttribute("aria-selected")).toBe("true");
  expect(document.activeElement).toBe(next);
  expect(screen.getByRole("tabpanel").scrollTop).toBe(0);
});

it("keeps the current category's scroll and unsaved input when it is selected again", async () => {
  draw("persona:persona");
  const prefix = await screen.findByRole<HTMLTextAreaElement>("textbox", { name: "系统提示词开头" });
  await waitFor(() => expect(prefix.disabled).toBe(false));
  await userEvent.type(prefix, "Unsaved draft");
  const panel = screen.getByRole("tabpanel");
  panel.scrollTop = 650;
  await userEvent.click(screen.getByRole("tab", { name: /^Agent 行为/ }));
  await userEvent.type(screen.getByRole("textbox", { name: "搜索设置" }), "字体");
  expect(screen.getByRole("tabpanel").scrollTop).toBe(650);
  expect(screen.getByRole<HTMLTextAreaElement>("textbox", { name: "系统提示词开头" }).value).toBe("Unsaved draft");
});

function anchorScrolling() {
  const rect = Element.prototype.getBoundingClientRect;
  vi.spyOn(Element.prototype, "getBoundingClientRect").mockImplementation(function (this: Element) {
    if (this.matches("[data-setting]")) return new DOMRect(0, 500 - (this.closest(".prefs-main")?.scrollTop ?? 0), 600, 100);
    if (this.matches(".page-index")) return new DOMRect(0, 0, 600, 64);
    return rect.call(this);
  });
  return vi.spyOn(Element.prototype, "scrollTo").mockImplementation(function (this: Element, options: number | ScrollToOptions) {
    this.scrollTop = typeof options === "object" ? options.top ?? 0 : 0;
  });
}

it("keeps a search result's anchor after switching away from a scrolled category", async () => {
  const scroll = anchorScrolling();
  draw("session");
  screen.getByRole("tabpanel").scrollTop = 1500;
  await userEvent.type(screen.getByRole("textbox", { name: "搜索设置" }), "柔和");
  await userEvent.click(await screen.findByRole("option", { name: /文字对比度/ }));
  const target = document.querySelector<HTMLElement>('[data-setting="contrast"]')!;
  expect(target.hasAttribute("data-landed")).toBe(true);
  expect(scroll.mock.contexts).toContain(screen.getByRole("tabpanel"));
  expect(scroll).toHaveBeenCalledWith(expect.objectContaining({ top: 420 }));
  expect(screen.getByRole("tabpanel").scrollTop).toBe(420);
});

it("lands on an initial anchor and scrolls it into view", async () => {
  const scroll = anchorScrolling();
  draw("appearance:contrast");
  await screen.findByRole("tab", { name: /^外观/, selected: true });
  const target = document.querySelector<HTMLElement>('[data-setting="contrast"]')!;
  expect(target.hasAttribute("data-landed")).toBe(true);
  expect(scroll.mock.contexts).toContain(screen.getByRole("tabpanel"));
  expect(scroll).toHaveBeenCalledWith(expect.objectContaining({ top: 420 }));
  expect(screen.getByRole("tabpanel").scrollTop).toBe(420);
});

it("waits for a delayed setting before starting the landing feedback lifetime", async () => {
  const port = new MockPort();
  const saved = await port.observabilitySettings();
  let finish!: (value: typeof saved) => void;
  vi.spyOn(port, "observabilitySettings").mockReturnValue(new Promise((resolve) => { finish = resolve; }));
  vi.useFakeTimers();
  const scroll = anchorScrolling();
  draw("account:trace-export", port);
  await act(async () => { await vi.advanceTimersByTimeAsync(1200); });
  expect(document.querySelector('[data-setting="trace-export"]')).toBeNull();
  await act(async () => { finish(saved); });
  const target = document.querySelector<HTMLElement>('[data-setting="trace-export"]')!;
  expect(target.getAttribute("data-landed")).toBe("");
  expect(target.hasAttribute("data-landed")).toBe(true);
  expect(scroll.mock.contexts).toContain(screen.getByRole("tabpanel"));
  expect(scroll).toHaveBeenCalledWith(expect.objectContaining({ top: 420 }));
});

it("lands on a searched setting that arrives after navigation", async () => {
  const port = new MockPort();
  const saved = await port.observabilitySettings();
  let finish!: (value: typeof saved) => void;
  vi.spyOn(port, "observabilitySettings").mockReturnValue(new Promise((resolve) => { finish = resolve; }));
  const scroll = anchorScrolling();
  draw("session", port);
  await userEvent.type(screen.getByRole("textbox", { name: "搜索设置" }), "追踪导出");
  await userEvent.click(await screen.findByRole("option", { name: /追踪导出/ }));
  await act(async () => { finish(saved); });
  const target = document.querySelector<HTMLElement>('[data-setting="trace-export"]')!;
  expect(target.getAttribute("data-landed")).toBe("");
  expect(target.hasAttribute("data-landed")).toBe(true);
  expect(scroll.mock.contexts).toContain(screen.getByRole("tabpanel"));
  expect(scroll).toHaveBeenCalledWith(expect.objectContaining({ top: 420 }));
});

it("cancels a pending landing when the user chooses another category", async () => {
  const port = new MockPort();
  const saved = await port.observabilitySettings();
  let finish!: (value: typeof saved) => void;
  vi.spyOn(port, "observabilitySettings").mockReturnValue(new Promise((resolve) => { finish = resolve; }));
  draw("account:trace-export", port);
  await userEvent.click(screen.getByRole("tab", { name: /^数据/ }));
  await act(async () => { finish(saved); });
  await userEvent.click(screen.getByRole("tab", { name: /^账号/ }));
  const target = await waitFor(() => {
    const found = document.querySelector<HTMLElement>('[data-setting="trace-export"]');
    expect(found).toBeTruthy();
    return found!;
  });
  expect(target.hasAttribute("data-landed")).toBe(false);
});
