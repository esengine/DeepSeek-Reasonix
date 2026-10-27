// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";
import { boot, STORAGE } from "../i18n";
import { refresh } from "./viewport";

const KEY = "rx-nav-rail";

// jsdom lays nothing out, so the width the fold reads is whatever is said here.
function room(width: number) {
  vi.spyOn(document.body, "clientWidth", "get").mockReturnValue(width);
  vi.spyOn(document.body, "clientHeight", "get").mockReturnValue(900);
  refresh();
}

beforeEach(async () => {
  await import("./Settings");
  sessionStorage.clear();
  localStorage.clear();
  localStorage.setItem(STORAGE, "zh");
  boot();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
  HTMLElement.prototype.setPointerCapture = vi.fn();
  room(1440);
});
afterEach(() => {
  cleanup();
  sessionStorage.clear();
  localStorage.clear();
  delete document.documentElement.dataset.fold;
  vi.restoreAllMocks();
});

async function open() {
  const hub = new MockHub();
  const build = hub.portFor.bind(hub);
  const prepared = new WeakSet();
  hub.portFor = (rt) => {
    const port = build(rt);
    if (!prepared.has(port)) {
      prepared.add(port);
      port.providerSetup = async () => null;
      port.welcomeSeen = async () => true;
    }
    return port;
  };
  const view = render(<App hub={hub} />);
  await screen.findByRole("combobox", { name: "任务输入" });
  return { hub, view };
}

const nav = () => screen.queryByRole("navigation", { name: "主导航" });
const navEl = () => document.querySelector<HTMLElement>("nav.nav");
const app = () => document.querySelector<HTMLElement>(".app")!;
const toggleSidebar = () => userEvent.click(document.querySelector<HTMLElement>(".gutter-l .grip")!);

async function openAppearance() {
  await userEvent.click(document.querySelector<HTMLElement>('[data-action="chrome.settings"]')!);
  await userEvent.click(await screen.findByRole("tab", { name: /外观/ }));
  await screen.findByRole("group", { name: "图标栏" });
}
const choice = (name: string) => screen.getByRole("button", { name });
const pressed = (name: string) => choice(name).getAttribute("aria-pressed");

it("offers the tools destination only through the navigation entries, not a second button in the top bar", async () => {
  await open();
  expect(screen.queryByRole("button", { name: "沙盒" })).toBeNull();
  expect(within(screen.getByRole("navigation", { name: "主导航" })).getByRole("button", { name: "工具与权限" })).toBeTruthy();
});

it("always draws the rail by default, with every entry in it", async () => {
  await open();
  expect(nav()).not.toBeNull();
  expect(app().dataset.nav).toBe("on");
  expect(document.querySelector(".railfoot")).toBeNull();
  for (const name of ["会话", "用量", "文件", "工具与权限", "扩展", "记忆", "远程", "发送反馈", "账号", "设置"]) {
    expect(within(navEl()!).getByRole("button", { name: new RegExp(`^${name}`) }), name).toBeTruthy();
  }
});

it.each([
  [null, "on"],
  ["on", "on"],
  ["off", "off"],
  ["collapsed", "collapsed"],
  ["true", "on"],
  ["", "on"],
])("reads a stored %j as %s", async (stored, mode) => {
  if (stored !== null) localStorage.setItem(KEY, stored);
  await open();
  await openAppearance();
  const label = { on: "始终显示", collapsed: "仅侧栏收起时显示", off: "不显示" }[mode as "on"];
  expect(pressed(label)).toBe("true");
});

it("moves between the three states at once and keeps the choice across a reload", async () => {
  const first = await open();
  await openAppearance();
  await userEvent.click(choice("不显示"));
  expect(nav()).toBeNull();
  expect(app().dataset.nav).toBe("off");
  expect(localStorage.getItem(KEY)).toBe("off");
  await userEvent.click(choice("仅侧栏收起时显示"));
  expect(localStorage.getItem(KEY)).toBe("collapsed");
  await userEvent.click(choice("始终显示"));
  expect(nav()).not.toBeNull();
  expect(localStorage.getItem(KEY)).toBe("on");
  await userEvent.click(choice("仅侧栏收起时显示"));
  first.view.unmount();
  await open();
  expect(app().dataset.nav).toBe("off");
  expect(localStorage.getItem(KEY)).toBe("collapsed");
});

