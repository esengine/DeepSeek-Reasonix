// Run: tsx src/__tests__/worktree-merge-lifecycle.test.ts
import assert from "node:assert/strict";
import { runWorktreeMergeLifecycle } from "../lib/worktreeMergeLifecycle";
import type { TabMeta, WorktreeMergeResult } from "../lib/types";

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

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

async function staleEnsureDoesNotCloseOrFinalize() {
  const ensure = deferred<TabMeta>();
  let current = true;
  let closes = 0;
  let finalizes = 0;
  let preserved = 0;
  const running = runWorktreeMergeLifecycle(receipt, worktreeTab.id, "nav-1", {
    ensureSource: () => ensure.promise,
    isNavigationCurrent: () => current,
    seedSource: () => undefined,
    listTabs: async () => [sourceTab, worktreeTab],
    closeWorktree: async () => { closes++; return { closed: true, idempotent: false }; },
    finalize: async () => { finalizes++; return { completed: true, worktreeRemoved: true, branchDeleted: true, blockers: [] }; },
    onNavigationPreserved: () => { preserved++; },
    onCloseBlocked: () => undefined,
  });
  current = false;
  ensure.resolve(sourceTab);
  assert.deepEqual(await running, { phase: "preserved" });
  assert.equal(closes, 0);
  assert.equal(finalizes, 0);
  assert.equal(preserved, 1);
}

async function staleListDoesNotCloseOrFinalize() {
  const listing = deferred<TabMeta[]>();
  let current = true;
  let closes = 0;
  let finalizes = 0;
  const running = runWorktreeMergeLifecycle(receipt, worktreeTab.id, "nav-2", {
    ensureSource: async () => sourceTab,
    isNavigationCurrent: () => current,
    seedSource: () => undefined,
    listTabs: () => listing.promise,
    closeWorktree: async () => { closes++; return { closed: true, idempotent: false }; },
    finalize: async () => { finalizes++; return { completed: true, worktreeRemoved: true, branchDeleted: true, blockers: [] }; },
    onNavigationPreserved: () => undefined,
    onCloseBlocked: () => undefined,
  });
  await Promise.resolve();
  current = false;
  listing.resolve([sourceTab, worktreeTab]);
  assert.deepEqual(await running, { phase: "preserved" });
  assert.equal(closes, 0);
  assert.equal(finalizes, 0);
}

async function stableLifecycleClosesBeforeFinalize() {
  const calls: string[] = [];
  const result = await runWorktreeMergeLifecycle(receipt, worktreeTab.id, "nav-3", {
    ensureSource: async () => { calls.push("ensure"); return sourceTab; },
    isNavigationCurrent: () => true,
    seedSource: () => { calls.push("seed"); },
    listTabs: async () => { calls.push("list"); return [sourceTab, worktreeTab]; },
    closeWorktree: async () => { calls.push("close"); return { closed: true, idempotent: false }; },
    finalize: async () => {
      calls.push("finalize");
      return {
        completed: false,
        worktreeRemoved: false,
        branchDeleted: false,
        recoveryRetained: true,
        recoveryRoot: "/recovery",
        recoveryWorktreeRegistered: true,
        branchRetained: true,
        blockers: [],
      };
    },
    onNavigationPreserved: () => undefined,
    onCloseBlocked: () => undefined,
  });
  assert.equal(result.phase, "finalized");
  assert.deepEqual(calls, ["ensure", "seed", "list", "close", "finalize"]);
  if (result.phase === "finalized") {
    assert.equal(result.cleanup.recoveryRetained, true);
    assert.equal(result.cleanup.completed, false);
  }
}

async function singleSurfaceStillUsesIdempotentCloseProof() {
  const calls: string[] = [];
  const result = await runWorktreeMergeLifecycle(receipt, worktreeTab.id, "nav-4", {
    ensureSource: async () => sourceTab,
    isNavigationCurrent: () => true,
    seedSource: () => undefined,
    listTabs: async () => [sourceTab],
    closeWorktree: async () => { calls.push("close"); return { closed: true, idempotent: true }; },
    finalize: async () => { calls.push("finalize"); return { completed: true, worktreeRemoved: true, branchDeleted: true, blockers: [] }; },
    onNavigationPreserved: () => undefined,
    onCloseBlocked: () => undefined,
  });
  assert.equal(result.phase, "finalized");
  assert.deepEqual(calls, ["close", "finalize"]);
}

async function closeCarriesFenceAndNeverFinalizesAfterBackendRejectsStaleIntent() {
  const close = deferred<{ closed: boolean; idempotent: boolean }>();
  let finalizes = 0;
  let closeToken = "";
  const running = runWorktreeMergeLifecycle(receipt, worktreeTab.id, "nav-close-fence", {
    ensureSource: async () => sourceTab,
    isNavigationCurrent: () => true,
    seedSource: () => undefined,
    listTabs: async () => [sourceTab, worktreeTab],
    closeWorktree: (request) => {
      closeToken = request.navigationIntentToken;
      return close.promise;
    },
    finalize: async () => {
      finalizes++;
      return { completed: true, worktreeRemoved: true, branchDeleted: true, blockers: [] };
    },
    onNavigationPreserved: () => undefined,
    onCloseBlocked: () => undefined,
  });
  await Promise.resolve();
  close.resolve({ closed: false, idempotent: false });
  assert.deepEqual(await running, { phase: "close-blocked" });
  assert.equal(closeToken, "nav-close-fence");
  assert.equal(finalizes, 0);
}

await staleEnsureDoesNotCloseOrFinalize();
await staleListDoesNotCloseOrFinalize();
await stableLifecycleClosesBeforeFinalize();
await singleSurfaceStillUsesIdempotentCloseProof();
await closeCarriesFenceAndNeverFinalizesAfterBackendRejectsStaleIntent();
console.log("worktree merge lifecycle tests passed");
