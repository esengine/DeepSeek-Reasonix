// Run: node --import tsx src/__tests__/transcript-scroll-release.test.ts

import {
  INITIAL_TRANSCRIPT_SCROLL_STATE,
  isSubstantialTranscriptDisplacement,
  isTranscriptContentShrink,
  reduceTranscriptScroll,
  transcriptReaderBufferingForMode,
  transcriptReaderViewportBuffer,
  TRANSCRIPT_READER_OVERSCAN_ROWS,
  TRANSCRIPT_READER_VIEWPORT_BUFFER,
  type TranscriptScrollEvent,
  type TranscriptScrollState,
} from "../lib/transcriptScrollArbiter";
import { pinTranscriptTailAfterViewportShrink } from "../lib/transcriptScrollGeometry";
import {
  TRANSCRIPT_TAIL_REARM_MIN_HEIGHT_PX,
  transcriptTailSettleBudgetExhausted,
  transcriptTailShouldReaim,
} from "../lib/transcriptTailSettle";
import { shouldClaimTranscriptTailFromWheel } from "../lib/transcriptWheelTailClaim";

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

check(shouldClaimTranscriptTailFromWheel(17, 24, false), "a stable final downward wheel claims the remaining native tail range");
check(!shouldClaimTranscriptTailFromWheel(0, 24, false), "a reader gesture already at the physical tail keeps its correction transaction");
check(!shouldClaimTranscriptTailFromWheel(97, 24, false), "a mid-transcript wheel keeps native reader ownership");
check(!shouldClaimTranscriptTailFromWheel(17, 24, true), "transient layout never claims the tail from estimated geometry");

const streaming = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "TAIL_CONTENT_CHANGED" },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
  { type: "LAYOUT_HEIGHT_CHANGED" },
]);
check(streaming.state.mode === "tail-follow", "dynamic atBottom=false does not steal tail ownership");
check(
  streaming.commands.join(",") === "AUTOSCROLL_TO_BOTTOM,AUTOSCROLL_TO_BOTTOM",
  "only explicit geometry changes emit tail commands; scroll delivery is observation",
);

const manual = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "USER_SCROLL_INTENT", canClaimTail: false },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
  { type: "TAIL_CONTENT_CHANGED" },
  { type: "VIEWPORT_RESIZED" },
]);
check(manual.state.mode === "reader-gesture", "explicit user intent releases tail-follow into a reader transaction");
check(manual.commands.length === 0, "manual reading never receives tail commands");

const upwardIntentAtBottomRace = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "USER_SCROLL_INTENT", canClaimTail: false },
  // A scroll delivery queued before the trusted wheel's native default action
  // must not reclaim the tail from an upward reader gesture.
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
]);
check(upwardIntentAtBottomRace.state.mode === "reader-gesture", "upward reader intent survives a stale at-bottom delivery");

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

const stationaryNativeThumb = run([
  { type: "NATIVE_SCROLLBAR_BEGIN" },
  { type: "NATIVE_SCROLLBAR_END", canClaimTail: false },
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true, tailMounted: true },
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true, tailMounted: true },
]);
check(stationaryNativeThumb.state.mode === "reader-gesture", "a stationary native thumb cannot claim tail ownership");

const movedNativeThumb = run([
  { type: "NATIVE_SCROLLBAR_BEGIN" },
  { type: "NATIVE_SCROLLBAR_END", canClaimTail: true },
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true, tailMounted: true },
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true, tailMounted: true },
]);
check(movedNativeThumb.state.mode === "tail-follow", "a moved native thumb can transfer a stable bottom to tail-follow");

const geometryChangedBetweenBottomSamples = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "USER_SCROLL_INTENT", canClaimTail: true },
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true, tailMounted: true },
  { type: "GEOMETRY_CHANGED", revision: 4 },
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true, tailMounted: true },
]);
check(
  geometryChangedBetweenBottomSamples.state.mode === "reader-gesture",
  "an extent revision between bottom samples restarts the stable-tail hold",
);
const stableAfterGeometry = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true, tailMounted: true },
], geometryChangedBetweenBottomSamples.state);
check(stableAfterGeometry.state.mode === "tail-follow", "two bottom samples on the same revised extent claim the tail");

