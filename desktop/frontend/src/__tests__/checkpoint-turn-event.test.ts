// Run: tsx src/__tests__/checkpoint-turn-event.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createTurnSubmissionId, initialState, reducer } from "../lib/useController";

type ReducerState = ReturnType<typeof reducer>;

let passed = 0;
let failed = 0;

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
    failed += 1;
  }
}

function submit(state: ReducerState, text: string, seq: number): ReducerState {
  return reducer(state, { type: "user", text, seq, submissionId: `s${seq}` });
}

function userById(state: ReducerState, id: string) {
  const item = state.items.find((candidate) => candidate.kind === "user" && candidate.id === id);
  return item?.kind === "user" ? item : undefined;
}

function checkpoint(state: ReducerState, id: string): number | undefined {
  return userById(state, id)?.checkpointTurn;
}

console.log("\nturn checkpoint submission binding");

{
  const base = createTurnSubmissionId("tab-a", 0, 0, "runtime-a");
  eq(createTurnSubmissionId("tab-b", 0, 0, "runtime-a") === base, false, "submission correlation is tab-scoped");
  eq(createTurnSubmissionId("tab-a", 1, 0, "runtime-a") === base, false, "submission correlation changes across session reset");
  eq(createTurnSubmissionId("tab-a", 0, 0, "runtime-b") === base, false, "submission correlation changes across runtime epoch");
  eq(createTurnSubmissionId("tab-a", 0, 1, "runtime-a") === base, false, "submission correlation changes across local sequence");
}

{
  let state = submit(initialState, "turn zero", 0);
  eq(userById(state, "u0")?.submissionId, "s0", "render item id and opaque submission correlation stay distinct");
  state = reducer(state, { type: "event", e: { kind: "turn_started" } });
  state = reducer(state, { type: "event", e: { kind: "notice", level: "info", text: "runtime notice" } });
  state = reducer(state, { type: "event", e: { kind: "turn_done", submissionId: "s0", checkpointTurn: 0 } });

  eq(checkpoint(state, "u0"), 0, "turn zero is assigned by exact submission id");
  eq(userById(state, "u0")?.submissionId, undefined, "TurnDone clears the settled item's correlation");
  eq(state.pendingSubmissionId, undefined, "matching TurnDone confirms the optimistic submit");
  eq(state.items.some((item) => item.kind === "notice" && item.text === "runtime notice"), true, "intervening items cannot change the target");
}

{
  let state = submit(initialState, "legacy turn", 1);
  state = reducer(state, { type: "event", e: { kind: "turn_done", checkpointTurn: 9 } });
  eq(checkpoint(state, "u1"), undefined, "TurnDone without submission id cannot stamp a local user");
  eq(state.pendingSubmissionId, "s1", "id-less TurnDone cannot confirm a pending submission");

  state = reducer(state, { type: "event", e: { kind: "turn_done", submissionId: "missing", checkpointTurn: 10 } });
  eq(checkpoint(state, "u1"), undefined, "unknown submission id cannot rewrite another user");
  state = reducer(state, { type: "event", e: { kind: "turn_done", submissionId: "s1" } });
  eq(userById(state, "u1")?.submissionId, undefined, "matching checkpoint-less TurnDone still settles the correlation");
  state = reducer(state, { type: "event", e: { kind: "turn_done", submissionId: "s1", checkpointTurn: 11 } });
  eq(checkpoint(state, "u1"), undefined, "duplicate TurnDone cannot backfill a settled checkpoint-less turn");
}

{
  let state = submit(initialState, "/effort max", 10);
  state = reducer(state, { type: "send_confirmed", submissionId: "s10" });
  eq(state.pendingSubmissionId, undefined, "successful local command Promise confirms its exact correlation");
  state = reducer(state, { type: "controller_rebuilt" });

  state = submit(state, "normal after effort", 11);
  state = reducer(state, { type: "event", e: { kind: "turn_done", submissionId: "s11", checkpointTurn: 1 } });
  eq(checkpoint(state, "u10"), undefined, "management command stays non-rewindable");
  eq(checkpoint(state, "u11"), 1, "the next normal turn maps independently after a no-TurnDone command");
}

{
  let state = submit(initialState, "activity A", 15);
  state = reducer(state, { type: "unsend" });
  state = submit(state, "activity B", 16);
  state = reducer(state, { type: "event", e: { kind: "turn_started", submissionId: "s15" } });
  eq(state.pendingSubmissionId, "s16", "old-id runtime activity cannot confirm the new pending user");
  state = reducer(state, { type: "event", e: { kind: "notice", submissionId: "s16", level: "info", text: "admitted" } });
  eq(state.pendingSubmissionId, undefined, "matching non-TurnDone activity confirms the pending user");
}

