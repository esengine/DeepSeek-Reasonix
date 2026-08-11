// Run: tsx src/__tests__/transcript-scroll-controller.test.ts

import {
  canTranscriptScrollOwnerWrite,
  shouldAdjustScrollOnItemSizeChange,
  shouldRunStreamEndRepin,
} from "../lib/transcriptScrollController";

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

ok(!shouldAdjustScrollOnItemSizeChange(true, "tail-follow"), "pinned tail-follow does not use anchor compensation");
ok(!shouldAdjustScrollOnItemSizeChange(true, "manual"), "a pinned viewport never uses anchor compensation");
ok(shouldAdjustScrollOnItemSizeChange(false, "tail-follow"), "an unpinned viewport may preserve its anchor");
ok(shouldAdjustScrollOnItemSizeChange(false, "manual"), "manual reading may preserve its anchor");
ok(!shouldAdjustScrollOnItemSizeChange(false, "native-selecting"), "native selection blocks anchor compensation");
ok(!shouldAdjustScrollOnItemSizeChange(false, "logical-selecting"), "logical selection blocks anchor compensation");

ok(shouldRunStreamEndRepin(true, false, true), "live-to-settled transition repins while pinned");
ok(!shouldRunStreamEndRepin(true, false, false), "stream end never yanks a detached reader");
ok(!shouldRunStreamEndRepin(false, true, true), "stream start does not schedule an end repin");
ok(!shouldRunStreamEndRepin(true, true, true), "an active stream does not run the end fallback");
ok(!shouldRunStreamEndRepin(false, false, true), "an idle transcript does not run the end fallback");

ok(canTranscriptScrollOwnerWrite("tail-follow", "row-size"), "tail-follow allows row-size repin");
ok(!canTranscriptScrollOwnerWrite("manual", "row-size"), "manual mode blocks row-size repin");
ok(!canTranscriptScrollOwnerWrite("programmatic", "row-size"), "programmatic mode blocks row-size repin");
ok(!canTranscriptScrollOwnerWrite("native-selecting", "row-size"), "selection mode blocks row-size repin");

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