it("in collapsed mode follows the sidebar without remounting the rail", async () => {
  localStorage.setItem(KEY, "collapsed");
  await open();
  const el = navEl()!;
  expect(el).not.toBeNull();
  expect(app().dataset.rail).toBe("on");
  expect(app().dataset.nav).toBe("off");
  expect(nav()).toBeNull();
  expect(document.querySelector(".railfoot")).not.toBeNull();
  await toggleSidebar();
  expect(app().dataset.rail).toBe("off");
  expect(app().dataset.nav).toBe("on");
  expect(nav()).not.toBeNull();
  expect(document.querySelector(".railfoot")).toBeNull();
  expect(navEl()).toBe(el);
  await toggleSidebar();
  expect(app().dataset.nav).toBe("off");
  expect(document.querySelector(".railfoot")).not.toBeNull();
  expect(navEl()).toBe(el);
});

it("always mode keeps the rail beside an open sidebar and a closed one", async () => {
  await open();
  const el = navEl()!;
  await toggleSidebar();
  expect(app().dataset.nav).toBe("on");
  expect(navEl()).toBe(el);
});

it("still reaches settings with the rail off", async () => {
  localStorage.setItem(KEY, "off");
  await open();
  const footer = document.querySelector<HTMLElement>(".railfoot")!;
  expect(footer).not.toBeNull();
  for (const name of ["钱包与用量", "发送反馈", "账号", "设置"]) {
    expect(within(footer).getByRole("button", { name: new RegExp(`^${name}`) }), name).toBeTruthy();
  }
  await openAppearance();
  expect(pressed("不显示")).toBe("true");
});

it.each(["on", "off"])("reaches usage, account, and settings with the rail %s", async (mode) => {
  localStorage.setItem(KEY, mode);
  await open();
  const entries = mode === "on" ? navEl()! : document.querySelector<HTMLElement>(".railfoot")!;
  for (const [label, section, action] of [[mode === "on" ? "用量" : "钱包与用量", "usage"], ["账号", "account", "chrome.account"], ["设置", "session", "chrome.settings"]]) {
    const button = within(entries).getByRole("button", { name: new RegExp(`^${label}`) });
    if (action) expect(entries.querySelector(`[data-action="${action}"]`)).toBe(button);
    await userEvent.click(button);
    expect((await screen.findByRole("tabpanel")).getAttribute("data-sec")).toBe(section);
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog", { name: "设置" })).toBeNull();
  }
});

it("never draws the rail under the scene fold, in any state, and restores it after", async () => {
  for (const mode of ["on", "collapsed", "off"]) {
    cleanup();
    localStorage.setItem(KEY, mode);
    room(1440);
    await open();
    await toggleSidebar();
    expect(nav() !== null, mode).toBe(mode !== "off");
    act(() => room(390));
    await waitFor(() => expect(nav()).toBeNull());
    expect(navEl()).toBeNull();
    expect(document.querySelector(".railfoot")).not.toBeNull();
    expect(localStorage.getItem(KEY)).toBe(mode);
    act(() => room(1440));
    await waitFor(() => expect(nav() !== null).toBe(mode !== "off"));
    expect(document.querySelector(".railfoot") !== null).toBe(mode === "off");
  }
});

it("opens feedback from the rail like the sidebar entry does, badge included", async () => {
  const hub = new MockHub();
  const build = hub.portFor.bind(hub);
  const prepared = new WeakSet();
  hub.portFor = (rt) => {
    const port = build(rt);
    if (!prepared.has(port)) {
      prepared.add(port);
      port.providerSetup = async () => null;
      port.welcomeSeen = async () => true;
    }
    return port;
  };
  render(<App hub={hub} />);
  await screen.findByRole("combobox", { name: "任务输入" });
  const entry = await within(navEl()!).findByRole("button", { name: /发送反馈.*3 项待查看/ });
  expect(entry.querySelector(".fbk-badge")?.textContent).toBe("3");
  await userEvent.click(entry);
  expect(await screen.findByRole("tab", { name: "我的反馈", selected: true })).toBeTruthy();
});

it("the conversation item opens and closes the workspace column, and from settings it goes home", async () => {
  await open();
  const item = () => within(navEl()!).getByRole("button", { name: /^会话/ });
  expect(app().dataset.rail).toBe("on");
  await userEvent.click(item());
  expect(app().dataset.rail).toBe("off");
  await userEvent.click(item());
  expect(app().dataset.rail).toBe("on");
  await userEvent.click(within(navEl()!).getByRole("button", { name: /^用量/ }));
  expect(item().getAttribute("aria-current")).toBeNull();
  await userEvent.click(item());
  expect(item().getAttribute("aria-current")).toBe("page");
  expect(item().hasAttribute("aria-pressed")).toBe(false);
  expect(app().dataset.rail).toBe("on");
});

