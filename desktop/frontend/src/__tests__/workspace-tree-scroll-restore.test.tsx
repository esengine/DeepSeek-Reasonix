// Run: tsx src/__tests__/workspace-tree-scroll-restore.test.tsx
// zk-ge CLAIM.TREE.008: 切换 dock 标签（组件 unmount/remount）后文件树滚动位置恢复

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import type { Root } from "react-dom/client";
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
  Object.defineProperty(dom.window.HTMLElement.prototype, "clientHeight", {
    configurable: true,
    get: function clientHeight(this: HTMLElement) {
      return this.classList.contains("workspace-tree") ? 300 : 0;
    },
  });
  Object.defineProperty(dom.window.HTMLElement.prototype, "scrollHeight", {
    configurable: true,
    get: function scrollHeight(this: HTMLElement) {
      return this.classList.contains("workspace-tree") ? 40 * 24 : 0;
    },
  });
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
  Object.defineProperty(dom.window.HTMLElement.prototype, "scrollTo", {
    configurable: true,
    value: function scrollTo(this: HTMLElement, options?: ScrollToOptions | number) {
      const requested = typeof options === "number" ? options : options?.top ?? 0;
      const maximum = Math.max(0, this.scrollHeight - this.clientHeight);
      this.scrollTop = Math.max(0, Math.min(maximum, requested));
    },
  });
  return dom;
}

const MEMORY_KEY = "scroll-test\u0000/repo";

function renderPanel(root: Root) {
  return act(async () => {
    root.render(
      <LocaleProvider>
        <WorkspacePanel
          open
          tabId="scroll-tab"
          cwd="/repo"
          workspaceMemoryKey={MEMORY_KEY}
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
}

console.log("\nworkspace tree scroll restore (CLAIM.TREE.008)");

resetWorkspaceTreeMemoryForTests();
const dom = installDom();
dom.window.localStorage.clear();
const RESTORED_SCROLL_TOP = 200;
dom.window.localStorage.setItem("reasonix.workspaceState.v2", JSON.stringify({
  version: 2,
  projects: [{
    key: MEMORY_KEY,
    state: {
      openDirs: [""],
      scrollTop: RESTORED_SCROLL_TOP,
      updatedAt: Date.now(),
    },
  }],
}));

window.go = {
  main: {
    App: {
      ListDirForTab: async (_tabId, dir) => {
        if (dir === "") {
          return Array.from({ length: 40 }, (_, i) => ({ name: `file-${i}.ts`, isDir: false }));
        }
        return [];
      },
      SearchFileRefsForTab: async () => [],
      WorkspaceGitHistory: async () => [],
      WorkspaceChanges: async () => ({ files: [], gitAvailable: true }),
      WorkspaceChangeDetail: async () => ({}),
      ReadFileForTab: async (_tabId, path) => ({ path, body: "", size: 0, truncated: false, binary: false }),
      ResolveWorkspacePathForTab: async (_tabId, path) => `/repo/${path}`,
      RevealWorkspacePathForTab: async () => {},
      OpenWorkspacePathForTab: async () => {},
    } as Partial<AppBindings> as AppBindings,
  },
};

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);

// Phase 1: mount from a persisted deep offset and verify the virtualizer writes
// that offset into a genuinely scrollable DOM fixture.
await renderPanel(root);
await waitFor("workspace rows render", () => document.querySelectorAll(".workspace-tree__row").length > 0);

const treeEl = document.querySelector<HTMLElement>(".workspace-tree");
if (!treeEl) throw new Error("missing workspace-tree element");
ok(treeEl.scrollHeight > treeEl.clientHeight, "fixture has a real scrollable extent");
await waitFor("persisted tree offset restore", () => treeEl.scrollTop === RESTORED_SCROLL_TOP);
ok(treeEl.scrollTop === RESTORED_SCROLL_TOP, "initial mount restores the persisted DOM scroll offset");

// Scroll position should have been persisted to the upstream per-project
// workspace state envelope (reasonix.workspaceState.v2).
function persistedScrollTop(): number | null {
  const raw = dom.window.localStorage.getItem("reasonix.workspaceState.v2");
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw) as { projects?: Array<{ key: string; state: { scrollTop?: number } }> };
    const project = parsed.projects?.find((entry) => entry.key === MEMORY_KEY);
    return typeof project?.state?.scrollTop === "number" ? project.state.scrollTop : null;
  } catch {
    return null;
  }
}

ok(persistedScrollTop() === RESTORED_SCROLL_TOP, "restoration preserves the persisted localStorage offset");

// Phase 2: unmount (simulates switching from 文件 to 概览 dock tab)
await act(async () => {
  root.render(<></>);
  await flushTimers();
});

// Phase 3: remount (simulates switching back to 文件)
await renderPanel(root);
await waitFor("workspace rows re-render", () => document.querySelectorAll(".workspace-tree__row").length > 0);

const treeEl2 = document.querySelector<HTMLElement>(".workspace-tree");
if (!treeEl2) throw new Error("missing workspace-tree element after remount");

// The persisted value must survive the remount — verify the workspace memory
// still holds it (this isolates persistence loss from virtualizer restore).
ok(persistedScrollTop() === RESTORED_SCROLL_TOP, "persisted scroll offset survives remount in localStorage");

// Remount must re-render the tree rows (otherwise the restore effect's
// treeRows.length guard never passes).
const rowsAfterRemount = document.querySelectorAll(".workspace-tree__row").length;
ok(rowsAfterRemount > 0, `tree rows re-render after remount (got ${rowsAfterRemount})`);

await waitFor("remounted tree DOM restore", () => treeEl2.scrollTop === RESTORED_SCROLL_TOP);
ok(treeEl2.scrollTop === RESTORED_SCROLL_TOP, "dock-tab remount restores the actual DOM scroll offset");

// Phase 4: switch to the "changed" view via the tab button, then back to
// "files". The pending scroll restore must be reset on viewMode change — the
// saved offset belongs to the previous mode's tree and re-applying it to a
// shorter list (files -> changed) would scroll past the end and leave a blank
// band at the top. We assert the panel still renders rows after the round
// trip; the reset itself is what makes that render coherent.
const tabs = document.querySelectorAll(".workspace-files__tab");
ok(tabs.length >= 2, "files and changed view tabs render in the fixture");
if (tabs.length >= 2) {
  await act(async () => {
    (tabs[1] as HTMLElement).click();
    await flushTimers();
  });
  await act(async () => {
    (tabs[0] as HTMLElement).click();
    await flushTimers();
  });
  await waitFor("files rows re-render after view-mode round trip", () => document.querySelectorAll(".workspace-tree__row").length > 0);
  ok(document.querySelectorAll(".workspace-tree__row").length > 0, "tree rows re-render after files<->changed view switch");
}

console.log(`\nworkspace tree scroll restore: ${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
