// Run: node --import tsx src/__tests__/transcript-scroll-release.test.ts

import {
  INITIAL_TRANSCRIPT_SCROLL_STATE,
  isSubstantialTranscriptDisplacement,
  isTranscriptContentShrink,
  reduceTranscriptScroll,
  transcriptTailSettleBudgetExhausted,
  transcriptTailShouldReaim,
  TRANSCRIPT_TAIL_SETTLE_MAX_ATTEMPTS,
  TRANSCRIPT_TAIL_REARM_MIN_HEIGHT_PX,
  type TranscriptScrollEvent,
  type TranscriptScrollState,
} from "../lib/transcriptScrollArbiter";
import { pinTranscriptScrollerToNativeTail, pinTranscriptTailAfterViewportShrink } from "../lib/transcriptScrollGeometry";

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

function run(events: readonly TranscriptScrollEvent[], initial = INITIAL_TRANSCRIPT_SCROLL_STATE) {
  let state: TranscriptScrollState = initial;
  const commands: string[] = [];
  for (const event of events) {
    const next = reduceTranscriptScroll(state, event);
    state = next.state;
    commands.push(...next.commands.map((command) => command.type));
  }
  return { state, commands };
}

console.log("\ntranscript scroll controller");

const streaming = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "TAIL_CONTENT_CHANGED" },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
  { type: "LAYOUT_HEIGHT_CHANGED" },
]);
check(streaming.state.mode === "tail-follow", "dynamic atBottom=false does not steal tail ownership");
check(
  streaming.commands.join(",") === "AUTOSCROLL_TO_BOTTOM,AUTOSCROLL_TO_BOTTOM,AUTOSCROLL_TO_BOTTOM",
  "tail growth and delivered displacement emit only Virtuoso autoscroll commands",
);

const manual = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "USER_SCROLL_INTENT", canClaimTail: false },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
  { type: "TAIL_CONTENT_CHANGED" },
  { type: "VIEWPORT_RESIZED" },
]);
check(manual.state.mode === "manual", "explicit user intent releases tail-follow");
check(manual.commands.length === 0, "manual reading never receives tail commands");

const upwardIntentAtBottomRace = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "USER_SCROLL_INTENT", canClaimTail: false },
  // A scroll delivery queued before the trusted wheel's native default action
  // must not reclaim the tail from an upward reader gesture.
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
]);
check(upwardIntentAtBottomRace.state.mode === "manual", "upward reader intent survives a stale at-bottom delivery");

const returned = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "USER_SCROLL_INTENT", canClaimTail: true },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
  // The bottom must be held: two consecutive at-bottom deliveries inside the
  // same reader-intent window re-engage tail-follow (#8709/#9099).
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
]);
check(returned.state.mode === "tail-follow", "holding the real bottom across two deliveries re-engages tail-follow");

const touchDownOnce = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "USER_SCROLL_INTENT", canClaimTail: true },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
]);
check(touchDownOnce.state.mode === "manual", "a single touch-down at the bottom stays manual");

const holdBrokenByUpwardGesture = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "USER_SCROLL_INTENT", canClaimTail: true },
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  // An upward gesture inside the streak resets the hold; the next downward
  // gesture starts again from zero.
  { type: "USER_SCROLL_INTENT", canClaimTail: false },
  { type: "USER_SCROLL_INTENT", canClaimTail: true },
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
]);
check(holdBrokenByUpwardGesture.state.mode === "manual", "an upward gesture breaks the bottom-hold streak");

const holdEndsWithIntentWindow = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "USER_SCROLL_INTENT", canClaimTail: true },
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "READER_INTENT_ENDED" },
  { type: "USER_SCROLL_INTENT", canClaimTail: true },
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
]);
check(holdEndsWithIntentWindow.state.mode === "manual", "a closed intent window ends the bottom-hold streak");

const steadyStateOffsetKeepsManual = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "USER_SCROLL_INTENT", canClaimTail: false },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
  { type: "READER_INTENT_ENDED" },
  { type: "SCROLL_TO_OFFSET", owner: "anchor-compensation", top: 640 },
  { type: "SCROLL_TO_OFFSET", owner: "block-window-prepend", top: 680 },
]);
check(steadyStateOffsetKeepsManual.state.mode === "manual", "steady-state offset corrections keep manual ownership");
check(
  steadyStateOffsetKeepsManual.commands.join(",") === "SCROLL_TO_OFFSET,SCROLL_TO_OFFSET",
  "steady-state offset corrections emit only their own commands",
);

