// Run: tsx src/__tests__/pending-prompt-stale-status.test.tsx
//
// Regression for #6429 (also #5561/#5481): switching to a session whose plan
// approval / ask is pending flashed the prompt and then lost it. The backend
// replays the prompt event when the detached runtime re-attaches, but a
// runtime snapshot fetched BEFORE that event (pre-attach ListTabs, activation
// metas) could be dispatched AFTER it — reporting the tab idle, clearing the
// prompt, and skipping the compensating replay because its pendingPrompt was
// false. Snapshots that predate the live prompt event must be ignored.

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import {
  initialState,
  promptEventClock,
  reducer,
  runtimeSnapshotPredatesPrompt,
  useController,
} from "../lib/useController";
import type { AppBindings } from "../lib/bridge";
import type { ContextInfo, EffortInfo, Meta, TabMeta, WireEvent } from "../lib/types";

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

function eq(actual: unknown, expected: unknown, label: string) {
  ok(actual === expected, `${label}${actual === expected ? "" : `: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`}`);
}

console.log("\npending prompt vs stale runtime snapshots");

// ---- reducer invariants ----

const planApprovalEvent = { kind: "approval_request", approval: { id: "plan-1", tool: "exit_plan_mode", subject: "Approve plan" } } as WireEvent;
const askEvent = { kind: "ask_request", ask: { id: "ask-1", question: "Which option?" } } as WireEvent;
const idleStatus = { type: "backend_status", running: false, pendingPrompt: false, backgroundJobs: 0, cancelRequested: false, cancellable: false } as const;

const beforePrompt = promptEventClock();
const withApproval = reducer({ ...initialState }, { type: "event", e: planApprovalEvent });
const afterPrompt = promptEventClock();

eq(withApproval.approval?.id, "plan-1", "approval event arms the prompt");
ok(typeof withApproval.promptArrivedAt === "number", "approval event records its arrival time");

const staleIdle = reducer(withApproval, { ...idleStatus, snapshotAt: beforePrompt });
eq(staleIdle, withApproval, "idle snapshot fetched before the prompt event is ignored");
eq(staleIdle.approval?.id, "plan-1", "stale idle snapshot keeps the approval visible");
eq(staleIdle.pendingPrompt, true, "stale idle snapshot keeps the prompt gate");
eq(staleIdle.running, true, "stale idle snapshot keeps the tab blocked on the user");

const tieIdle = reducer(withApproval, { ...idleStatus, snapshotAt: withApproval.promptArrivedAt });
eq(tieIdle, withApproval, "snapshot tied with the prompt arrival counts as stale");

const staleRunning = reducer(withApproval, { type: "backend_status", running: true, pendingPrompt: false, backgroundJobs: 0, cancelRequested: false, cancellable: true, snapshotAt: beforePrompt });
eq(staleRunning, withApproval, "stale running snapshot cannot drop the prompt gate either");

const freshIdle = reducer(withApproval, { ...idleStatus, snapshotAt: afterPrompt });
eq(freshIdle.approval, undefined, "idle snapshot fetched after the prompt event still reconciles a dead prompt");
eq(freshIdle.running, false, "fresh idle snapshot ends the turn");

const legacyIdle = reducer(withApproval, { ...idleStatus });
eq(legacyIdle.approval, undefined, "snapshot without freshness metadata keeps the legacy clearing behavior");

const withAsk = reducer({ ...initialState }, { type: "event", e: askEvent });
const staleAskIdle = reducer(withAsk, { ...idleStatus, snapshotAt: beforePrompt });
eq(staleAskIdle.ask?.id, "ask-1", "stale idle snapshot keeps the ask card visible");
const freshAskIdle = reducer(withAsk, { ...idleStatus, snapshotAt: promptEventClock() });
eq(freshAskIdle.ask, undefined, "fresh idle snapshot still reconciles a dead ask");

// A replay of the SAME prompt id keeps the original arrival time — it must not
// advance the anchor, or an authoritative post-answer idle snapshot would look
// stale (#6432 reverse race).
const replayed = reducer(withApproval, { type: "event", e: planApprovalEvent });
eq(replayed.promptArrivedAt, withApproval.promptArrivedAt, "same-id replay keeps the original arrival time");
eq(replayed.promptArrivedId, "plan-1", "same-id replay keeps the anchor id");

