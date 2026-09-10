// Run: node --import ./scripts/css-stub-register.mjs --import tsx src/__tests__/browser-panel.test.tsx
import assert from "node:assert/strict";
import React, { act } from "react";
import { JSDOM } from "jsdom";

import type { BrowserDownloadView, BrowserLayoutRect, BrowserTabView, DesktopBrowserHost } from "../lib/browserHost";
import type { ReasonixDesktopHost } from "../lib/desktopHost";

const dom = new JSDOM("<div id='root'></div>", { url: "http://localhost", pretendToBeVisual: true });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, localStorage: dom.window.localStorage,
  Event: dom.window.Event, KeyboardEvent: dom.window.KeyboardEvent, InputEvent: dom.window.InputEvent,
  MouseEvent: dom.window.MouseEvent, MutationObserver: dom.window.MutationObserver,
  Node: dom.window.Node, HTMLElement: dom.window.HTMLElement, HTMLInputElement: dom.window.HTMLInputElement,
  requestAnimationFrame: dom.window.requestAnimationFrame.bind(dom.window),
  cancelAnimationFrame: dom.window.cancelAnimationFrame.bind(dom.window),
  IS_REACT_ACT_ENVIRONMENT: true });
class TestResizeObserver { observe() {} unobserve() {} disconnect() {} }
(dom.window as unknown as { ResizeObserver: unknown }).ResizeObserver = TestResizeObserver;
// React's legacy input-event fallback expects these IE hooks when JSDOM does
// not advertise native InputEvent support.
Object.defineProperty(dom.window.HTMLElement.prototype, "attachEvent", { configurable: true, value: () => {} });
Object.defineProperty(dom.window.HTMLElement.prototype, "detachEvent", { configurable: true, value: () => {} });

// Import ReactDOM and the component only after installing the DOM so React
// selects its native input-event path instead of the legacy IE polyfill.
const [{ createRoot }, { BrowserPanel }, { useBrowserPanelStore }, { LocaleProvider }, { ToastProvider }] = await Promise.all([
  import("react-dom/client"),
  import("../components/BrowserPanel"),
  import("../lib/browserPanelStore"),
  import("../lib/i18n"),
  import("../lib/toast"),
]);

const tab = (overrides: Partial<BrowserTabView>): BrowserTabView => ({
  id: "t1", taskId: "A", url: "https://example.com/", title: "Example", loading: false, canGoBack: false, canGoForward: false,
  temporary: false, mode: "agent", epoch: 0, zoom: 1, error: null, ...overrides,
});
const calls: string[] = [];
const layouts: (BrowserLayoutRect | null)[] = [];
const overlays: boolean[] = [];
let tabsCb: ((tabs: BrowserTabView[]) => void) | null = null;
let downloadCb: ((entry: BrowserDownloadView) => void) | null = null;
let closeError: Error | null = null;
const browser: DesktopBrowserHost = {
  list: async () => [],
  open: async (url, opts) => { calls.push(`open ${url} ${opts?.taskId}`); return tab({ id: "opened", url, title: "", taskId: opts?.taskId ?? "" }); },
  close: async (id) => { calls.push(`close ${id}`); if (closeError) throw closeError; },
  activate: async (id) => { calls.push(`activate ${id}`); },
  navigate: async (id, target) => { calls.push(`navigate ${id} ${target.action ?? target.url}`); },
  setZoom: async (id, factor) => { calls.push(`zoom ${id} ${factor}`); },
  toggleDevTools: async (id) => { calls.push(`devtools ${id}`); },
  resume: async (id) => { calls.push(`resume ${id}`); },
  takeover: async (id) => { calls.push(`takeover ${id}`); },
  setLayout: (rect) => { layouts.push(rect); },
  setOverlay: (active) => { overlays.push(active); },
  onTabs: (cb) => { tabsCb = cb; return () => { tabsCb = null; }; },
  onDownload: (cb) => { downloadCb = cb; return () => { downloadCb = null; }; },
};
const noop = () => {};
window.reasonixDesktop = {
  kind: "electron",
  contract: { protocolVersion: 1, digest: "test", commands: [] },
  platform: { os: "darwin", arch: "arm64", versions: {} },
  invoke: async () => undefined,
  on: () => noop,
  native: {
    openExternal: async () => {},
    clipboard: { writeText: async () => true, readText: async () => "" },
    window: { setTheme: noop, setBackgroundColour: noop, getBounds: async () => ({ x: 0, y: 0, width: 0, height: 0, maximised: false }),
      isMaximised: async () => false, minimise: noop, toggleMaximise: noop, close: noop },
    getPathForFile: () => "",
    onServiceState: () => noop,
  },
  browser,
} satisfies ReasonixDesktopHost;

