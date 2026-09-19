import assert from "node:assert/strict";
import test from "node:test";
import type { FollowRequest, TranscriptFollowResponse } from "../generated/desktopContract.generated";
import { installDesktopHostStub } from "./desktopHostStub";

Object.defineProperty(globalThis, "window", { configurable: true, value: {} });
const commands: Record<string, unknown> = {};
installDesktopHostStub(commands);
const [{ TranscriptSessionFollower }, { initialState, reducer }, { getTranscriptStore }] = await Promise.all([
  import("../lib/transcriptSessionFollower"), import("../lib/useController"), import("../lib/transcriptStore"),
]);
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(done => { resolve = done; });
  return { promise, resolve };
}

test("expired settled content requests the owning follower to resynchronize", async () => {
  const { canonicalHistoryContent, registerTranscriptContentRecovery } = await import("../lib/canonicalTranscriptBackend");
  let recoveries = 0;
  commands.TranscriptContentForTab = async () => ({ stale: true });
  const release = registerTranscriptContentRecovery("expired-content", () => { recoveries++; });
  const ref = { entryId: "m:answer", field: "content", size: 1, chunks: 1, revision: 1, digest: "expired",
    transcriptRef: { snapshotId: "expired", recordId: "m:answer", path: ["content"], bytes: 1 } };
  await assert.rejects(canonicalHistoryContent("expired-content", ref, 0), /synchronizing/);
  assert.equal(recoveries, 1);
  release();
  await assert.rejects(canonicalHistoryContent("expired-content", ref, 0), /synchronizing/);
  assert.equal(recoveries, 1, "released owner cannot be restarted by a late content result");
  const delayed = deferred<{ stale: boolean }>();
  commands.TranscriptContentForTab = () => delayed.promise;
  const oldRelease = registerTranscriptContentRecovery("expired-content", () => { recoveries++; });
  const staleRead = canonicalHistoryContent("expired-content", ref, 0);
  oldRelease();
  const newRelease = registerTranscriptContentRecovery("expired-content", () => { recoveries += 100; });
  delayed.resolve({ stale: true });
  await assert.rejects(staleRead, /synchronizing/);
  assert.equal(recoveries, 1, "late stale reference cannot resynchronize a replacement session");
  newRelease();
});

test("reading old pages isolates live output and rejoins the current stable node", () => {
  let state: import("../lib/useController").State = { ...initialState, transcriptProtocol: 2, historyHasNewer: true,
    items: [{ kind: "user" as const, id: "old-reader", text: "old page" }] };
  const resident = state.items;
  state = reducer(state, { type: "event", e: { kind: "text", messageId: "active", text: "full prefix" } });
  assert.equal(state.items, resident);
  assert.equal(state.offscreenItems?.find(item => item.id === "m:active")?.kind, "assistant");
  const active = state.offscreenItems!.find(item => item.id === "m:active")!;
  state = reducer(state, { type: "history_append", items: [], startTurn: 0, endTurn: 1, totalTurns: 1, hasOlder: true, hasNewer: false });
  assert.equal(state.items.find(item => item.id === "m:active"), active);
  assert.equal(state.offscreenItems, undefined);
});

test("business replacement preserves authoritative final turn annotations", () => {
  const final = { kind: "assistant" as const, id: "m:final", text: "answer", reasoning: "", streaming: false, turnFinal: true, turnDurationMs: 933524, samplingCount: 72, toolCount: 72 };
  const state = reducer({ ...initialState, items: [final] }, { type: "transcript_records", confirmedUsers: [], projection: {
    items: [{ ...final, turnFinal: undefined, turnDurationMs: undefined, samplingCount: undefined, toolCount: undefined }],
    removeIds: [], startTurn: 0, endTurn: 1, totalTurns: 1, hasOlder: false, hasNewer: false, revision: 2, revisionKnown: true, digest: "cut",
  } });
  assert.equal(state.items[0].kind === "assistant" && state.items[0].turnDurationMs, 933524);
});
async function microtasks() { for (let i = 0; i < 16; i++) await Promise.resolve(); }

