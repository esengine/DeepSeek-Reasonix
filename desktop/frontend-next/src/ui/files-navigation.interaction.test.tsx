// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import "./testkit";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";
import { boot, STORAGE } from "../i18n";
import { refresh } from "./viewport";

beforeEach(() => {
  sessionStorage.clear();
  localStorage.clear();
  localStorage.setItem("rx-nav-rail", "on");
  localStorage.setItem(STORAGE, "zh");
  boot();
  vi.spyOn(document.body, "clientWidth", "get").mockReturnValue(1440);
  vi.spyOn(document.body, "clientHeight", "get").mockReturnValue(900);
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
  refresh();
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); localStorage.clear(); sessionStorage.clear(); });

async function mount(empty = false) {
  const hub = new MockHub();
  const runtime = empty ? null : await hub.open({ root: "~/projects/my-website", sessionPath: "/sessions/site.jsonl" });
  if (empty) await hub.close("r1");
  const ports = (await hub.runtimes()).map((rt) => hub.portFor(rt));
  for (const port of ports) {
    port.providerSetup = async () => null;
    port.welcomeSeen = async () => true;
    vi.spyOn(port, "browserOpen");
  }
  if (runtime) sessionStorage.setItem("rx-active-pane", runtime.id);
  const open = vi.spyOn(hub, "open");
  await act(async () => { render(<App hub={hub} />); });
  await waitFor(() => expect(document.querySelector(".rail")).toBeTruthy());
  return { ports, open };
}
const focused = () => document.querySelector<HTMLElement>(".pane[data-active]")!;
const filesShown = (pane = focused()) => pane.querySelector(".workbench-body")!.hasAttribute("data-files");
// A typed query ranks a settings match above an action, so Enter would take
// whichever setting mentions the word; the action is picked by its identity.
async function palette(query: string, action = "files") {
  fireEvent.keyDown(window, { key: "k", ctrlKey: true });
  await userEvent.type(await screen.findByRole("textbox", { name: "搜索会话或操作" }), query);
  await userEvent.click(await waitFor(() => {
    const hit = document.querySelector<HTMLElement>(`.palette-hit[data-target="c:${action}"]`);
    expect(hit).toBeTruthy();
    return hit!;
  }));
}

it.each(["rail", "sidebar", "palette"])("opens only the focused pane's files from %s and handles repeated navigation", async (entry) => {
  if (entry === "sidebar") localStorage.setItem("rx-nav-rail", "off");
  const { ports } = await mount();
  const openFiles = async () => {
    if (entry === "palette") return palette("文件");
    const scope = document.querySelector<HTMLElement>(entry === "rail" ? ".nav" : ".studio-quicknav")!;
    await userEvent.click(within(scope).getByRole("button", { name: "文件" }));
  };
  await openFiles();
  await waitFor(() => expect(filesShown()).toBe(true));
  expect(document.querySelector(".app")!.getAttribute("data-browser")).toBe("on");
  expect(document.querySelector(".app")!.hasAttribute("data-prefs")).toBe(false);
  const background = document.querySelector<HTMLElement>(".pane[data-off]")!;
  expect(filesShown(background)).toBe(false);
  expect(focused().querySelectorAll(".workbench-tab")).toHaveLength(0);
  await userEvent.click(within(focused()).getByRole("button", { name: "README.md" }));
  expect(filesShown()).toBe(false);
  await openFiles();
  expect(filesShown()).toBe(true);
  expect(filesShown(background)).toBe(false);
  expect(screen.queryByRole("dialog", { name: "搜索与快捷操作" })).toBeNull();
  for (const port of ports) expect(port.browserOpen).not.toHaveBeenCalled();
});

it("leaves Storage in settings and dismisses settings when Files is chosen", async () => {
  await mount();
  await palette("storage", "storage");
  await waitFor(() => expect(document.querySelector(".app")!.hasAttribute("data-prefs")).toBe(true));
  await waitFor(() => expect(document.querySelector("#set-storage")).toBeTruthy());
  await palette("文件");
  await waitFor(() => expect(filesShown()).toBe(true));
  expect(document.querySelector(".app")!.hasAttribute("data-prefs")).toBe(false);
});

it("closes the phone-width sidebar drawer after choosing files", async () => {
  vi.mocked(Object.getOwnPropertyDescriptor(document.body, "clientWidth")!.get!).mockReturnValue(390);
  refresh();
  await mount();
  fireEvent.click(document.querySelector(".studio-menu")!);
  expect(document.querySelector(".app")!.getAttribute("data-rail")).toBe("on");
  fireEvent.click(document.querySelector('.studio-quicknav [data-action="files.open"]')!);
  await waitFor(() => expect(filesShown()).toBe(true));
  expect(document.querySelector(".app")!.getAttribute("data-rail")).toBe("off");
});

it("does not open an arbitrary project when no session is open", async () => {
  const { open } = await mount(true);
  await palette("文件");
  expect(open).not.toHaveBeenCalled();
  expect(document.querySelector(".errbar")!.textContent).toContain("请先打开一个会话");
  expect(document.querySelector(".app")!.hasAttribute("data-prefs")).toBe(false);
});

it("does not replay a Files request into a newly focused session", async () => {
  localStorage.setItem("rx-show-session-tabs", "on");
  await mount();
  await palette("文件");
  await userEvent.click(within(focused()).getByRole("button", { name: "README.md" }));
  const previous = focused();
  fireEvent.click(document.querySelector('.ptab[data-pane="r1"]')!);
  expect(focused()).not.toBe(previous);
  expect(filesShown()).toBe(false);
  await palette("文件");
  expect(filesShown()).toBe(true);
  expect(filesShown(previous)).toBe(false);
  fireEvent.click(document.querySelector('.ptab[data-pane="r2"]')!);
  expect(filesShown()).toBe(false);
});

const railFiles = () => within(document.querySelector<HTMLElement>(".nav")!).getByRole("button", { name: "文件" });

it("folds the open files panel when the Files button is pressed again", async () => {
  await mount();
  await userEvent.click(railFiles());
  await waitFor(() => expect(filesShown()).toBe(true));
  await userEvent.click(railFiles());
  await waitFor(() => expect(filesShown()).toBe(false));
  await userEvent.click(railFiles());
  await waitFor(() => expect(filesShown()).toBe(true));
});

it("reads whether the panel is open from the workbench, so a panel closed elsewhere opens on the next press", async () => {
  await mount();
  await userEvent.click(railFiles());
  await waitFor(() => expect(filesShown()).toBe(true));
  await userEvent.click(focused().querySelector<HTMLElement>("[data-action=\"workbench.files\"]")!);
  expect(filesShown()).toBe(false);
  await userEvent.click(railFiles());
  await waitFor(() => expect(filesShown()).toBe(true));
});

it("folds only the focused session's panel, and a press after switching session opens that session's", async () => {
  localStorage.setItem("rx-show-session-tabs", "on");
  await mount();
  await userEvent.click(railFiles());
  await waitFor(() => expect(filesShown()).toBe(true));
  const first = focused();
  fireEvent.click(document.querySelector('.ptab[data-pane="r1"]')!);
  expect(focused()).not.toBe(first);
  expect(filesShown()).toBe(false);
  await userEvent.click(railFiles());
  await waitFor(() => expect(filesShown()).toBe(true));
  expect(filesShown(first)).toBe(true);
  await userEvent.click(railFiles());
  await waitFor(() => expect(filesShown()).toBe(false));
  expect(filesShown(first)).toBe(true);
});
