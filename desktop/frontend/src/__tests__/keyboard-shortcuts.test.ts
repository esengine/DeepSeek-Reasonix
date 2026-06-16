// Run: tsx src/__tests__/keyboard-shortcuts.test.ts

import {
  defaultShortcutCombo,
  formatShortcutCombo,
  isCloseTabShortcut,
  matchesShortcut,
  shortcutConflict,
  type ShortcutPlatform,
} from "../lib/keyboardShortcuts";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

function event(key: string, modifiers: { ctrlKey?: boolean; metaKey?: boolean } = {}) {
  return {
    key,
    ctrlKey: modifiers.ctrlKey ?? false,
    metaKey: modifiers.metaKey ?? false,
    altKey: false,
    shiftKey: false,
  };
}

console.log("\nkeyboard shortcuts");

eq(isCloseTabShortcut(event("w", { metaKey: true }), "darwin"), true, "Cmd+W closes tabs on macOS");
eq(isCloseTabShortcut(event("W", { metaKey: true }), "darwin"), true, "Cmd+Shift+W key value still matches W on macOS");
eq(isCloseTabShortcut(event("w", { ctrlKey: true }), "darwin"), false, "Control+W does not close tabs on macOS");
eq(isCloseTabShortcut(event("w", { metaKey: true }), "windows"), false, "Meta+W does not close tabs on Windows");
eq(isCloseTabShortcut(event("w", { ctrlKey: true }), "windows"), true, "Ctrl+W closes tabs on Windows");
eq(isCloseTabShortcut(event("w", { ctrlKey: true }), "linux"), true, "Ctrl+W closes tabs on Linux");

for (const platform of ["darwin", "windows", "linux"] satisfies ShortcutPlatform[]) {
  eq(isCloseTabShortcut(event("k", { ctrlKey: true, metaKey: true }), platform), false, `${platform} ignores non-W keys`);
eq(isCloseTabShortcut(event("w"), platform), false, `${platform} requires the platform modifier`);
}

eq(matchesShortcut(event("k", { metaKey: true }), "commandPalette.open", "darwin"), true, "Cmd+K opens the palette on macOS");
eq(matchesShortcut(event("k", { ctrlKey: true }), "commandPalette.open", "windows"), true, "Ctrl+K opens the palette on Windows");
eq(matchesShortcut({ key: "?", shiftKey: true }, "shortcuts.show", "darwin"), true, "? opens shortcut help");
eq(matchesShortcut({ key: "+", metaKey: true, shiftKey: true }, "textSize.increase", "darwin"), true, "Cmd+Plus still increases text size");
eq(formatShortcutCombo(defaultShortcutCombo("settings.open", "darwin"), "darwin"), "⌘,", "formats mac settings shortcut");
eq(formatShortcutCombo(defaultShortcutCombo("settings.open", "windows"), "windows"), "Ctrl+,", "formats Windows settings shortcut");
eq(shortcutConflict("settings.open", defaultShortcutCombo("commandPalette.open", "darwin"), "darwin")?.action, "commandPalette.open", "detects shortcut conflicts");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
