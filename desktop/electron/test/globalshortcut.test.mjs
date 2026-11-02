import test from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
const require = createRequire(import.meta.url);
const { createGlobalShortcut, registerGlobalShortcutIPC, PREF_KEY } = require("../src/globalshortcut");
const { savePrefs, loadPrefs, registerPrefs } = require("../src/prefs");

function fixture(binding = "") {
  const registered = new Map();
  const saved = [];
  const win = { visible: true, focused: true, minimized: false, destroyed: false,
    isVisible() { return this.visible; }, isFocused() { return this.focused; },
    isMinimized() { return this.minimized; }, isDestroyed() { return this.destroyed; },
    hide() { this.visible = false; this.focused = false; },
  };
  let shown = 0;
  let failSave = false;
  const api = {
    register(key, callback) {
      if (key === "Control+invalid") throw new TypeError("invalid accelerator");
      if (key === "Control+occupied") return false;
      registered.set(key, callback); return true;
    },
    unregister(key) { registered.delete(key); },
  };
  const shortcut = createGlobalShortcut({ globalShortcut: api, binding,
    save(value) { if (failSave) throw new Error("disk full"); saved.push(value); },
    windowOf: () => win, showWindow() { shown++; win.visible = true; win.focused = true; win.minimized = false; },
  });
  return { shortcut, registered, saved, win, get shown() { return shown; }, failSave() { failSave = true; } };
}

test("starts disabled without claiming an OS binding and restores a saved binding before any renderer", () => {
  const off = fixture();
  assert.deepEqual(off.shortcut.start(), { binding: "", status: "disabled" });
  assert.equal(off.registered.size, 0);
  const on = fixture("Control+Space");
  assert.deepEqual(on.shortcut.start(), { binding: "Control+Space", status: "registered" });
  assert.equal(on.registered.size, 1);
  assert.deepEqual(on.saved, []);
});

test("bare keys and Shift-only chords cannot claim machine-wide input through apply or restored preferences", () => {
  for (const binding of ["C", "Space", "F9", "Plus", " Shift ", "Shift+A", "Shift+Space"]) {
    const restored = fixture(binding);
    assert.deepEqual(restored.shortcut.start(), { binding, status: "invalid" });
    assert.equal(restored.registered.size, 0);
    const f = fixture("Control+Space"); f.shortcut.start();
    assert.deepEqual(f.shortcut.apply(binding), { binding: "Control+Space", status: "registered", error: "invalid" });
    assert.deepEqual([...f.registered.keys()], ["Control+Space"]);
    assert.deepEqual(f.saved, []);
  }
  for (const modifier of ["CommandOrControl", "CmdOrCtrl", "Command", "Cmd", "Control", "Ctrl", "Alt", "Option", "AltGr", "Super", "Meta"]) {
    const f = fixture();
    assert.equal(f.shortcut.apply(`${modifier}+Space`).status, "registered");
    assert.equal(f.shortcut.apply(`${modifier}+Shift+Space`).status, "registered");
  }
});

test("focused hides; unfocused, hidden and minimized show through the existing path", () => {
  for (const changed of [{}, { focused: false }, { visible: false, focused: false }, { minimized: true }]) {
    const f = fixture("Control+Space"); f.shortcut.start(); Object.assign(f.win, changed);
    f.registered.get("Control+Space")();
    assert.equal(f.shown, Object.keys(changed).length ? 1 : 0);
    assert.equal(f.win.visible, Object.keys(changed).length > 0);
  }
});

test("failed rebind preserves both registration and saved choice, successful replacement removes only the old binding", () => {
  const f = fixture("Control+Space"); f.shortcut.start();
  for (const [binding, error] of [["Control+invalid", "invalid"], ["Control+occupied", "unavailable"], [42, "invalid"]]) {
    assert.deepEqual(f.shortcut.apply(binding), { binding: "Control+Space", status: "registered", error });
    assert.deepEqual([...f.registered.keys()], ["Control+Space"]);
    assert.deepEqual(f.saved, []);
  }
  assert.deepEqual(f.shortcut.apply("  Control+Shift+Space  "), { binding: "Control+Shift+Space", status: "registered" });
  assert.deepEqual([...f.registered.keys()], ["Control+Shift+Space"]);
  assert.deepEqual(f.saved, ["Control+Shift+Space"]);
  f.shortcut.apply("");
  assert.equal(f.registered.size, 0);
  assert.deepEqual(f.saved, ["Control+Shift+Space", ""]);
});

