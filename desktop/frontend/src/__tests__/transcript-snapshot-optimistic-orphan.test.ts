// Run: tsx src/__tests__/transcript-snapshot-optimistic-orphan.test.ts

import { initialState, reducer } from "../lib/useController";
import { historyMessagesToItems } from "../lib/historyItems";
import { transcriptSnapshotState } from "../lib/transcriptSnapshotState";
import type { HistoryMessage } from "../lib/types";
import type { TranscriptSnapshot } from "../lib/transcriptProtocol";

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

function snapshotOf(messages: HistoryMessage[]): TranscriptSnapshot {
  const records = messages.map((message, order) => ({ id: `r${order}`, order, message, refs: [] }));
  return {
    protocolVersion: 1,
    snapshotId: "snap-1",
    identity: { sessionId: "s", headId: "h", rewriteEpoch: 0, runtimeEpoch: "epoch-2" },
    projectionRevision: 1,
    coveredThroughSeq: 10,
    records,
    activeRecords: [],
    runtime: { status: "completed", pendingEvents: [] },
    activeAttempts: [],
    before: 0,
    hasOlder: false,
    totalRecords: records.length,
    totalTurns: records.length,
    stale: false,
  } as unknown as TranscriptSnapshot;
}

function submit(state: ReducerState, text: string, seq: number, submissionId: string): ReducerState {
  return reducer(state, { type: "user", text, seq, submissionId });
}

type UserItem = Extract<ReducerState["items"][number], { kind: "user" }>;

function userTexts(state: ReducerState): string[] {
  return state.items.filter((item): item is UserItem => item.kind === "user").map((item) => item.text);
}

const noopApply = ((state: ReducerState) => state) as never;

console.log("\ntranscript snapshot optimistic orphan reconciliation");

// The observed failure: a runtime rebuild (model switch, serve restart) re-bases
// the snapshot while a submission is pending, and history records carry no
// submission ids. The orphaned optimistic bubble must not survive as a
// duplicate turn stuck at "processing".
{
  let state = submit(initialState, "7", 0, "s1");
  const snap = snapshotOf([
    { role: "user", content: "1", raw_content: "1" },
    { role: "assistant", content: "1" },
    { role: "user", content: "7", raw_content: "7" },
    { role: "assistant", content: "7 ok" },
  ] as unknown as HistoryMessage[]);
  state = transcriptSnapshotState(state, snap, historyMessagesToItems, noopApply, Date.now());
  eq(userTexts(state).filter((text) => text === "7").length, 1, "id-less trailing user record absorbs the orphaned optimistic copy");
  eq(state.running, false, "absorbed orphan no longer reports a running turn");
  eq(state.pendingSubmissionId, undefined, "absorbed orphan clears the pending submission");
}

// A different trailing text means the server has not journaled the submission;
// the optimistic copy stays so an in-flight or lost message remains visible.
{
  let state = submit(initialState, "8", 0, "s1");
  const snap = snapshotOf([
    { role: "user", content: "1", raw_content: "1" },
    { role: "assistant", content: "1" },
  ] as unknown as HistoryMessage[]);
  state = transcriptSnapshotState(state, snap, historyMessagesToItems, noopApply, Date.now());
  eq(userTexts(state).includes("8"), true, "unmatched pending submission survives the rebase");
  eq(state.running, true, "unmatched pending submission keeps the turn indicator");
}

// Snapshots that do carry the submission id keep linking through the id path.
{
  let state = submit(initialState, "9", 0, "s1");
  const snap = snapshotOf([
    { role: "user", content: "9", raw_content: "9", submissionId: "s1" },
    { role: "assistant", content: "9 ok" },
  ] as unknown as HistoryMessage[]);
  state = transcriptSnapshotState(state, snap, historyMessagesToItems, noopApply, Date.now());
  eq(userTexts(state).filter((text) => text === "9").length, 1, "id-linked record absorbs the optimistic copy");
  eq(state.running, false, "id-linked absorption settles the turn");
}

// While the runtime still reports an active turn, the text fallback stays off:
// the trailing record may be an older sibling of the pending submission.
{
  let state = submit(initialState, "7", 0, "s2");
  const snap = snapshotOf([
    { role: "user", content: "7", raw_content: "7" },
  ] as unknown as HistoryMessage[]);
  snap.runtime = { status: "in_progress", turnId: "t9", pendingEvents: [] };
  state = transcriptSnapshotState(state, snap, historyMessagesToItems, noopApply, Date.now());
  eq(state.running, true, "active runtime keeps the optimistic submission pending");
  eq(userTexts(state).filter((text) => text === "7").length, 2, "active runtime does not absorb by text");
}

if (failed > 0) {
  console.error(`\n${failed} check(s) failed`);
  process.exit(1);
}
process.stdout.write(`\nall ${passed} checks passed\n`);
