import { JSDOM } from "jsdom";
import { registerHooks } from "node:module";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { WorkspacePanel } from "../components/WorkspacePanel";
import type { AppBindings } from "../lib/bridge";
import { LocaleProvider } from "../lib/i18n";
import type { GitCommitView, WireCompletionSummary, WorkspaceChangeDetailView, WorkspaceChangesView } from "../lib/types";
import { resetWorkspaceTreeMemoryForTests } from "../lib/workspaceTreeMemory";

registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier.endsWith(".css")) {
      return nextResolve("./asset-stub-for-tests.ts", { ...context, parentURL: import.meta.url });
    }
    return nextResolve(specifier, context);
  },
});

class TestResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

export function flushPromises(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

export async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 20; attempt += 1) {
    await act(async () => {
      await flushPromises();
    });
    if (predicate()) return;
  }
  throw new Error(`timed out waiting for ${label}`);
}

function installDom() {
  const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
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
  (dom.window.HTMLElement.prototype as unknown as { attachEvent: () => void }).attachEvent = () => {};
  (dom.window.HTMLElement.prototype as unknown as { detachEvent: () => void }).detachEvent = () => {};
  Object.defineProperty(dom.window.HTMLElement.prototype, "scrollIntoView", { configurable: true, value: () => {} });
  Object.defineProperty(dom.window.HTMLElement.prototype, "offsetWidth", {
    configurable: true,
    get: () => 320,
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
      return {
        x: 0,
        y: 0,
        top: 0,
        left: 0,
        right: width,
        bottom: height,
        width,
        height,
        toJSON: () => ({}),
      } as DOMRect;
    },
  });
  return dom;
}

export async function renderWorkspace(
  changes: WorkspaceChangesView,
  options: { creationMode?: boolean; history?: GitCommitView[]; detail?: WorkspaceChangeDetailView; completionSummary?: WireCompletionSummary } = {},
) {
  resetWorkspaceTreeMemoryForTests();
  const dom = installDom();
  window.go = {
    main: {
      App: {
        ListDirForTab: async () => [],
        WorkspaceGitHistory: async () => options.history ?? [],
        WorkspaceChanges: async () => changes,
        WorkspaceChangeDetail: async () => options.detail ?? {},
        ReadFileForTab: async (_tabID, path) => ({ path, body: "", size: 0, truncated: false, binary: false }),
      } as Partial<AppBindings> as AppBindings,
    },
  };
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  await act(async () => {
    root.render(
      <LocaleProvider>
        <WorkspacePanel
          open
          tabId="tab-a"
          cwd="/repo"
          maximized={false}
          initialViewMode="changed"
          creationMode={options.creationMode}
          completionSummary={options.completionSummary}
          onClose={() => {}}
          onToggleMaximized={() => {}}
        />
      </LocaleProvider>,
    );
    await flushPromises();
  });
  await waitFor("workspace changes", () => Boolean(document.querySelector(".workspace-preview__body")));
  return { dom, root };
}

export async function renderFilesWorkspace(methods: Partial<AppBindings>, props: Partial<Parameters<typeof WorkspacePanel>[0]> = {}) {
  resetWorkspaceTreeMemoryForTests();
  const dom = installDom();
  window.go = {
    main: {
      App: {
        ListDirForTab: async () => [],
        SearchFileRefsForTab: async () => [],
        WorkspaceGitHistory: async () => [],
        WorkspaceChanges: async () => ({ files: [], gitAvailable: true }),
        WorkspaceChangeDetail: async () => ({}),
        ReadFileForTab: async (_tabID, path) => ({ path, body: "", size: 0, truncated: false, binary: false }),
        ...methods,
      } as Partial<AppBindings> as AppBindings,
    },
  };
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  let currentProps: Parameters<typeof WorkspacePanel>[0] = {
    open: true,
    tabId: "tab-a",
    cwd: "/repo",
    maximized: false,
    initialViewMode: "files",
    onClose: () => {},
    onToggleMaximized: () => {},
    ...props,
  };
  const rerender = async (nextProps: Partial<Parameters<typeof WorkspacePanel>[0]> = {}) => {
    currentProps = { ...currentProps, ...nextProps };
    await act(async () => {
      root.render(
        <LocaleProvider>
          <WorkspacePanel {...currentProps} />
        </LocaleProvider>,
      );
      await flushPromises();
    });
  };
  await rerender();
  return { dom, root, rerender };
}
