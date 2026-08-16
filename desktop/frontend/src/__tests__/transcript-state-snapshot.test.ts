// Run: tsx src/__tests__/transcript-state-snapshot.test.ts

import type { StateSnapshot } from "react-virtuoso";
import { resolveTranscriptStateSnapshot } from "../lib/transcriptStateSnapshot";

let passed = 0;
let failed = 0;

function check(condition: unknown, label: string) {
  if (condition) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

console.log("\ntranscript state snapshot");

const base: StateSnapshot = {
  scrollTop: 420,
  ranges: [
    { startIndex: 0, endIndex: 4, size: 40 },
    { startIndex: 5, endIndex: 5, size: 120 },
    { startIndex: 6, endIndex: Infinity, size: 64 },
  ],
};
const keys = ["a", "b", "c"];

// ── T6: identical keys restore the snapshot as-is
const identical = resolveTranscriptStateSnapshot({ keys, snapshot: base }, ["a", "b", "c"]);
check(identical === base, "identical row keys restore the captured snapshot");

// React Virtuoso requires the same data and totalCount for restoreStateFrom.
const appended = resolveTranscriptStateSnapshot({ keys, snapshot: base }, ["a", "b", "c", "d", "e"]);
check(appended === undefined, "appended rows discard a snapshot with a different totalCount");

// ── T6: disjoint keys discard the snapshot (session switch falls back to
// measured-height estimates)
check(
  resolveTranscriptStateSnapshot({ keys, snapshot: base }, ["x", "y", "z"]) === undefined,
  "a different key sequence discards the snapshot",
);
check(
  resolveTranscriptStateSnapshot({ keys, snapshot: base }, ["a", "b"]) === undefined,
  "a truncated key sequence (rewind) discards the snapshot",
);
check(resolveTranscriptStateSnapshot(null, keys) === undefined, "no capture means no restore");
check(
  resolveTranscriptStateSnapshot({ keys: [], snapshot: base }, keys) === undefined,
  "an empty capture never restores",
);

// ── T9: prepended rows also change data/totalCount and must discard
const prepended = resolveTranscriptStateSnapshot({ keys, snapshot: base }, ["n1", "n2", "a", "b", "c"]);
check(prepended === undefined, "prepended rows discard the snapshot instead of translating internal ranges");
check(base.ranges[0].startIndex === 0, "discarding changed data never mutates the captured snapshot");

if (failed > 0) {
  console.error(`\n${failed} transcript state snapshot test(s) failed; ${passed} passed.`);
  process.exit(1);
}
console.log(`\n${passed} transcript state snapshot tests passed.`);
