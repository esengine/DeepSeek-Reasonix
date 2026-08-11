// Run: tsx src/__tests__/transcript-scroll-controller.test.ts
//
// Pure-function tests for the transcript scroll arbitration rules
// (#7420-family: pinned tail-follow must not be derailed by the virtualizer's
// anchor compensation, and the stream-end fallback repin must never yank a
// user who scrolled away).

import { canTranscriptScrollOwnerWrite, shouldAdjustScrollOnItemSizeChange, shouldRunStreamEndRepin } from "../lib/transcriptScrollController";

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

// ── shouldAdjustScrollOnItemSizeChange ──────────────────────────────────────
// While pinned (tail-follow), row-size changes must be handled by the stream
// repin path, never by the virtualizer's anchor compensation.

ok(shouldAdjustScrollOnItemSizeChange(true, "tail-follow") === false, "pinned tail-follow: virtualizer must not compensate");
ok(shouldAdjustScrollOnItemSizeChange(true, "manual") === false, "pinned manual: virtualizer must not compensate");
ok(shouldAdjustScrollOnItemSizeChange(true, "programmatic") === false, "pinned programmatic: virtualizer must not compensate");

ok(shouldAdjustScrollOnItemSizeChange(false, "tail-follow") === true, "not pinned: compensation allowed (anchor keeps the read position)");
ok(shouldAdjustScrollOnItemSizeChange(false, "manual") === true, "not pinned manual: compensation allowed");
ok(shouldAdjustScrollOnItemSizeChange(false, "programmatic") === true, "not pinned programmatic: compensation allowed");

// Selection modes never compensate — the selection-edge scroll owns the viewport.
ok(shouldAdjustScrollOnItemSizeChange(false, "native-selecting") === false, "selection: compensation disabled");
ok(shouldAdjustScrollOnItemSizeChange(false, "logical-selecting") === false, "logical selection: compensation disabled");
ok(shouldAdjustScrollOnItemSizeChange(true, "native-selecting") === false, "pinned selection: compensation disabled");

// ── shouldRunStreamEndRepin ─────────────────────────────────────────────────
// Only the live→absent transition while pinned triggers the fallback repin.

ok(shouldRunStreamEndRepin(true, false, true) === true, "stream ended while pinned: repin");
ok(shouldRunStreamEndRepin(true, false, false) === false, "stream ended while user scrolled away: no yank");
ok(shouldRunStreamEndRepin(false, true, true) === false, "stream started: no repin");
ok(shouldRunStreamEndRepin(false, false, true) === false, "no live at all: no repin");
ok(shouldRunStreamEndRepin(true, true, true) === false, "stream still live: no repin");

// ── canTranscriptScrollOwnerWrite: row-size owner ───────────────────────────
// row-size repins behave like stream/container-resize repins: only in
// tail-follow mode.

ok(canTranscriptScrollOwnerWrite("tail-follow", "row-size") === true, "tail-follow allows row-size repin");
ok(canTranscriptScrollOwnerWrite("manual", "row-size") === false, "manual mode blocks row-size repin");
ok(canTranscriptScrollOwnerWrite("programmatic", "row-size") === false, "programmatic mode blocks row-size repin");
ok(canTranscriptScrollOwnerWrite("native-selecting", "row-size") === false, "selection mode blocks row-size repin");
ok(canTranscriptScrollOwnerWrite("tail-follow", "stream") === true, "tail-follow still allows stream repin");
ok(canTranscriptScrollOwnerWrite("tail-follow", "virtualizer") === true, "virtualizer owner remains always writable for non-selection modes");

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
