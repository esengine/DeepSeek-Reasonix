// Run: tsx src/__tests__/transcript-measured-sizes.test.ts
//
// Phase 1 of the scroll-arbiter refactor (#8657):
// - EAW-aware text estimation: full-width scripts wrap at roughly half the
//   character count, so CJK rows must estimate higher than the old
//   char-based formula; half-width text stays close to the old behavior.
// - Measured-size cache: exact rowKey hit beats kind median beats static
//   prior; medians converge on recorded samples; clear() drops everything.

import {
  eastAsianWidthColumns,
  estimateTranscriptTextHeight,
  TRANSCRIPT_ESTIMATED_LINE_COLUMNS,
} from "../lib/transcriptRowEstimates";
import { estimateTranscriptRowSize, type TranscriptRow } from "../lib/transcriptRows";
import { createTranscriptMeasuredSizes } from "../lib/transcriptMeasuredSizes";

let passed = 0;
let failed = 0;

function ok(cond: unknown, label: string) {
  if (cond) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function eq<T>(actual: T, expected: T, label: string) {
  if (actual === expected) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${String(expected)}, got ${String(actual)}\n`);
    failed += 1;
  }
}

// ── EAW column counting ────────────────────────────────────────────────────

eq(eastAsianWidthColumns("hello"), 5, "half-width ASCII counts one column each");
eq(eastAsianWidthColumns("你好世界"), 8, "CJK ideographs count two columns each");
eq(eastAsianWidthColumns("你好ab"), 6, "mixed width adds up per code point");
eq(eastAsianWidthColumns("，。"), 4, "fullwidth punctuation counts two columns");
eq(eastAsianWidthColumns("é"), 1, "combining mark adds zero columns");

// ── EAW-aware height estimation ────────────────────────────────────────────

{
  const halfWidth = "a".repeat(TRANSCRIPT_ESTIMATED_LINE_COLUMNS * 10);
  const fullWidth = "你".repeat(TRANSCRIPT_ESTIMATED_LINE_COLUMNS * 10);
  const halfHeight = estimateTranscriptTextHeight(halfWidth, 44);
  const fullHeight = estimateTranscriptTextHeight(fullWidth, 44);
  // Compare the wrapped-line portions (height minus the 44px block base):
  // full-width text wraps at half the character count, so ~2x the lines.
  ok(
    fullHeight - 44 >= (halfHeight - 44) * 1.9,
    `CJK text wraps ~2x the lines of equal-length ASCII (half=${halfHeight}, full=${fullHeight})`,
  );
}

{
  // Regression against the old char-based formula: 800 CJK chars must
  // estimate at least 1.5x what 88-chars-per-line wrapping predicted.
  const text = "汉".repeat(800);
  const oldFormulaLines = Math.ceil(800 / 88);
  const oldHeight = 44 + oldFormulaLines * 20;
  const newHeight = estimateTranscriptTextHeight(text, 44);
  ok(newHeight >= oldHeight * 1.5, `800 CJK chars estimate >= 1.5x the old formula (old=${oldHeight}, new=${newHeight})`);
}

{
  const text = "abc\ndef";
  eq(estimateTranscriptTextHeight(text, 44), 44 + 2 * 20, "explicit newlines each wrap independently");
}

{
  const ascii = "a".repeat(TRANSCRIPT_ESTIMATED_LINE_COLUMNS * 4);
  eq(
    estimateTranscriptTextHeight(ascii, 44),
    44 + 4 * 20,
    "ASCII wrapping matches the column capacity",
  );
}

ok(
  estimateTranscriptTextHeight(undefined, 96) === 96 && estimateTranscriptTextHeight("", 96) === 96,
  "empty text falls back to the kind minimum",
);

// ── Measured-size cache ────────────────────────────────────────────────────

function fakeRow(key: string, kind: TranscriptRow["kind"], text: string): TranscriptRow {
  return { key, kind, item: { id: key, text } } as unknown as TranscriptRow;
}

{
  const store = createTranscriptMeasuredSizes();
  const row = fakeRow("r1", "answer", "hello");
  ok(store.estimateFor(row) === estimateTranscriptRowSize(row), "no data falls back to the static prior");
  store.record("r1", "answer", 432);
  eq(store.estimateFor(row), 432, "exact rowKey measurement wins");
  store.record("r1", "answer", 0);
  store.record("r1", "answer", Number.NaN);
  eq(store.estimateFor(row), 432, "non-positive and NaN measurements are ignored");
}

{
  const store = createTranscriptMeasuredSizes();
  store.record("a1", "answer", 100);
  store.record("a2", "answer", 300);
  store.record("a3", "answer", 200);
  const unseen = fakeRow("a4", "answer", "some answer text that is long enough to matter");
  eq(store.estimateFor(unseen), 200, "unseen row of a sampled kind uses the kind median");
  const toolRow = fakeRow("t1", "tool", "");
  eq(store.estimateFor(toolRow), estimateTranscriptRowSize(toolRow), "unsampled kind still uses the static prior");
}

{
  const store = createTranscriptMeasuredSizes();
  const rows = [fakeRow("r1", "user", "hi"), fakeRow("r2", "answer", "hello there")];
  store.record("r1", "user", 64);
  const estimates = store.synthesize(rows);
  eq(estimates.length, 2, "synthesize stays aligned with the row array");
  eq(estimates[0], 64, "synthesize uses the exact measurement where present");
  ok(estimates[1] > 0, "synthesize falls back cleanly for unmeasured rows");
}

{
  const store = createTranscriptMeasuredSizes();
  store.record("r1", "answer", 500);
  store.clear();
  const row = fakeRow("r1", "answer", "hello");
  ok(store.estimateFor(row) === estimateTranscriptRowSize(row), "clear() drops measurements and medians");
}

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
