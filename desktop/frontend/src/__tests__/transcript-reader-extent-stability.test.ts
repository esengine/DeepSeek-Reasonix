// Run: tsx src/__tests__/transcript-reader-extent-stability.test.ts

import assert from "node:assert/strict";
import type { TranscriptScrollEvent } from "../lib/transcriptScrollArbiter";
import {
  createTranscriptReaderExtentGuard,
  extendTranscriptReaderExtentGuard,
  observeTranscriptReaderExtent,
  resolveTranscriptReaderPaintedReverse,
  resolveTranscriptReaderExtentCorrection,
  transcriptScrollEventCancelsReaderExtentGuard,
  transcriptKeyboardScrollDelta,
  transcriptReaderPaintedSlideIsAdjacent,
  transcriptReaderExtentCanCorrect,
} from "../lib/transcriptReaderExtentStability";

console.log("\ntranscript reader extent stability");

assert.equal(transcriptReaderPaintedSlideIsAdjacent(690, 596), true);
assert.equal(transcriptReaderPaintedSlideIsAdjacent(17_588, 596), false,
  "a multi-range replacement is not an adjacent painted-frame correction");
const newestPaintedReverse = resolveTranscriptReaderPaintedReverse(
  [new Map([["newest", -21]]), new Map([["stale", -900]])],
  new Map([["newest", 552], ["stale", 400]]), 1,
);
assert.equal(newestPaintedReverse?.screenDelta, 573,
  "an older retained range cannot mask the newest repairable reverse frame");

const reported = createTranscriptReaderExtentGuard(
  { scrollTop: 14_567.47, scrollHeight: 15_829, clientHeight: 725 },
  { mode: "manual", rowKey: "visible-row", offset: 20 },
  133.33,
)!;
observeTranscriptReaderExtent(
  reported,
  { scrollTop: 12_618.67, scrollHeight: 13_344, clientHeight: 725 },
);
assert.equal(
  resolveTranscriptReaderExtentCorrection(
    reported,
    { scrollTop: 12_618.67, scrollHeight: 13_344, clientHeight: 725 },
    1_836,
  ),
  undefined,
  "a still-collapsed extent cannot consume the correction budget",
);
const reportedCorrection = resolveTranscriptReaderExtentCorrection(
  reported,
  { scrollTop: 12_618.67, scrollHeight: 15_829, clientHeight: 725 },
  1_836,
);
assert.ok(reportedCorrection !== undefined && reportedCorrection > 1_900,
  `the returned Windows geometry restores its logical anchor (${reportedCorrection})`);

const fallbackCorrection = resolveTranscriptReaderExtentCorrection(
  reported,
  { scrollTop: 12_618.67, scrollHeight: 15_829, clientHeight: 725 },
);
assert.equal(Math.round(fallbackCorrection ?? 0), 2_082,
  "an unmounted anchor falls back to the expected native wheel landing");

assert.equal(
  transcriptReaderExtentCanCorrect(
    reported,
    { scrollTop: 14_500, scrollHeight: 15_829, clientHeight: 725 },
  ),
  false,
  "sub-viewport reverse jitter remains browser-owned",
);
assert.equal(
  transcriptReaderExtentCanCorrect(
    reported,
    { scrollTop: 12_618.67, scrollHeight: 13_344, clientHeight: 725 },
  ),
  false,
  "a real persistent content shrink is not mistaken for a transient rebound",
);
assert.equal(
  transcriptReaderExtentCanCorrect(
    reported,
    { scrollTop: 12_618.67, scrollHeight: 15_829, clientHeight: 900 },
  ),
  false,
  "viewport resize invalidates the reader geometry transaction",
);

const upward = createTranscriptReaderExtentGuard(
  { scrollTop: 2_000, scrollHeight: 5_000, clientHeight: 800 },
  { mode: "manual", rowKey: "visible-row", offset: 20 },
  -120,
)!;
observeTranscriptReaderExtent(
  upward,
  { scrollTop: 1_400, scrollHeight: 4_200, clientHeight: 800 },
);
assert.equal(
  resolveTranscriptReaderExtentCorrection(
    upward,
    { scrollTop: 2_600, scrollHeight: 5_000, clientHeight: 800 },
    -580,
  ),
  -720,
  "an upward gesture corrects only a catastrophic downward reversal",
);