for (const remote of [false, true]) for (const scenario of ["direct", "event-first", "late", "stale", "missing"] as const) {
  const lateBinding = scenario !== "direct" && scenario !== "event-first";
  test(`${remote ? "remote" : "local"} offscreen submission confirmation (${scenario})`, async () => {
    const tab = `offscreen-${remote}-${scenario}`, path = `/session/${tab}`;
    let state = { ...initialState };
    const polls: Array<ReturnType<typeof deferred<TranscriptFollowResponse>>> = [];
    const readKey = remote ? "RemoteTranscriptFollowForTab" : "TranscriptFollowForTab";
    commands[readKey] = (_tab: string, request: FollowRequest) => {
      if (request.close) return Promise.resolve({ protocolVersion: 2, changes: [], resetRequired: false, subscription: tab });
      if (!request.subscription) return Promise.resolve(initial(tab));
      const poll = deferred<TranscriptFollowResponse>(); polls.push(poll); return poll.promise;
    };
    let lookups = 0;
    const lookup = deferred<{ status: string; messages: Array<{ role: string; messageId: string }> }>();
    commands[remote ? "RemoteSessionHistoryWindowForTab" : "SessionHistoryWindowForTab"] = (_tab: string, request: { anchor: string; messageId: string }) => {
      lookups++; assert.equal(request.anchor, "message"); assert.equal(request.messageId, "sent"); return lookup.promise;
    };
    const follower = new TranscriptSessionFollower(tab, path, remote, action => { state = reducer(state, action); }, () => state);
    await follower.start();
    try {
      state = reducer(state, { type: "user", seq: 0, text: "question", submissionId: "submit" });
      const old = getTranscriptStore().installSlice(tab, path, {
        entries: [{ entryId: "m:old", turn: 1, order: 0, message: { role: "user", content: "old page", messageId: "old" }, refs: [] }],
        nextCursor: "", newerCursor: "next", hasOlder: false, hasNewer: true, startTurn: 1, endTurn: 1, totalTurns: 100,
        revision: 4, digest: "generation", stale: false,
      });
      state = reducer(state, { type: "history_replace", ...old });
      const resident = state.items;
      if (scenario === "event-first") {
        polls.shift()!.resolve({ protocolVersion: 2, subscription: tab, changes: [{ revision: 11, commitSeq: 4, durableSeq: 4, index: 0,
          event: { kind: "user_message", messageId: "sent", submissionId: "submit", source: "executor" } }], resetRequired: false });
        await microtasks();
        assert.equal(lookups, 0, "an identity event alone must not initiate a history read");
        assert.ok(state.localSubmissions.submit, "identity alone keeps the echo until the formal record");
      }
      polls.shift()!.resolve({ protocolVersion: 2, subscription: tab, changes: [{ revision: scenario === "event-first" ? 12 : 11, firstSeq: 5, commitSeq: 5, durableSeq: 5, index: 0,
        records: [{ role: "user", messageId: "sent", submissionId: lateBinding ? undefined : "submit", content: "question" }] }], resetRequired: false });
      await microtasks();
      if (lateBinding) {
        assert.ok(state.localSubmissions.submit);
        polls.shift()!.resolve({ protocolVersion: 2, subscription: tab, changes: [{ revision: 12, commitSeq: 5, durableSeq: 5, index: 0,
          event: { kind: "user_message", messageId: "sent", submissionId: "submit", source: "executor" } }], resetRequired: false });
        await microtasks();
        assert.equal(lookups, 1);
        if (scenario === "missing") {
          polls.shift()!.resolve({ protocolVersion: 2, subscription: tab, changes: [{ revision: 13, commitSeq: 5, durableSeq: 5, index: 0,
            event: { kind: "user_message", messageId: "sent", submissionId: "submit", source: "executor" } }], resetRequired: false });
          await microtasks();
          assert.equal(lookups, 1, "duplicate binding does not create a second in-flight read");
        }
        if (scenario === "stale") {
          state = reducer(state, { type: "reset" });
          state = reducer(state, { type: "user", seq: 0, text: "replacement", submissionId: "submit" });
          state = { ...state, localSubmissions: { submit: { ...state.localSubmissions.submit, messageId: "sent" } } };
        }
        lookup.resolve({ status: "ready", messages: scenario === "missing" ? [] : [{ role: "user", messageId: "sent" }] });
        await microtasks();
        if (scenario === "stale" || scenario === "missing") {
          assert.ok(state.localSubmissions.submit, "stale or inconclusive reads must retain the current echo");
          assert.equal(state.localSubmissions.submit.status, scenario === "stale" ? "sending" : "accepted");
          if (scenario === "missing") {
            assert.deepEqual(state.items, resident);
            polls.shift()!.resolve({ protocolVersion: 2, subscription: tab, changes: [{ revision: 14, firstSeq: 6, commitSeq: 6, durableSeq: 6, index: 0,
              records: [{ role: "user", messageId: "unrelated", content: "unrelated" }] }], resetRequired: false });
            await microtasks();
            assert.equal(lookups, 2, "new committed coverage permits one retry of an inconclusive read");
            assert.ok(state.localSubmissions.submit);
          }
          return;
        }
      } else assert.equal(lookups, 0, "ordinary formal handoff makes no extra history read");
      assert.equal(state.localSubmissionOrder.length, 0);
      assert.deepEqual(state.items, resident);
      assert.equal(Object.keys(state.visibleSubmissionHandoffs).length, 0);
      state = reducer(state, { type: "history_replace", items: [{ kind: "assistant", id: "m:later", text: "later", reasoning: "", streaming: false }],
        startTurn: 101, totalTurns: 101, hasOlder: true, hasNewer: false, revision: 6 });
      assert.equal(state.localSubmissionOrder.length, 0);
    } finally { follower.stop(); getTranscriptStore().evictTab(tab); }
  });
}
function initial(subscription: string): TranscriptFollowResponse {
  return {
    protocolVersion: 2, subscription, changes: [], resetRequired: false,
    snapshot: {
      protocolVersion: 1, snapshotId: "cut", identity: { sessionId: subscription, runtimeEpoch: "epoch", rewriteEpoch: 0, headId: "" },
      projectionRevision: 10, coveredThroughSeq: 4, durableSeq: 4, records: [], activeRecords: [], activeAttempts: [],
      runtime: { status: "completed", pendingEvents: [], samplingCount: 0, toolCount: 0 }, before: 0, hasOlder: false, totalRecords: 1, totalTurns: 1, stale: false,
    },
    history: {
      status: "ready", snapshotSequence: 4, coverageSequence: 4, generation: "generation", totalTurns: 1, hasOlder: false, hasNewer: false,
      messages: [{ messageId: "answer", position: 0, version: 1, role: "assistant", eventSequence: 4, visibleTurn: 1,
        preview: "", contentRef: { digest: "canonical-message", bytes: 8192 } }],
    },
  };
}

