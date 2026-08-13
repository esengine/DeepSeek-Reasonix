// Run: tsx src/__tests__/workspace-context-menu.test.tsx

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

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function flushTimers(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 20; attempt += 1) {
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

function installDom() {
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
  Object.defineProperty(dom.window.HTMLElement.prototype, "offsetWidth", { configurable: true, get: () => 320 });
  Object.defineProperty(dom.window.HTMLElement.prototype, "offsetHeight", {
    configurable: true,
    get: function offsetHeight(this: HTMLElement) {
      return this.classList.contains("workspace-tree") ? 300 : this.dataset.index ? 24 : 0;
    },
  });
  Object.defineProperty(dom.window.HTMLElement.prototype, "getBoundingClientRect", {
    configurable: true,
    value: function getBoundingClientRect(this: HTMLElement) {
      const width = 320;
      const height = this.classList.contains("workspace-tree") ? 300 : this.dataset.index ? 24 : 0;
      return { x: 0, y: 0, top: 0, left: 0, right: width, bottom: height, width, height, toJSON: () => ({}) } as DOMRect;
    },
  });
  return dom;
}

function menuLabels(): string[] {
  return Array.from(document.querySelectorAll<HTMLButtonElement>(".workspace-tree-menu button")).map(
    (button) => button.textContent?.trim() ?? "",
  );
}

async function openRowMenu(path: string) {
  const row = document.querySelector<HTMLButtonElement>(`[data-workspace-path="${path}"]`);
  if (!row) throw new Error(`missing workspace row ${path}`);
  await act(async () => {
    row.dispatchEvent(new window.MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 30, clientY: 30 }));
    await flushTimers();
  });
}

console.log("\nworkspace file context menu");

resetWorkspaceTreeMemoryForTests();
const dom = installDom();
const openCalls: Array<{ tabId: string; path: string }> = [];
window.go = {
  main: {
    App: {
      ListDirForTab: async (_tabId, dir) => dir === ""
        ? [
            { name: "docs", isDir: true },
            { name: "README.md", isDir: false },
          ]
        : [],
      SearchFileRefsForTab: async () => [],
      WorkspaceGitHistory: async () => [],
      WorkspaceChanges: async () => ({ files: [], gitAvailable: true }),
      WorkspaceChangeDetail: async () => ({}),
      ReadFileForTab: async (_tabId, path) => ({ path, body: "", size: 0, truncated: false, binary: false }),
      ResolveWorkspacePathForTab: async (_tabId, path) => `/repo/${path}`,
      RevealWorkspacePathForTab: async () => {},
      OpenWorkspacePathForTab: async (tabId, path) => {
        openCalls.push({ tabId, path });
      },
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
        tabId="workspace-tab"
        cwd="/repo"
        maximized={false}
        initialViewMode="files"
        onClose={() => {}}
        onToggleMaximized={() => {}}
        onOpenInTerminal={() => {}}
      />
    </LocaleProvider>,
  );
  await flushTimers();
});

await waitFor("workspace rows", () => document.querySelector('[data-workspace-path="README.md"]') != null);
await openRowMenu("README.md");

const fileLabels = [
  "Open with default app",
  "Show in file manager",
  "Open in integrated terminal",
  "Copy relative path",
  "Copy absolute path",
  "Add file reference",
  "Add file contents",
];
ok(JSON.stringify(menuLabels()) === JSON.stringify(fileLabels), "file menu keeps the default-open action first and preserves command order");
ok(document.querySelectorAll(".workspace-tree-menu [role=separator]").length === 1, "file menu separates path commands from chat commands");

const defaultOpen = Array.from(document.querySelectorAll<HTMLButtonElement>(".workspace-tree-menu button")).find(
  (button) => button.textContent?.trim() === "Open with default app",
);
await act(async () => {
  defaultOpen?.click();
  await flushTimers();
});
ok(
  JSON.stringify(openCalls) === JSON.stringify([{ tabId: "workspace-tab", path: "README.md" }]),
  "default-open routes the selected relative file path through the active workspace tab",
);
ok(document.querySelector(".workspace-tree-menu") == null, "default-open closes the file menu");

await openRowMenu("docs/");
ok(!menuLabels().includes("Open with default app"), "folder menu does not offer default-open");
ok(
  JSON.stringify(menuLabels()) === JSON.stringify([
    "Show in file manager",
    "Open in integrated terminal",
    "Copy relative path",
    "Copy absolute path",
    "Add folder reference",
  ]),
  "folder menu preserves its existing command order",
);
ok(document.querySelectorAll(".workspace-tree-menu [role=separator]").length === 1, "folder menu keeps one chat-command separator");

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
