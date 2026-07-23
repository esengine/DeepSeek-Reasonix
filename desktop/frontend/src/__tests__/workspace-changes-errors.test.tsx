// Run: tsx src/__tests__/workspace-changes-errors.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { workspaceFileIcon } from "../components/WorkspaceFileIcon";
import { WorkspacePanel } from "../components/WorkspacePanel";
import { LocaleProvider } from "../lib/i18n";
import { resetWorkspaceTreeMemoryForTests } from "../lib/workspaceTreeMemory";
import type { AppBindings } from "../lib/bridge";
import type { DirEntry, GitCommitView, WorkspaceChangeDetailView, WorkspaceChangesView } from "../lib/types";

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

function flushPromises(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 20; attempt += 1) {
    await act(async () => {
      await flushPromises();
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

async function renderWorkspace(
  changes: WorkspaceChangesView,
  options: { creationMode?: boolean; history?: GitCommitView[]; detail?: WorkspaceChangeDetailView } = {},
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

async function renderFilesWorkspace(methods: Partial<AppBindings>, props: Partial<Parameters<typeof WorkspacePanel>[0]> = {}) {
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

console.log("\nworkspace changes git errors");

{
  const { dom, root } = await renderWorkspace({ files: [], gitAvailable: false });
  await waitFor("git unavailable warning", () => document.body.textContent?.includes("Git status is unavailable for this workspace.") === true);
  ok(document.body.textContent?.includes("Git status is unavailable for this workspace.") === true, "gitAvailable=false renders a warning");
  ok(document.body.textContent?.includes("No changed files") === false, "gitAvailable=false is not shown as a clean workspace");
  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const { dom, root } = await renderWorkspace({
    files: [],
    gitAvailable: true,
    gitErr: "git status timed out",
  });
  await waitFor("git error warning without files", () => document.body.textContent?.includes("Git status is unavailable for this workspace.") === true);
  ok(document.body.textContent?.includes("Git status is unavailable for this workspace.") === true, "gitErr without files renders a warning");
  ok(document.body.textContent?.includes("No changed files") === false, "empty files plus gitErr is not shown as a clean workspace");
  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const { dom, root } = await renderWorkspace({
    files: [
      {
        path: "src/app.ts",
        sources: ["session"],
        gitStatus: "modified",
        latestPrompt: "edit app",
      },
    ],
    gitAvailable: true,
    gitErr: "git status timed out",
  });
  await waitFor("git error warning with files", () => document.body.textContent?.includes("app.ts") === true);
  ok(document.body.textContent?.includes("Git status is unavailable for this workspace.") === true, "gitErr renders a warning");
  ok(document.body.textContent?.includes("app.ts") === true, "files still render when gitErr is present");
  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const { dom, root } = await renderWorkspace(
    {
      files: [
        { path: "src/session.ts", sources: ["session"], gitStatus: "M", latestPrompt: "edit session file" },
        { path: "README.md", sources: ["git"], gitStatus: "M" },
      ],
      gitAvailable: true,
    },
    {
      creationMode: true,
      history: [{ hash: "1234567890", author: "Agent", date: "2026-07-10T12:00:00Z", message: "older commit" }],
    },
  );
  await waitFor("creation changes sections", () => document.body.textContent?.includes("Session changes") === true);
  ok(document.body.textContent?.includes("Session changes") === true, "Creation changes prioritizes session files");
  ok(document.body.textContent?.includes("Uncommitted workspace changes") === true, "Creation changes keeps git-only files separate");
  ok(document.body.textContent?.includes("Commit history") === true, "Creation changes exposes commit history as a secondary section");
  ok(document.body.textContent?.includes("older commit") === false, "Creation commit history starts collapsed");
  const historyToggle = document.querySelector<HTMLButtonElement>(".workspace-commit-history__toggle");
  await act(async () => {
    historyToggle?.dispatchEvent(new window.MouseEvent("click", { bubbles: true }));
    await flushPromises();
  });
  await waitFor("expanded creation commit history", () => document.body.textContent?.includes("older commit") === true);
  ok(document.body.textContent?.includes("older commit") === true, "Creation commit history expands on demand");
  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const { dom, root } = await renderWorkspace(
    {
      files: [{ path: "src/current.ts", sources: ["git"], gitStatus: "M" }],
      gitAvailable: true,
    },
    {
      history: [{ hash: "abcdef123456", author: "Agent", date: "2026-07-20T12:00:00Z", message: "historical commit" }],
      detail: {
        source: "git",
        added: 2,
        removed: 1,
        diff: "diff --git a/src/current.ts b/src/current.ts\n--- a/src/current.ts\n+++ b/src/current.ts\n@@ -10,2 +10,3 @@\n-old value\n+new value\n context\n+another value",
      },
    },
  );
  await waitFor("git-only working change", () => document.body.textContent?.includes("current.ts") === true);
  ok(document.body.textContent?.includes("No changed files") === false, "git-only working changes are not reported as a clean workspace");
  const changeButton = document.querySelector<HTMLButtonElement>(".workspace-change");
  await act(async () => {
    changeButton?.dispatchEvent(new window.MouseEvent("click", { bubbles: true }));
    await flushPromises();
  });
  await waitFor("current semantic diff", () => document.body.textContent?.includes("new value") === true);
  ok(document.body.textContent?.includes("Current changes") === true, "selected working file shows the current patch before history");
  ok(document.body.textContent?.includes("+2") === true && document.body.textContent?.includes("-1") === true, "current patch shows added and removed line totals");
  ok(document.body.textContent?.includes("historical commit") === false, "file commit history starts collapsed");
  const historyToggle = document.querySelector<HTMLButtonElement>(".workspace-commit-history__toggle");
  await act(async () => {
    historyToggle?.dispatchEvent(new window.MouseEvent("click", { bubbles: true }));
    await flushPromises();
  });
  await waitFor("file commit history", () => document.body.textContent?.includes("historical commit") === true);
  ok(document.body.textContent?.includes("historical commit") === true, "file commit history remains available on demand");
  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const { dom, root } = await renderWorkspace(
    {
      files: [{ path: "generated/large.txt", sources: ["git"], gitStatus: "M" }],
      gitAvailable: true,
    },
    { detail: { source: "git", truncated: true } },
  );
  await waitFor("large working change", () => document.body.textContent?.includes("large.txt") === true);
  const changeButton = document.querySelector<HTMLButtonElement>(".workspace-change");
  await act(async () => {
    changeButton?.dispatchEvent(new window.MouseEvent("click", { bubbles: true }));
    await flushPromises();
  });
  await waitFor("bounded change detail", () => document.body.textContent?.includes("too large to display") === true);
  ok(document.body.textContent?.includes("too large to display") === true, "oversized workspace diffs render a bounded-state message");
  ok(document.body.textContent?.includes("no text diff") === false, "oversized workspace diffs are not reported as empty");
  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const { dom, root } = await renderFilesWorkspace({
    ListDirForTab: async (_tabId, dir) => {
      if (dir === "") {
        return [
          { name: "src", isDir: true },
          { name: "tail-a.ts", isDir: false },
          { name: "tail-b.ts", isDir: false },
        ];
      }
      if (dir === "src/") {
        return [
          { name: "child-a.ts", isDir: false },
          { name: "child-b.ts", isDir: false },
        ];
      }
      return [];
    },
  });

  const positionedRows = () =>
    Array.from(document.querySelectorAll<HTMLElement>(".workspace-tree__sizer > div")).map((wrapper) => ({
      path: wrapper.querySelector<HTMLElement>("[data-workspace-path]")?.dataset.workspacePath ?? "",
      transform: wrapper.style.transform,
    }));
  const positionsAreUnique = (paths: string[]) => {
    const rows = positionedRows().filter((row) => paths.includes(row.path));
    return rows.length === paths.length && new Set(rows.map((row) => row.transform)).size === paths.length;
  };

  const collapsedPaths = ["src/", "tail-a.ts", "tail-b.ts"];
  await waitFor("initial positioned workspace rows", () => positionsAreUnique(collapsedPaths));

  const toggleSrc = () => document.querySelector<HTMLButtonElement>('[data-workspace-path="src/"]');
  await act(async () => {
    toggleSrc()?.dispatchEvent(new window.MouseEvent("click", { bubbles: true }));
    await flushPromises();
  });
  const expandedPaths = ["src/", "src/child-a.ts", "src/child-b.ts", "tail-a.ts", "tail-b.ts"];
  await waitFor("expanded workspace rows", () => document.body.textContent?.includes("child-b.ts") === true);
  ok(positionsAreUnique(expandedPaths), "expanded workspace rows keep unique virtual positions");

  await act(async () => {
    toggleSrc()?.dispatchEvent(new window.MouseEvent("click", { bubbles: true }));
    await flushPromises();
  });
  await waitFor("collapsed workspace rows", () => document.body.textContent?.includes("child-a.ts") === false);
  ok(positionsAreUnique(collapsedPaths), "collapsed workspace rows keep unique virtual positions");

  await act(async () => {
    toggleSrc()?.dispatchEvent(new window.MouseEvent("click", { bubbles: true }));
    await flushPromises();
  });
  await waitFor("re-expanded workspace rows", () => document.body.textContent?.includes("child-b.ts") === true);
  ok(positionsAreUnique(expandedPaths), "re-expanded workspace rows keep unique virtual positions");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const calls: string[] = [];
  const listDirForTab = async (tabId: string, dir: string): Promise<DirEntry[]> => {
    calls.push(`${tabId}:${dir}`);
    return [];
  };
  const { dom, root, rerender } = await renderFilesWorkspace(
    { ListDirForTab: listDirForTab },
    { fileListRequest: { id: 1, paths: ["src/app.ts"] } },
  );

  await waitFor("initial referenced file dirs", () => calls.filter((call) => call === "tab-a:src/").length === 1);
  await rerender({ fileListRequest: { id: 2, paths: ["src/app.ts"] } });
  await waitFor("referenced file dirs revalidated", () => calls.filter((call) => call === "tab-a:src/").length === 2);

  ok(calls.filter((call) => call === "tab-a:src/").length === 2, "workspace file tree revalidates cached directories for repeated file-list requests");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const pending: Array<{ tabId: string; resolve: (entries: DirEntry[]) => void }> = [];
  const listDirForTab = (tabId: string, dir: string): Promise<DirEntry[]> => {
    if (dir !== "") return Promise.resolve([]);
    return new Promise((resolve) => pending.push({ tabId, resolve }));
  };
  const { dom, root, rerender } = await renderFilesWorkspace(
    { ListDirForTab: listDirForTab },
    { tabId: "parent-tab", cwd: "/repo" },
  );

  await waitFor("parent workspace request", () => pending.some((request) => request.tabId === "parent-tab"));
  await rerender({ tabId: "child-tab", cwd: "/repo/child" });
  await waitFor("child workspace request", () => pending.some((request) => request.tabId === "child-tab"));

  await act(async () => {
    pending.filter((request) => request.tabId === "child-tab").forEach((request) => request.resolve([
      { name: "child-a.txt", isDir: false },
      { name: "child-b.txt", isDir: false },
    ]));
    await flushPromises();
  });
  await waitFor("child workspace entries", () => (document.querySelector(".workspace-tree__sizer") as HTMLElement | null)?.style.height === "48px");

  await act(async () => {
    pending.filter((request) => request.tabId === "parent-tab").forEach((request) => request.resolve([{ name: "parent-only.txt", isDir: false }]));
    await flushPromises();
  });

  ok((document.querySelector(".workspace-tree__sizer") as HTMLElement | null)?.style.height === "48px", "late parent workspace response cannot overwrite the two-row child tree");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const pending: Array<(entries: DirEntry[]) => void> = [];
  const listDirForTab = (_tabId: string, dir: string): Promise<DirEntry[]> => {
    if (dir !== "") return Promise.resolve([]);
    return new Promise((resolve) => pending.push(resolve));
  };
  const { dom, root, rerender } = await renderFilesWorkspace(
    { ListDirForTab: listDirForTab },
    { tabId: "shared-tab", cwd: "/repo", workspaceScopeKey: "session-a" },
  );

  await waitFor("initial session A workspace request", () => pending.length === 1);
  await rerender({ workspaceScopeKey: "session-b" });
  await waitFor("session B workspace request", () => pending.length === 2);
  await rerender({ workspaceScopeKey: "session-a" });
  await waitFor("revisited session A workspace request", () => pending.length === 3);

  await act(async () => {
    pending[2]([
      { name: "current-a.txt", isDir: false },
      { name: "current-b.txt", isDir: false },
    ]);
    await flushPromises();
  });
  await waitFor("revisited session A entries", () => (document.querySelector(".workspace-tree__sizer") as HTMLElement | null)?.style.height === "48px");

  await act(async () => {
    pending[0]([{ name: "stale-initial-a.txt", isDir: false }]);
    pending[1]([{ name: "stale-b.txt", isDir: false }]);
    await flushPromises();
  });

  ok(
    (document.querySelector(".workspace-tree__sizer") as HTMLElement | null)?.style.height === "48px",
    "same-tab A→B→A session switches reject stale workspace responses",
  );

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const pending: Array<(changes: WorkspaceChangesView) => void> = [];
  const workspaceChanges = (): Promise<WorkspaceChangesView> => new Promise((resolve) => pending.push(resolve));
  const { dom, root, rerender } = await renderFilesWorkspace(
    { WorkspaceChanges: workspaceChanges },
    {
      tabId: "shared-tab",
      cwd: "/repo",
      workspaceScopeKey: "session-a",
      initialViewMode: "changed",
    },
  );

  await waitFor("initial session changes request", () => pending.length === 1);
  await rerender({ workspaceScopeKey: "session-b" });
  await waitFor("next session changes request", () => pending.length === 2);

  await act(async () => {
    pending[1]({
      files: [{ path: "session-b.ts", sources: ["session"] }],
      gitAvailable: true,
    });
    await flushPromises();
  });
  await waitFor("session B changes", () => document.body.textContent?.includes("session-b.ts") === true);

  await act(async () => {
    pending[0]({
      files: [{ path: "stale-session-a.ts", sources: ["session"] }],
      gitAvailable: true,
    });
    await flushPromises();
  });

  ok(document.body.textContent?.includes("session-b.ts") === true, "current same-tab session changes stay visible");
  ok(document.body.textContent?.includes("stale-session-a.ts") === false, "late same-tab session changes cannot overwrite the current session");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const pending = new Map<string, (detail: WorkspaceChangeDetailView) => void>();
  const workspaceChangeDetail = (_tabID: string, path: string): Promise<WorkspaceChangeDetailView> =>
    new Promise((resolve) => pending.set(path, resolve));
  const { dom, root, rerender } = await renderFilesWorkspace(
    {
      WorkspaceChanges: async () => ({
        files: [
          { path: "session-a.ts", sources: ["session"] },
          { path: "session-b.ts", sources: ["session"] },
        ],
        gitAvailable: true,
      }),
      WorkspaceChangeDetail: workspaceChangeDetail,
    },
    {
      tabId: "shared-tab",
      cwd: "/repo",
      workspaceScopeKey: "session-a",
      initialViewMode: "changed",
      changeRevealRequest: { id: 1, path: "session-a.ts" },
    },
  );

  await waitFor("session A change detail request", () => pending.has("session-a.ts"));
  await rerender({ workspaceScopeKey: "session-b", changeRevealRequest: { id: 2, path: "session-b.ts" } });
  await waitFor("session B change detail request", () => pending.has("session-b.ts"));

  await act(async () => {
    pending.get("session-b.ts")?.({
      source: "session",
      added: 1,
      diff: "--- a/session-b.ts\n+++ b/session-b.ts\n@@ -1 +1 @@\n-old-b\n+current-b",
    });
    await flushPromises();
  });
  await waitFor("current session B detail", () => document.body.textContent?.includes("current-b") === true);

  await act(async () => {
    pending.get("session-a.ts")?.({
      source: "session",
      added: 1,
      diff: "--- a/session-a.ts\n+++ b/session-a.ts\n@@ -1 +1 @@\n-old-a\n+stale-a",
    });
    await flushPromises();
  });
  ok(document.body.textContent?.includes("current-b") === true, "same-tab session switch keeps the current change detail");
  ok(document.body.textContent?.includes("stale-a") === false, "late change detail cannot overwrite the current session");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  // A keyboard tab switch fires no mousedown/scroll/Escape, so floating menus
  // that captured the previous scope's text/paths must be discarded when the
  // tab/scope changes — otherwise Add to Chat would route the old scope's
  // selection into the newly active session.
  const { dom, root, rerender } = await renderFilesWorkspace(
    {
      ListDirForTab: async () => [{ name: "app.ts", isDir: false }],
      ReadFileForTab: async () => ({
        path: "app.ts",
        body: "const value = 1;",
        size: 16,
        truncated: false,
        binary: false,
      }),
    },
    { revealPathRequest: { id: 1, path: "app.ts" } },
  );

  await waitFor("code preview", () => document.body.textContent?.includes("const value = 1;") === true);
  const previewBody = document.querySelector(".workspace-preview__body") as HTMLElement;
  const textNode = document.createTreeWalker(previewBody, 4 /* NodeFilter.SHOW_TEXT */).nextNode();
  if (!textNode) throw new Error("preview rendered no text node to select");
  const range = document.createRange();
  range.selectNodeContents(textNode);
  const selection = document.getSelection();
  selection?.removeAllRanges();
  selection?.addRange(range);
  await act(async () => {
    previewBody.dispatchEvent(new window.MouseEvent("mouseup", { bubbles: true, clientX: 60, clientY: 60 }));
    await flushPromises();
  });
  ok(document.querySelector(".floating-menu") != null, "selecting preview code pops the Add to Chat toolbar");
  await rerender({ tabId: "tab-b" });
  ok(document.querySelector(".floating-menu") == null, "a tab switch discards the selection toolbar");

  const tree = document.querySelector(".workspace-tree") as HTMLElement;
  await act(async () => {
    tree.dispatchEvent(new window.MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 30, clientY: 200 }));
    await flushPromises();
  });
  ok(document.querySelector(".context-menu") != null, "right-clicking blank tree space opens the tree menu");
  await rerender({ workspaceScopeKey: "scope-b" });
  ok(document.querySelector(".context-menu") == null, "a scope switch discards the tree menu");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const { dom, root, rerender } = await renderFilesWorkspace(
    {
      ListDirForTab: async (_tabId, dir) => {
        if (dir === "") {
          return [
            { name: "alpha", isDir: true },
            { name: "beta", isDir: true },
          ];
        }
        if (dir === "alpha/") {
          return [
            { name: "nested", isDir: true },
            { name: "alpha.txt", isDir: false },
          ];
        }
        if (dir === "alpha/nested/") return [{ name: "deep.ts", isDir: false }];
        if (dir === "beta/") return [{ name: "beta.txt", isDir: false }];
        return [];
      },
    },
    {
      workspaceScopeKey: "scope-a",
      workspaceMemoryKey: "session-a",
      workspaceMemoryVisitId: 1,
    },
  );

  const clickPath = async (path: string) => {
    const row = document.querySelector<HTMLButtonElement>(`[data-workspace-path="${path}"]`);
    if (!row) throw new Error(`missing workspace row ${path}`);
    await act(async () => {
      row.dispatchEvent(new window.MouseEvent("click", { bubbles: true }));
      await flushPromises();
    });
  };

  await waitFor("session A roots", () => document.querySelector('[data-workspace-path="alpha/"]') != null);
  await clickPath("alpha/");
  await waitFor("session A nested directory", () => document.querySelector('[data-workspace-path="alpha/nested/"]') != null);
  await clickPath("alpha/nested/");
  await waitFor("session A deep file", () => document.querySelector('[data-workspace-path="alpha/nested/deep.ts"]') != null);
  await clickPath("beta/");
  await waitFor("session A beta file", () => document.querySelector('[data-workspace-path="beta/beta.txt"]') != null);

  await rerender({ initialViewMode: "changed" });
  await rerender({ initialViewMode: "files" });
  await waitFor("same-session restored tree", () => document.querySelector('[data-workspace-path="alpha/nested/deep.ts"]') != null);
  ok(
    document.querySelector('[data-workspace-path="beta/beta.txt"]') != null,
    "Files → Changes → Files preserves the exact expanded tree in one session",
  );

  await rerender({
    workspaceScopeKey: "scope-b",
    workspaceMemoryKey: "session-b",
    workspaceMemoryVisitId: 2,
  });
  await waitFor("session B roots", () => document.querySelector('[data-workspace-path="alpha/"]') != null);
  await rerender({
    workspaceScopeKey: "scope-a-returned",
    workspaceMemoryKey: "session-a",
    workspaceMemoryVisitId: 3,
  });
  await waitFor("returned session A roots", () => document.querySelector('[data-workspace-path="alpha/"]') != null);
  ok(
    document.querySelector('[data-workspace-path="alpha/nested/"]') == null &&
      document.querySelector('[data-workspace-path="beta/beta.txt"]') == null,
    "returning to a session presents every remembered root collapsed",
  );

  await clickPath("alpha/");
  await waitFor("restored alpha subtree", () => document.querySelector('[data-workspace-path="alpha/nested/deep.ts"]') != null);
  ok(
    document.querySelector('[data-workspace-path="beta/beta.txt"]') == null,
    "opening one returned root restores only that root's remembered subtree",
  );

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const { dom, root } = await renderFilesWorkspace({
    ListDirForTab: async (_tabId, dir) => {
      if (dir === "") return [{ name: "src", isDir: true }];
      if (dir === "src/") return [{ name: "main", isDir: true }];
      if (dir === "src/main/") return [{ name: "java", isDir: true }];
      if (dir === "src/main/java/") return [{ name: "App.java", isDir: false }];
      return [];
    },
  });

  await waitFor("compacted directory chain", () =>
    document.querySelector('[data-workspace-path="src/main/java/"]')?.textContent?.includes("src / main / java") === true,
  );
  ok(
    document.querySelectorAll('[data-workspace-path="src/main/java/"]').length === 1 &&
      document.querySelector('[data-workspace-path="src/"]') == null,
    "single-child directory chains render as one compact folder row",
  );

  await act(async () => {
    document
      .querySelector<HTMLButtonElement>('[data-workspace-path="src/main/java/"]')
      ?.dispatchEvent(new window.MouseEvent("click", { bubbles: true }));
    await flushPromises();
  });
  await waitFor("compact directory child", () => document.querySelector('[data-workspace-path="src/main/java/App.java"]') != null);
  ok(
    document.querySelectorAll('[data-workspace-path="src/main/java/App.java"] .workspace-tree__guide').length === 1,
    "nested file rows render one guide for each visible ancestor level",
  );
  ok(
    document.querySelector('[data-workspace-path="src/main/java/App.java"] .workspace-file-icon')?.textContent !== "",
    "workspace files render a Seti file-type icon",
  );

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const javaIcon = workspaceFileIcon("App.java");
  const markdownIcon = workspaceFileIcon("README.md");
  const mavenIcon = workspaceFileIcon("pom.xml");
  const xmlIcon = workspaceFileIcon("layout.xml");
  ok(javaIcon.glyph !== "" && javaIcon.glyph !== markdownIcon.glyph, "Seti icons distinguish common file extensions");
  ok(mavenIcon.glyph !== xmlIcon.glyph, "Seti exact-name mappings take precedence over generic extensions");
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
