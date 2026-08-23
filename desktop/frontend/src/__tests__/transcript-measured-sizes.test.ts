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
import {
  buildTranscriptRows,
  buildTurnModels,
  EMPTY_FOLDS,
  estimateTranscriptRowSize,
  transcriptRowMeasurementVersion,
  type FoldMap,
  type TranscriptRow,
} from "../lib/transcriptRows";
import { createTranscriptMeasuredSizes } from "../lib/transcriptMeasuredSizes";
import { transcriptRowLayoutVariant } from "../lib/transcriptRowGeometry";
import { initialState, reducer, type Item } from "../lib/useController";

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

function estimateFor(store: ReturnType<typeof createTranscriptMeasuredSizes>, row: TranscriptRow, width?: number) {
  return estimatesFor(store, [row], width)[0];
}

function estimatesFor(store: ReturnType<typeof createTranscriptMeasuredSizes>, rows: readonly TranscriptRow[], width?: number) {
  return store.synthesizeDetailed("test-session", rows, { contentWidth: width, typographySignature: "test-font" }).heightEstimates;
}

function recordRow(store: ReturnType<typeof createTranscriptMeasuredSizes>, row: TranscriptRow, height: number, width?: number) {
  store.recordGeometry("test-session", {
    rowKey: String(row.key),
    kind: row.kind,
    layoutVariant: transcriptRowLayoutVariant(row),
    height,
    environment: { contentWidth: width, typographySignature: "test-font" },
    measurementVersion: transcriptRowMeasurementVersion(row),
    staticEstimate: estimateTranscriptRowSize(row, width),
  });
}

{
  const store = createTranscriptMeasuredSizes();
  const row = fakeRow("r1", "answer", "hello");
  ok(estimateFor(store, row) === estimateTranscriptRowSize(row), "no data falls back to the static prior");
  recordRow(store, row, 432);
  eq(estimateFor(store, row), 432, "exact rowKey measurement wins");
  recordRow(store, row, 0);
  recordRow(store, row, Number.NaN);
  eq(estimateFor(store, row), 432, "non-positive and NaN measurements are ignored");
}

// ── Geometry-contract cache ────────────────────────────────────────────────

{
  const store = createTranscriptMeasuredSizes();
  const collapsed = fakeRow("reasoning", "reasoning", "hidden body");
  collapsed.layoutVariant = "reasoning-summary";
  const expanded = { ...collapsed, layoutVariant: "reasoning-expanded" as const };
  const environment = { contentWidth: 960, typographySignature: "font-a" };
  store.recordGeometry("session-a", {
    rowKey: "reasoning",
    kind: "reasoning",
    layoutVariant: "reasoning-expanded",
    height: 1_200,
    environment,
    measurementVersion: transcriptRowMeasurementVersion(expanded),
    staticEstimate: estimateTranscriptRowSize(expanded, environment.contentWidth),
  });
  const synthesized = store.synthesizeDetailed("session-a", [collapsed], environment);
  eq(synthesized.heightEstimates[0], 64, "expanded reasoning measurement cannot poison collapsed geometry");
  eq(synthesized.estimateSources[0], "static", "cross-state rejection reports the static source");
}

{
  const store = createTranscriptMeasuredSizes();
  const row = fakeRow("answer", "answer", "wrapped answer");
  row.layoutVariant = "text-flow";
  const environment = { contentWidth: 960, typographySignature: "font-a" };
  store.recordGeometry("session-a", {
    rowKey: "answer",
    kind: "answer",
    layoutVariant: "text-flow",
    height: 320,
    environment,
    measurementVersion: transcriptRowMeasurementVersion(row),
    staticEstimate: estimateTranscriptRowSize(row, environment.contentWidth),
  });
  eq(store.synthesizeDetailed("session-a", [row], environment).heightEstimates[0], 320, "exact geometry matches all cache dimensions");
  ok(store.synthesizeDetailed("session-a", [row], { ...environment, contentWidth: 760 }).heightEstimates[0] !== 320, "content width rejects an exact sample");
  ok(store.synthesizeDetailed("session-a", [row], { ...environment, typographySignature: "font-b" }).heightEstimates[0] !== 320, "typography rejects an exact sample");
}

