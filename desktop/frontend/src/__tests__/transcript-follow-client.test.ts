import assert from "node:assert/strict";
import test from "node:test";
import type { Change, FollowRequest, Snapshot, TranscriptFollowResponse } from "../generated/desktopContract.generated";
import { TranscriptFollowClient, type FollowConsumer } from "../lib/transcriptFollowClient";

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(done => { resolve = done; });
  return { promise, resolve };
}
async function microtasks() { for (let i = 0; i < 16; i++) await Promise.resolve(); }
function baseline(subscription = "subscription", overrides: Partial<Snapshot> = {}): TranscriptFollowResponse {
  return { protocolVersion: 2, subscription, changes: [], resetRequired: false, snapshot: {
    protocolVersion: 1, snapshotId: "cut", identity: { sessionId: "session", headId: "", runtimeEpoch: "epoch", rewriteEpoch: 0 },
    projectionRevision: 10, coveredThroughSeq: 4, durableSeq: 4,
    records: [{ id: "m:answer", order: 0, message: { role: "assistant", messageId: "answer", content: "prefix" }, refs: [] }],
    activeRecords: [], activeAttempts: [], runtime: { status: "in_progress", pendingEvents: [], samplingCount: 0, toolCount: 0 },
    before: 0, hasOlder: false, totalRecords: 1, totalTurns: 1, stale: false, ...overrides,
  } };
}
function suffix(changes: Change[], resetRequired = false): TranscriptFollowResponse {
  return { protocolVersion: 2, subscription: "subscription", changes, resetRequired };
}
function frame(revision: number, text: string): Change {
  return { revision, commitSeq: 4, durableSeq: 4, index: 0, event: { kind: "text", messageId: "answer", text } } as Change;
}
function transport() {
  const requests: FollowRequest[] = [];
  const pending: ReturnType<typeof deferred<TranscriptFollowResponse>>[] = [];
  const read = (request: FollowRequest) => {
    requests.push(request);
    if (request.close) return Promise.resolve(suffix([]));
    const next = deferred<TranscriptFollowResponse>(); pending.push(next); return next.promise;
  };
  return { requests, pending, client: new TranscriptFollowClient(read) };
}
function consumer(): FollowConsumer & { text: string; installed: number; delivered: Change[]; states: string[] } {
  return {
    text: "", installed: 0, delivered: [], states: [],
    install(response) { this.installed++; this.text = response.snapshot!.records[0]?.message.content ?? ""; },
    changes(changes) { this.delivered.push(...changes); for (const change of changes) this.text += change.event?.text ?? ""; },
    connection(state) { this.states.push(state); },
  };
}

test("follow installs the complete baseline before asking for its suffix", async () => {
  const io = transport(); const view = consumer(); const installed = deferred<void>();
  const original = view.install.bind(view);
  view.install = async response => { await installed.promise; await original(response); };
  const starting = io.client.start(view);
  io.pending.shift()!.resolve(baseline());
  await microtasks();
  assert.equal(io.requests.length, 1, "suffix must wait until installation commits");
  installed.resolve(); await starting;
  assert.equal(view.text, "prefix");
  assert.deepEqual(io.requests[1], { subscription: "subscription", afterRevision: 10 });
  io.pending.shift()!.resolve(suffix([frame(11, " suffix")])); await microtasks();
  assert.equal(view.text, "prefix suffix"); io.client.stop();
});

test("follow acknowledges display revisions independently and ignores duplicate frames", async () => {
  const io = transport(); const view = consumer(); const starting = io.client.start(view);
  io.pending.shift()!.resolve(baseline()); await starting;
  io.pending.shift()!.resolve(suffix([frame(11, " first"), frame(11, " first"), frame(12, " second")]));
  await microtasks();
  assert.equal(view.text, "prefix first second"); assert.equal(view.delivered.length, 2);
  assert.deepEqual(io.requests[io.requests.length - 1], { subscription: "subscription", afterRevision: 12 });
  io.pending.shift()!.resolve(suffix([{ revision: 13, firstSeq: 5, commitSeq: 8, durableSeq: 4, index: 0 }]));
  await microtasks(); assert.equal(view.delivered[view.delivered.length - 1]?.commitSeq, 8); io.client.stop();
});

