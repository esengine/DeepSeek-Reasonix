// Run: tsx src/__tests__/dpi-scale.test.ts
//
// Coverage for the DPI zoom helpers (src/lib/dpiScale.ts): snapZoom clamps to
// [0.5, 2.0] and snaps to the 0.05 step grid; zoomToPercent / percentToZoom
// convert between the factor and the 50-200 integer percentage.

import { snapZoom, zoomToPercent, percentToZoom } from "../lib/dpiScale";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (JSON.stringify(a) === JSON.stringify(b)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\ndpi-scale zoom helpers");

// ── snapZoom: range boundaries ──────────────────────────────────────
eq(snapZoom(0.5), 0.5, "snapZoom(0.5) stays at the minimum");
eq(snapZoom(2.0), 2.0, "snapZoom(2.0) stays at the maximum");
eq(snapZoom(1.0), 1.0, "snapZoom(1.0) is the identity at default zoom");

// ── snapZoom: below / above range are clamped ───────────────────────
eq(snapZoom(0.1), 0.5, "snapZoom(0.1) clamps up to the minimum");
eq(snapZoom(0.0), 0.5, "snapZoom(0) clamps up to the minimum");
eq(snapZoom(-1), 0.5, "snapZoom(-1) clamps up to the minimum");
eq(snapZoom(0.499), 0.5, "snapZoom(0.499) rounds down to the minimum");
eq(snapZoom(3), 2.0, "snapZoom(3) clamps down to the maximum");
eq(snapZoom(2.001), 2.0, "snapZoom(2.001) clamps down to the maximum");

// ── snapZoom: 0.05 step snapping ────────────────────────────────────
eq(snapZoom(1.975), 2.0, "snapZoom(1.975) snaps up to 2.0 (half-step rounds up)");
eq(snapZoom(1.03), 1.05, "snapZoom(1.03) snaps to the nearest step (1.05)");
eq(snapZoom(1.02), 1.0, "snapZoom(1.02) snaps to the nearest step (1.0)");
eq(snapZoom(1.08), 1.1, "snapZoom(1.08) snaps to the nearest step (1.1)");
eq(snapZoom(1.47), 1.45, "snapZoom(1.47) snaps to the nearest step (1.45)");
eq(snapZoom(0.99), 1.0, "snapZoom(0.99) snaps up to 1.0");

// ── zoomToPercent ───────────────────────────────────────────────────
eq(zoomToPercent(1.0), 100, "zoomToPercent(1.0) is 100");
eq(zoomToPercent(0.5), 50, "zoomToPercent(0.5) is 50");
eq(zoomToPercent(2.0), 200, "zoomToPercent(2.0) is 200");
eq(zoomToPercent(1.05), 105, "zoomToPercent(1.05) is 105");
eq(zoomToPercent(1.234), 123, "zoomToPercent(1.234) rounds to 123");
eq(zoomToPercent(1.975), 198, "zoomToPercent(1.975) rounds to 198");

// ── percentToZoom ───────────────────────────────────────────────────
eq(percentToZoom(50), 0.5, "percentToZoom(50) is 0.5");
eq(percentToZoom(200), 2.0, "percentToZoom(200) is 2.0");
eq(percentToZoom(100), 1.0, "percentToZoom(100) is 1.0");
eq(percentToZoom(125), 1.25, "percentToZoom(125) is 1.25");
eq(percentToZoom(37), 0.5, "percentToZoom(37) clamps up to 0.5");
eq(percentToZoom(250), 2.0, "percentToZoom(250) clamps down to 2.0");
eq(percentToZoom(127), 1.25, "percentToZoom(127) snaps to 1.25");
eq(percentToZoom(199), 2.0, "percentToZoom(199) snaps up to 2.0");

console.log(`\n${passed} passed, ${failed} failed\n`);
process.exit(failed === 0 ? 0 : 1);
