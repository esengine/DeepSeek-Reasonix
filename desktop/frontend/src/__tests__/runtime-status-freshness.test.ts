import assert from "node:assert/strict";
import { initialState, reducer, promptEventClock } from "../lib/useController";
import { findTabAfterSubmitFailure } from "../lib/turnSubmissionFailure";
import type { TabMeta } from "../lib/types";

const idle = { type: "backend_status", running: false, pendingPrompt: false, backgroundJobs: 0,
  cancelRequested: false, cancellable: false, runtimeEpoch: "epoch-a", turnEventSeq: 7 } as const;
const now = promptEventClock();
const baseline = reducer(initialState, { ...idle, snapshotAt: now });
const optimistic = reducer(baseline, { type: "user", text: "hello", seq: 0, submissionId: "send" });
const rejected = reducer(optimistic, { type: "turn_submit_rejected", submissionId: "send", error: "not ready" });
assert.equal(reducer(rejected, { ...idle, snapshotAt: now }), rejected, "pre-submit snapshot cannot undo a later lifecycle event");
const fresh = promptEventClock() + 1;
const settled = reducer(rejected, { ...idle, snapshotAt: fresh });
assert.equal(settled.running, false, "same-sequence fresh idle clears a rejected optimistic send");
assert.equal(settled.cancellable, false);
assert.equal(reducer(settled, { ...idle, running: true, cancellable: true, snapshotAt: now }), settled, "out-of-order equal-sequence response cannot restore running");
assert.equal(reducer(settled, { ...idle, running: true, cancellable: true, snapshotAt: fresh }), settled, "duplicate snapshot is ignored");
assert.equal(reducer(settled, { ...idle, running: true, cancellable: true }), settled, "equal sequence without freshness cannot override state");
assert.equal(reducer(settled, { ...idle, running: true, cancellable: true, turnEventSeq: 6, snapshotAt: fresh + 1 }), settled, "older backend sequence is always stale");
const active = reducer(settled, { ...idle, running: true, cancellable: true, turnEventSeq: 8, snapshotAt: fresh + 2 });
assert.equal(active.running, true, "newer backend event still advances state");
const replaced = reducer(active, { ...idle, runtimeEpoch: "epoch-b", turnEventSeq: 1, snapshotAt: fresh + 3 });
assert.equal(replaced.running, false, "new runtime may restart its event sequence");

let clock = 100;
let finishRead: (tabs: TabMeta[]) => void = () => {};
const read = findTabAfterSubmitFailure({ ListTabs: () => new Promise((resolve) => { finishRead = resolve; }) }, "tab", [0], () => clock);
// A newer lifecycle event arrives while the older status read is in flight.
clock = 200;
finishRead([{ id: "tab", running: false }] as TabMeta[]);
const snapshot = await read;
assert.ok(snapshot && snapshot[1] < 150, "reconciliation carries read-start time, not response time");
const newerTurn = { ...rejected, turnLifecycleObservedAt: 150 };
assert.equal(reducer(newerTurn, { ...idle, snapshotAt: snapshot[1] }), newerTurn, "delayed failed-send reconciliation cannot clear a newer turn");
console.log("  PASS  runtime snapshots use event order and read freshness without trapping optimistic state");
