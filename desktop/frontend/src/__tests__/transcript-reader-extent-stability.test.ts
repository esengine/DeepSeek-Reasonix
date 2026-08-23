// Run: tsx src/__tests__/transcript-reader-extent-stability.test.ts

import assert from "node:assert/strict";
import type { TranscriptScrollEvent } from "../lib/transcriptScrollArbiter";
import {
  createTranscriptReaderExtentGuard,
  observeTranscriptReaderExtent,
  resolveTranscriptReaderExtentCorrection,
  transcriptScrollEventCancelsReaderExtentGuard,
  transcriptKeyboardScrollDelta,
  transcriptReaderExtentCanCorrect,
} from "../lib/transcriptReaderExtentStability";

console.log("\ntranscript reader extent stability");

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
  "USER_SCROLL_INTENT",
  "MANUAL_READING",
  "VIEWPORT_RESIZED",
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
  "READER_INTENT_ENDED",
  "SCROLL_DELIVERED",
  "TAIL_CONTENT_CHANGED",
  "CONTENT_SHRANK",
  "LAYOUT_HEIGHT_CHANGED",
  "USER_RESIZE_END",
  "SELECTION_END",
  "PROGRAMMATIC_END",
  "RECOVERY_END",
];
for (const event of observingEvents) {
  assert.equal(transcriptScrollEventCancelsReaderExtentGuard(event), false,
    `${event} leaves rebound observation active`);
}

console.log("transcript reader extent stability tests passed");