for (const remote of [false, true]) test(`${remote ? "remote" : "local"} follower preserves an outer snapshot identity from an older peer`, async () => {
  const tab = `outer-record-${remote}`, path = `/session/${tab}`;
  const response = initial(tab);
  response.snapshot!.records = [
    { id: "view:older:1", order: 0, message: { role: "notice", content: "outer identity" }, refs: [] },
    { id: "tool:older-call", order: 1, message: { role: "tool", messageId: "tool-message", toolCallId: "older-call", toolName: "read_file", content: "result" }, refs: [] },
    { id: "", order: 2, message: { role: "notice", content: "legacy empty identity" }, refs: [] },
  ];
  response.snapshot!.totalRecords = 3;
  const key = remote ? "RemoteTranscriptFollowForTab" : "TranscriptFollowForTab";
  const pending = deferred<TranscriptFollowResponse>();
  commands[key] = (_tab: string, request: FollowRequest) => request.close
    ? Promise.resolve({ protocolVersion: 2, subscription: tab, changes: [], resetRequired: false })
    : request.subscription ? pending.promise : Promise.resolve(response);
  let state = initialState;
  const follower = new TranscriptSessionFollower(tab, path, remote, action => { state = reducer(state, action); });
  try {
    await follower.start();
    assert.ok(state.items.some(item => item.kind === "notice" && item.text === "outer identity"));
    assert.ok(getTranscriptStore().peek(tab, path)?.items.some(item => item.id === "he:view:older:1"));
    assert.ok(state.items.some(item => item.kind === "tool" && item.id === "older-call"));
    assert.ok(state.items.some(item => item.kind === "notice" && item.text === "legacy empty identity"));
    assert.ok(!getTranscriptStore().peek(tab, path)?.items.some(item => item.id === "he:undefined"));
  } finally { follower.stop(); getTranscriptStore().evictTab(tab); }
});

