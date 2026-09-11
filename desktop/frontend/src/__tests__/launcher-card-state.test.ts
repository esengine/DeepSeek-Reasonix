// Run: tsx src/__tests__/launcher-card-state.test.ts
//
// Guards the floating launcher card state machine (the regressions this
// protects: the card's render condition and the toggle's pressed state must
// never diverge). Two layers:
//   1. Truth-table over resolveLauncherCardState — every combination of
//      spaceMode × dismissed. The card is independent of the dock panel's
//      open state: the panel has its own toggle.
//   2. Source contracts — App must drive the toggle through the shared
//      resolver (not inline the condition again), and DockLauncher must keep
//      reporting its space-yield mode upward.

import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { resolveLauncherCardState, type SpaceMode } from "../lib/launcherCardState";

let passed = 0;
let failed = 0;

function eq<T>(actual: T, expected: T, label: string) {
  if (Object.is(actual, expected)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${String(expected)}, got ${String(actual)}\n`);
    failed += 1;
  }
}

const modes: SpaceMode[] = ["full", "hidden"];

// ---- 1. Truth table -------------------------------------------------------
process.stdout.write("truth table: spaceMode × dismissed\n");
for (const spaceMode of modes) {
  for (const dismissed of [false, true]) {
    const { renderable, visible } = resolveLauncherCardState({ spaceMode, dismissed });
    const tag = `m=${spaceMode} d=${dismissed}`;
    eq(renderable, spaceMode === "full", `renderable ${tag}`);
    eq(visible, spaceMode === "full" && !dismissed, `visible ${tag}`);
    // A visible card is always renderable (consistency invariant).
    assert.ok(!visible || renderable, `invariant: visible implies renderable ${tag}`);
  }
}

// ---- 2. Source contracts --------------------------------------------------
process.stdout.write("source contracts\n");
const testDir = dirname(fileURLToPath(import.meta.url));
const commandsSource = readFileSync(resolve(testDir, "../app-runtime/useWorkspacePanelCommands.ts"), "utf8");
const spaceSource = readFileSync(resolve(testDir, "../lib/useDockLauncherSpace.ts"), "utf8");
const toggleSource = readFileSync(resolve(testDir, "../app-shell/LauncherToggleButton.tsx"), "utf8");

assert.match(
  commandsSource,
  /const launcherCard = resolveLauncherCardState\(\{ spaceMode: launcherCardSpaceMode, dismissed: launcherDismissed \}\);/,
  "the dock command owner drives the card through the shared resolver",
);
assert.doesNotMatch(
  commandsSource,
  /const launcherCardRenderable = !/,
  "the renderable condition is not inlined (single source of truth)",
);
assert.match(
  commandsSource,
  /if \(launcherCardSpaceMode === "hidden"\) return;/,
  "the toggle stays inert only while the surface is too narrow for the card",
);
assert.match(
  spaceSource,
  /onSpaceModeChange\?\.\(next\)/,
  "the space hook keeps reporting the card's space-yield mode upward",
);
assert.match(
  toggleSource,
  /aria-pressed=\{visible\}/,
  "the toggle's pressed state mirrors the card's actual visibility",
);

passed += 5; // the five assert.* checks above
process.stdout.write(`launcher card state: ${passed} checks passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
