// Run: tsx src/__tests__/creation-transcript-scrollbar.test.ts

import {
  mapFrozenScrollbarDrag,
  rebaseFrozenScrollbarDrag,
  readCreationScrollbarGeometry,
} from "../lib/useCreationTranscriptScrollbar";

let passed = 0;
let failed = 0;

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
    failed += 1;
  }
}

console.log("\ncreation transcript scrollbar");

const initial = readCreationScrollbarGeometry(800, 4_000);
if (!initial) throw new Error("expected scrollable geometry");
eq(initial.overflow, 3_200, "geometry records the pointerdown overflow");
eq(initial.thumbHeight, 160, "geometry derives the thumb from the pointerdown layout");
eq(initial.maxThumbTop, 640, "geometry records the pointerdown thumb track");

const drag = {
  startY: 100,
  startThumbTop: 320,
  overflow: initial.overflow,
  maxThumbTop: initial.maxThumbTop,
};
const moved = mapFrozenScrollbarDrag(drag, 200);
eq(moved.thumbTop, 420, "drag follows pointer pixels on the frozen track");
eq(moved.scrollTop, 2_100, "drag maps through the frozen pointerdown overflow");

// Content can grow while virtual rows mount. Rebase the active drag at the
// current physical position so the next pointer move cannot write through a
// stale extent while preserving the thumb's visible position.
const grown = readCreationScrollbarGeometry(800, 8_000);
if (!grown) throw new Error("expected grown scrollable geometry");
eq(grown.overflow, 7_200, "content growth would otherwise change the live mapping");
const rebased = rebaseFrozenScrollbarDrag(drag, grown, 2_100, 200);
eq(rebased.startY, 200, "geometry rebase pins the drag to the latest pointer sample");
eq(rebased.startThumbTop, 210, "geometry rebase preserves the current physical scroll ratio");
eq(mapFrozenScrollbarDrag(rebased, 300).scrollTop, 3_100, "subsequent drag movement uses the current extent");

eq(mapFrozenScrollbarDrag(drag, -1_000).thumbTop, 0, "drag clamps at the top");
eq(mapFrozenScrollbarDrag(drag, 2_000).thumbTop, 640, "drag clamps at the bottom");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