const prepend = createTranscriptReaderExtentGuard(
  { scrollTop: 2_000, scrollHeight: 5_000, clientHeight: 800 },
  { mode: "manual", rowKey: "visible-row", offset: 20 },
  -40,
)!;
observeTranscriptReaderExtent(
  prepend,
  { scrollTop: 3_500, scrollHeight: 6_500, clientHeight: 800 },
);
assert.equal(
  resolveTranscriptReaderExtentCorrection(
    prepend,
    { scrollTop: 3_500, scrollHeight: 6_500, clientHeight: 800 },
    20,
  ),
  undefined,
  "prepended history growth preserves Virtuoso's logical-anchor compensation",
);

const keyboardSnapshot = { scrollTop: 2_000, scrollHeight: 5_000, clientHeight: 800 };
assert.equal(transcriptKeyboardScrollDelta(" ", true, keyboardSnapshot), -720,
  "Shift+Space uses the browser's upward direction");
assert.equal(transcriptKeyboardScrollDelta(" ", false, keyboardSnapshot), 720,
  "Space uses the browser's downward direction");
assert.equal(transcriptKeyboardScrollDelta("Home", false, keyboardSnapshot), -2_000,
  "Home targets the native top");
assert.equal(transcriptKeyboardScrollDelta("End", false, keyboardSnapshot), 2_200,
  "End targets the native bottom");
const cancellingEvents: TranscriptScrollEvent["type"][] = [
  "RESET",
  "MANUAL_READING",
  "NATIVE_SCROLLBAR_BEGIN",
  "USER_RESIZE_BEGIN",
  "SELECTION_BEGIN",
  "PROGRAMMATIC_BEGIN",
  "JUMP_TO_BOTTOM",
  "JUMP_TO_INDEX",
  "SCROLL_TO_OFFSET",
  "RECOVERY_BEGIN",
];
for (const event of cancellingEvents) {
  assert.equal(transcriptScrollEventCancelsReaderExtentGuard(event), true,
    `${event} cancels stale reader geometry`);
}

const observingEvents: TranscriptScrollEvent["type"][] = [
  "USER_SCROLL_INTENT",
  "READER_INTENT_ENDED",
  "NATIVE_SCROLLBAR_END",
  "SCROLL_DELIVERED",
  "GEOMETRY_CHANGED",
  "TAIL_CONTENT_CHANGED",
  "CONTENT_SHRANK",
  "LAYOUT_HEIGHT_CHANGED",
  "USER_RESIZE_END",
  "SELECTION_END",
  "PROGRAMMATIC_END",
  "RECOVERY_END",
  "VIEWPORT_RESIZED",
];
for (const event of observingEvents) {
  assert.equal(transcriptScrollEventCancelsReaderExtentGuard(event), false,
    `${event} leaves rebound observation active`);
}

const continuous = createTranscriptReaderExtentGuard(
  { scrollTop: 1_000, scrollHeight: 5_000, clientHeight: 800 },
  { mode: "manual", rowKey: "visible-row", offset: 20 },
  40,
)!;
assert.equal(extendTranscriptReaderExtentGuard(
  continuous,
  { scrollTop: 1_000, scrollHeight: 5_000, clientHeight: 800 },
  { mode: "manual", rowKey: "visible-row", offset: 20 },
  40,
), true, "same-direction input extends the active reader transaction");
assert.equal(continuous.expectedTop, 1_080, "continuous input accumulates before scroll delivery");
assert.equal(extendTranscriptReaderExtentGuard(
  continuous,
  { scrollTop: 1_000, scrollHeight: 5_000, clientHeight: 800 },
  { mode: "manual", rowKey: "visible-row", offset: 20 },
  -40,
), false, "direction reversal starts a new reader transaction");

