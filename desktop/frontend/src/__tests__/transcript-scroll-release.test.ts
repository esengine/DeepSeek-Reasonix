// Run: node --import tsx src/__tests__/transcript-scroll-release.test.ts
// Regression: non-wheel upward scroll must release tail-follow immediately,
// not wait 500ms. A LAST undershoot on a history session must finish at the
// native extent so wheel/jump-bottom can reveal the last rows.

import {
  isPinnedTranscriptLayoutGrowth,
  isPinnedTranscriptViewportChange,
  nativeTranscriptBottomTop,
  nativeTranscriptDistanceFromBottom,
  shouldClearBottomRequestOnAtBottomTrue,
  shouldClearBottomRequestOnWriteOffset,
  shouldFinishTailOnAtBottomFalse,
  shouldFinishTailOnBottomRequestTimer,
  shouldKeepPinnedOnAtBottomFalse,
  shouldReleaseBottomRequestOnAtBottomFalse,
  shouldRemeasureMountedRowsForTailFinish,
  shouldSnapPinnedWheelToNativeBottom,
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
  previousScrollTop = 9397,
) {
  if (!atBottom && bottomRequestActive) {
    if (scrollElement && shouldReleaseBottomRequestOnAtBottomFalse({
      distanceFromBottom: nativeTranscriptDistanceFromBottom(scrollElement),
      scrollTop: scrollElement.scrollTop,
      previousScrollTop,
      scrollHeight: scrollElement.scrollHeight,
      clientHeight: scrollElement.clientHeight,
    })) {
      return "manual";
    }
    return "suppressed";
  }
  return atBottom ? "tail-follow" : "manual";
}

// Scenario 1: atBottom=false during bottomRequest, but physically at bottom.
// Expected: suppressed (Virtuoso is still converging; keep tail intent).
const s1 = mockAtBottomStateChange(false, true, { scrollHeight: 10000, scrollTop: 9397, clientHeight: 600 });
check(s1 === "suppressed", "physically at bottom during request window: intent preserved");

// Scenario 2: LAST after a native snap pulls scrollTop back from the native max.
// Expected: suppressed (undershoot; remasure + snap, do not unpin).
const s2 = mockAtBottomStateChange(false, true, { scrollHeight: 10000, scrollTop: 9000, clientHeight: 600 }, 9400);
check(s2 === "suppressed", "LAST pullback from the native max during request window: keep tail intent");

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

console.log("\ntranscript history native bottom");

const lastItemUndershoot = {
  scrollHeight: 10000,
  scrollTop: 9320,
  clientHeight: 600,
};
check(
  nativeTranscriptDistanceFromBottom(lastItemUndershoot) === 80,
  "an underestimated last row leaves a native gap below Virtuoso LAST",
);
check(
  nativeTranscriptBottomTop(lastItemUndershoot) === 9400,
  "jump-bottom and tail-follow finish at the native scroll extent",
);
check(
  !shouldReleaseBottomRequestOnAtBottomFalse({
    distanceFromBottom: nativeTranscriptDistanceFromBottom(lastItemUndershoot),
    scrollTop: lastItemUndershoot.scrollTop,
    previousScrollTop: 9320,
    scrollHeight: lastItemUndershoot.scrollHeight,
    clientHeight: lastItemUndershoot.clientHeight,
  }),
  "LAST undershoot during jump-bottom is not the reader leaving the tail",
);
check(
  !shouldReleaseBottomRequestOnAtBottomFalse({
    distanceFromBottom: 400,
    scrollTop: 9000,
    previousScrollTop: 9400,
    previousScrollHeight: 10000,
    previousClientHeight: 600,
    scrollHeight: 10000,
    clientHeight: 600,
  }),
  "LAST pullback from the native max is not the reader leaving",
);
check(
  !shouldReleaseBottomRequestOnAtBottomFalse({
    distanceFromBottom: 1200,
    scrollTop: 9000,
    previousScrollTop: 9400,
    previousScrollHeight: 10000,
    previousClientHeight: 600,
    scrollHeight: 10800,
    clientHeight: 600,
  }),
  "LAST pullback after last-row growth is not the reader leaving",
);
check(
  !shouldClearBottomRequestOnAtBottomTrue(),
  "a native snap at-bottom report must not cancel the jump-bottom timer",
);
check(
  shouldReleaseBottomRequestOnAtBottomFalse({
    distanceFromBottom: 900,
    scrollTop: 7500,
    previousScrollTop: 8500,
    scrollHeight: 10000,
    clientHeight: 600,
  }),
  "an upward leave that did not start at the native max still releases",
);
check(
  shouldRemeasureMountedRowsForTailFinish({ remeasuredThisCommand: false }),
  "the first tail finish remasures mounted rows",
);
check(
  !shouldRemeasureMountedRowsForTailFinish({ remeasuredThisCommand: true }),
  "a post-success tail finish does not remasure again",
);
check(
  !shouldRemeasureMountedRowsForTailFinish({ remeasuredThisCommand: false, allowRemeasure: false }),
  "a pinned growth finish snaps without remasuring",
);
check(
  shouldFinishTailOnBottomRequestTimer({ pinned: true, bottomRequestWasActive: true }),
  "the bottom-request timer still finishes a pinned tail after LAST overwrite",
);
check(
  !shouldFinishTailOnBottomRequestTimer({ pinned: false, bottomRequestWasActive: true }),
  "an unpinned leave during the jump window is not force-snapped back",
);
check(
  !shouldFinishTailOnBottomRequestTimer({ pinned: false, bottomRequestWasActive: false }),
  "the timer does not re-pin after an explicit release cleared the request",
);
check(
  shouldClearBottomRequestOnWriteOffset("custom-scrollbar"),
  "creation-mode scrollbar drag cancels the jump-bottom window",
);
check(
  shouldClearBottomRequestOnWriteOffset("rewind"),
  "a programmatic rewind leave cancels the jump-bottom window",
);
check(
  !shouldClearBottomRequestOnWriteOffset("jump-bottom"),
  "jump-bottom itself still opens the request window",
);
check(
  !shouldClearBottomRequestOnWriteOffset("selection-edge-scroll"),
  "selection edge scroll does not cancel a jump window",
);
check(
  shouldFinishTailOnAtBottomFalse({ pinned: true, bottomRequestActive: false }),
  "a pinned LAST overwrite after the jump window still finishes the tail",
);
check(
  shouldFinishTailOnAtBottomFalse({ pinned: false, bottomRequestActive: true }),
  "a jump-bottom request still finishes the tail even if LAST briefly unpinned",
);
check(
  !shouldFinishTailOnAtBottomFalse({ pinned: false, bottomRequestActive: false }),
  "an unpinned reader without a jump request keeps ordinary at-bottom updates",
);
check(
  shouldSnapPinnedWheelToNativeBottom({
    pinned: true,
    deltaY: 120,
    distanceFromBottom: 80,
  }),
  "wheel-down while pinned to a false tail consumes the native gap",
);
check(
  !shouldSnapPinnedWheelToNativeBottom({
    pinned: true,
    deltaY: 120,
    distanceFromBottom: 0,
  }),
  "wheel-down at the physical bottom stays in ordinary tail-follow",
);
check(
  !shouldSnapPinnedWheelToNativeBottom({
    pinned: true,
    deltaY: -120,
    distanceFromBottom: 80,
  }),
  "wheel-up still leaves tail-follow instead of snapping back down",
);
check(
  !shouldSnapPinnedWheelToNativeBottom({
    pinned: false,
    deltaY: 120,
    distanceFromBottom: 80,
  }),
  "an unpinned reader keeps ordinary wheel scrolling",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