// #6432 reverse race: user answers, a delayed replay of the SAME answered
// prompt id must not re-arm it at all — no downstream snapshot or turn_done
// is guaranteed to ever get a chance to disprove it (round 2 review: an idle
// snapshot dispatched before the replay has nothing to reject, and a fresh
// running=true/pendingPrompt=false snapshot never touches approval/ask).
{
  const armed = reducer({ ...initialState }, { type: "event", e: planApprovalEvent });
  const originalArrival = armed.promptArrivedAt!;
  const answeredEarly = reducer(armed, { type: "clearApproval" });
  eq(answeredEarly.resolvedPromptId, "plan-1", "answering records the resolved prompt id");
  const replayed = reducer(answeredEarly, { type: "event", e: planApprovalEvent });
  eq(replayed.approval, undefined, "a same-id replay of an answered prompt is ignored, not re-armed");
  eq(replayed.running, answeredEarly.running, "an ignored replay leaves running/turnActive exactly as the answer left them");
  eq(replayed.promptArrivedAt, originalArrival, "an ignored replay leaves the original anchor untouched");
  const afterTurnDone = reducer(replayed, { type: "event", e: { kind: "turn_done" } as WireEvent });
  eq(afterTurnDone.approval, undefined, "turn_done cannot resurrect a replay that was never re-armed");

  // Round 2, sequence 1: an idle snapshot dispatched between the answer and
  // the delayed replay has nothing to reject (no live approval to compare
  // against) — the replay must still be suppressed when it lands after.
  const idleBetween = reducer(answeredEarly, { ...idleStatus, snapshotAt: promptEventClock() });
  const replayAfterIdle = reducer(idleBetween, { type: "event", e: planApprovalEvent });
  eq(replayAfterIdle.approval, undefined, "a replay landing after an already-applied idle snapshot is still ignored");
  const afterTurnDone2 = reducer(replayAfterIdle, { type: "event", e: { kind: "turn_done" } as WireEvent });
  eq(afterTurnDone2.approval, undefined, "turn_done stays clear after the idle-then-replay ordering");

  // Round 2, sequence 2: a fresh running=true/pendingPrompt=false snapshot
  // (backend genuinely executing the approved plan, no prompt pending) must
  // not be able to inherit a zombie approval, because there is none to inherit.
  const busySnapshot = reducer(answeredEarly, {
    type: "backend_status",
    running: true,
    pendingPrompt: false,
    backgroundJobs: 0,
    cancelRequested: false,
    cancellable: true,
    snapshotAt: promptEventClock(),
  });
  const replayDuringBusy = reducer(busySnapshot, { type: "event", e: planApprovalEvent });
  eq(replayDuringBusy.approval, undefined, "a replay during a genuinely busy, non-pending turn is still ignored");
}

// #6432 round 3, finding 1 (P1): a controller rebuild (model/effort/token-mode
// switch) reissues approval/ask ids from "1" (per-controller counters, see
// sound.ts). Without resetting the id-anchored bookkeeping, a genuinely new
// prompt from the rebuilt controller reusing an old id would be misread as a
// stale replay of one the OLD controller already resolved, and silently
// swallowed forever.
{
  const armed = reducer({ ...initialState }, { type: "event", e: planApprovalEvent });
  const answeredEarly = reducer(armed, { type: "clearApproval" });
  eq(answeredEarly.resolvedPromptId, "plan-1", "answering records the resolved id before the rebuild");
  const rebuilt = reducer(answeredEarly, { type: "controller_rebuilt" });
  eq(rebuilt.resolvedPromptId, undefined, "a controller rebuild drops the resolved-id bookkeeping");
  eq(rebuilt.promptArrivedId, undefined, "a controller rebuild drops the prompt arrival anchor id");
  eq(rebuilt.promptArrivedAt, undefined, "a controller rebuild drops the prompt arrival anchor time");
  // The new controller's own first prompt happens to reuse id "plan-1".
  const freshPromptSameId = reducer(rebuilt, { type: "event", e: planApprovalEvent });
  eq(freshPromptSameId.approval?.id, "plan-1", "a genuinely new prompt reusing an old id after rebuild is armed, not swallowed");
  eq(freshPromptSameId.pendingPrompt, true, "the rebuilt controller's new prompt blocks the tab as expected");
}

