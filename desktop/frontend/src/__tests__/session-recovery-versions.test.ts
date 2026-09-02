import assert from "node:assert/strict";
import {
  normalizeRecoveryLineageView,
  pendingRecoveryMatchesRoots,
  pendingSessionRecovery,
  recoveryLineageResolution,
  sanitizedRecoveryReason,
  SessionRecoveryDivergenceTracker,
  userVisibleRecoveryVersions,
} from "../lib/sessionRecoveryVersions";
import type { RecoveryLineageView, SessionRecoveryEvent } from "../lib/types";

const view = (overrides: Partial<RecoveryLineageView> = {}): RecoveryLineageView => ({
  groupId: "group",
  state: "diverged",
  branchCount: 3,
  unresolved: 1,
  cleanupEligible: 1,
  members: [
    { path: "/s/root.jsonl", role: "normal", canonical: true, turns: 3, open: true, running: false },
    { path: "/s/copy.jsonl", role: "covered_copy", canonical: false, turns: 3, open: false, running: false },
    { path: "/s/fork.jsonl", role: "diverged", canonical: false, turns: 4, open: false, running: false },
  ],
  ...overrides,
});

assert.deepEqual(normalizeRecoveryLineageView({ members: null }).members, [], "null Wails arrays normalize to []");
assert.equal(userVisibleRecoveryVersions(view()).length, 2, "covered copies never enter the user-facing version list");
assert.equal(recoveryLineageResolution(view()), "notify", "confirmed unique divergence notifies");
assert.equal(recoveryLineageResolution(view({ state: "repairing" })), "wait", "catalog rebuilding waits for a later revision");
assert.equal(recoveryLineageResolution(view({ state: "covered", unresolved: 0 })), "clear", "proven covered copies clear silently");
assert.equal(pendingRecoveryMatchesRoots({ eventKey: "e", topic: { scope: "project", workspaceRoot: "/a", topicId: "t" } }, ["/b"]), false, "unrelated catalog roots do not trigger lineage reads");
assert.equal(pendingRecoveryMatchesRoots({ eventKey: "e", topic: { scope: "global", topicId: "t" } }, [""]), true, "global catalog revisions match the global pending item");
assert.equal(pendingRecoveryMatchesRoots({ eventKey: "e", topic: { scope: "project", workspaceRoot: "/a", topicId: "t" } }, []), true, "rootless rebuild revisions recheck every pending item");
assert.equal(recoveryLineageResolution(view({ state: "adopted", unresolved: 0 })), "clear", "adopted lineages stay silent");
assert.equal(recoveryLineageResolution(view({ state: "preferred", unresolved: 0 })), "clear", "preferred lineages stay silent");
assert.equal(recoveryLineageResolution(view({ state: "", unresolved: 0 })), "clear", "classified covered lineages stay silent");
assert.equal(recoveryLineageResolution(view({
  branchCount: 1,
  unresolved: 1,
  cleanupEligible: 0,
  members: [{ path: "/s/only.jsonl", role: "diverged", canonical: true, turns: 4, open: false, running: false }],
})), "clear", "a unique rootless recovery version clears instead of leaking a pending check");

const event: SessionRecoveryEvent = {
  recoveryPath: "/s/fork.jsonl",
  scope: "project",
  workspaceRoot: "/work",
  topicId: "topic",
  recoveryParentId: "root",
  recoveryReason: "snapshot_conflict",
};
const pending = pendingSessionRecovery(event);
assert.ok(pending, "valid recovery events produce a pending catalog check");
assert.equal(pending?.topic.path, event.recoveryPath, "pending checks bind lineage reads to the recovered physical group");
assert.equal(pendingSessionRecovery({ recoveryPath: "/s/fork.jsonl" }), null, "legacy events without a topic cannot be misrouted");
assert.equal(sanitizedRecoveryReason("snapshot_conflict"), "snapshot_conflict");
assert.equal(sanitizedRecoveryReason("shutdown file lock"), "shutdown_lock");
assert.equal(sanitizedRecoveryReason("private arbitrary reason"), "other", "diagnostics do not retain arbitrary backend text");

const tracker = new SessionRecoveryDivergenceTracker();
const first = tracker.register(event);
assert.equal(first.isNew, true);
assert.equal(first.occurrence, 1);
assert.equal(tracker.resolve(first.pending!.eventKey, view({ state: "repairing" })), "wait", "out-of-order recovery remains pending");
assert.equal(tracker.entries().length, 1);
assert.equal(tracker.resolve(first.pending!.eventKey, view()), "notify", "later catalog revision confirms the divergence");
assert.equal(tracker.entries().length, 0);
assert.equal(tracker.register(event).isNew, false, "duplicate delivery after notification is suppressed");

const second = tracker.register({ ...event, recoveryPath: "/s/fork-2.jsonl" });
assert.equal(second.occurrence, 2, "a later distinct recovery on the same topic is counted separately");
assert.equal(tracker.resolve(second.pending!.eventKey, view({ state: "adopted", unresolved: 0 })), "clear");

console.log("  PASS  session recovery divergence state machine");
