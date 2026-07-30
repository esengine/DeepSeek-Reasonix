// Run: tsx src/__tests__/keyboard-shortcuts.test.ts

import { JSDOM } from "jsdom";

import {
  defaultShortcutCombo,
  formatShortcutCombo,
  formatShortcutComboParts,
  isCloseTabShortcut,
  isReservedComposerHistoryShortcut,
  matchesShortcut,
  shortcutAcceptsCombo,
  shortcutConflict,
  shortcutDefinition,
  type ShortcutPlatform,
} from "../lib/keyboardShortcuts";
import { topicShortcutIndexFromEvent, topicShortcutLabel } from "../lib/topicShortcuts";

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

function event(key: string, modifiers: { ctrlKey?: boolean; metaKey?: boolean; altKey?: boolean; shiftKey?: boolean; defaultPrevented?: boolean } = {}) {
  return {
    key,
    ctrlKey: modifiers.ctrlKey ?? false,
    metaKey: modifiers.metaKey ?? false,
    altKey: modifiers.altKey ?? false,
    shiftKey: modifiers.shiftKey ?? false,
    defaultPrevented: modifiers.defaultPrevented ?? false,
    target: null,
  };
}

function eventWithTarget(
  key: string,
  target: EventTarget | null,
  modifiers: { ctrlKey?: boolean; metaKey?: boolean; altKey?: boolean; shiftKey?: boolean; defaultPrevented?: boolean } = {},
) {
  return {
    key,
    ctrlKey: modifiers.ctrlKey ?? false,
    metaKey: modifiers.metaKey ?? false,
    altKey: modifiers.altKey ?? false,
    shiftKey: modifiers.shiftKey ?? false,
    defaultPrevented: modifiers.defaultPrevented ?? false,
    target,
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
eq(JSON.stringify(formatShortcutComboParts(defaultShortcutCombo("settings.open", "darwin"), "darwin")), JSON.stringify(["⌘", ","]), "splits mac settings shortcut for display");
eq(formatShortcutCombo(defaultShortcutCombo("settings.open", "windows"), "windows"), "Ctrl+,", "formats Windows settings shortcut");
eq(JSON.stringify(formatShortcutComboParts(defaultShortcutCombo("settings.open", "windows"), "windows")), JSON.stringify(["Ctrl", ","]), "splits Windows settings shortcut for display");
eq(shortcutConflict("settings.open", defaultShortcutCombo("commandPalette.open", "darwin"), "darwin")?.action, "commandPalette.open", "detects shortcut conflicts");
eq(matchesShortcut(event("l", { metaKey: true }), "selection.addToChat", "darwin"), true, "Cmd+L adds the selection to chat on macOS");
eq(matchesShortcut(event("l", { ctrlKey: true }), "selection.addToChat", "windows"), true, "Ctrl+L adds the selection to chat on Windows");
eq(matchesShortcut(event("l", { ctrlKey: true, metaKey: true }), "selection.addToChat", "darwin"), false, "extra modifiers do not trigger the selection shortcut");
eq(shortcutConflict("app.newSession", { key: "l", ctrl: true }, "linux")?.action, "selection.addToChat", "rebinding another action onto Ctrl+L conflicts with the selection shortcut");
eq(topicShortcutIndexFromEvent(event("1", { metaKey: true }), "darwin"), 0, "Cmd+1 maps to the first topic shortcut on macOS");
eq(topicShortcutIndexFromEvent(event("1", { ctrlKey: true }), "darwin"), null, "Ctrl+1 is not a topic shortcut on macOS");
eq(topicShortcutIndexFromEvent(event("9", { ctrlKey: true }), "windows"), 8, "Ctrl+9 maps to the ninth topic shortcut on Windows");
eq(topicShortcutIndexFromEvent(event("9", { metaKey: true }), "windows"), null, "Meta+9 is not a topic shortcut on Windows");
eq(topicShortcutIndexFromEvent(event("1", { ctrlKey: true, shiftKey: true }), "linux"), null, "topic shortcuts reject extra modifiers");
eq(topicShortcutIndexFromEvent(event("0", { metaKey: true }), "darwin"), null, "Cmd+0 is not a topic shortcut");
eq(topicShortcutIndexFromEvent(event("1", { metaKey: true, defaultPrevented: true }), "darwin"), null, "topic shortcuts yield to already-handled custom shortcuts");
{
  const dom = new JSDOM("<!doctype html><html><body><input /><textarea></textarea></body></html>");
  const previousHTMLElement = globalThis.HTMLElement;
  globalThis.HTMLElement = dom.window.HTMLElement;
  const input = dom.window.document.querySelector("input");
  const textarea = dom.window.document.querySelector("textarea");
  eq(topicShortcutIndexFromEvent(eventWithTarget("1", input, { metaKey: true }), "darwin"), 0, "Cmd+1 still works when an input has focus");
  eq(topicShortcutIndexFromEvent(eventWithTarget("9", textarea, { ctrlKey: true }), "windows"), 8, "Ctrl+9 still works when a textarea has focus");
  globalThis.HTMLElement = previousHTMLElement;
}
eq(topicShortcutLabel(1, "darwin"), "⌘1", "topic badge uses the macOS command glyph");
eq(topicShortcutLabel(1, "windows"), "Ctrl+1", "topic badge uses the Windows control modifier");

for (const platform of ["darwin", "windows", "linux"] satisfies ShortcutPlatform[]) {
  eq(matchesShortcut(event("Enter", { shiftKey: true }), "composer.newline", platform), true, `${platform} newline chord defaults to Shift+Enter`);
  eq(matchesShortcut(event("Enter"), "composer.newline", platform), false, `${platform} plain Enter is not the default newline chord`);
  eq(matchesShortcut(event("Enter", { ctrlKey: true }), "composer.newline", platform), false, `${platform} Ctrl+Enter is not the default newline chord`);
  eq(matchesShortcut(event("Enter"), "composer.send", platform), true, `${platform} send chord defaults to plain Enter`);
  eq(matchesShortcut(event("Enter", { shiftKey: true }), "composer.send", platform), false, `${platform} Shift+Enter is not the default send chord`);
}
eq(shortcutConflict("app.newSession", { key: "Enter", shift: true }, "darwin")?.action, "composer.newline", "rebinding another action onto Shift+Enter conflicts with the newline chord");
eq(shortcutConflict("composer.newline", { key: "Enter" }, "darwin")?.action, "composer.send", "rebinding the newline chord onto plain Enter conflicts with the send chord");
eq(shortcutAcceptsCombo("composer.send", { key: "Enter", ctrl: true }), true, "composer send accepts modified Enter");
eq(shortcutAcceptsCombo("composer.newline", { key: "Enter", alt: true }), true, "composer newline accepts modified Enter");
eq(shortcutAcceptsCombo("composer.send", { key: "s", ctrl: true }), false, "composer send rejects non-Enter keys");
eq(shortcutAcceptsCombo("app.newSession", { key: "s", ctrl: true }), true, "unrestricted shortcuts still accept other keys");
eq(formatShortcutCombo(defaultShortcutCombo("composer.newline", "darwin"), "darwin"), "⇧Enter", "formats the mac newline chord");
eq(formatShortcutCombo(defaultShortcutCombo("composer.newline", "windows"), "windows"), "Shift+Enter", "formats the Windows newline chord");
eq(matchesShortcut(event("z", { metaKey: true }), "composer.undo", "darwin"), true, "Cmd+Z matches composer undo on macOS");
eq(matchesShortcut(event("z", { ctrlKey: true }), "composer.undo", "windows"), true, "Ctrl+Z matches composer undo on Windows");
eq(matchesShortcut(event("z", { metaKey: true, shiftKey: true }), "composer.redo", "darwin"), true, "Cmd+Shift+Z matches composer redo on macOS");
eq(matchesShortcut(event("z", { ctrlKey: true, shiftKey: true }), "composer.redo", "linux"), true, "Ctrl+Shift+Z matches composer redo on Linux");
eq(matchesShortcut(event("y", { ctrlKey: true }), "composer.redo", "windows"), false, "Ctrl+Y remains a Composer-level conditional fallback");
eq(isReservedComposerHistoryShortcut(event("z", { metaKey: true }), "darwin"), true, "macOS undo is reserved while editing");
eq(isReservedComposerHistoryShortcut(event("z", { ctrlKey: true, shiftKey: true }), "windows"), true, "Windows redo is reserved while editing");
eq(isReservedComposerHistoryShortcut(event("y", { ctrlKey: true }), "windows"), false, "conditional Ctrl+Y redo does not reserve a global shortcut");
eq(shortcutDefinition("composer.undo").configurable, false, "composer undo is visible but locked to the native editing chord");
eq(shortcutDefinition("composer.redo").configurable, false, "composer redo is visible but locked to the native editing chord");
eq(shortcutConflict("app.newSession", { key: "z", ctrl: true }, "windows")?.action, "composer.undo", "custom shortcuts cannot take the Windows undo chord");
eq(shortcutConflict("app.newSession", { key: "z", ctrl: true, shift: true }, "windows")?.action, "composer.redo", "custom shortcuts cannot take the Windows redo chord");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
