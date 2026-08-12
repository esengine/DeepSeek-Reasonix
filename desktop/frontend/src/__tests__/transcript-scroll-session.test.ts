// Run: tsx src/__tests__/transcript-scroll-session.test.ts

import {
  canTranscriptScrollOwnerWriteNow,
  canVirtualizerAdjustScroll,
  canScrollEndSettle,
  GESTURE_HOLD_MS,
  isUserGestureActive,
  noteUserGesture,
} from "../lib/transcriptScrollSession";

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) ok(true, label);
  else ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

console.log("\ntranscript scroll session");

const t0 = 1_000_000;
const until = noteUserGesture(t0, GESTURE_HOLD_MS);
eq(until, t0 + GESTURE_HOLD_MS, "noteUserGesture extends hold from now");
ok(isUserGestureActive(until, t0 + 10), "gesture is active just after the sample");
ok(!isUserGestureActive(until, t0 + GESTURE_HOLD_MS + 1), "gesture expires after the hold window");
ok(!canScrollEndSettle(t0, t0 + 10), "stale scrollend cannot terminate fresh input");
ok(canScrollEndSettle(t0, t0 + 100), "scrollend settles after a quiet input window");

ok(
  !canVirtualizerAdjustScroll("manual", until, t0 + 1),
  "virtualizer adjust is frozen during user gesture",
);
ok(
  canVirtualizerAdjustScroll("manual", until, t0 + GESTURE_HOLD_MS + 1),
  "virtualizer adjust resumes after the gesture hold",
);
ok(
  !canVirtualizerAdjustScroll("native-selecting", 0, t0),
  "virtualizer adjust stays off during native selection",
);

ok(
  !canTranscriptScrollOwnerWriteNow("manual", "virtualizer", until, t0 + 1),
  "virtualizer cannot rewrite scrollTop mid-gesture",
);
ok(
  !canTranscriptScrollOwnerWriteNow("tail-follow", "stream", until, t0 + 1),
  "stream cannot yank to bottom mid-gesture",
);
ok(
  !canTranscriptScrollOwnerWriteNow("tail-follow", "container-resize", until, t0 + 1),
  "container-resize cannot rewrite scrollTop mid-gesture",
);
ok(
  !canTranscriptScrollOwnerWriteNow("tail-follow", "row-size", until, t0 + 1),
  "row-size cannot rewrite scrollTop mid-gesture",
);
ok(
  canTranscriptScrollOwnerWriteNow("manual", "jump", until, t0 + 1),
  "explicit jump still works during gesture",
);
ok(
  canTranscriptScrollOwnerWriteNow("manual", "jump-bottom", until, t0 + 1),
  "explicit jump-bottom still works during gesture",
);
ok(
  canTranscriptScrollOwnerWriteNow("manual", "custom-scrollbar", until, t0 + 1),
  "custom scrollbar drag still works during gesture",
);
ok(
  canTranscriptScrollOwnerWriteNow("tail-follow", "stream", 0, t0),
  "stream can write in tail-follow when no gesture is active",
);
ok(
  canTranscriptScrollOwnerWriteNow("manual", "virtualizer", 0, t0),
  "virtualizer can compensate after gesture ends",
);
ok(
  !canTranscriptScrollOwnerWriteNow("native-selecting", "virtualizer", 0, t0),
  "selection mode still blocks virtualizer writes",
);
ok(
  canTranscriptScrollOwnerWriteNow("native-selecting", "selection-edge-scroll", 0, t0),
  "selection edge scroll still works in selection mode",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
