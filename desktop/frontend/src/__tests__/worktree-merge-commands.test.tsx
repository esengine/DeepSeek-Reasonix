import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { useWorktreeMergeCommands } from "../app-runtime/useWorktreeMergeCommands";
import type { TabMeta, WorktreeMergeResult } from "../lib/types";
import type { Translator } from "../lib/i18n";

const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);

const t = ((key: string) => key) as Translator;
const sourceTab = { id: "source-tab", workspaceRoot: "/source" } as TabMeta;
const worktreeTab = { id: "worktree-tab", workspaceRoot: "/worktree" } as TabMeta;
const receipt: WorktreeMergeResult = {
  merged: true,
  alreadyMerged: false,
  recoveryRequired: false,
  sourceRoot: "/source",
  targetBranch: "main",
  mergedCommit: "merge-head",
  worktreeRoot: "/worktree",
  worktreeBranch: "reasonix/delivery-test",
  worktreeHead: "worktree-head",
};

const toasts: string[] = [];
const cleanups: unknown[] = [];
const lifecycleCalls: string[] = [];
let navigationToken: string | null = "nav-token";
let navigationCurrent = true;
let staleAfterEnsure = false;

let states!: ReturnType<typeof useWorktreeMergeCommands>;
function Probe() {
  states = useWorktreeMergeCommands({
    singleSurfaceLayout: false,
    noteNavigationIntent: () => 42,
    registeredNavigationIntent: async () => navigationToken,
    isNavigationIntentCurrent: () => navigationCurrent,
    ensureBlankSurface: async () => sourceTab,
    ensureBlankTab: async () => {
      lifecycleCalls.push("ensure");
      if (staleAfterEnsure) navigationCurrent = false;
      return sourceTab;
    },
    seedSource: () => { lifecycleCalls.push("seed"); },
    listTabs: async () => { lifecycleCalls.push("list"); return [sourceTab, worktreeTab]; },
    closeWorktree: async () => { lifecycleCalls.push("close"); return { closed: true, idempotent: false }; },
    finalize: async () => {
      lifecycleCalls.push("finalize");
      return { completed: true, worktreeRemoved: true, branchDeleted: true, blockers: [] };
    },
    showToast: (message) => { toasts.push(message); },
    t,
    showCleanup: (cleanup) => { cleanups.push(cleanup); },
  });
  return null;
}

try {
  await act(async () => root.render(<Probe />));
  assert.equal(states.worktreeMergeTabId, null, "the merge overlay starts closed");

  await act(async () => { states.openWorktreeMerge("worktree-tab"); });
  assert.equal(states.worktreeMergeTabId, "worktree-tab", "the topicbar merge action opens the overlay for its tab");
  await act(async () => { states.closeWorktreeMerge(); });
  assert.equal(states.worktreeMergeTabId, null, "the overlay close command clears the tab");

  await assert.rejects(
    () => states.handleWorktreeMerged({ ...receipt, mergedCommit: "" }),
    /worktree\.mergeReceiptInvalid/,
    "an invalid receipt rejects without touching navigation",
  );
  assert.equal(toasts.length, 0, "an invalid receipt surfaces through the throw, not a toast");

  navigationToken = null;
  await act(async () => { states.openWorktreeMerge("worktree-tab"); });
  await act(async () => { await states.handleWorktreeMerged(receipt); });
  assert.deepEqual(toasts, ["worktree.navigationChangedPreserved"], "a superseded navigation intent preserves the worktree with a toast");
  assert.deepEqual(lifecycleCalls, [], "a superseded intent runs no close or finalize");
  navigationToken = "nav-token";

  toasts.length = 0;
  await act(async () => { await states.handleWorktreeMerged(receipt); });
  assert.deepEqual(lifecycleCalls, ["ensure", "seed", "list", "close", "finalize"], "a stable intent runs the full close/finalize chain in order");
  assert.equal(cleanups.length, 1, "a finalized merge hands the cleanup receipt to the notice");

  staleAfterEnsure = true;
  toasts.length = 0;
  lifecycleCalls.length = 0;
  await act(async () => { await states.handleWorktreeMerged(receipt); });
  assert.deepEqual(lifecycleCalls, ["ensure"], "a mid-flight navigation change stops the lifecycle before closing anything");
  assert.deepEqual(toasts, ["worktree.navigationChangedPreserved"], "the preserved path reports through the lifecycle toast");
  staleAfterEnsure = false;
  navigationCurrent = true;

  await act(async () => root.unmount());
  console.log("worktree merge commands: overlay state, receipt gate, intent fences and close/finalize chain passed");
} finally { dom.window.close(); }