// #6432 round 3, finding 2 (P2): the optimistic clearApproval/clearAsk
// tombstone must not be permanent when the backend call it anticipated
// actually fails — the prompt is still genuinely pending server-side, and a
// later replay must be able to recover it instead of being swallowed forever.
{
  const armed = reducer({ ...initialState }, { type: "event", e: planApprovalEvent });
  const answeredOptimistically = reducer(armed, { type: "clearApproval" });
  eq(answeredOptimistically.resolvedPromptId, "plan-1", "the optimistic answer records a tombstone before the backend call resolves");
  const submitFailed = reducer(answeredOptimistically, { type: "submit_prompt_failed", id: "plan-1", epoch: answeredOptimistically.promptEpoch });
  eq(submitFailed.resolvedPromptId, undefined, "a failed submit undoes the tombstone for that id");
  const recovered = reducer(submitFailed, { type: "event", e: planApprovalEvent });
  eq(recovered.approval?.id, "plan-1", "a replay after a failed submit can recover the still-pending prompt");

  // A failure report for an id that is no longer the current tombstone (e.g.
  // a stale/duplicate failure callback) must not clobber a newer one.
  const armed2 = reducer({ ...initialState }, { type: "event", e: { kind: "approval_request", approval: { id: "plan-2", tool: "exit_plan_mode", subject: "Approve plan" } } as WireEvent });
  const answered2 = reducer(armed2, { type: "clearApproval" });
  eq(answered2.resolvedPromptId, "plan-2", "answering the second prompt records its own tombstone");
  const staleFailure = reducer(answered2, { type: "submit_prompt_failed", id: "plan-1", epoch: answered2.promptEpoch });
  eq(staleFailure.resolvedPromptId, "plan-2", "a stale failure for an older id does not clobber the current tombstone");
}

// #6432 round 4, finding 1 (P1): a tool-approval posture switch (auto/yolo)
// only auto-allows a SUBSET of pending approvals backend-side (drainLocked
// keeps fresh plan/memory/sandbox-escape decisions pending, and auto keeps
// approvals an allow policy would not cover). The frontend must dismiss +
// tombstone only the prompt ids the backend reports as drained — blanket-
// tombstoning the visible prompt would filter every future replay of a
// prompt the backend still holds, stranding the turn with no card to answer.
{
  // Backend kept the fresh plan approval: not in the drained set → stays.
  const armed = reducer({ ...initialState }, { type: "event", e: planApprovalEvent });
  const notDrained = reducer(armed, { type: "approval_drained", ids: ["7"], epoch: armed.promptEpoch });
  eq(notDrained, armed, "a drain report not covering the visible approval leaves the state untouched");
  eq(notDrained.approval?.id, "plan-1", "the still-pending plan approval card survives the yolo switch");
  eq(notDrained.resolvedPromptId, undefined, "no tombstone is written for a prompt the backend still holds");
  const replayed = reducer(notDrained, { type: "event", e: planApprovalEvent });
  eq(replayed.approval?.id, "plan-1", "a later replay of the still-pending prompt re-arms it");

  const emptyDrain = reducer(armed, { type: "approval_drained", ids: [], epoch: armed.promptEpoch });
  eq(emptyDrain, armed, "an empty drain report is a no-op");

  // Backend drained the ordinary tool approval: dismissed + tombstoned so a
  // delayed re-delivery cannot resurrect it (round 2 contract preserved).
  const bashEvent = { kind: "approval_request", approval: { id: "bash-3", tool: "bash", subject: "rm -rf build" } } as WireEvent;
  const armedBash = reducer({ ...initialState }, { type: "event", e: bashEvent });
  const drained = reducer(armedBash, { type: "approval_drained", ids: ["bash-3"], epoch: armedBash.promptEpoch });
  eq(drained.approval, undefined, "a drained approval is dismissed");
  eq(drained.resolvedPromptId, "bash-3", "a drained approval is tombstoned like an answered one");
  const zombieReplay = reducer(drained, { type: "event", e: bashEvent });
  eq(zombieReplay.approval, undefined, "a delayed re-delivery of the drained prompt stays suppressed");
}

