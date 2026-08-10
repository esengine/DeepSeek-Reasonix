// Run: tsx src/__tests__/terminal-clear-shortcut.test.ts
//
// Contract test: the terminal's clear-screen-and-scrollback action lives in
// the shared shortcut registry ("terminal.clear", Cmd+K / Ctrl+K like VSCode's
// integrated terminal) and TerminalView resolves it through matchesShortcut, so
// the shortcut settings panel, conflict detection, and custom bindings all
// apply to it automatically.

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

let passed = 0;
let failed = 0;

function ok(condition: boolean, label: string) {
  if (condition) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

const testDir = dirname(fileURLToPath(import.meta.url));
const terminalViewSource = readFileSync(resolve(testDir, "../components/TerminalView.tsx"), "utf8");
const keyboardSource = readFileSync(resolve(testDir, "../lib/keyboardShortcuts.ts"), "utf8");

console.log("\nterminal clear shortcut");

// The action must be a first-class registry citizen so the settings panel and
// the shortcuts cheatsheet render it and users can rebind it.
ok(/action: "terminal\.clear"/.test(keyboardSource), "terminal.clear is a registered shortcut action");
ok(/labelKey: "shortcuts\.action\.terminalClear"/.test(keyboardSource), "terminal.clear has a settings label key");
ok(/descriptionKey: "shortcuts\.desc\.terminalClear"/.test(keyboardSource), "terminal.clear has a settings description key");
ok(/defaults: modCombo\("k"\)/.test(keyboardSource), "terminal.clear defaults to Cmd+K on macOS, Ctrl+K on Windows/Linux");
ok(/allowInEditable: true/.test(keyboardSource), "terminal.clear may fire from the terminal's editable surface");
ok(/action: "commandPalette\.open"[\s\S]*?defaults: allPlatforms\(\{ key: "F1" \}\)/.test(keyboardSource), "command palette is F1 on every platform (VSCode convention)");

// TerminalView must resolve the chord through the shared matcher, not a
// hardcoded key check, so custom bindings and conflicts are respected.
ok(/matchesShortcut\(event, "terminal\.clear", detectShortcutPlatform\(\)\)/.test(terminalViewSource), "TerminalView resolves the chord via the shared shortcut matcher");
ok(/host\.closest\("\.terminal-panel"\)/.test(terminalViewSource) && /panel\.contains\(event\.target\)/.test(terminalViewSource), "handler only fires while focus is inside the terminal panel");
ok(/event\.preventDefault\(\)/.test(terminalViewSource), "handler stops the browser from processing the chord");
ok(/event\.stopImmediatePropagation\(\)/.test(terminalViewSource), "handler suppresses other global shortcut handlers");
ok(/terminal\.clear\(\)/.test(terminalViewSource), "handler clears the full buffer (screen + scrollback)");
ok(/addEventListener\("keydown", clearTerminal, \{ capture: true \}\)/.test(terminalViewSource), "handler registers in the capture phase before xterm");
ok(/removeEventListener\("keydown", clearTerminal, \{ capture: true \}\)/.test(terminalViewSource), "handler unregisters when the terminal unmounts");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
