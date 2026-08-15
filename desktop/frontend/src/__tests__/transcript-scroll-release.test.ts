// Run: node --import tsx src/__tests__/transcript-scroll-release.test.ts

import {
  INITIAL_TRANSCRIPT_SCROLL_STATE,
  reduceTranscriptScroll,
  type TranscriptScrollEvent,
  type TranscriptScrollState,
} from "../lib/transcriptScrollController";

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
  { type: "AT_BOTTOM_CHANGED", atBottom: true, scrollable: true },
  { type: "TAIL_CONTENT_CHANGED" },
  { type: "AT_BOTTOM_CHANGED", atBottom: false, scrollable: true },
  { type: "LAYOUT_HEIGHT_CHANGED" },
]);
check(streaming.state.mode === "tail-follow", "dynamic atBottom=false does not steal tail ownership");
check(streaming.commands.join(",") === "AUTOSCROLL_TO_BOTTOM,AUTOSCROLL_TO_BOTTOM", "tail growth emits only Virtuoso autoscroll commands");

const manual = run([
  { type: "AT_BOTTOM_CHANGED", atBottom: true, scrollable: true },
  { type: "USER_SCROLL_INTENT" },
  { type: "AT_BOTTOM_CHANGED", atBottom: false, scrollable: true },
  { type: "TAIL_CONTENT_CHANGED" },
  { type: "VIEWPORT_RESIZED" },
]);
check(manual.state.mode === "manual", "explicit user intent releases tail-follow");
check(manual.commands.length === 0, "manual reading never receives tail commands");

const returned = run([
  { type: "AT_BOTTOM_CHANGED", atBottom: true, scrollable: true },
  { type: "USER_SCROLL_INTENT" },
  { type: "AT_BOTTOM_CHANGED", atBottom: false, scrollable: true },
  { type: "AT_BOTTOM_CHANGED", atBottom: true, scrollable: true },
]);
check(returned.state.mode === "tail-follow", "reaching the real bottom re-engages tail-follow");

const shortTranscript = run([
  { type: "AT_BOTTOM_CHANGED", atBottom: true, scrollable: false },
  { type: "USER_SCROLL_INTENT" },
]);
check(shortTranscript.state.mode === "tail-follow", "non-overflow transcript always stays tail-follow");

const fold = run([
  { type: "AT_BOTTOM_CHANGED", atBottom: true, scrollable: true },
  { type: "USER_RESIZE_BEGIN" },
  { type: "LAYOUT_HEIGHT_CHANGED" },
  { type: "USER_RESIZE_END" },
]);
check(fold.state.mode === "manual", "user fold resize settles in manual mode");
check(fold.commands.length === 0, "user fold resize cannot tug the viewport to the tail");

const selection = run([
  { type: "AT_BOTTOM_CHANGED", atBottom: true, scrollable: true },
  { type: "SELECTION_BEGIN" },
  { type: "SCROLL_TO_OFFSET", owner: "selection-edge-scroll", top: 120 },
  { type: "LAYOUT_HEIGHT_CHANGED" },
  { type: "SELECTION_END" },
]);
check(selection.state.mode === "manual", "selection returns to manual reading");
check(selection.commands.join(",") === "SCROLL_TO_OFFSET", "selection owns only its explicit edge-scroll command");

const jump = run([
  { type: "AT_BOTTOM_CHANGED", atBottom: false, scrollable: true },
  { type: "USER_SCROLL_INTENT" },
  { type: "JUMP_TO_BOTTOM", behavior: "smooth" },
]);
check(jump.state.mode === "tail-follow", "jump-bottom explicitly owns the tail");
check(jump.commands.join(",") === "SCROLL_TO_LAST", "jump-bottom uses Virtuoso scrollToIndex only");

const restore = run([
  { type: "AT_BOTTOM_CHANGED", atBottom: true, scrollable: true },
  { type: "JUMP_TO_INDEX", index: 42 },
  { type: "PROGRAMMATIC_END" },
]);
check(restore.state.mode === "manual", "question/rewind navigation settles in manual mode");
check(restore.commands.join(",") === "SCROLL_TO_INDEX", "navigation emits one indexed Virtuoso command");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
