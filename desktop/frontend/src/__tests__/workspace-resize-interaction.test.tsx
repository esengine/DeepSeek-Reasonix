// Run: tsx src/__tests__/workspace-resize-interaction.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { WorkspacePanel } from "../components/WorkspacePanel";
import type { AppBindings } from "../lib/bridge";
import { LocaleProvider } from "../lib/i18n";
import { resetWorkspaceTreeMemoryForTests } from "../lib/workspaceTreeMemory";

let passed = 0;
let failed = 0;

function eq<T>(actual: T, expected: T, label: string) {
  if (Object.is(actual, expected)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${String(expected)}, got ${String(actual)}\n`);
    failed += 1;
  }
}

function flushTimers(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 30; attempt += 1) {
    await act(async () => {
      await flushTimers();
    });
    if (predicate()) return;
  }
  throw new Error(`timed out waiting for ${label}`);
}

class TestResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(dom.window.navigator, "language", { configurable: true, value: "en-US" });
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.Node = dom.window.Node;
globalThis.Element = dom.window.Element;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.CustomEvent = dom.window.CustomEvent;
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.PointerEvent = dom.window.MouseEvent as unknown as typeof PointerEvent;
globalThis.MutationObserver = dom.window.MutationObserver;
globalThis.ResizeObserver = TestResizeObserver;
dom.window.ResizeObserver = TestResizeObserver;
globalThis.localStorage = dom.window.localStorage;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
Object.defineProperty(dom.window.HTMLElement.prototype, "scrollIntoView", { configurable: true, value: () => {} });
Object.defineProperty(dom.window.HTMLElement.prototype, "offsetWidth", { configurable: true, get: () => 800 });
Object.defineProperty(dom.window.HTMLElement.prototype, "offsetHeight", {
  configurable: true,
  get: function offsetHeight(this: HTMLElement) {
    return this.classList.contains("workspace-tree") ? 300 : this.dataset.index ? 24 : 0;
  },
});
Object.defineProperty(dom.window.HTMLElement.prototype, "getBoundingClientRect", {
  configurable: true,
  value: function getBoundingClientRect(this: HTMLElement) {
    const width = 800;
    const height = this.classList.contains("workspace-tree") ? 300 : this.dataset.index ? 24 : 0;
    return { x: 100, y: 0, top: 0, left: 100, right: 900, bottom: height, width, height, toJSON: () => ({}) } as DOMRect;
  },
});

console.log("\nworkspace right-side tree resize interaction");

resetWorkspaceTreeMemoryForTests();
window.go = {
  main: {
    App: {
      ListDirForTab: async (_tabId, dir) => dir === "" ? [{ name: "app.ts", isDir: false }] : [],
      SearchFileRefsForTab: async () => [],
      WorkspaceGitHistory: async () => [],
      WorkspaceChanges: async () => ({ files: [], gitAvailable: true }),
      WorkspaceChangeDetail: async () => ({}),
      ReadFileForTab: async (_tabId, path) => ({ path, body: "const value = 1;", size: 16, truncated: false, binary: false }),
    } as Partial<AppBindings> as AppBindings,
  },
};

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);
await act(async () => {
  root.render(
    <LocaleProvider>
      <WorkspacePanel
        open
        tabId="resize-tab"
        cwd="/repo"
        maximized={false}
        panelWidth={800}
        initialViewMode="files"
        onClose={() => {}}
        onToggleMaximized={() => {}}
      />
    </LocaleProvider>,
  );
  await flushTimers();
});

await waitFor("workspace file", () => document.querySelector('[data-workspace-path="app.ts"]') !== null);
await act(async () => {
  document.querySelector<HTMLButtonElement>('[data-workspace-path="app.ts"]')?.click();
  await flushTimers();
});
await waitFor("tree separator", () => document.querySelector(".workspace-tree-resizer") !== null);

const separator = document.querySelector<HTMLButtonElement>(".workspace-tree-resizer");
if (!separator) throw new Error("missing tree separator");
const initialWidth = Number(separator.getAttribute("aria-valuenow"));

await act(async () => {
  separator.dispatchEvent(new window.KeyboardEvent("keydown", { bubbles: true, cancelable: true, key: "ArrowRight" }));
  await flushTimers();
});
eq(Number(separator.getAttribute("aria-valuenow")), initialWidth - 16, "ArrowRight moves the separator right and narrows the right-side tree");

await act(async () => {
  separator.dispatchEvent(new window.KeyboardEvent("keydown", { bubbles: true, cancelable: true, key: "ArrowLeft" }));
  await flushTimers();
});
eq(Number(separator.getAttribute("aria-valuenow")), initialWidth, "ArrowLeft moves the separator left and widens the right-side tree");

await act(async () => {
  separator.dispatchEvent(new window.MouseEvent("pointerdown", { bubbles: true, cancelable: true, clientX: 600 }));
  window.dispatchEvent(new window.MouseEvent("pointermove", { bubbles: true, clientX: 650 }));
  window.dispatchEvent(new window.MouseEvent("pointerup", { bubbles: true, clientX: 650 }));
  await flushTimers();
});
eq(Number(separator.getAttribute("aria-valuenow")), 250, "pointer position measures tree width from the panel's right edge");

await act(async () => {
  separator.dispatchEvent(new window.KeyboardEvent("keydown", { bubbles: true, cancelable: true, key: "End" }));
  await flushTimers();
});
eq(Number(separator.getAttribute("aria-valuenow")), 660, "End widens the tree to keep only the preview minimum");

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