{
  const store = createTranscriptMeasuredSizes({ maxSessions: 2, maxRowsPerSession: 2, maxCalibrationSamples: 1 });
  const rowA = fakeRow("a", "answer", "answer A");
  rowA.layoutVariant = "text-flow";
  const environment = { contentWidth: 960, typographySignature: "font-a" };
  store.recordGeometry("session-a", {
    rowKey: "a", kind: "answer", layoutVariant: "text-flow", height: 222,
    environment, measurementVersion: transcriptRowMeasurementVersion(rowA), staticEstimate: estimateTranscriptRowSize(rowA, 960),
  });
  store.synthesizeDetailed("session-b", [], environment);
  eq(store.synthesizeDetailed("session-a", [rowA], environment).heightEstimates[0], 222, "A to B to A restores safe geometry without a scroll position");
  store.synthesizeDetailed("session-c", [], environment);
  store.synthesizeDetailed("session-d", [], environment);
  ok(store.synthesizeDetailed("session-a", [rowA], environment).heightEstimates[0] !== 222, "session LRU evicts beyond the configured cap");
  ok(store.synthesizeDetailed("session-b", [rowA], environment).heightEstimates[0] !== 222, "session LRU remains bounded");
}

{
  const store = createTranscriptMeasuredSizes();
  const before = fakeRow("late", "answer", "preview");
  const after = fakeRow("late", "answer", "resolved content");
  recordRow(store, before, 777, 960);
  eq(estimateFor(store, before, 960), 777, "an exact sample matches its row content version");
  ok(estimateFor(store, after, 960) !== 777, "the same row key and width reject an old content version");
}

{
  const store = createTranscriptMeasuredSizes();
  const changedBefore = fakeRow("changed", "answer", "preview");
  const changedAfter = fakeRow("changed", "answer", "resolved");
  const untouched = fakeRow("untouched", "answer", "stable");
  recordRow(store, changedBefore, 900, 960);
  recordRow(store, untouched, 240, 960);
  const estimates = estimatesFor(store, [changedAfter, untouched], 960);
  ok(estimates[0] !== 900, "patching one row invalidates its own exact sample");
  eq(estimates[1], 240, "patching one row preserves another row's exact sample");
}

{
  const store = createTranscriptMeasuredSizes();
  const staleBefore = fakeRow("stale", "answer", "preview");
  const stale = fakeRow("stale", "answer", "resolved");
  const unseen = fakeRow("unseen", "answer", "fresh");
  recordRow(store, staleBefore, 999, 960);
  const estimates = estimatesFor(store, [stale, unseen], 960);
  ok(estimates[1] !== 999, "a stale-version sample is excluded from the kind median");
}

{
  const user: Item = { kind: "user", id: "user-1", text: "question" };
  const firstTool: Item = { kind: "tool", id: "tool-1", name: "read_file", args: "{}", readOnly: true, status: "done" };
  const secondTool: Item = { kind: "tool", id: "tool-2", name: "grep", args: "{}", readOnly: true, status: "done" };
  const answer: Item = { kind: "assistant", id: "answer-1", text: "done", reasoning: "", streaming: false };
  const models = buildTurnModels([user, firstTool, secondTool, answer]);
  const folds: FoldMap = new Map([[
    "tool-1",
    { open: true, userOverridden: true, running: false },
  ]]);
  const options = {
    folds,
    foldPreference: "auto" as const,
    hasOlderHistory: false,
    turnForUser: () => 0,
  };
  const batch = buildTranscriptRows(models, { ...options, creationMode: false }).find((row) => row.kind === "tool-batch");
  const group = buildTranscriptRows(models, { ...options, creationMode: true }).find((row) => row.kind === "tool-group");
  const batchVersion = batch && transcriptRowMeasurementVersion(batch);
  const groupVersion = group && transcriptRowMeasurementVersion(group);
  ok(batchVersion === groupVersion, "batch and group rows combine the same member versions");
  const patchedTool = { ...secondTool, output: "late member content" };
  const patchedModels = buildTurnModels([user, firstTool, patchedTool, answer]);
  const patchedBatch = buildTranscriptRows(patchedModels, { ...options, creationMode: false }).find((row) => row.kind === "tool-batch");
  const patchedGroup = buildTranscriptRows(patchedModels, { ...options, creationMode: true }).find((row) => row.kind === "tool-group");
  ok(patchedBatch && transcriptRowMeasurementVersion(patchedBatch) !== batchVersion, "a member patch advances its read-only batch version");
  ok(patchedGroup && transcriptRowMeasurementVersion(patchedGroup) !== groupVersion, "a member patch advances its creation group version");
}

