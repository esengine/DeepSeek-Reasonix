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
  const state = reducer({ ...initialState, items: [final] }, { type: "transcript_records", projection: {
    items: [{ ...final, turnFinal: undefined, turnDurationMs: undefined, samplingCount: undefined, toolCount: undefined }],
    removeIds: [], startTurn: 0, endTurn: 1, totalTurns: 1, hasOlder: false, hasNewer: false, revision: 2, revisionKnown: true, digest: "cut",
  } });
  assert.equal(state.items[0].kind === "assistant" && state.items[0].turnDurationMs, 933524);
});
async function microtasks() { for (let i = 0; i < 16; i++) await Promise.resolve(); }
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
  const requests: FollowRequest[] = [];
  commands.TranscriptFollowForTab = (_tab: string, request: FollowRequest) => {
    requests.push(request);
    return request.close ? Promise.resolve({ protocolVersion: 2, subscription: "stale", changes: [], resetRequired: false }) : delayed.promise;
  };
  let state = initialState;
  const follower = new TranscriptSessionFollower("stale-tab", "/session/stale", false, action => { state = reducer(state, action); });
  const loading = follower.start(); follower.stop(); delayed.resolve(initial("stale")); await loading; await microtasks();
  assert.equal(state.items.length, 0);
  assert.ok(requests.some(request => request.close && request.subscription === "stale"));
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
