// Run: tsx src/__tests__/workspace-changes-errors.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { WorkspacePanel } from "../components/WorkspacePanel";
import { LocaleProvider } from "../lib/i18n";
import type { AppBindings } from "../lib/bridge";
import type { DirEntry, GitCommitView, WorkspaceChangesView } from "../lib/types";

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
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.Event = dom.window.Event;
  globalThis.CustomEvent = dom.window.CustomEvent;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.PointerEvent = dom.window.MouseEvent as unknown as typeof PointerEvent;
  globalThis.MutationObserver = dom.window.MutationObserver;
  globalThis.ResizeObserver = TestResizeObserver;
  globalThis.localStorage = dom.window.localStorage;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  Object.defineProperty(dom.window.HTMLElement.prototype, "scrollIntoView", { configurable: true, value: () => {} });
  return dom;
}

async function renderWorkspace(
  changes: WorkspaceChangesView,
  options: { creationMode?: boolean; history?: GitCommitView[] } = {},
) {
  const dom = installDom();
  window.go = {
    main: {
      App: {
        ListDirForTab: async () => [],
        WorkspaceGitHistory: async () => options.history ?? [],
        WorkspaceChanges: async () => changes,
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
  const dom = installDom();
  window.go = {
    main: {
      App: {
        ListDirForTab: async () => [],
        SearchFileRefsForTab: async () => [],
        WorkspaceGitHistory: async () => [],
        WorkspaceChanges: async () => ({ files: [], gitAvailable: true }),
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

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