test("disk failure rolls back the prospective registration, including disabling", () => {
  const f = fixture("Control+Space"); f.shortcut.start(); f.failSave();
  for (const value of ["Control+Shift+Space", ""]) {
    assert.deepEqual(f.shortcut.apply(value), { binding: "Control+Space", status: "registered", error: "save" });
    assert.deepEqual([...f.registered.keys()], ["Control+Space"]);
  }
});

test("startup failure is honest and may be replaced or disabled", () => {
  for (const [binding, status] of [["Control+invalid", "invalid"], ["Control+occupied", "unavailable"]]) {
    const f = fixture(binding);
    assert.deepEqual(f.shortcut.start(), { binding, status });
    assert.equal(f.registered.size, 0);
    assert.deepEqual(f.shortcut.apply(""), { binding: "", status: "disabled" });
  }
});

test("quit unregisters its binding, suppresses queued presses and refuses new registration", () => {
  const f = fixture("Control+Space"); f.shortcut.start();
  const queued = f.registered.get("Control+Space");
  f.shortcut.close(); f.shortcut.close(); queued();
  assert.equal(f.registered.size, 0); assert.equal(f.shown, 0); assert.equal(f.win.visible, true);
  assert.equal(f.shortcut.apply("Control+Shift+Space").error, "quitting");
});

test("only the authorized Studio main frame can read or immediately apply a shortcut", () => {
  const f = fixture(); f.shortcut.start();
  const handlers = new Map(); const sender = { mainFrame: {} };
  registerGlobalShortcutIPC({ handle: (key, fn) => handlers.set(key, fn) }, event => event.sender === sender, () => f.shortcut);
  for (const event of [{ sender: { mainFrame: {} }, senderFrame: {} }, { sender, senderFrame: {} }]) {
    assert.equal(handlers.get("shell:global-shortcut")(event), null);
    assert.equal(handlers.get("shell:set-global-shortcut")(event, "Control+Space"), null);
  }
  assert.equal(f.registered.size, 0);
  const event = { sender, senderFrame: sender.mainFrame };
  assert.equal(handlers.get("shell:set-global-shortcut")(event, "Control+Space").status, "registered");
  assert.equal(handlers.get("shell:global-shortcut")(event).binding, "Control+Space");
  assert.deepEqual(handlers.get("shell:set-global-shortcut")(event, " ".repeat(128) + "Control+Space"), { binding: "Control+Space", status: "registered", error: "invalid" });
  assert.deepEqual([...f.registered.keys()], ["Control+Space"]);
  assert.deepEqual(f.saved, ["Control+Space"]);
});

test("page preference flushing cannot overwrite the shell-owned saved binding", t => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "rx-shortcut-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const file = path.join(dir, "window-prefs.json");
  savePrefs(file, { [PREF_KEY]: "Control+Space", "rx-lang": "en" });
  const handlers = {};
  registerPrefs({ on: (key, fn) => handlers[key] = fn }, () => file, () => true);
  const event = {};
  handlers["prefs:load"](event);
  assert.deepEqual(event.returnValue, { "rx-lang": "en" });
  handlers["prefs:save"](event, { "rx-lang": "zh", [PREF_KEY]: "occupied" });
  assert.deepEqual(loadPrefs(file), { "rx-lang": "zh", [PREF_KEY]: "Control+Space" });
});

test("oversized restored preferences never reach native registration", () => {
  const binding = "Control+" + "x".repeat(128);
  const f = fixture(binding);
  assert.deepEqual(f.shortcut.start(), { binding, status: "invalid" });
  assert.equal(f.registered.size, 0);
  assert.deepEqual(f.saved, []);
  assert.deepEqual(f.shortcut.apply(""), { binding: "", status: "disabled" });
});
