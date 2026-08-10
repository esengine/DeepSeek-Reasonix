import {
  applyReasoningDisplayMode,
  getReasoningDisplayMode,
  hydrateReasoningDisplayMode,
  resolveReasoningDisplayMode,
  setReasoningDisplayPending,
} from "../lib/reasoningDisplayPreference";

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

console.log("\nreasoning display preference");

const storage = new Map<string, string>();
Object.defineProperty(globalThis, "localStorage", {
  configurable: true,
  value: {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => void storage.set(key, value),
    removeItem: (key: string) => void storage.delete(key),
  },
});

storage.clear();
hydrateReasoningDisplayMode(undefined, false);
ok(getReasoningDisplayMode() === "auto", "missing preferences default to live follow");

hydrateReasoningDisplayMode("future-mode", false);
ok(getReasoningDisplayMode() === "auto", "unknown preferences use the live-follow fallback");

storage.set("reasonix-reasoning-summary", "0");
hydrateReasoningDisplayMode("auto", false);
ok(getReasoningDisplayMode() === "legacy-collapsed", "legacy summary-off preserves the old compatible behavior");

storage.set("reasonix-reasoning-summary", "1");
hydrateReasoningDisplayMode("auto", false);
ok(getReasoningDisplayMode() === "summary", "legacy summary-on wins over the old expand flag");

storage.set("reasonix-reasoning-summary", "0");
hydrateReasoningDisplayMode("hidden", true);
ok(getReasoningDisplayMode() === "hidden", "explicit new mode wins over every legacy preference");

hydrateReasoningDisplayMode("summary", true);
ok(getReasoningDisplayMode() === "summary", "an explicit summary selection remains persisted");

storage.set("reasonix-reasoning-summary", "0");
ok(resolveReasoningDisplayMode("auto", false) === "legacy-collapsed", "resolver applies legacy precedence without hydration");
hydrateReasoningDisplayMode("auto", false);
applyReasoningDisplayMode("auto");
ok(getReasoningDisplayMode() === "auto", "successful selection applies the new mode");
ok(!storage.has("reasonix-reasoning-summary"), "successful selection clears the legacy key");

setReasoningDisplayPending();
ok(getReasoningDisplayMode() === "pending", "startup can hold reasoning until settings hydrate");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