const browserClamp = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "USER_SCROLL_INTENT", canClaimTail: true },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
  { type: "READER_INTENT_ENDED" },
  { type: "CONTENT_SHRANK" },
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
]);
check(browserClamp.state.mode === "manual", "a browser clamp without fresh reader intent does not resume tail-follow");

const manualResize = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "USER_SCROLL_INTENT", canClaimTail: false },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
  { type: "READER_INTENT_ENDED" },
  { type: "USER_RESIZE_BEGIN" },
  { type: "LAYOUT_HEIGHT_CHANGED" },
  { type: "USER_RESIZE_END" },
]);
check(manualResize.state.mode === "manual", "a resize preserves manual reading ownership");
check(manualResize.commands.length === 0, "manual reading receives no tail write during resize");

const shortTranscript = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: false },
  { type: "USER_SCROLL_INTENT", canClaimTail: false },
]);
check(shortTranscript.state.mode === "tail-follow", "non-overflow transcript always stays tail-follow");

const fold = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "USER_RESIZE_BEGIN" },
  { type: "LAYOUT_HEIGHT_CHANGED" },
  { type: "USER_RESIZE_END" },
]);
check(fold.state.mode === "tail-follow", "a fold resize preserves existing tail ownership");
check(fold.commands.join(",") === "AUTOSCROLL_TO_BOTTOM", "a fold resize reconverges only when it began at the tail");

const selection = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "SELECTION_BEGIN" },
  { type: "SCROLL_TO_OFFSET", owner: "selection-edge-scroll", top: 120 },
  { type: "LAYOUT_HEIGHT_CHANGED" },
  { type: "SELECTION_END" },
]);
check(selection.state.mode === "manual", "selection returns to manual reading");
check(selection.commands.join(",") === "SCROLL_TO_OFFSET", "selection owns only its explicit edge-scroll command");

const jump = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "USER_SCROLL_INTENT", canClaimTail: false },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
  { type: "JUMP_TO_BOTTOM", behavior: "smooth" },
]);
check(jump.state.mode === "tail-follow", "jump-bottom explicitly owns the tail");
check(jump.commands.join(",") === "SCROLL_TO_LAST", "jump-bottom emits only the tail command");

const repeatedJump = run([
  { type: "JUMP_TO_BOTTOM" },
  { type: "JUMP_TO_BOTTOM" },
]);
check(repeatedJump.commands.join(",") === "SCROLL_TO_LAST,SCROLL_TO_LAST", "repeated bottom requests each produce a fresh command");

const restore = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "JUMP_TO_INDEX", index: 42 },
  { type: "PROGRAMMATIC_END" },
]);
check(restore.state.mode === "manual", "question/rewind navigation settles in manual mode");
check(restore.commands.join(",") === "SCROLL_TO_INDEX", "navigation emits one indexed Virtuoso command");

const selectionThenQuestionJump = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "SELECTION_BEGIN" },
  { type: "SELECTION_END" },
  { type: "JUMP_TO_INDEX", index: 7 },
]);
check(selectionThenQuestionJump.state.mode === "restoring", "question navigation takes ownership after clearing a stale selection gesture");
check(selectionThenQuestionJump.commands.join(",") === "SCROLL_TO_INDEX", "selection cleanup is followed by exactly one indexed jump");

const shrink = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "CONTENT_SHRANK" },
]);
check(shrink.state.mode === "tail-follow", "auto fold collapse keeps tail-follow");
check(shrink.commands.length === 0, "auto fold collapse does not tug the viewport to the tail");

const shrinkOffBottom = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
  { type: "CONTENT_SHRANK" },
  { type: "LAYOUT_HEIGHT_CHANGED" },
]);
check(shrinkOffBottom.state.mode === "tail-follow", "a shrink does not steal tail ownership");
check(
  shrinkOffBottom.commands.join(",") === "AUTOSCROLL_TO_BOTTOM,AUTOSCROLL_TO_BOTTOM",
  "delivered displacement and later growth both reconverge while tail-follow owns the viewport",
);

const repeatedDisplacement = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
  { type: "LAYOUT_HEIGHT_CHANGED" },
]);
check(
  repeatedDisplacement.commands.join(",") === "AUTOSCROLL_TO_BOTTOM,AUTOSCROLL_TO_BOTTOM",
  "repeated non-bottom deliveries do not loop tail writes, while a layout change can reconverge",
);

