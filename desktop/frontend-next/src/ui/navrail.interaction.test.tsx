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
const toggleSidebar = () => userEvent.click(screen.getAllByRole("button", { name: /收起工作区栏|展开工作区栏/ })[0]);

async function openAppearance() {
  await userEvent.click(screen.getByRole("button", { name: "沙盒" }));
  await userEvent.click(await screen.findByRole("tab", { name: /外观/ }));
  await screen.findByRole("group", { name: "图标栏" });
}
const choice = (name: string) => screen.getByRole("button", { name });
const pressed = (name: string) => choice(name).getAttribute("aria-pressed");

it("always draws the rail by default, with every entry in it", async () => {
  await open();
  expect(nav()).not.toBeNull();
  expect(app().dataset.nav).toBe("on");
  for (const name of ["会话", "用量", "工具与权限", "扩展", "记忆", "远程", "发送反馈", "账号", "设置"]) {
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
  await toggleSidebar();
  expect(app().dataset.rail).toBe("off");
  expect(app().dataset.nav).toBe("on");
  expect(nav()).not.toBeNull();
  expect(navEl()).toBe(el);
  await toggleSidebar();
  expect(app().dataset.nav).toBe("off");
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
  await openAppearance();
  expect(pressed("不显示")).toBe("true");
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
    expect(localStorage.getItem(KEY)).toBe(mode);
    act(() => room(1440));
    await waitFor(() => expect(nav() !== null).toBe(mode !== "off"));
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