// #6432 round 4, finding 2 (P2): a late submit failure from BEFORE a
// controller rebuild must not undo the tombstone the NEW controller's answer
// wrote for the same numeric id — approval ids restart from "1" per
// controller, so the old failure names a different prompt.
{
  const armed = reducer({ ...initialState }, { type: "event", e: planApprovalEvent });
  const epochA = armed.promptEpoch;
  const answeredA = reducer(armed, { type: "clearApproval" });
  // Controller rebuild lands while the epoch-A RPC is still in flight.
  const rebuilt = reducer(answeredA, { type: "controller_rebuilt" });
  eq(rebuilt.promptEpoch, epochA + 1, "a controller rebuild advances the prompt epoch");
  // The rebuilt controller reissues id "plan-1"; the user answers it too.
  const armedB = reducer(rebuilt, { type: "event", e: planApprovalEvent });
  const answeredB = reducer(armedB, { type: "clearApproval" });
  eq(answeredB.resolvedPromptId, "plan-1", "the new controller's answer records its own tombstone");
  // The old controller's RPC failure finally lands, carrying the old epoch.
  const staleEpochFailure = reducer(answeredB, { type: "submit_prompt_failed", id: "plan-1", epoch: epochA });
  eq(staleEpochFailure.resolvedPromptId, "plan-1", "a failure from a pre-rebuild epoch cannot erase the new controller's tombstone");
  const zombie = reducer(staleEpochFailure, { type: "event", e: planApprovalEvent });
  eq(zombie.approval, undefined, "the answered prompt's delayed replay stays suppressed after the stale failure");
  // Same-epoch failures still recover the genuinely-unresolved prompt.
  const currentEpochFailure = reducer(answeredB, { type: "submit_prompt_failed", id: "plan-1", epoch: answeredB.promptEpoch });
  eq(currentEpochFailure.resolvedPromptId, undefined, "a same-epoch failure still undoes the tombstone");
  // reset() starts a new session (new controller, ids restart) — the epoch
  // advances there too so pre-reset failures cannot touch post-reset state.
  const resetState = reducer(answeredB, { type: "reset" });
  eq(resetState.promptEpoch, answeredB.promptEpoch + 1, "a session reset advances the prompt epoch too");
}

// #6432 round 5 (P2): a mode-switch drain result belongs to the controller
// epoch where its RPC started. If that controller is rebuilt before the RPC
// resolves, the replacement controller may reuse the same approval id; the
// old result must not dismiss or tombstone the replacement's prompt.
{
  const approval = { kind: "approval_request", approval: { id: "1", tool: "bash", subject: "old controller" } } as WireEvent;
  const armedA = reducer({ ...initialState }, { type: "event", e: approval });
  const epochA = armedA.promptEpoch;
  const rebuilt = reducer(armedA, { type: "controller_rebuilt" });
  const freshApproval = { kind: "approval_request", approval: { id: "1", tool: "bash", subject: "new controller" } } as WireEvent;
  const armedB = reducer(rebuilt, { type: "event", e: freshApproval });

  const staleDrain = reducer(armedB, { type: "approval_drained", ids: ["1"], epoch: epochA });
  eq(staleDrain.approval?.subject, "new controller", "a pre-rebuild drain result cannot dismiss the new controller's same-id approval");
  eq(staleDrain.resolvedPromptId, undefined, "a stale drain result cannot tombstone the new controller's prompt id");

  const currentDrain = reducer(armedB, { type: "approval_drained", ids: ["1"], epoch: armedB.promptEpoch });
  eq(currentDrain.approval, undefined, "a same-epoch drain still dismisses the backend-drained approval");
  eq(currentDrain.resolvedPromptId, "1", "a same-epoch drain still tombstones the drained approval");
}

// A genuinely new prompt (different id) after an answer re-anchors, so its own
// stale pre-arrival snapshot is still rejected (#6429 preserved).
{
  const armed = reducer({ ...initialState }, { type: "event", e: planApprovalEvent });
  const answeredEarly = reducer(armed, { type: "clearApproval" });
  const betweenPrompts = promptEventClock();
  const nextPrompt = reducer(answeredEarly, { type: "event", e: { kind: "approval_request", approval: { id: "plan-2", tool: "exit_plan_mode", subject: "Approve plan" } } as WireEvent });
  ok((nextPrompt.promptArrivedAt ?? 0) > betweenPrompts, "a new prompt id re-anchors the arrival time");
  const staleForNext = reducer(nextPrompt, { ...idleStatus, snapshotAt: betweenPrompts });
  eq(staleForNext.approval?.id, "plan-2", "a stale snapshot predating the new prompt is still rejected");
}

