// Run: tsx src/__tests__/global-hotkey.test.ts
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const dir = dirname(fileURLToPath(import.meta.url));
const source = (path: string) => readFileSync(resolve(dir, path), "utf8");
const shortcuts = source("../lib/keyboardShortcuts.ts");
const settings = source("../components/SettingsPanel.tsx");
const bridge = source("../lib/bridge.ts");
const app = source("../App.tsx");

let failed = 0;
function ok(value: unknown, label: string) {
  if (value) process.stdout.write(`  PASS  ${label}\n`);
  else {
    failed += 1;
    process.stdout.write(`  FAIL  ${label}\n`);
  }
}

console.log("\nglobal hotkey (#7623)");
ok(/action: "app\.toggleWindow"/.test(shortcuts) && /osLevel: true/.test(shortcuts), "defines OS-level toggle action");
ok(/serializeShortcutCombo/.test(shortcuts) && /parseShortcutComboBinding/.test(shortcuts), "serializes bindings for desktop config");
ok(/SetDesktopGlobalHotkey/.test(bridge), "bridge exposes SetDesktopGlobalHotkey");
ok(/definition\.osLevel/.test(settings) && /SetDesktopGlobalHotkey/.test(settings), "settings persists OS hotkey via Go");
ok(/settings\.shortcutsOsConflict/.test(settings), "settings surfaces OS registration conflicts");
ok(/desktop:global-hotkey-error/.test(settings), "settings listens for startup OS hotkey errors");
ok(/globalHotkeyError/.test(settings), "settings loads persisted OS hotkey registration errors");
ok(/desktop:global-hotkey-error/.test(app), "app toasts OS hotkey registration errors");
ok(/SetDesktopGlobalHotkey\("off"\)/.test(settings) && /settings\.shortcutsDisable/.test(settings), "settings can disable the OS hotkey");
ok(/rawHotkey === "off"/.test(settings) && /settings\.shortcutsDisabled/.test(settings), "disabled hotkey displays as Off instead of the default");

if (failed) process.exit(1);
console.log("global hotkey tests passed");