{
  const preview: Item = { kind: "assistant", id: "he:late-answer", text: "preview", reasoning: "", streaming: false };
  const before = { ...initialState, items: [preview] };
  const beforeRows = buildTranscriptRows(buildTurnModels(before.items), {
    folds: EMPTY_FOLDS,
    foldPreference: "auto",
    hasOlderHistory: false,
    creationMode: false,
    turnForUser: () => undefined,
  });
  const beforeRow = beforeRows.find((row) => row.kind === "answer")!;
  const store = createTranscriptMeasuredSizes();
  const beforeVersion = transcriptRowMeasurementVersion(beforeRow);
  recordRow(store, beforeRow, 88, 960);
  const after = reducer(before, {
    type: "history_items_patch",
    patches: { [preview.id]: { ...preview, text: "resolved ".repeat(2_000) } },
  });
  const afterRows = buildTranscriptRows(buildTurnModels(after.items), {
    folds: EMPTY_FOLDS,
    foldPreference: "auto",
    hasOlderHistory: false,
    creationMode: false,
    turnForUser: () => undefined,
  });
  const afterRow = afterRows.find((row) => row.kind === "answer")!;
  const afterVersion = transcriptRowMeasurementVersion(afterRow);
  eq(String(afterRow.key), String(beforeRow.key), "late content preserves the stable React row key");
  ok(afterVersion !== beforeVersion, "late content advances only the row measurement version");
  ok(estimateFor(store, afterRow, 960) !== 88, "an offscreen late patch cannot reuse the preview height after remount");
}

{
  const store = createTranscriptMeasuredSizes();
  recordRow(store, fakeRow("a1", "answer", "first"), 100);
  recordRow(store, fakeRow("a2", "answer", "second"), 300);
  recordRow(store, fakeRow("a3", "answer", "third"), 200);
  const unseen = fakeRow("a4", "answer", "some answer text that is long enough to matter");
  eq(estimateFor(store, unseen), 200, "unseen row of a sampled kind uses the kind median");
  const toolRow = fakeRow("t1", "tool", "");
  eq(estimateFor(store, toolRow), estimateTranscriptRowSize(toolRow), "unsampled kind still uses the static prior");
}

{
  const store = createTranscriptMeasuredSizes();
  const first = fakeRow("a1", "answer", "first answer");
  const second = fakeRow("a2", "answer", "second answer");
  const unseen = fakeRow("a3", "answer", "unseen answer");
  recordRow(store, first, 291, 960);
  recordRow(store, first, 632, 960);
  recordRow(store, second, 400, 960);
  eq(estimateFor(store, first, 960), 632, "a later real measurement replaces the stale estimate for the same row");
  eq(estimateFor(store, second, 960), 400, "a second row keeps its own latest measurement");
  eq(estimateFor(store, unseen, 960), 516, "kind fallback uses one latest sample per row instead of duplicate observations");
}

{
  const store = createTranscriptMeasuredSizes();
  const row = fakeRow("width-sensitive", "answer", "wrapped answer");
  recordRow(store, row, 632, 960);
  eq(estimateFor(store, row, 960), 632, "an exact measurement is reused at the measured width");
  ok(
    estimateFor(store, row, 760) === estimateTranscriptRowSize(row),
    "a measurement from another content width is not reused after responsive reflow",
  );
}

{
  const store = createTranscriptMeasuredSizes();
  const rows = [fakeRow("r1", "user", "hi"), fakeRow("r2", "answer", "hello there")];
  recordRow(store, rows[0], 64);
  const estimates = estimatesFor(store, rows);
  eq(estimates.length, 2, "synthesize stays aligned with the row array");
  eq(estimates[0], 64, "synthesize uses the exact measurement where present");
  ok(estimates[1] > 0, "synthesize falls back cleanly for unmeasured rows");
}

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
