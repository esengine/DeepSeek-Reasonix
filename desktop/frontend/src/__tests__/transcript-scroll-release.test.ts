// Run: node --import tsx src/__tests__/transcript-scroll-release.test.ts

import {
  INITIAL_TRANSCRIPT_SCROLL_STATE,
  isSubstantialTranscriptDisplacement,
  isTranscriptContentShrink,
  reduceTranscriptScroll,
  type TranscriptScrollEvent,
  type TranscriptScrollState,
} from "../lib/transcriptScrollArbiter";
import {
  nativeTranscriptBottomTop,
  nativeTranscriptDistanceFromBottom,
  observeNativeTranscriptTailClamp,
  pinTranscriptTailAfterViewportShrink,
} from "../lib/transcriptScrollGeometry";
import {
  TRANSCRIPT_TAIL_REARM_MIN_HEIGHT_PX,
  transcriptTailIsStranded,
  transcriptTailSettleBudgetExhausted,
  transcriptTailShouldReaim,
} from "../lib/transcriptTailSettle";

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
  streaming.commands.join(",") === "AUTOSCROLL_TO_BOTTOM,AUTOSCROLL_TO_BOTTOM",
  "only geometry events emit tail autoscroll commands",
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
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "READER_IDLE_DEADLINE" },
  { type: "READER_STABILITY_SAMPLE", stable: true, tailEligible: true },
  { type: "READER_STABILITY_SAMPLE", stable: true, tailEligible: true },
  { type: "READER_TAIL_HANDOFF" },
]);
check(returned.state.mode === "tail-follow", "two stable geometry frames explicitly hand reader ownership to the tail");

const stableAwayFromTail = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "USER_SCROLL_INTENT", canClaimTail: true },
  { type: "READER_IDLE_DEADLINE" },
  { type: "READER_STABILITY_SAMPLE", stable: true, tailEligible: false },
  { type: "READER_STABILITY_SAMPLE", stable: true, tailEligible: false },
  { type: "READER_TAIL_HANDOFF" },
]);
check(stableAwayFromTail.state.mode === "manual", "stable geometry away from the real tail cannot enter handoff-pending");
check(stableAwayFromTail.state.readerPhase === "settling", "an ineligible reader transaction remains observationally settling");

const touchDownOnce = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "USER_SCROLL_INTENT", canClaimTail: true },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "READER_IDLE_DEADLINE" },
  { type: "READER_STABILITY_SAMPLE", stable: true, tailEligible: true },
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
  { type: "READER_IDLE_DEADLINE" },
  { type: "READER_STABILITY_SAMPLE", stable: true, tailEligible: true },
]);
check(holdBrokenByUpwardGesture.state.mode === "manual", "an upward gesture starts a new reader stability transaction");

const holdEndsWithIntentWindow = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "USER_SCROLL_INTENT", canClaimTail: true },
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "READER_TRANSACTION_END" },
  { type: "USER_SCROLL_INTENT", canClaimTail: true },
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
]);
check(holdEndsWithIntentWindow.state.mode === "manual", "a closed transaction discards prior stability samples");

const steadyStateOffsetKeepsManual = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "USER_SCROLL_INTENT", canClaimTail: false },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
  { type: "READER_TRANSACTION_END" },
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
  { type: "READER_TRANSACTION_END" },
  { type: "CONTENT_SHRANK" },
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
]);
check(browserClamp.state.mode === "manual", "a browser clamp without fresh reader intent does not resume tail-follow");

const manualResize = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "USER_SCROLL_INTENT", canClaimTail: false },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
  { type: "READER_TRANSACTION_END" },
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
  shrinkOffBottom.commands.join(",") === "AUTOSCROLL_TO_BOTTOM",
  "only the later geometry revision reconverges while tail-follow owns the viewport",
);