{
  let state = submit(initialState, "extension activity", 17);
  const extension = { pluginId: "demo", surfaceId: "status", kind: "status" as const, status: { label: "working" } };
  state = reducer(state, { type: "event", e: { kind: "extension_status", extension } });
  eq(state.pendingSubmissionId, "s17", "uncorrelated extension activity cannot confirm a pending user");
  state = reducer(state, { type: "event", e: { kind: "extension_status", submissionId: "s17", extension } });
  eq(state.pendingSubmissionId, undefined, "exactly correlated extension activity confirms the pending user");
}

{
  let state = submit(initialState, "lost A", 20);
  state = reducer(state, { type: "unsend" });
  state = submit(state, "B after lost A", 21);
  state = reducer(state, { type: "event", e: { kind: "turn_done", submissionId: "s21", checkpointTurn: 2 } });

  eq(checkpoint(state, "u20"), undefined, "a missing A TurnDone does not consume B ownership");
  eq(checkpoint(state, "u21"), 2, "B maps by id even when A TurnDone never arrives");
}

{
  let state = submit(initialState, "cancelled A", 30);
  state = reducer(state, { type: "unsend" });
  state = submit(state, "pending B", 31);
  state = reducer(state, { type: "event", e: { kind: "turn_done", submissionId: "s30", checkpointTurn: 3 } });

  eq(checkpoint(state, "u30"), 3, "late A TurnDone still maps exact A");
  eq(checkpoint(state, "u31"), undefined, "late A TurnDone cannot stamp B");
  eq(state.pendingSubmissionId, "s31", "late A TurnDone cannot confirm B pending state");
  eq(state.pendingUser, "pending B", "late A TurnDone cannot flush B pending text");

  state = reducer(state, { type: "event", e: { kind: "turn_done", submissionId: "s31", checkpointTurn: 4 } });
  eq(checkpoint(state, "u31"), 4, "B later receives its own checkpoint");
}

{
  let state = submit(initialState, "A before failed B", 40);
  state = reducer(state, { type: "unsend" });
  state = submit(state, "failed B", 41);
  state = reducer(state, { type: "event", e: { kind: "turn_done", submissionId: "s40", checkpointTurn: 5 } });
  state = reducer(state, { type: "send_failed", submissionId: "s41", error: "turn already running" });
  state = submit(state, "C after failed B", 42);
  state = reducer(state, { type: "event", e: { kind: "turn_done", submissionId: "s42", checkpointTurn: 6 } });

  eq(checkpoint(state, "u40"), 5, "A maps before B rejection");
  eq(userById(state, "u41")?.failed, true, "B rejection marks only B");
  eq(checkpoint(state, "u41"), undefined, "failed B remains non-rewindable");
  eq(checkpoint(state, "u42"), 6, "C maps normally after the rejected B gap");
}

{
  let state = submit(initialState, "rapid A", 50);
  state = reducer(state, { type: "unsend" });
  state = submit(state, "rapid B", 51);
  state = reducer(state, { type: "event", e: { kind: "turn_done", submissionId: "s51", checkpointTurn: 8 } });
  state = reducer(state, { type: "event", e: { kind: "turn_done", submissionId: "s50", checkpointTurn: 7 } });

  eq(checkpoint(state, "u50"), 7, "out-of-order A completion still maps A");
  eq(checkpoint(state, "u51"), 8, "out-of-order B completion still maps B");
}

{
  let state = submit(initialState, "!pwd", 60);
  state = reducer(state, { type: "event", e: { kind: "turn_done" } });
  eq(checkpoint(state, "u60"), undefined, "shell TurnDone without submission id stays non-rewindable");

  state = submit(state, "after shell", 61);
  state = reducer(state, { type: "event", e: { kind: "turn_done", submissionId: "s61", checkpointTurn: 9 } });
  eq(checkpoint(state, "u61"), 9, "shell completion cannot offset the following normal turn");
}

{
  let state = submit(initialState, "provider error", 70);
  state = reducer(state, { type: "event", e: { kind: "turn_done", submissionId: "s70", checkpointTurn: 10, err: "provider unavailable" } });
  eq(checkpoint(state, "u70"), 10, "error TurnDone maps an admitted checkpoint by id");
  eq(state.items.some((item) => item.kind === "notice" && item.text === "provider unavailable"), true, "error TurnDone still surfaces its warning");
}

{
  let state = submit(initialState, "same text", 80);
  state = reducer(state, { type: "event", e: { kind: "turn_done", submissionId: "s80", checkpointTurn: 11 } });
  state = submit(state, "same text", 81);
  state = reducer(state, { type: "event", e: { kind: "turn_done", submissionId: "s81", checkpointTurn: 12 } });
  eq(checkpoint(state, "u80"), 11, "first equal-text user keeps its checkpoint");
  eq(checkpoint(state, "u81"), 12, "second equal-text user maps independently by id");
}