for (const remote of [false, true]) test(`${remote ? "remote" : "local"} malformed snapshot leaves the resident store unchanged`, async () => {
  const tab = `invalid-record-${remote}`, path = `/session/${tab}`;
  getTranscriptStore().installSlice(tab, path, {
    entries: [{ entryId: "m:resident", turn: 1, order: 0, message: { role: "assistant", messageId: "resident", content: "resident" }, refs: [] }],
    nextCursor: "", hasOlder: false, newerCursor: "", hasNewer: false, totalTurns: 1, startTurn: 1, endTurn: 1,
    revision: 1, revisionKnown: true, digest: "resident", stale: false,
  });
  const response = initial(tab);
  response.snapshot!.records = [{ id: "", order: 0, message: { role: "notice", content: "invalid" },
    refs: [{ snapshotId: "cut", recordId: "", path: ["content"], bytes: 100 }] }];
  response.snapshot!.totalRecords = 1;
  const key = remote ? "RemoteTranscriptFollowForTab" : "TranscriptFollowForTab";
  commands[key] = (_tab: string, request: FollowRequest) => Promise.resolve(request.close
    ? { protocolVersion: 2, subscription: tab, changes: [], resetRequired: false }
    : response);
  const follower = new TranscriptSessionFollower(tab, path, remote, () => undefined);
  try {
    await assert.rejects(follower.start(), /transcript snapshot content identity missing/);
    const resident = getTranscriptStore().peek(tab, path);
    assert.equal(resident?.digest, "resident");
    assert.ok(resident?.items.some(item => item.id === "m:resident"));
    assert.ok(!resident?.items.some(item => item.id === "he:undefined"));
  } finally { follower.stop(); getTranscriptStore().evictTab(tab); }
});

test("reducer rejection does not commit a prepared transcript replacement", async () => {
  const tab = "reducer-reject", path = `/session/${tab}`;
  getTranscriptStore().installSlice(tab, path, {
    entries: [{ entryId: "m:resident", turn: 1, order: 0, message: { role: "assistant", messageId: "resident", content: "resident" }, refs: [] }],
    nextCursor: "", hasOlder: false, newerCursor: "", hasNewer: false, totalTurns: 1, startTurn: 1, endTurn: 1,
    revision: 1, revisionKnown: true, digest: "resident", stale: false,
  });
  commands.TranscriptFollowForTab = (_tab: string, request: FollowRequest) => Promise.resolve(request.close
    ? { protocolVersion: 2, subscription: tab, changes: [], resetRequired: false }
    : initial(tab));
  const follower = new TranscriptSessionFollower(tab, path, false, action => {
    if (action.type === "transcript_v2_snapshot") throw new Error("reducer rejected snapshot");
  });
  try {
    await assert.rejects(follower.start(), /reducer rejected snapshot/);
    const resident = getTranscriptStore().peek(tab, path);
    assert.equal(resident?.digest, "resident");
    assert.ok(resident?.items.some(item => item.id === "m:resident"));
  } finally { follower.stop(); getTranscriptStore().evictTab(tab); }
});