test("settlement pairs with the stable attempt and commit despite delivery order and duplication", async () => {
  const io = transport(); const view = consumer(); const starting = io.client.start(view);
  io.pending.shift()!.resolve(baseline("subscription", { activeAttempts: [{ id: "attempt", messageId: "answer", turnId: "turn", nextIndex: 1 }] }));
  await starting;
  const commit: Change = { revision: 11, firstSeq: 5, commitSeq: 5, durableSeq: 4, index: 0, records: [{ messageId: "answer", role: "assistant", content: "complete" }] };
  const end: Change = { revision: 12, commitSeq: 5, durableSeq: 5, index: 1, attemptId: "attempt", resultSeq: 5, resultKind: "message/complete",
    event: { kind: "stream_attempt", messageId: "answer", streamAttempt: { id: "attempt", action: "commit" } } } as Change;
  io.pending.shift()!.resolve(suffix([end, commit, end])); await microtasks();
  assert.deepEqual(view.delivered.map(change => change.revision), [11, 12]);
  assert.equal(view.states[view.states.length - 1], "connected"); io.client.stop();
});

for (const failure of ["overflow", "revision gap", "business gap", "sampling gap", "settlement mismatch"] as const) {
  test(`follow ${failure} preserves visible content and only requests a new baseline`, async t => {
    t.mock.timers.enable({ apis: ["setTimeout"] });
    const io = transport(); const view = consumer(); const starting = io.client.start(view);
    io.pending.shift()!.resolve(baseline()); await starting;
    const broken = failure === "overflow" ? suffix([], true)
      : failure === "revision gap" ? suffix([frame(12, "bad")])
      : failure === "business gap" ? suffix([{ revision: 11, firstSeq: 6, commitSeq: 6, durableSeq: 4, index: 0 }])
      : failure === "settlement mismatch" ? suffix([{ revision: 11, commitSeq: 4, durableSeq: 4, index: 0, attemptId: "unknown", resultSeq: 4, resultKind: "message/complete", event: { kind: "stream_attempt", messageId: "different", streamAttempt: { id: "unknown", action: "commit" } } } as Change])
      : suffix([{ ...frame(11, "bad"), attemptId: "missing-attempt", index: 3 }]);
    io.pending.shift()!.resolve(broken); await microtasks();
    assert.equal(view.text, "prefix"); assert.equal(view.delivered.length, 0);
    assert.equal(view.states[view.states.length - 1], "disconnected");
    assert.ok(io.requests.some(request => request.close && request.subscription === "subscription"));
    t.mock.timers.tick(1000); await microtasks();
    assert.deepEqual(io.requests[io.requests.length - 1], {}, "recovery issues a read, never a model submission");
    assert.equal(view.text, "prefix", "content remains visible while replacement is pending");
    io.pending.shift()!.resolve(baseline("replacement", { projectionRevision: 20, coveredThroughSeq: 8 }));
    await microtasks(); assert.equal(view.installed, 2); assert.equal(view.states[view.states.length - 1], "connected");
    assert.ok(io.requests.every(request => Object.keys(request).every(key => ["subscription", "afterRevision", "close"].includes(key))));
    io.client.stop();
  });
}

test("stopped generations ignore delayed suffixes and close their subscription", async () => {
  const io = transport(); const oldView = consumer(); const starting = io.client.start(oldView);
  io.pending.shift()!.resolve(baseline()); await starting;
  const oldSuffix = io.pending.shift()!; io.client.stop();
  const newView = consumer(); const restarting = io.client.start(newView);
  io.pending.shift()!.resolve(baseline("next", { identity: { sessionId: "next", headId: "", runtimeEpoch: "epoch-next", rewriteEpoch: 0 } }));
  await restarting;
  oldSuffix.resolve(suffix([frame(11, " stale")])); await microtasks();
  assert.equal(oldView.text, "prefix"); assert.equal(newView.text, "prefix");
  assert.ok(io.requests.some(request => request.close && request.subscription === "subscription")); io.client.stop();
});

test("stopping before baseline arrives closes the late subscription without installation", async () => {
  const io = transport(); const view = consumer(); const starting = io.client.start(view);
  const late = io.pending.shift()!; io.client.stop(); late.resolve(baseline()); await starting; await microtasks();
  assert.equal(view.installed, 0);
  assert.ok(io.requests.some(request => request.close && request.subscription === "subscription"));
});

test("stopping during asynchronous installation releases the newly opened subscription", async () => {
  const io = transport(); const installed = deferred<void>(); const view = consumer();
  view.install = async () => installed.promise;
  const starting = io.client.start(view); io.pending.shift()!.resolve(baseline()); await microtasks();
  io.client.stop(); installed.resolve(); await starting; await microtasks();
  assert.ok(io.requests.some(request => request.close && request.subscription === "subscription"), "cancelled install leaked its subscription");
  assert.ok(!view.states.includes("connected"));
});