it("keeps desktop navigation usable after closing settings without a top toggle", async () => {
  await open();
  await userEvent.click(within(navEl()!).getByRole("button", { name: /^用量/ }));
  expect(document.querySelector(".studio-menu")).toBeNull();
  expect(app().dataset.rail).toBe("on");
  await userEvent.keyboard("{Escape}");
  await toggleSidebar();
  expect(app().dataset.rail).toBe("off");
  await toggleSidebar();
  expect(app().dataset.rail).toBe("on");
});

it("declares the narrow drawer toggle after the shell's drag regions", async () => {
  room(390);
  await open();
  const toggle = document.querySelector<HTMLElement>(".studio-menu")!;
  expect(toggle).not.toBeNull();
  for (const sel of [".chrome", ".rail"]) {
    const region = document.querySelector<HTMLElement>(sel)!;
    expect(region, sel).not.toBeNull();
    expect(region.compareDocumentPosition(toggle) & Node.DOCUMENT_POSITION_FOLLOWING, `${sel} must come before the toggle`).toBeTruthy();
  }
});

it("uses only the divider to collapse and reopen the desktop sidebar", async () => {
  await open();
  const toggles = screen.queryAllByRole("button", { name: /收起工作区栏|展开工作区栏/ }).filter((b) => b.tabIndex !== -1);
  expect(toggles).toHaveLength(0);
  expect(document.querySelector(".studio-menu")).toBeNull();
  expect(document.querySelector(".studio-quicknav")).toBeNull();
  await toggleSidebar();
  expect(app().dataset.rail).toBe("off");
  expect(screen.getByRole("separator", { name: "调整工作区栏宽度" }).getAttribute("aria-valuenow")).toBe("0");
  await toggleSidebar();
  expect(app().dataset.rail).toBe("on");
  expect(document.querySelector(".studio-menu")).toBeNull();
});

it("remembers divider width through collapse, reopen, and reload with the icon rail off", async () => {
  localStorage.setItem(KEY, "off");
  const first = await open();
  const divider = screen.getByRole("separator", { name: "调整工作区栏宽度" });
  divider.focus();
  await userEvent.keyboard("{ArrowRight}");
  expect(divider.getAttribute("aria-valuenow")).toBe("328");
  expect(localStorage.getItem("rx-rail-w")).toBe("328");
  await userEvent.keyboard("{Enter}");
  expect(app().dataset.rail).toBe("off");
  await userEvent.keyboard(" ");
  expect(app().dataset.rail).toBe("on");
  expect(divider.getAttribute("aria-valuenow")).toBe("328");
  first.view.unmount();
  await open();
  expect(screen.getByRole("separator", { name: "调整工作区栏宽度" }).getAttribute("aria-valuenow")).toBe("328");
});

it.each(["on", "off"])("keeps one drawer entry at the scene boundary with the icon rail %s", async (mode) => {
  localStorage.setItem(KEY, mode);
  await open();
  act(() => room(641));
  expect(document.querySelector(".studio-menu")).toBeNull();
  act(() => room(640));
  const toggle = await screen.findByRole("button", { name: "展开工作区栏" });
  expect(document.querySelectorAll(".studio-menu")).toHaveLength(1);
  await userEvent.click(toggle);
  expect(app().dataset.rail).toBe("on");
  expect(toggle.getAttribute("aria-label")).toBe("收起工作区栏");
  await userEvent.click(toggle);
  expect(app().dataset.rail).toBe("off");
  act(() => room(390));
  await userEvent.click(screen.getByRole("button", { name: "展开工作区栏" }));
  expect(app().dataset.rail).toBe("on");
  act(() => room(1440));
  expect(document.querySelector(".studio-menu")).toBeNull();
  expect(app().dataset.rail).toBe("on");
  await toggleSidebar();
  act(() => room(390));
  act(() => room(1440));
  expect(app().dataset.rail).toBe("off");
  expect(document.querySelector(".studio-menu")).toBeNull();
});

it("with the rail off the sidebar keeps its own quick entries", async () => {
  localStorage.setItem(KEY, "off");
  await open();
  expect(document.querySelector(".studio-quicknav")).not.toBeNull();
});
