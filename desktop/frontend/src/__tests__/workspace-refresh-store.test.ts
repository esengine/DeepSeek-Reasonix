import assert from "node:assert/strict";
import {
  acceptWorkspaceRefreshForTests,
  activateWorkspaceRefreshScopeForTests,
  reconcileWorkspaceRefreshForTests,
  resetWorkspaceRefreshStoreForTests,
  workspaceRefreshSnapshotForTests,
} from "../lib/workspaceRefreshStore";
import { createWorkspaceRefreshScheduler, type WorkspaceRefreshTimer } from "../lib/workspaceRefreshScheduler";
import { workspaceRefreshActions } from "../lib/workspaceRefreshInvalidation";
import type { WireWorkspaceChanged, WorkspaceRevisions } from "../lib/types";

const revisions = (content: number): WorkspaceRevisions => ({ content, tree: content, workingTree: content, gitMeta: 0, session: content });
const event = (content: number): WireWorkspaceChanged => ({
  revisions: revisions(content), changes: [], allPaths: true, source: "filesystem", watchState: "active",
});

function workspaceScopesKeepIndependentRevisionBaselines() {
  resetWorkspaceRefreshStoreForTests();
  activateWorkspaceRefreshScopeForTests("tab", "root-a");
  acceptWorkspaceRefreshForTests("tab", event(100));
  assert.equal(workspaceRefreshSnapshotForTests("tab", "root-a").revisions.content, 100);

  activateWorkspaceRefreshScopeForTests("tab", "root-b");
  acceptWorkspaceRefreshForTests("tab", event(1));
  assert.equal(workspaceRefreshSnapshotForTests("tab", "root-b").revisions.content, 1,
    "a new root must not inherit the old root's monotonic baseline");
  assert.equal(workspaceRefreshSnapshotForTests("tab", "root-a").revisions.content, 100);
}

async function reconciliationForcesVisibleResourcesWithoutMovingBackwards() {
  resetWorkspaceRefreshStoreForTests();
  activateWorkspaceRefreshScopeForTests("tab", "root");
  acceptWorkspaceRefreshForTests("tab", event(5));
  const initialSequence = workspaceRefreshSnapshotForTests("tab", "root").sequence;
  reconcileWorkspaceRefreshForTests("tab", "root", { revisions: revisions(5), watchState: "active" });
  assert.equal(workspaceRefreshSnapshotForTests("tab", "root").sequence, initialSequence,
    "ordinary revision reconciliation must not reload an unchanged active workspace");
  reconcileWorkspaceRefreshForTests("tab", "root", { revisions: revisions(5), watchState: "active" }, true);
  const forced = workspaceRefreshSnapshotForTests("tab", "root");
  assert.equal(forced.sequence, initialSequence + 1, "focus reconciliation must refresh unwatched external resources even at the same revision");
  assert.equal(forced.allPaths, true);
  assert.equal(forced.source, "reconcile");

  acceptWorkspaceRefreshForTests("tab", event(6));
  reconcileWorkspaceRefreshForTests("tab", "root", { revisions: revisions(5), watchState: "active" }, true);
  const current = workspaceRefreshSnapshotForTests("tab", "root");
  assert.equal(current.revisions.content, 6, "an older focus response must not overwrite a newer workspace event");
  assert.equal(current.source, "reconcile", "the focus fallback must still invalidate visible resources after a newer event");
  assert.equal(current.allPaths, true);
}

function focusReconciliationInvalidatesVisibleResourcesAtEqualRevisions() {
  const current = revisions(5);
  const focused: WireWorkspaceChanged = {
    revisions: current, changes: [], allPaths: true, source: "reconcile", watchState: "active",
  };
  assert.deepEqual(workspaceRefreshActions(current, { ...focused, sequence: 2 }, false), {
    forceVisible: true, content: true, tree: true, workingTree: true, gitMeta: false,
  });
  assert.equal(workspaceRefreshActions(current, { ...focused, sequence: 3 }, true).gitMeta, true,
    "focus fallback refreshes history only while that resource is visible");

  const ordinary: WireWorkspaceChanged = {
    revisions: current, changes: [], allPaths: true, source: "filesystem", watchState: "active",
  };
  assert.deepEqual(workspaceRefreshActions(current, { ...ordinary, sequence: 4 }, true), {
    forceVisible: false, content: false, tree: false, workingTree: false, gitMeta: false,
  });
}

class FakeTimer implements WorkspaceRefreshTimer {
  callbacks: Array<() => void> = [];
  schedule(callback: () => void): unknown { this.callbacks.push(callback); return callback; }
  cancel(handle: unknown): void { this.callbacks = this.callbacks.filter((callback) => callback !== handle); }
  fire(): void { const callback = this.callbacks.shift(); assert.ok(callback); callback(); }
}

async function refreshSchedulerUsesTrailingQuietWindowAndBoundsConcurrency() {
  const timer = new FakeTimer();
  const scheduler = createWorkspaceRefreshScheduler(300, timer);
  const runs: string[] = [];
  let releaseFirst!: () => void;
  const first = new Promise<void>((resolve) => { releaseFirst = resolve; });
  let confirmTrailing!: () => void;
  const trailingStarted = new Promise<void>((resolve) => { confirmTrailing = resolve; });

  scheduler.trigger(() => { runs.push("superseded"); });
  scheduler.trigger(async () => { runs.push("first"); await first; });
  assert.equal(timer.callbacks.length, 1, "retriggering must reset the quiet window");
  timer.fire();
  await Promise.resolve();
  assert.deepEqual(runs, ["first"]);

  scheduler.trigger(() => { runs.push("trailing"); confirmTrailing(); });
  timer.fire();
  await Promise.resolve();
  assert.deepEqual(runs, ["first"], "a refresh must not overlap the in-flight load");
  releaseFirst();
  await trailingStarted;
  assert.deepEqual(runs, ["first", "trailing"]);
}

await workspaceScopesKeepIndependentRevisionBaselines();
await reconciliationForcesVisibleResourcesWithoutMovingBackwards();
focusReconciliationInvalidatesVisibleResourcesAtEqualRevisions();
await refreshSchedulerUsesTrailingQuietWindowAndBoundsConcurrency();
console.log("ok  workspace refresh scope isolation and scheduling");
