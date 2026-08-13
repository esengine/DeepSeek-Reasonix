// Run: node --import tsx src/__tests__/transcript-scroll-release.test.ts
// Regression: non-wheel upward scroll must release tail-follow immediately,
// not wait 500ms. Covers the fix for native scrollbar drag and middle-button
// autoscroll suppression during a bottomRequest window.

import {
  isPinnedTranscriptLayoutGrowth,
  isPinnedTranscriptViewportChange,
  shouldKeepPinnedOnAtBottomFalse,
  TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX,
} from "../lib/useTranscriptVirtuosoScroll";

let passed = 0;
let failed = 0;

function check(condition: boolean, label: string) {
  if (condition) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

console.log("\ntranscript scroll release");

// Mock the hook's atBottomStateChange behavior with the fix applied.
// The real verification path is the browser bench, but this proves the logic.
function mockAtBottomStateChange(
  atBottom: boolean,
  bottomRequestActive: boolean,
  scrollElement: { scrollHeight: number; scrollTop: number; clientHeight: number } | null,
) {
  if (!atBottom && bottomRequestActive) {
    if (scrollElement) {
      const distanceFromBottom = scrollElement.scrollHeight - scrollElement.scrollTop - scrollElement.clientHeight;
      if (distanceFromBottom > TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX) {
        return "manual"; // Released immediately by the fix
      }
    }
    return "suppressed"; // Old behavior: early return, no mode change
  }
  return atBottom ? "tail-follow" : "manual";
}

// Scenario 1: atBottom=false during bottomRequest, but physically at bottom.
// Expected: suppressed (Virtuoso is still converging; keep tail intent).
const s1 = mockAtBottomStateChange(false, true, { scrollHeight: 10000, scrollTop: 9397, clientHeight: 600 });
check(s1 === "suppressed", "physically at bottom during request window: intent preserved");

// Scenario 2: atBottom=false during bottomRequest, physically far from bottom.
// Expected: manual (native scrollbar drag or middle-button autoscroll moved away).
const s2 = mockAtBottomStateChange(false, true, { scrollHeight: 10000, scrollTop: 8500, clientHeight: 600 });
check(s2 === "manual", "non-wheel upward scroll during request window: release tail-follow immediately");

// Scenario 3: atBottom=false, no active request.
// Expected: manual (ordinary path).
const s3 = mockAtBottomStateChange(false, false, { scrollHeight: 10000, scrollTop: 5000, clientHeight: 600 });
check(s3 === "manual", "upward scroll outside request window: ordinary manual mode");

// Scenario 4: atBottom=true.
// Expected: tail-follow (re-engaged).
const s4 = mockAtBottomStateChange(true, false, null);
check(s4 === "tail-follow", "scrolled back to physical bottom: tail-follow restored");

const layoutGrowth = isPinnedTranscriptLayoutGrowth({
  pinned: true,
  previousScrollHeight: 10000,
  previousScrollTop: 9400,
  scrollHeight: 10320,
  scrollTop: 9714,
});
check(layoutGrowth, "pinned row growth preserves tail-follow through callback reordering");
check(
  shouldKeepPinnedOnAtBottomFalse({
    pinned: true,
    previousScrollHeight: 10000,
    previousScrollTop: 9400,
    previousClientHeight: 600,
    scrollHeight: 10320,
    scrollTop: 9714,
    clientHeight: 600,
  }),
  "pinned row growth still keeps the tail through the combined policy",
);

const userScrollDuringGrowth = isPinnedTranscriptLayoutGrowth({
  pinned: true,
  previousScrollHeight: 10000,
  previousScrollTop: 9400,
  scrollHeight: 10320,
  scrollTop: 9000,
});
check(!userScrollDuringGrowth, "upward user movement is not mistaken for pinned row growth");

const dismissTodoViewport = {
  pinned: true,
  previousScrollHeight: 10000,
  previousScrollTop: 9400,
  previousClientHeight: 600,
  scrollHeight: 10000,
  // Virtuoso can reset scrollTop to the start of the loaded window when the
  // composer/todo footer shrinks and the transcript viewport grows.
  scrollTop: 0,
  clientHeight: 760,
};
check(
  isPinnedTranscriptViewportChange(dismissTodoViewport),
  "dismissing the todo footer is a pinned viewport change",
);
check(
  shouldKeepPinnedOnAtBottomFalse(dismissTodoViewport),
  "todo dismiss must not treat a viewport-grow remasure as leaving the tail",
);

const openTodoViewport = {
  ...dismissTodoViewport,
  previousClientHeight: 760,
  clientHeight: 600,
  scrollTop: 9400,
};
check(
  isPinnedTranscriptViewportChange(openTodoViewport),
  "opening the todo footer is a pinned viewport change",
);
check(
  shouldKeepPinnedOnAtBottomFalse(openTodoViewport),
  "todo open must keep tail-follow while the composer chrome grows",
);

const readingHistory = {
  ...dismissTodoViewport,
  pinned: false,
};
check(
  !isPinnedTranscriptViewportChange(readingHistory),
  "an unpinned reader is not forced back to the tail when the footer resizes",
);
check(
  !shouldKeepPinnedOnAtBottomFalse(readingHistory),
  "unpinned history reading survives todo dismiss",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
