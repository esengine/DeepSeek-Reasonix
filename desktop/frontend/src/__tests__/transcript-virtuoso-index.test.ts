// Run: node --import tsx src/__tests__/transcript-virtuoso-index.test.ts

import {
  TRANSCRIPT_VIRTUOSO_INDEX_BASE,
  reconcileTranscriptVirtuosoIndex,
  type TranscriptVirtuosoIndexState,
} from "../lib/transcriptVirtuosoIndex";
import type { TranscriptRow } from "../lib/transcriptRows";

let passed = 0;
let failed = 0;

function equal(actual: unknown, expected: unknown, label: string) {
  if (JSON.stringify(actual) === JSON.stringify(expected)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
    failed += 1;
  }
}

function row(key: string): TranscriptRow {
  return { kind: "turn-actions", key, turn: 0, text: key };
}

console.log("\ntranscript Virtuoso index");

const initialRows = [row("a"), row("b"), row("c")];
const initial: TranscriptVirtuosoIndexState = {
  resetKey: "tab-a:0",
  keys: initialRows.map((item) => String(item.key)),
  firstItemIndex: TRANSCRIPT_VIRTUOSO_INDEX_BASE,
};

const prepend = reconcileTranscriptVirtuosoIndex(initial, [row("old-1"), row("old-2"), ...initialRows], "tab-a:0");
equal(prepend.firstItemIndex, TRANSCRIPT_VIRTUOSO_INDEX_BASE - 2, "prepend decreases firstItemIndex by the inserted row count");
equal(prepend.firstItemIndex + 2, initial.firstItemIndex, "the old first row keeps its absolute Virtuoso index");

const append = reconcileTranscriptVirtuosoIndex(initial, [...initialRows, row("d")], "tab-a:0");
equal(append.firstItemIndex, initial.firstItemIndex, "tail append keeps firstItemIndex stable");

const contentOnly = reconcileTranscriptVirtuosoIndex(initial, initialRows.map((item) => ({ ...item })), "tab-a:0");
equal(contentOnly, initial, "content-only updates do not perturb scroll indexing");

const switched = reconcileTranscriptVirtuosoIndex(prepend, [row("x"), row("y")], "tab-b:0");
equal(switched.firstItemIndex, TRANSCRIPT_VIRTUOSO_INDEX_BASE, "tab switch resets the independent index space");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