test("snapshot rejects duplicate durable identities and mismatched content references", async () => {
  const cases: Array<{ name: string; records: NonNullable<TranscriptFollowResponse["snapshot"]>["records"]; pattern: RegExp }> = [
    { name: "duplicate", records: [
      { id: "same", order: 0, message: { role: "notice", recordId: "same", content: "first" }, refs: [] },
      { id: "same", order: 1, message: { role: "notice", recordId: "same", content: "second" }, refs: [] },
    ], pattern: /duplicate transcript snapshot record identity/ },
    { name: "content-ref", records: [{ id: "owner", order: 0, message: { role: "notice", recordId: "owner", content: "preview" },
      refs: [{ snapshotId: "cut", recordId: "different", path: ["content"], bytes: 100 }] }], pattern: /content identity mismatch/ },
    { name: "embedded-record", records: [{ id: "owner", order: 0, message: { role: "notice", recordId: "different", content: "preview" }, refs: [] }],
      pattern: /record identity mismatch/ },
  ];
  for (const fixture of cases) {
    const tab = `invalid-${fixture.name}`;
    const response = initial(tab);
    response.snapshot!.records = fixture.records;
    response.snapshot!.totalRecords = fixture.records.length;
    commands.TranscriptFollowForTab = (_tab: string, request: FollowRequest) => Promise.resolve(request.close
      ? { protocolVersion: 2, subscription: tab, changes: [], resetRequired: false }
      : response);
    const follower = new TranscriptSessionFollower(tab, "", false, () => undefined);
    try { await assert.rejects(follower.start(), fixture.pattern); }
    finally { follower.stop(); getTranscriptStore().evictTab(tab); }
  }
});

for (const remote of [false, true]) {
  test(`${remote ? "remote" : "local"} follower retains an empty canonical body reference as a loadable assistant node`, async () => {
    const tab = remote ? "follow-ref-remote" : "follow-ref-local";
    let state = initialState;
    const requests: FollowRequest[] = [];
    const poll = deferred<TranscriptFollowResponse>();
    const read = async (tabId: string, request: FollowRequest): Promise<TranscriptFollowResponse> => {
      assert.equal(tabId, tab); requests.push(request);
      if (request.close) return { protocolVersion: 2, subscription: tab, changes: [], resetRequired: false };
      if (!request.subscription) return initial(tab);
      return poll.promise;
    };
    const key = remote ? "RemoteTranscriptFollowForTab" : "TranscriptFollowForTab";
    const wrong = remote ? "TranscriptFollowForTab" : "RemoteTranscriptFollowForTab";
    commands[key] = read;
    commands[wrong] = () => { throw new Error("cross-host fallback is forbidden"); };
    commands.SendForTab = () => { throw new Error("history recovery must not invoke the model"); };
    commands.RemoteSendForTab = commands.SendForTab;
    const follower = new TranscriptSessionFollower(tab, `/session/${tab}`, remote, action => { state = reducer(state, action); });
    try {
      await follower.start();
      const assistant = state.items.find(item => item.kind === "assistant");
      assert.ok(assistant, "empty inline preview with a canonical ref must not disappear");
      assert.equal(assistant.id, "m:answer");
      assert.equal(getTranscriptStore().hasContentReference(tab, "m:answer", "content"), true);
      assert.equal(state.running, false);
      assert.equal(state.transcriptProtocol, 2);
      assert.equal(requests.length, 2, "follow starts only after initial installation");
    } finally { follower.stop(); }
    await microtasks();
    assert.ok(requests.some(request => request.close && request.subscription === tab));
  });
}

test("stopped session follower cannot install a delayed baseline into its replaced tab", async () => {
  const delayed = deferred<TranscriptFollowResponse>();
  const started = deferred<void>();
  const requests: FollowRequest[] = [];
  commands.TranscriptFollowForTab = (_tab: string, request: FollowRequest) => {
    requests.push(request);
    started.resolve();
    return request.close ? Promise.resolve({ protocolVersion: 2, subscription: "stale", changes: [], resetRequired: false }) : delayed.promise;
  };
  let state = initialState;
  const follower = new TranscriptSessionFollower("stale-tab", "/session/stale", false, action => { state = reducer(state, action); });
  const loading = follower.start(); await started.promise; follower.stop(); delayed.resolve(initial("stale")); await loading; await microtasks();
  assert.equal(state.items.length, 0);
  assert.ok(requests.some(request => request.close && request.subscription === "stale"));
});

