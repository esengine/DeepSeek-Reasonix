// Run: node --import tsx src/__tests__/transcript-scroll-release.test.ts

import {
  INITIAL_TRANSCRIPT_SCROLL_STATE,
  isSubstantialTranscriptDisplacement,
  isTranscriptContentShrink,
  reduceTranscriptScroll,
  type TranscriptScrollEvent,
  type TranscriptScrollState,
} from "../lib/transcriptScrollArbiter";

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
  { type: "SCROLL_DELIVERED", atBottom: true, scrollable: true },
]);
check(returned.state.mode === "tail-follow", "reaching the real bottom re-engages tail-follow");

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

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
