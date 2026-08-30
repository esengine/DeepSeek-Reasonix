// Run: tsx src/__tests__/workspace-changes-errors.test.tsx

import { act } from "react";
import { workspaceFileIcon } from "../components/WorkspaceFileIcon";
import type { DirEntry, FilePreview, WorkspaceChangeDetailView, WorkspaceChangesView } from "../lib/types";
import { flushPromises, renderFilesWorkspace, renderWorkspace, waitFor } from "./workspace-panel-test-harness";

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

console.log("\nworkspace changes git errors");

{
  const { dom, root, rerender } = await renderFilesWorkspace({}, { open: false });
  let threw = false;
  try {
    await rerender({ open: true });
  } catch (error) {
    threw = true;
    process.stdout.write(`  ERROR ${String(error)}\n`);
  }
  ok(!threw, "workspace panel can open after a closed render without changing hook order");
  ok(document.querySelector(".workspace-panel") !== null, "workspace panel renders after the closed-to-open transition");
  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

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
  const { dom, root } = await renderWorkspace(
    { files: [], gitAvailable: true },
    {
      completionSummary: {
        preset: "balanced",
        verdict: "partial",
        mutations: 3,
        checks_passed: 12,
        checks_failed: 1,
        checks_suppressed: 2,
        review: "passed",
        gap_kinds: ["stale_check", "future_internal_value"],
        constraint_degraded: true,
      },
    },
  );
  await waitFor("turn verification summary", () => document.body.textContent?.includes("Turn verification") === true);
  const text = document.querySelector(".workspace-completion-summary")?.textContent ?? "";
  ok(text.includes("Partially complete"), "change panel localizes the completion verdict");
  ok(text.includes("1 checks failed") && text.includes("2 checks skipped"), "change panel shows detailed check counts on demand");
  ok(text.includes("stale checks") && text.includes("Other"), "change panel uses safe labels for known and unknown gaps");
  ok(text.includes("Turn verification limited"), "change panel explains constrained verification without exposing an internal flag");
  ok(!text.includes("balanced") && !text.includes("partial") && !text.includes("stale_check") && !text.includes("future_internal_value"), "change panel exposes no raw enum values");
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
  let resolveCodePreview!: (preview: FilePreview) => void;
  const codePreview = new Promise<FilePreview>((resolve) => { resolveCodePreview = resolve; });
  const { dom, root } = await renderFilesWorkspace({
    ListDirForTab: async (_tabId, dir) => dir === ""
      ? [
          { name: "code.ts", isDir: false },
          { name: "README.md", isDir: false },
        ]
      : [],
    ReadFileForTab: async (_tabId, path) => path === "README.md"
      ? { path, body: "# Documentation", size: 15, truncated: false, binary: false }
      : codePreview,
  });

  await waitFor("searchable code file", () => document.querySelector('[data-workspace-path="code.ts"]') != null);
  await act(async () => {
    document
      .querySelector<HTMLButtonElement>('[data-workspace-path="code.ts"]')
      ?.dispatchEvent(new window.MouseEvent("click", { bubbles: true }));
    await flushPromises();
  });
  await waitFor("pending code preview layout", () => document.querySelector(".workspace-preview__body--code") != null);
  ok(document.querySelector(".workspace-preview__body--code") != null, "code preview uses its final layout while loading");
  await act(async () => {
    resolveCodePreview({ path: "code.ts", body: "const value = 42;", size: 17, truncated: false, binary: false });
    await flushPromises();
  });
  await waitFor("code preview search action", () => document.querySelector('button[aria-label="Find"]') != null);
  ok(document.querySelector(".workspace-preview__body--code") != null, "resolved code preview keeps the loading layout");
  ok(document.querySelector('button[aria-label="Find"]') != null, "searchable code previews expose a visible search action");

  const filterInput = document.querySelector<HTMLInputElement>('input[placeholder="Filter files…"]');
  await act(async () => {
    filterInput?.focus();
    filterInput?.dispatchEvent(new window.KeyboardEvent("keydown", {
      key: "f",
      ctrlKey: true,
      bubbles: true,
      cancelable: true,
    }));
    await flushPromises();
  });
  await waitFor("panel-scoped code search", () => document.querySelector(".code-search__input") != null);
  ok(document.activeElement === document.querySelector(".code-search__input"), "workspace find shortcut opens and focuses code search from the file filter");

  await act(async () => {
    document
      .querySelector<HTMLButtonElement>(".code-search__close")
      ?.dispatchEvent(new window.MouseEvent("click", { bubbles: true }));
    await flushPromises();
  });
  await waitFor("closed code search", () => document.querySelector(".code-search") == null);
  await act(async () => {
    document
      .querySelector<HTMLButtonElement>('button[aria-label="Find"]')
      ?.dispatchEvent(new window.MouseEvent("click", { bubbles: true }));
    await flushPromises();
  });
  await waitFor("button-opened code search", () => document.querySelector(".code-search__input") != null);
  ok(document.activeElement === document.querySelector(".code-search__input"), "visible search action opens and focuses the same search UI");

  await act(async () => {
    document
      .querySelector<HTMLButtonElement>('[data-workspace-path="README.md"]')
      ?.dispatchEvent(new window.MouseEvent("click", { bubbles: true }));
    await flushPromises();
  });
  await waitFor("markdown preview", () => document.body.textContent?.includes("Documentation") === true);
  ok(document.querySelector('button[aria-label="Find"]') == null, "Markdown previews do not expose the code-search action");
  const markdownFindEvent = new window.KeyboardEvent("keydown", {
    key: "f",
    ctrlKey: true,
    bubbles: true,
    cancelable: true,
  });
  filterInput?.dispatchEvent(markdownFindEvent);
  ok(!markdownFindEvent.defaultPrevented, "Markdown previews preserve the host find shortcut");

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
