// Run: tsx src/__tests__/composer-content-sizing.test.ts

import { resolveComposerContentSizing } from "../lib/composerSizing";

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

console.log("\ncomposer content sizing");

const automatic = resolveComposerContentSizing({
  contentHeight: 86,
  manualLogicalHeight: null,
  maxLogicalHeight: 360,
  reservedHeight: 58,
});
eq(automatic.inputHeight, 86, "automatic composer follows its content height");
eq(automatic.logicalHeight, null, "automatic composer keeps natural card sizing");
eq(automatic.overflow, false, "automatic composer stays overflow-free below the cap");

const shortManual = resolveComposerContentSizing({
  contentHeight: 22,
  manualLogicalHeight: 128,
  maxLogicalHeight: 360,
  reservedHeight: 58,
});
eq(shortManual.inputHeight, 70, "manual height remains the minimum input baseline");
eq(shortManual.logicalHeight, 128, "short content does not shrink below the manual baseline");

const growingManual = resolveComposerContentSizing({
  contentHeight: 108,
  manualLogicalHeight: 128,
  maxLogicalHeight: 360,
  reservedHeight: 58,
});
eq(growingManual.inputHeight, 108, "content can grow beyond the manual input baseline");
eq(growingManual.logicalHeight, 166, "manual composer grows to reveal the longer draft");
eq(growingManual.overflow, false, "growing content remains visible before the cap");

const cappedManual = resolveComposerContentSizing({
  contentHeight: 420,
  manualLogicalHeight: 128,
  maxLogicalHeight: 360,
  reservedHeight: 58,
});
eq(cappedManual.inputHeight, 302, "manual composer input stops at the viewport-aware cap");
eq(cappedManual.logicalHeight, 360, "manual composer card stops at the logical maximum");
eq(cappedManual.overflow, true, "content scrolls internally only after reaching the cap");

const shrunkManual = resolveComposerContentSizing({
  contentHeight: 30,
  manualLogicalHeight: 128,
  maxLogicalHeight: 360,
  reservedHeight: 58,
});
eq(shrunkManual.logicalHeight, 128, "deleting content returns the composer to its manual baseline");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