check(transcriptTailSettleBudgetExhausted(0) === false, "tail settle may re-aim before its bounded budget is spent");
check(transcriptTailSettleBudgetExhausted(8) === true, "tail settle stops at its bounded re-aim budget");
check(transcriptTailShouldReaim(null, 1_000) === true, "a fresh tail settle always re-aims");
check(transcriptTailShouldReaim(1_000, 1_000 + TRANSCRIPT_TAIL_REARM_MIN_HEIGHT_PX - 1) === false, "sub-threshold tail measurement jitter does not re-aim");
check(transcriptTailShouldReaim(1_000, 1_000 + TRANSCRIPT_TAIL_REARM_MIN_HEIGHT_PX) === true, "real tail growth re-arms the settle writer");
check(transcriptTailIsStranded("tail-follow", 37, true), "an exhausted off-bottom tail-follow is recoverable");
check(!transcriptTailIsStranded("tail-follow", 37, false), "an active tail settle retains automatic ownership");
check(!transcriptTailIsStranded("manual", 37, true), "manual reading never triggers tail exhaustion recovery");

const strandedTail = run([
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true, substantial: true },
  { type: "TAIL_SETTLE_EXHAUSTED" },
]);
check(strandedTail.state.mode === "tail-follow" && !strandedTail.state.atBottom, "exhausted tail repair exposes recovery without revoking ownership");

const repeatedDisplacement = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
  { type: "LAYOUT_HEIGHT_CHANGED" },
]);
check(
  repeatedDisplacement.commands.join(",") === "AUTOSCROLL_TO_BOTTOM",
  "scroll deliveries remain observational while a layout change can reconverge",
);

check(isTranscriptContentShrink(-48), "a fold-sized height drop is a shrink");
check(!isTranscriptContentShrink(-8), "measurement jitter is not a shrink");
check(!isTranscriptContentShrink(80), "content growth is not a shrink");

check(isSubstantialTranscriptDisplacement(1200), "a thumb-drop-sized gap is a substantial displacement");
check(!isSubstantialTranscriptDisplacement(4), "bottom-adjacent jitter is not substantial");

const webView2Scroller = { scrollHeight: 21_442, scrollTop: 20_827, clientHeight: 578 };
check(nativeTranscriptBottomTop(webView2Scroller) === 20_864, "unobserved WebView2 geometry retains the theoretical tail");
check(
  observeNativeTranscriptTailClamp(webView2Scroller, 20_827),
  "a small no-op WebView2 tail write records the reachable native clamp",
);
check(nativeTranscriptBottomTop(webView2Scroller) === 20_827, "the observed WebView2 tail target stays physically reachable");
check(nativeTranscriptDistanceFromBottom(webView2Scroller) === 0, "the observed reachable tail is classified at bottom");
webView2Scroller.scrollHeight += 40;
check(nativeTranscriptBottomTop(webView2Scroller) === 20_867, "content growth preserves the observed terminal residual");
check(nativeTranscriptDistanceFromBottom(webView2Scroller) === 40, "content growth still re-arms tail convergence");
const staleWebView2Range = { scrollHeight: 3_000, scrollTop: 1_000, clientHeight: 500 };
check(
  !observeNativeTranscriptTailClamp(staleWebView2Range, 1_000),
  "a large unmounted WebView2 range is not mistaken for a terminal clamp",
);
check(nativeTranscriptBottomTop(staleWebView2Range) === 2_500, "large gaps retain the LAST-item recovery target");

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
  strandedAfterMisreadShrink.commands.length === 0,
  "substantial scroll deliveries cannot restart a tail feedback loop",
);

const wrapScroller = { scrollHeight: 500, scrollTop: 400, clientHeight: 80 };
check(pinTranscriptTailAfterViewportShrink(wrapScroller, { contentExtent: 500, viewportExtent: 100 }, true) === 420, "a composer-wrap shrink returns the native tail target");
check(wrapScroller.scrollTop === 400, "geometry helper does not write the native scroll position");
check(pinTranscriptTailAfterViewportShrink(wrapScroller, { contentExtent: 500, viewportExtent: 80 }, true) === null, "the same shrink revision does not schedule a second tail write");

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

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