const growingReverse = createTranscriptReaderExtentGuard(
  { scrollTop: 1_000, scrollHeight: 5_000, clientHeight: 800 },
  undefined,
  40,
)!;
observeTranscriptReaderExtent(growingReverse, { scrollTop: 1_040, scrollHeight: 5_040, clientHeight: 800 });
assert.equal(extendTranscriptReaderExtentGuard(
  growingReverse,
  { scrollTop: 1_040, scrollHeight: 5_040, clientHeight: 800 },
  undefined,
  40,
), true);
const reverseWithoutCollapse = { scrollTop: 900, scrollHeight: 5_080, clientHeight: 800 };
observeTranscriptReaderExtent(growingReverse, reverseWithoutCollapse);
assert.equal(transcriptReaderExtentCanCorrect(growingReverse, reverseWithoutCollapse), true,
  "a >96px reverse jump is rejected even while the external extent grows");
assert.equal(resolveTranscriptReaderExtentCorrection(growingReverse, reverseWithoutCollapse), 180,
  "the continuous transaction restores its last accepted forward position");

const visualReverse = createTranscriptReaderExtentGuard(
  { scrollTop: 18_401, scrollHeight: 23_105, clientHeight: 596 },
  { mode: "manual", rowKey: "visible-row", offset: -29 },
  24,
)!;
const visualReverseSnapshot = { scrollTop: 18_425, scrollHeight: 23_385, clientHeight: 596 };
observeTranscriptReaderExtent(visualReverse, visualReverseSnapshot, 479);
assert.equal(visualReverse.acceptedTop, 18_401,
  "a visual reverse cannot replace the last accepted logical reader position");
assert.equal(
  transcriptReaderExtentCanCorrect(visualReverse, visualReverseSnapshot, 479),
  true,
  "a Virtuoso height-tree rebuild is rejected when rows reverse while scrollTop still advances",
);
assert.equal(
  resolveTranscriptReaderExtentCorrection(visualReverse, visualReverseSnapshot, 479),
  532,
  "the reader restores the last logical row offset after a visual-only reverse jump",
);

const directNativeJump = createTranscriptReaderExtentGuard(
  { scrollTop: 20_156, scrollHeight: 36_928, clientHeight: 596 },
  { mode: "manual", rowKey: "deep-row", offset: 20 },
  -1,
)!;
const directNativeTop = { scrollTop: 0, scrollHeight: 36_928, clientHeight: 596 };
observeTranscriptReaderExtent(directNativeJump, directNativeTop, undefined, true);
assert.equal(
  resolveTranscriptReaderExtentCorrection(directNativeJump, directNativeTop),
  undefined,
  "a multi-viewport native reposition is not mistaken for a wheel-range blank",
);

const reverseBlank = createTranscriptReaderExtentGuard(
  { scrollTop: 18_553, scrollHeight: 21_689, clientHeight: 596 },
  { mode: "manual", rowKey: "row-573", offset: -26 },
  24,
)!;
const reverseBlankSnapshot = { scrollTop: 1_323, scrollHeight: 21_918, clientHeight: 596 };
observeTranscriptReaderExtent(reverseBlank, reverseBlankSnapshot, undefined, true);
assert.equal(
  resolveTranscriptReaderExtentCorrection(reverseBlank, reverseBlankSnapshot),
  17_254,
  "a catastrophic reverse blank restores the requested logical reader target",
);

const coalescedForwardBlank = createTranscriptReaderExtentGuard(
  { scrollTop: 5_000, scrollHeight: 20_000, clientHeight: 596 },
  { mode: "manual", rowKey: "row-200", offset: 20 },
  24,
)!;
for (let index = 1; index < 100; index += 1) {
  assert.equal(extendTranscriptReaderExtentGuard(
    coalescedForwardBlank,
    { scrollTop: 5_000, scrollHeight: 20_000, clientHeight: 596 },
    { mode: "manual", rowKey: "row-200", offset: 20 },
    24,
  ), true);
}
const coalescedForwardSnapshot = { scrollTop: 7_600, scrollHeight: 20_000, clientHeight: 596 };
observeTranscriptReaderExtent(coalescedForwardBlank, coalescedForwardSnapshot, undefined, true);
assert.equal(
  resolveTranscriptReaderExtentCorrection(coalescedForwardBlank, coalescedForwardSnapshot),
  -2_600,
  "a coalesced requested wheel range holds its last nonblank logical target",
);

console.log("transcript reader extent stability tests passed");