// backend_activation_start drops the anchor so a post-activation replay
// re-anchors against the activation (#6429 tab-switch path).
{
  const stale = reducer({ ...initialState }, { type: "event", e: planApprovalEvent });
  const activated = reducer(stale, { type: "backend_activation_start" });
  eq(activated.promptArrivedId, undefined, "activation drops the prompt anchor");
  eq(activated.promptArrivedAt, undefined, "activation drops the prompt arrival time");
}

// A new user turn drops the anchor so the next turn's prompts re-anchor fresh.
{
  const armed = reducer({ ...initialState }, { type: "event", e: planApprovalEvent });
  const answeredEarly = reducer(armed, { type: "clearApproval" });
  const nextTurn = reducer(answeredEarly, { type: "user", text: "continue", seq: 0 });
  eq(nextTurn.promptArrivedId, undefined, "a new user message drops the prompt anchor id");
  eq(nextTurn.promptArrivedAt, undefined, "a new user message drops the prompt arrival time");
}

const answered = reducer(withApproval, { type: "clearApproval" });
eq(answered.approval, undefined, "explicit answer clears the prompt");
const idleAfterAnswer = reducer(answered, { ...idleStatus, snapshotAt: beforePrompt });
eq(idleAfterAnswer.running, false, "without a live prompt, even old snapshots reconcile normally");

eq(runtimeSnapshotPredatesPrompt(withApproval, beforePrompt), true, "predates: snapshot older than the prompt");
eq(runtimeSnapshotPredatesPrompt(withApproval, afterPrompt), false, "predates: snapshot newer than the prompt");
eq(runtimeSnapshotPredatesPrompt(withApproval, undefined), false, "predates: unknown snapshot freshness is not stale");
eq(runtimeSnapshotPredatesPrompt({ ...initialState }, beforePrompt), false, "predates: no live prompt means nothing to protect");
eq(runtimeSnapshotPredatesPrompt(undefined, beforePrompt), false, "predates: missing state is not stale");

// Every runtime-status dispatch must carry the fetch time of its snapshot; a
// two-argument call reintroduces the unguarded clearing path.
const here = dirname(fileURLToPath(import.meta.url));
const controllerSource = readFileSync(resolve(here, "../lib/useController.ts"), "utf8");
const twoArgStatusCalls = controllerSource.match(/dispatchRuntimeStatusForTab\(\s*[^(),]+,\s*[^(),]+\s*\)/g) ?? [];
eq(twoArgStatusCalls.length, 0, "every dispatchRuntimeStatusForTab call passes its snapshot fetch time");

// ---- hook-level race: replayed approval vs in-flight stale ListTabs ----

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
globalThis.localStorage = dom.window.localStorage;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);

function flushPromises(): Promise<void> {
  return new Promise((resolvePromise) => setTimeout(resolvePromise, 0));
}

function deferred<T>() {
  let resolvePromise!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolvePromise = res;
  });
  return { promise, resolve: resolvePromise };
}

async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 50; attempt += 1) {
    await act(async () => {
      await flushPromises();
    });
    if (predicate()) return;
  }
  throw new Error(`timed out waiting for ${label}`);
}

function tabMeta(): TabMeta {
  return {
    id: "tab-a",
    scope: "project",
    workspaceRoot: "/repo",
    workspaceName: "repo",
    workspacePath: "/repo",
    topicId: "topic-a",
    topicTitle: "General",
    sessionPath: "/repo/sessions/tab-a.jsonl",
    label: "model",
    ready: true,
    running: false,
    cancellable: false,
    mode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    active: true,
    cwd: "/repo",
  };
}

function metaForTab(): Meta {
  return {
    label: "model",
    ready: true,
    eventChannel: "agent:event",
    cwd: "/repo",
    workspaceRoot: "/repo",
    workspaceName: "repo",
    workspacePath: "/repo",
    autoApproveTools: false,
    bypass: false,
    collaborationMode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    goal: "",
    goalStatus: "stopped",
  };
}