const root = createRoot(document.getElementById("root")!);
const paint = (taskId: string) => act(async () => root.render(
  <LocaleProvider><ToastProvider><BrowserPanel taskId={taskId} /></ToastProvider></LocaleProvider>,
));
const emitTabs = (tabs: BrowserTabView[]) => act(async () => { tabsCb?.(tabs); });
const byLabel = (label: string) => [...document.querySelectorAll<HTMLElement>("[aria-label]")].find((el) => el.getAttribute("aria-label") === label);
const setInputValue = (input: HTMLInputElement, value: string) => {
  Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, "value")!.set!.call(input, value);
  input.dispatchEvent(new dom.window.InputEvent("input", { bubbles: true }));
};
const key = (target: Element, init: KeyboardEventInit) => target.dispatchEvent(new dom.window.KeyboardEvent("keydown", { bubbles: true, cancelable: true, ...init }));

console.log("\nbrowser panel");
try {
  await paint("A");
  assert.ok(document.querySelector(".browser-panel__empty"), "no tabs renders the compact empty state");
  assert.ok(byLabel("Address"), "the empty state carries the address bar");
  assert.equal(document.querySelector("[data-browser-surface]"), null, "no native surface without a tab");
  assert.deepEqual(overlays, [false], "the overlay gate reports its initial state");
  assert.deepEqual(layouts, [], "no layout is reported without a surface");
  assert.ok(tabsCb, "the panel subscribes to tab updates");

  const address = byLabel("Address") as HTMLInputElement;
  await act(async () => { setInputValue(address, "example.com"); key(address, { key: "Enter" }); });
  assert.equal(calls.at(-2), "open https://example.com user", "Enter in the empty state opens a user tab with https");
  assert.equal(calls.at(-1), "activate opened");
  assert.ok(document.querySelector("[data-browser-surface]"), "an open tab mounts the native surface");
  assert.deepEqual(layouts.at(-1), { x: 0, y: 0, width: 0, height: 0 }, "the surface rect reaches the shell");
  assert.equal(document.querySelectorAll("[role='tab']").length, 1);

  await emitTabs([tab({ id: "t1", loading: true, canGoBack: true }), tab({ id: "other", taskId: "B", title: "Other task" })]);
  assert.equal(document.querySelectorAll("[role='tab']").length, 1, "another task's tab is not shown");
  assert.ok(byLabel("Loading"), "a loading tab shows its spinner");
  assert.ok(byLabel("Stop loading"), "loading swaps reload for stop");
  assert.equal(byLabel("Reload"), undefined);
  assert.equal((byLabel("Back") as HTMLButtonElement).disabled, false);
  assert.equal((byLabel("Forward") as HTMLButtonElement).disabled, true);
  await act(async () => byLabel("Stop loading")!.click());
  assert.equal(calls.at(-1), "navigate t1 stop");

  const layoutCount = layouts.length;
  await emitTabs([tab({ id: "t1", error: { code: -105, description: "ERR_NAME_NOT_RESOLVED" } })]);
  const alert = document.querySelector("[role='alert']");
  assert.ok(alert?.textContent?.includes("ERR_NAME_NOT_RESOLVED (-105)"), "the Chromium error code and description are shown");
  assert.equal(document.querySelector("[data-browser-surface]"), null, "the error state replaces the native surface");
  assert.equal(layouts.at(-1), null, "hiding the surface releases the native view");
  assert.equal(layouts.length, layoutCount + 1);
  await act(async () => [...document.querySelectorAll("button")].find((el) => el.textContent === "Retry")!.click());
  assert.equal(calls.at(-1), "navigate t1 reload", "Retry reloads the tab");

  const takeOver = [...document.querySelectorAll("button")].find((el) => el.textContent === "Take over");
  assert.ok(takeOver, "agent mode exposes an explicit takeover button");
  await act(async () => takeOver.click());
  assert.equal(calls.at(-1), "takeover t1", "Take over uses the application host operation");
  await emitTabs([tab({ id: "t1", mode: "human", temporary: true, zoom: 1.25, epoch: 1 })]);
  assert.equal([...document.querySelectorAll("button")].some((el) => el.textContent === "Take over"), false);
  const banner = document.querySelector(".browser-panel__takeover");
  assert.ok(banner?.textContent?.includes("You are controlling this page; the agent is paused"), "human mode shows the take-over banner");
  assert.ok(document.querySelector(".browser-tab__badge")?.textContent === "Temporary", "temporary tabs carry a badge");
  assert.equal(byLabel("Reset zoom (125%)")?.textContent, "125%");
  await act(async () => [...banner!.querySelectorAll("button")].find((el) => el.textContent === "Resume")!.click());
  assert.equal(calls.at(-1), "resume t1", "Resume hands the page back to the agent");
  await emitTabs([tab({ id: "t1", mode: "agent", temporary: true, zoom: 1.25, epoch: 2 })]);
  assert.equal(document.querySelector(".browser-panel__takeover"), null, "authoritative Resume clears the takeover banner");
  assert.ok([...document.querySelectorAll("button")].find((el) => el.textContent === "Take over"), "Resume restores the manual takeover entry");
  await act(async () => byLabel("Zoom in")!.click());
  assert.equal(calls.at(-1), "zoom t1 1.5");
  await act(async () => byLabel("Toggle DevTools")!.click());
  assert.equal(calls.at(-1), "devtools t1");

  await act(async () => { downloadCb?.({ id: "d1", tabId: "t1", url: "https://example.com/a.zip", filename: "a.zip", path: "/tmp/a.zip", state: "progressing", received: 50, total: 100 }); });
  const strip = document.querySelector(".browser-panel__downloads");
  assert.ok(strip?.textContent?.includes("a.zip") && strip.textContent.includes("50%"), "downloads show filename and progress");
  await act(async () => { downloadCb?.({ id: "d1", tabId: "t1", url: "https://example.com/a.zip", filename: "a.zip", path: "/tmp/a.zip", state: "completed", received: 100, total: 100 }); });
  assert.ok(document.querySelector(".browser-panel__downloads")?.textContent?.includes("Completed"));

  byLabel("Back")!.focus();
  await act(async () => { key(byLabel("Back")!, { key: "l", ctrlKey: true }); });
  assert.equal(document.activeElement, byLabel("Address"), "Ctrl+L focuses the address bar while the panel has focus");
  await act(async () => { setInputValue(byLabel("Address") as HTMLInputElement, "docs.example"); byLabel("New tab from address")!.click(); });
  assert.equal(calls.at(-2), "open https://docs.example user", "+ opens a tab from the typed draft");

  closeError = new Error("tab is busy");
  await act(async () => byLabel("Close Example")!.click());
  assert.ok(document.querySelector(".toast--error")?.textContent?.includes("Browser: tab is busy"), "host failures surface as error toasts");

  await act(async () => root.unmount());
  assert.equal(layouts.at(-1), null, "unmount releases the layout");
  assert.equal(calls.at(-1), "activate null", "unmount deactivates the native view");
  assert.equal(tabsCb, null, "unmount unsubscribes from the host");
  assert.equal(useBrowserPanelStore.getState().host, null);
  console.log("browser panel: empty, loading, error, take-over, downloads, keyboard and toast states passed");
} finally {
  dom.window.close();
}
