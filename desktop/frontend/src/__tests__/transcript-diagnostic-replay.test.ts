// Run: tsx src/__tests__/transcript-diagnostic-replay.test.ts

import assert from "node:assert/strict";
import {
  createTranscriptReaderExtentGuard,
  extendTranscriptReaderExtentGuard,
  observeTranscriptReaderExtent,
  retainTranscriptReaderPaintedBaseline,
  resolveTranscriptReaderExtentCorrection,
  transcriptReaderExtentHasCollapsed,
} from "../lib/transcriptReaderExtentStability";
import { transcriptTailExtentCollapsed } from "../lib/transcriptTailSettle";
import {
  nativeDownwardCollapseReplay,
  reasoningExtentReplay,
  unloadedQuestionJumpReplay,
} from "./transcript-diagnostic-replay.fixtures";

console.log("\ntranscript diagnostic geometry replays");

const native = nativeDownwardCollapseReplay;
const guard = createTranscriptReaderExtentGuard(native.baseline, undefined, native.gestureDeltas[0])!;
for (const delta of native.gestureDeltas.slice(1)) {
  assert.equal(extendTranscriptReaderExtentGuard(guard, native.baseline, undefined, delta), true);
}
observeTranscriptReaderExtent(guard, native.collapsed);
assert.equal(transcriptReaderExtentHasCollapsed(guard), true,
  "the first diagnostic's multi-screen extent loss is classified as transient");
assert.equal(resolveTranscriptReaderExtentCorrection(guard, native.collapsed), undefined,
  "the reader does not chase the collapsed native range");
const correction = resolveTranscriptReaderExtentCorrection(guard, native.rebound);
assert.ok(correction !== undefined && correction > 1_900,
  `the rebound restores the last accepted downward position (${correction}px)`);

let paintedRange = new Map([["row-592", -103]]);
for (const rowKey of ["row-580", "row-574"]) {
  const candidate = new Map([[rowKey, 0]]);
  assert.equal(retainTranscriptReaderPaintedBaseline(paintedRange, candidate, 22_584, 22_584), true,
    `the 22584/+690 replay retains its user-painted boundary across ${rowKey}`);
  if (!retainTranscriptReaderPaintedBaseline(paintedRange, candidate, 22_584, 22_584)) paintedRange = candidate;
}
assert.equal(paintedRange.has("row-592"), true,
  "multiple no-common range commits cannot rotate away the last painted boundary");

const reasoning = reasoningExtentReplay;
const collapseFlags = reasoning.extents.slice(1).map((height, index) =>
  transcriptTailExtentCollapsed(reasoning.extents[index], height, reasoning.clientHeight));
assert.deepEqual(collapseFlags, [true, false, false, true],
  "the second diagnostic's large one-frame contractions enter the collapse filter");
assert.equal(
  transcriptTailExtentCollapsed(19_037, 19_000, reasoning.clientHeight),
  false,
  "ordinary streaming measurement jitter remains live",
);

const questionJump = unloadedQuestionJumpReplay;
assert.deepEqual(questionJump.windows.map((window) => window.rowCount), [434, 847, 994],
  "the unloaded question-jump replay preserves the observed row-count sequence");
assert.equal(questionJump.windows[questionJump.windows.length - 1]?.firstTurn, questionJump.requestedTurn,
  "the final targeted page contains the requested anonymous turn");

const serialized = JSON.stringify({ native, reasoning, questionJump });
for (const forbidden of ["text", "rowKey", "session", "path", "model", "content"]) {
  assert.equal(serialized.includes(forbidden), false, `replay fixtures exclude ${forbidden}`);
}

console.log("transcript diagnostic geometry replay tests passed");