const context: ContextInfo = { used: 0, window: 100, sessionTokens: 0 };
const effortInfo: EffortInfo = { supported: true, current: "auto", default: "auto", levels: ["auto"] };
const eventHandlers: Array<(e: WireEvent) => void> = [];
const rebuiltHandlers: Array<(tabId?: string) => void> = [];
let holdNextListTabs: Promise<void> | undefined;
let modeDrain: ReturnType<typeof deferred<string[]>> | undefined;
let toolApprovalModeDrain: ReturnType<typeof deferred<string[]>> | undefined;

window.runtime = {
  EventsOn: (name: string, cb: (payload: unknown) => void) => {
    if (name === "agent:event") eventHandlers.push(cb as (e: WireEvent) => void);
    if (name === "runtime:rebuilt") rebuiltHandlers.push(cb as (tabId?: string) => void);
    return () => {};
  },
  BrowserOpenURL: () => {},
};
window.go = {
  main: {
    App: {
      ListTabs: async () => {
        if (holdNextListTabs) {
          const gatePromise = holdNextListTabs;
          holdNextListTabs = undefined;
          await gatePromise;
        }
        return [tabMeta()];
      },
      MetaForTab: async () => metaForTab(),
      ContextUsageForTab: async () => context,
      EffortForTab: async () => effortInfo,
      BalanceForTab: async () => ({ available: false, display: "" }),
      JobsForTab: async () => [],
      CheckpointsForTab: async () => [],
      HistoryForTab: async () => [],
      HistoryPageForTab: async () => ({ messages: [], startTurn: 0, endTurn: 0, totalTurns: 0, hasOlder: false }),
      HistoryCheckpointTurnsForTab: async () => [],
      ReplayPendingPrompts: async () => {},
      SetActiveTab: async () => {},
      SetModeForTab: async () => modeDrain?.promise ?? [],
      SetToolApprovalModeForTab: async () => toolApprovalModeDrain?.promise ?? [],
    } as Partial<AppBindings> as AppBindings,
  },
};

type Controller = ReturnType<typeof useController>;
let controller: Controller | undefined;

function Probe() {
  controller = useController();
  return null;
}

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");
const root = createRoot(rootEl);

await act(async () => {
  root.render(<Probe />);
  await flushPromises();
});
await waitFor("active tab", () => controller?.activeTabId === "tab-a");
await act(async () => {
  await flushPromises();
  await flushPromises();
});

// A reconciliation fetch starts (its snapshot time is captured now), then the
// backend attach replays the pending plan approval before the fetch resolves.
const gate = deferred<void>();
holdNextListTabs = gate.promise;
let syncPromise: Promise<string | undefined> | undefined;
await act(async () => {
  syncPromise = controller?.syncActiveTab(false);
  await flushPromises();
});
await act(async () => {
  for (const handler of eventHandlers) {
    handler({ kind: "approval_request", tabId: "tab-a", approval: { id: "plan-live", tool: "exit_plan_mode", subject: "Approve plan" } } as WireEvent);
  }
  await flushPromises();
});
eq(controller?.state.approval?.id, "plan-live", "replayed plan approval renders while a snapshot fetch is in flight");

await act(async () => {
  gate.resolve();
  await syncPromise;
  await flushPromises();
});
eq(controller?.state.approval?.id, "plan-live", "a snapshot fetched before the prompt event cannot clear the approval");
eq(controller?.state.pendingPrompt, true, "the prompt gate survives the stale reconciliation");
eq(controller?.state.running, true, "the tab stays blocked on the user after the stale reconciliation");

// A snapshot fetched after the event still reconciles: if the backend truly
// has no pending prompt anymore, the zombie prompt is cleared.
await act(async () => {
  await controller?.syncActiveTab(false);
  await flushPromises();
});
eq(controller?.state.approval?.id, undefined, "a snapshot fetched after the prompt event still reconciles a dead prompt");
eq(controller?.state.running, false, "fresh idle snapshot releases the blocked state");