check(isTranscriptContentShrink(-48), "a fold-sized height drop is a shrink");
check(!isTranscriptContentShrink(-8), "measurement jitter is not a shrink");
check(!isTranscriptContentShrink(80), "content growth is not a shrink");

check(isSubstantialTranscriptDisplacement(1200), "a thumb-drop-sized gap is a substantial displacement");
check(!isSubstantialTranscriptDisplacement(4), "bottom-adjacent jitter is not substantial");

// A misread shrink (native-thumb release remeasure seen as a height drop)
// leaves layout convergence inert; a later substantial displacement delivery
// must still reconverge the tail instead of stranding the viewport.
const strandedAfterMisreadShrink = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true, substantial: true },
  { type: "CONTENT_SHRANK" },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true, substantial: true },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true, substantial: true },
]);
check(
  strandedAfterMisreadShrink.commands.join(",") === "AUTOSCROLL_TO_BOTTOM,AUTOSCROLL_TO_BOTTOM,AUTOSCROLL_TO_BOTTOM",
  "substantial displacements keep reconverging after a misread shrink",
);

const wrapScroller = { scrollHeight: 500, scrollTop: 400, clientHeight: 80 };
check(pinTranscriptScrollerToNativeTail(wrapScroller) === true, "a composer-wrap viewport shrink is off-bottom and gets pinned");
check(wrapScroller.scrollTop === 420, "pin writes the native tail top");
check(pinTranscriptScrollerToNativeTail(wrapScroller) === false, "an already-pinned scroller is left alone");

const foldScroller = { scrollHeight: 500, scrollTop: 400, clientHeight: 80 };
check(
  pinTranscriptTailAfterViewportShrink(foldScroller, { contentExtent: 540, viewportExtent: 100 }, true) === null,
  "content collapse suppresses a coincident viewport-shrink pin",
);
check(foldScroller.scrollTop === 400, "content collapse leaves the browser-owned offset unchanged");
check(
  pinTranscriptTailAfterViewportShrink(foldScroller, { contentExtent: 500, viewportExtent: 100 }, false) === null,
  "manual reading suppresses viewport-shrink pinning",
);

// #9208 — the tail-settle loop needs a bounded re-aim budget so virtualized
// bottom rows that keep re-measuring cannot loop the convergence rAF forever.
check(transcriptTailSettleBudgetExhausted(0) === false, "tail settle may re-aim from zero attempts");
check(transcriptTailSettleBudgetExhausted(TRANSCRIPT_TAIL_SETTLE_MAX_ATTEMPTS - 1) === false, "tail settle keeps re-aiming before the budget is fully spent");
check(transcriptTailSettleBudgetExhausted(TRANSCRIPT_TAIL_SETTLE_MAX_ATTEMPTS) === true, "tail settle stops once the re-aim budget is exhausted");
check(transcriptTailSettleBudgetExhausted(99) === true, "tail settle stops for any attempt count above the budget");
check(TRANSCRIPT_TAIL_SETTLE_MAX_ATTEMPTS > 0, "the tail settle budget is positive");

// #9208 — a settled tail only re-aims on real growth, not on the small
// measurement jitter that virtualized rows emit while drawing in.
check(transcriptTailShouldReaim(null, 1_000) === true, "a fresh settle with no recorded bottom always re-aims");
check(transcriptTailShouldReaim(1_000, 1_000) === false, "zero height growth does not re-aim a pinned tail");
check(transcriptTailShouldReaim(1_000, 1_000 + TRANSCRIPT_TAIL_REARM_MIN_HEIGHT_PX - 1) === false, "sub-threshold re-measurement jitter does not re-aim");
check(transcriptTailShouldReaim(1_000, 1_000 + TRANSCRIPT_TAIL_REARM_MIN_HEIGHT_PX) === true, "threshold-height growth re-aims the pinned tail");
check(transcriptTailShouldReaim(1_000, 1_000 + TRANSCRIPT_TAIL_REARM_MIN_HEIGHT_PX + 1) === true, "supra-threshold growth re-aims the pinned tail");
check(TRANSCRIPT_TAIL_REARM_MIN_HEIGHT_PX > 0, "the re-aim threshold is positive");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