{
  let state = submit(initialState, "old pending", 90);
  state = reducer(state, { type: "unsend" });
  state = submit(state, "new pending", 91);
  const unchanged = state;
  state = reducer(state, { type: "send_failed", submissionId: "s90", error: "late old rejection" });
  eq(state, unchanged, "stale send_failed is a complete no-op for a newer pending id");
  state = reducer(state, { type: "send_failed", submissionId: "s91", error: "current rejection" });
  eq(userById(state, "u91")?.failed, true, "current send_failed marks the exact pending user");
  eq(userById(state, "u91")?.submissionId, undefined, "send_failed clears the rejected item's correlation");
  eq(userById(state, "u90")?.failed, undefined, "current send_failed cannot mark an older user");
}

{
  let state = submit(initialState, "confirmed delivery", 100);
  state = reducer(state, { type: "event", e: { kind: "turn_done", submissionId: "s100", checkpointTurn: 13 } });
  const confirmed = state;
  state = reducer(state, { type: "send_failed", submissionId: "s100", error: "late bridge rejection" });
  eq(state, confirmed, "send_failed after matching TurnDone is a complete no-op");
  eq(checkpoint(state, "u100"), 13, "late failure cannot erase an assigned checkpoint");
  state = reducer(state, { type: "event", e: { kind: "turn_done", submissionId: "s100", checkpointTurn: 99 } });
  eq(checkpoint(state, "u100"), 13, "duplicate TurnDone cannot overwrite an existing checkpoint");
}

{
  let state = submit(initialState, "reset prompt", 110);
  state = reducer(state, { type: "reset" });
  state = reducer(state, { type: "event", e: { kind: "turn_done", submissionId: "s110", checkpointTurn: 14 } });
  eq(state.items.length, 0, "session reset removes the old optimistic id target");

  state = submit(state, "new runtime prompt", 111);
  state = reducer(state, { type: "controller_rebuilt" });
  eq(userById(state, "u111")?.submissionId, undefined, "controller rebuild retires the old item correlation");
  state = reducer(state, { type: "event", e: { kind: "turn_done", submissionId: "s111", checkpointTurn: 99 } });
  eq(checkpoint(state, "u111"), undefined, "late pre-rebuild TurnDone cannot stamp the retired item");
  state = submit(state, "post-rebuild prompt", 112);
  state = reducer(state, { type: "send_failed", submissionId: "s111", error: "old runtime rejection" });
  eq(state.pendingSubmissionId, "s112", "old-runtime failure cannot clear the rebuilt runtime pending correlation");
  state = reducer(state, { type: "event", e: { kind: "turn_done", submissionId: "s112", checkpointTurn: 15 } });
  eq(checkpoint(state, "u112"), 15, "post-rebuild submission maps normally");
}

{
  let state = reducer(initialState, {
    type: "history_page",
    mode: "replace",
    page: { messages: [{ role: "user", content: "recent", checkpointTurn: 60 }], startTurn: 60, endTurn: 61, totalTurns: 61, hasOlder: true },
  });
  state = submit(state, "live after pagination", 120);
  state = reducer(state, {
    type: "history_page",
    mode: "prepend",
    page: { messages: [{ role: "user", content: "older", checkpointTurn: 5 }], startTurn: 5, endTurn: 6, totalTurns: 61, hasOlder: true },
  });
  state = reducer(state, { type: "event", e: { kind: "turn_done", submissionId: "s120", checkpointTurn: 61 } });
  eq(checkpoint(state, "u120"), 61, "history prepend preserves the exact live item target");
  eq(state.items.some((item) => item.kind === "user" && item.text === "recent" && item.checkpointTurn === 60), true, "recent history keeps authoritative metadata");
  eq(state.items.some((item) => item.kind === "user" && item.text === "older" && item.checkpointTurn === 5), true, "prepended history keeps authoritative metadata");
}

{
  let state = reducer(initialState, {
    type: "history_page",
    mode: "replace",
    page: { messages: [{ role: "user", content: "authoritative", checkpointTurn: 7 }], startTurn: 7, endTurn: 8, totalTurns: 8, hasOlder: false },
  });
  state = reducer(state, { type: "event", e: { kind: "turn_done", submissionId: "u999", checkpointTurn: 99 } });
  eq(state.items[0].kind === "user" && state.items[0].checkpointTurn, 7, "unknown local id cannot rewrite hydrated history");
}

{
  const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), "../lib/useController.ts"), "utf8");
  eq(source.includes("turnUserItemIds"), false, "FIFO ownership state is completely removed");
  eq(source.includes("HistoryCheckpointTurnsForTab(targetTabId)"), false, "TurnDone hot path does not refresh full checkpoint history");
  eq(source.includes('type: "history_checkpoint_turns"'), false, "positional checkpoint merge action remains removed");
  eq(source.includes("void refreshCheckpoints(targetTabId)"), true, "TurnDone still refreshes checkpoint metadata");
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
