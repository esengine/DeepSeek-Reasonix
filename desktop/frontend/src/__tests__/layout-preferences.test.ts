// Run: tsx src/__tests__/layout-preferences.test.ts
// (sandbox note: `node --import tsx src/__tests__/layout-preferences.test.ts`)

import { JSDOM } from "jsdom";

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

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

console.log("\nlayout preferences");

const {
  loadLayoutSize,
  loadOptionalLayoutSize,
  saveLayoutSize,
  clearLayoutSize,
} = await import("../lib/layoutPreferences");

// --- SSR / no window: every accessor must degrade gracefully ---
eq(loadLayoutSize("sidebarWidth", 320), 320, "SSR: loadLayoutSize falls back without a window");
eq(loadLayoutSize("sidebarWidth", 320, (v) => Math.min(v, 300)), 300, "SSR: clamp still applies to the fallback");
eq(loadOptionalLayoutSize("sidebarWidth"), null, "SSR: optional load returns null");
saveLayoutSize("sidebarWidth", 300);
clearLayoutSize("sidebarWidth");
ok(true, "SSR: save/clear are no-ops (no throw)");

// --- jsdom environment with a real localStorage ---
const dom = new JSDOM("<!doctype html><html><body></body></html>", { url: "http://localhost" });
globalThis.window = dom.window as unknown as Window & typeof globalThis;
const store = dom.window.localStorage;

// defaults when storage is empty
eq(loadLayoutSize("sidebarWidth", 320), 320, "empty storage: loadLayoutSize returns the fallback");
eq(loadLayoutSize("sidebarWidthGraphite", 280, (v) => Math.min(v, 240)), 240, "empty storage: clamp applies to the fallback");
eq(loadOptionalLayoutSize("composerHeight"), null, "empty storage: optional load returns null");

// stored v1 prefs win over fallback and legacy keys
store.setItem("reasonix.layoutPreferences.v1", JSON.stringify({ sizes: { sidebarWidth: 420 } }));
eq(loadLayoutSize("sidebarWidth", 320), 420, "stored pref beats the fallback");
eq(loadOptionalLayoutSize("sidebarWidth"), 420, "stored pref returned by optional load");

// legacy migration paths
store.removeItem("reasonix.layoutPreferences.v1");
store.setItem("reasonix.sidebar.width", "240");
eq(loadLayoutSize("sidebarWidth", 320), 240, "legacy key migrates when no v1 pref exists");
eq(loadLayoutSize("sidebarWidthGraphite", 320), 320, "key without a legacy mapping falls back");
store.setItem("reasonix.layoutPreferences.v1", JSON.stringify({ sizes: { sidebarWidth: 400 } }));
eq(loadLayoutSize("sidebarWidth", 320), 400, "v1 pref beats a legacy key");

// legacy values: invalid entries are skipped, later valid ones win
store.setItem("reasonix.layoutPreferences.v1", JSON.stringify({}));
store.setItem("reasonix.composerHeight", "abc");
eq(loadLayoutSize("composerHeight", 300), 300, "non-numeric legacy value ignored");
store.setItem("reasonix.composerHeight", "0");
eq(loadLayoutSize("composerHeight", 300), 300, "zero legacy value ignored");
store.setItem("reasonix.composerHeight", "-5");
eq(loadLayoutSize("composerHeight", 300), 300, "negative legacy value ignored");
store.setItem("reasonix.composerHeight", "150.6");
eq(loadLayoutSize("composerHeight", 300), 151, "valid legacy value is read and rounded");

// invalid stored values are ignored (fall through to legacy/fallback)
store.removeItem("reasonix.sidebar.width");
store.setItem("reasonix.layoutPreferences.v1", JSON.stringify({ sizes: { sidebarWidth: "300" } }));
eq(loadLayoutSize("sidebarWidth", 320), 320, "string stored value ignored");
store.setItem("reasonix.layoutPreferences.v1", JSON.stringify({ sizes: { sidebarWidth: -10 } }));
eq(loadLayoutSize("sidebarWidth", 320), 320, "negative stored value ignored");
store.setItem("reasonix.layoutPreferences.v1", JSON.stringify({ sizes: { sidebarWidth: 0 } }));
eq(loadLayoutSize("sidebarWidth", 320), 320, "zero stored value ignored");
store.setItem("reasonix.layoutPreferences.v1", JSON.stringify({ sizes: { sidebarWidth: null } }));
eq(loadLayoutSize("sidebarWidth", 320), 320, "null stored value ignored");
store.setItem("reasonix.layoutPreferences.v1", "{not json");
eq(loadLayoutSize("sidebarWidth", 320), 320, "corrupt JSON falls back to the default");
store.setItem("reasonix.layoutPreferences.v1", JSON.stringify("a string"));
eq(loadLayoutSize("sidebarWidth", 320), 320, "non-object stored payload falls back");

// rounding and clamping of stored values
store.setItem("reasonix.layoutPreferences.v1", JSON.stringify({ sizes: { sidebarWidth: 300.6 } }));
eq(loadLayoutSize("sidebarWidth", 320), 301, "stored value is rounded");
store.setItem("reasonix.layoutPreferences.v1", JSON.stringify({ sizes: { sidebarWidth: 500 } }));
eq(loadLayoutSize("sidebarWidth", 320, (v) => Math.min(v, 400)), 400, "clamp applies to a stored value");
eq(loadOptionalLayoutSize("sidebarWidth", (v) => Math.min(v, 400)), 400, "clamp applies to an optional load");

// save: merging with stored values, preserving other keys
store.setItem("reasonix.layoutPreferences.v1", JSON.stringify({ sizes: { sidebarWidth: 300 } }));
saveLayoutSize("composerHeight", 140);
const merged = JSON.parse(store.getItem("reasonix.layoutPreferences.v1")!) as { sizes: Record<string, number> };
eq(merged.sizes.sidebarWidth, 300, "save keeps existing keys");
eq(merged.sizes.composerHeight, 140, "save writes the new key");
eq(loadLayoutSize("composerHeight", 300), 140, "saved value loads back");
saveLayoutSize("drawerWidth", 9999, (v) => Math.min(v, 800));
eq(loadLayoutSize("drawerWidth", 400), 800, "save clamps before writing");
saveLayoutSize("settingsDrawerWidth", 333.5);
eq(loadLayoutSize("settingsDrawerWidth", 100), 334, "save rounds before writing");
store.removeItem("reasonix.layoutPreferences.v1");
saveLayoutSize("rightDockWidth", 260);
eq(loadLayoutSize("rightDockWidth", 200), 260, "save works on empty storage");

// clear: removes only the requested key
store.setItem("reasonix.layoutPreferences.v1", JSON.stringify({ sizes: { sidebarWidth: 300, composerHeight: 140 } }));
clearLayoutSize("sidebarWidth");
eq(loadLayoutSize("sidebarWidth", 320), 320, "cleared key falls back to the default");
eq(loadLayoutSize("composerHeight", 300), 140, "clear keeps sibling keys");
const afterClear = JSON.parse(store.getItem("reasonix.layoutPreferences.v1")!) as { sizes: Record<string, number> };
eq(Object.keys(afterClear.sizes).sort().join(","), "composerHeight", "cleared key removed from storage");
clearLayoutSize("composerHeight");
eq(JSON.stringify(JSON.parse(store.getItem("reasonix.layoutPreferences.v1")!).sizes), "{}", "empty sizes object written after clearing the last key");

dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