// #6432 backstop (reviewer round 2): after navigation drops the prompt anchor
// (backend_activation_start on a rapid A→B→A, or single-surface state wipe), a
// delayed replay of an already-answered prompt re-anchors it, so the
// authoritative post-answer idle snapshot looks stale and is rejected — leaving
// a zombie the frontend heuristic cannot disprove. The rejection must schedule a
// fresh reconcile that refetches backend truth and clears the resolved prompt.
{
  // A snapshot fetch starts (its time is captured), then a prompt event arrives,
  // so the snapshot is stale relative to the prompt when it finally dispatches.
  const staleGate = deferred<void>();
  holdNextListTabs = staleGate.promise;
  let staleSync: Promise<string | undefined> | undefined;
  await act(async () => {
    staleSync = controller?.syncActiveTab(false);
    await flushPromises();
  });
  await act(async () => {
    for (const handler of eventHandlers) {
      handler({ kind: "approval_request", tabId: "tab-a", approval: { id: "plan-zombie", tool: "exit_plan_mode", subject: "Approve plan" } } as WireEvent);
    }
    await flushPromises();
  });
  eq(controller?.state.approval?.id, "plan-zombie", "zombie approval is armed after the snapshot fetch started");
  await act(async () => {
    staleGate.resolve();
    await staleSync;
    await flushPromises();
  });
  eq(controller?.state.approval?.id, "plan-zombie", "the stale idle snapshot is rejected, the prompt survives for now");
  // The backend reports idle (the prompt was resolved); the scheduled fresh
  // reconcile refetches that truth and clears the zombie, unlocking input.
  await act(async () => {
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 300));
    await flushPromises();
  });
  eq(controller?.state.approval?.id, undefined, "the scheduled fresh reconcile clears the zombie the stale rejection preserved");
  eq(controller?.state.running, false, "the fresh reconcile unlocks the input after clearing the zombie");
}

// Both mode-switch entry points capture the prompt epoch before starting their
// backend RPC. A rebuild and same-id prompt can arrive while either call is in
// flight; its old drain result must then be ignored.
{
  const approvalID = "mode-drain-1";
  await act(async () => {
    for (const handler of eventHandlers) {
      handler({ kind: "approval_request", tabId: "tab-a", approval: { id: approvalID, tool: "bash", subject: "old controller mode prompt" } } as WireEvent);
    }
    await flushPromises();
  });
  modeDrain = deferred<string[]>();
  let switchPromise: Promise<void> | undefined;
  await act(async () => {
    switchPromise = controller?.setControllerMode("plan");
    await flushPromises();
  });
  await act(async () => {
    for (const handler of rebuiltHandlers) handler("tab-a");
    for (const handler of eventHandlers) {
      handler({ kind: "approval_request", tabId: "tab-a", approval: { id: approvalID, tool: "bash", subject: "new controller mode prompt" } } as WireEvent);
    }
    await flushPromises();
  });
  await act(async () => {
    modeDrain?.resolve([approvalID]);
    await switchPromise;
    await flushPromises();
  });
  eq(controller?.state.approval?.subject, "new controller mode prompt", "a late SetModeForTab drain cannot dismiss a new same-id prompt");

  const toolApprovalID = "tool-mode-drain-1";
  await act(async () => {
    for (const handler of eventHandlers) {
      handler({ kind: "approval_request", tabId: "tab-a", approval: { id: toolApprovalID, tool: "bash", subject: "old tool-approval prompt" } } as WireEvent);
    }
    await flushPromises();
  });
  toolApprovalModeDrain = deferred<string[]>();
  let toolSwitchPromise: Promise<void> | undefined;
  await act(async () => {
    toolSwitchPromise = controller?.setToolApprovalModeForTab("tab-a", "auto");
    await flushPromises();
  });
  await act(async () => {
    for (const handler of rebuiltHandlers) handler("tab-a");
    for (const handler of eventHandlers) {
      handler({ kind: "approval_request", tabId: "tab-a", approval: { id: toolApprovalID, tool: "bash", subject: "new tool-approval prompt" } } as WireEvent);
    }
    await flushPromises();
  });
  await act(async () => {
    toolApprovalModeDrain?.resolve([toolApprovalID]);
    await toolSwitchPromise;
    await flushPromises();
  });
  eq(controller?.state.approval?.subject, "new tool-approval prompt", "a late SetToolApprovalModeForTab drain cannot dismiss a new same-id prompt");
}

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