test("stopping before lazy follow startup prevents a backend subscription", async () => {
  let requests = 0;
  commands.TranscriptFollowForTab = async () => { requests++; return initial("cancelled-load"); };
  const follower = new TranscriptSessionFollower("cancelled-load", "/session/cancelled-load", false, () => {
    assert.fail("cancelled module load cannot publish state");
  });
  const loading = follower.start();
  follower.stop();
  await loading;
  assert.equal(requests, 0);
});

for (const remote of [false, true]) test(`${remote ? "remote" : "local"} active reference is complete before suffix polling`, async () => {
  const response = initial(`prefix-${remote}`);
  response.snapshot!.runtime.status = "in_progress";
  response.snapshot!.activeAttempts = [{ id: "attempt", messageId: "answer", turnId: "turn", nextIndex: 1 }];
  response.snapshot!.records = [{ id: "m:answer", order: 0, message: { role: "assistant", messageId: "answer", content: "truncated" },
    refs: [{ snapshotId: "cut", recordId: "m:answer", path: ["content"], bytes: 20 }] }];
  const content = deferred<{ data: string; nextOffset: number; done: boolean; stale: boolean }>();
  const poll = deferred<TranscriptFollowResponse>();
  let polls = 0;
  commands[remote ? "RemoteTranscriptContentForTab" : "TranscriptContentForTab"] = () => content.promise;
  commands[remote ? "RemoteTranscriptFollowForTab" : "TranscriptFollowForTab"] = (_tab: string, request: FollowRequest) => {
    if (request.close) return Promise.resolve({ protocolVersion: 2, changes: [], resetRequired: false, subscription: response.subscription });
    if (!request.subscription) return Promise.resolve(response);
    polls++; return poll.promise;
  };
  let state = initialState;
  const follower = new TranscriptSessionFollower(`prefix-${remote}`, "", remote, action => { state = reducer(state, action); });
  const starting = follower.start();
  await microtasks();
  assert.equal(polls, 0);
  assert.equal(state.items.length, 0, "partial baseline is not published");
  content.resolve({ data: "complete prefix", nextOffset: 15, done: true, stale: false });
  await starting;
  assert.equal(state.live?.text, "complete prefix");
  assert.equal(polls, 1);
  follower.stop();
});

test("v2 keeps deferred bodies through subsequent samples and attaches terminal time by backend message identity", () => {
  let state: import("../lib/useController").State = { ...initialState, transcriptProtocol: 2, running: true, activeTurnId: "turn",
    items: [
      { kind: "assistant" as const, id: "m:deferred", text: "", reasoning: "", streaming: false },
      { kind: "assistant" as const, id: "m:final", text: "answer", reasoning: "thinking", streaming: false },
    ] };
  state = reducer(state, { type: "event", e: { kind: "text", messageId: "next", text: "later sample" } });
  assert.ok(state.items.some(item => item.id === "m:deferred"), "text frames must not remove unloaded body owners");
  state = reducer(state, { type: "event", e: { kind: "turn_done", turnId: "turn" } });
  state = reducer(state, { type: "transcript_runtime", runtime: { turnId: "turn", status: "completed",
    finalMessageId: "final", durationMs: 933524, samplingCount: 72, toolCount: 72, pendingEvents: [] } });
  const final = state.items.find(item => item.id === "m:final");
  assert.ok(final?.kind === "assistant");
  assert.equal(final.turnDurationMs, 933524);
  assert.equal(final.turnFinal, true);
  assert.ok(state.items.some(item => item.id === "m:deferred"), "terminal must retain deferred body owners");
  const later = state.items.find(item => item.id === "m:next");
  assert.ok(later?.kind === "assistant");
  assert.equal(later.turnDurationMs, undefined, "array-tail sample is not the final reply");
});