const touchDownOnce = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "USER_SCROLL_INTENT", canClaimTail: true },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
]);
check(touchDownOnce.state.mode === "reader-gesture", "a single touch-down at the bottom stays reader-owned");

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
check(holdBrokenByUpwardGesture.state.mode === "reader-gesture", "an upward gesture breaks the bottom-hold streak");

const holdEndsWithIntentWindow = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "USER_SCROLL_INTENT", canClaimTail: true },
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "READER_INTENT_ENDED" },
  { type: "USER_SCROLL_INTENT", canClaimTail: true },
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
]);
check(holdEndsWithIntentWindow.state.mode === "reader-gesture", "a fresh gesture after idle rebuilds the bottom-hold streak");

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
check(selectionThenQuestionJump.state.mode === "programmatic", "question navigation takes ownership after clearing a stale selection gesture");
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
  "later geometry growth reconverges without scroll-delivery feedback",
);

check(transcriptTailSettleBudgetExhausted(0) === false, "tail settle may re-aim before its bounded budget is spent");
check(transcriptTailSettleBudgetExhausted(1) === true, "one geometry revision has a one-write budget");
check(transcriptTailShouldReaim(null, 1_000) === true, "a fresh tail settle always re-aims");
check(transcriptTailShouldReaim(1_000, 1_000 + TRANSCRIPT_TAIL_REARM_MIN_HEIGHT_PX - 1) === false, "sub-threshold tail measurement jitter does not re-aim");
check(transcriptTailShouldReaim(1_000, 1_000 + TRANSCRIPT_TAIL_REARM_MIN_HEIGHT_PX) === true, "real tail growth re-arms the settle writer");

const repeatedDisplacement = run([
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
  { type: "SCROLL_DELIVERED", atBottom: false, scrollable: true },
  { type: "LAYOUT_HEIGHT_CHANGED" },
]);
check(
  repeatedDisplacement.commands.join(",") === "AUTOSCROLL_TO_BOTTOM",
  "repeated non-bottom deliveries never write; a layout change can reconverge once",
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
check(strandedAfterMisreadShrink.commands.length === 0,
  "substantial scroll deliveries cannot restart tail writes after a misread shrink");

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
check(transcriptReaderBufferingForMode(false, "reader-gesture"), "reader input enables the bounded virtual-row buffer");
check(transcriptReaderBufferingForMode(true, "tail-follow"), "tail handoff retains the active reader buffer");
check(transcriptReaderBufferingForMode(true, "manual"), "reader idle retains an established reader buffer");
check(!transcriptReaderBufferingForMode(false, "manual"), "programmatic manual mode does not create a reader buffer");
check(!transcriptReaderBufferingForMode(true, "manual", "READER_INTENT_ENDED"), "settled reader input releases its extra virtual range");
check(transcriptReaderBufferingForMode(true, "tail-follow", "READER_INTENT_ENDED"), "tail handoff retains its painted virtual range after intent expiry");
check(transcriptReaderBufferingForMode(true, "selection"), "selection retains the painted reader buffer");
check(transcriptReaderBufferingForMode(true, "selection", "READER_INTENT_ENDED"), "selection retains its range when an older reader timer expires");
check(!transcriptReaderBufferingForMode(false, "selection"), "selection does not create a reader buffer from an idle surface");
check(TRANSCRIPT_READER_VIEWPORT_BUFFER === 1, "reader buffering retains one mounted native viewport");
check(TRANSCRIPT_READER_OVERSCAN_ROWS === 24, "reader buffering retains a bounded 24-row window per edge");
check(transcriptReaderViewportBuffer("Mozilla/5.0 AppleWebKit/605.1.15 Version/17.5 Safari/605.1.15") === 2, "WKWebView retains a second compositor viewport");
check(transcriptReaderViewportBuffer("Mozilla/5.0 AppleWebKit/537.36 Chrome/128.0 Safari/537.36 Edg/128.0") === 1, "WebView2 retains the bounded default viewport");
check(transcriptReaderViewportBuffer("Mozilla/5.0 AppleWebKit/537.36 Chrome/128.0 Safari/537.36 Edg/128.0", true) === 3, "native WebView2 retains three compositor viewports");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
