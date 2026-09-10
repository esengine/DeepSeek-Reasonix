// Run: tsx src/__tests__/browser-dock-mode.test.tsx
import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";

import { WorkspaceDockRegion, type WorkspaceDockRegionProps } from "../app-shell/WorkspaceDockRegion";
import type { DesktopBrowserHost } from "../lib/browserHost";
import type { ReasonixDesktopHost } from "../lib/desktopHost";
import { LocaleProvider, type Translator } from "../lib/i18n";
import { useLayoutStore, type RightDockMode } from "../store/layout";

const dom = new JSDOM("<div id='root'></div>", { url: "http://localhost", pretendToBeVisual: true });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, localStorage: dom.window.localStorage,
  IS_REACT_ACT_ENVIRONMENT: true });
class TestResizeObserver { observe() {} unobserve() {} disconnect() {} }
(dom.window as unknown as { ResizeObserver: unknown }).ResizeObserver = TestResizeObserver;

const noop = () => {};
const browser: DesktopBrowserHost = {
  list: async () => [], open: async () => { throw new Error("unused"); }, close: async () => {}, activate: async () => {},
  navigate: async () => {}, setZoom: async () => {}, toggleDevTools: async () => {}, resume: async () => {}, takeover: async () => {},
  setLayout: noop, setOverlay: noop, onTabs: () => noop, onDownload: () => noop,
};
const electron: ReasonixDesktopHost = {
  kind: "electron",
  contract: { protocolVersion: 1, digest: "test", commands: [] },
  platform: { os: "linux", arch: "x64", versions: {} },
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
};

const modes: RightDockMode[] = [];
const props = (mode: RightDockMode): WorkspaceDockRegionProps => ({
  visible: true, overlay: false, mode, creation: false, remoteAvailable: false, showContext: true,
  t: ((key: string) => key) as Translator,
  onMode: (next) => { modes.push(next); }, onRemote: noop,
  remote: {} as WorkspaceDockRegionProps["remote"], context: {} as WorkspaceDockRegionProps["context"],
  workspace: { tabId: "A" } as WorkspaceDockRegionProps["workspace"], workspaceKey: "k",
});
const root = createRoot(document.getElementById("root")!);
const paint = (mode: RightDockMode) => act(async () => root.render(<LocaleProvider><WorkspaceDockRegion {...props(mode)} /></LocaleProvider>));
const tabLabels = () => [...document.querySelectorAll(".workbench-dock__tab-label")].map((el) => el.textContent);
const settle = () => act(async () => { await new Promise((resolve) => setTimeout(resolve, 20)); });

console.log("\nbrowser dock mode");
try {
  await paint("files");
  assert.deepEqual(tabLabels(), ["rightDock.overview", "workspace.filesTab", "workspace.changedTab"], "no shell host: no browser tab");

  window.reasonixDesktop = electron;
  await paint("files");
  await settle();
  assert.deepEqual(tabLabels(), ["rightDock.overview", "workspace.filesTab", "workspace.changedTab", "Browser"], "the Electron host adds the browser tab");
  const browserTab = [...document.querySelectorAll<HTMLButtonElement>("[role='tab']")].find((el) => el.textContent === "Browser")!;
  assert.equal(browserTab.getAttribute("aria-selected"), "false");
  await act(async () => browserTab.click());
  assert.deepEqual(modes, ["browser"], "the tab requests the browser dock mode through the shared mode command");
  assert.equal(document.querySelector(".browser-panel"), null, "files mode does not mount the browser panel");

  await paint("browser");
  await settle();
  assert.equal([...document.querySelectorAll("[role='tab']")].find((el) => el.textContent === "Browser")?.getAttribute("aria-selected"), "true");
  assert.ok(document.querySelector(".browser-panel"), "browser mode mounts the lazy panel in the dock body");
  assert.ok(document.querySelector(".workbench-dock--browser"), "the dock carries the mode modifier");

  useLayoutStore.getState().setRightDockMode("browser");
  assert.equal(useLayoutStore.getState().rightDockMode, "browser", "the layout store accepts the browser dock mode");
  useLayoutStore.getState().setRightDockMode("context");

  delete window.reasonixDesktop;
  await act(async () => root.unmount());
  console.log("browser dock mode: gating, mode switch and lazy mount passed");
} finally {
  dom.window.close();
}
