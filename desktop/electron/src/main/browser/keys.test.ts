import assert from "node:assert/strict";
import { test } from "node:test";
import { chordEvents, parseChord, parseKeySequence } from "./keys.js";

test("chords map DOM key names and modifiers onto Electron key codes", () => {
  assert.deepEqual(parseChord("Control+Shift+Enter"), { keyCode: "Return", modifiers: ["control", "shift"], char: null });
  assert.deepEqual(parseChord("Meta+a"), { keyCode: "a", modifiers: ["meta"], char: null });
  assert.deepEqual(parseChord("Escape"), { keyCode: "Escape", modifiers: [], char: null });
  assert.deepEqual(parseChord("ArrowDown"), { keyCode: "Down", modifiers: [], char: null });
  assert.deepEqual(parseChord("Enter"), { keyCode: "Return", modifiers: [], char: "\r" });
  assert.deepEqual(parseChord("Tab"), { keyCode: "Tab", modifiers: [], char: "\t" });
  assert.deepEqual(parseChord("a"), { keyCode: "a", modifiers: [], char: "a" });
  assert.deepEqual(parseChord("Shift+a"), { keyCode: "a", modifiers: ["shift"], char: "A" });
  assert.deepEqual(parseChord("ctrl+alt+Delete"), { keyCode: "Delete", modifiers: ["control", "alt"], char: null });
  assert.deepEqual(parseChord("Shift++"), { keyCode: "Plus", modifiers: ["shift"], char: "+" });
  assert.deepEqual(parseChord("+"), { keyCode: "Plus", modifiers: [], char: "+" });
  assert.deepEqual(parseChord("F5"), { keyCode: "F5", modifiers: [], char: null });
  assert.deepEqual(parseChord("pageDown"), { keyCode: "PageDown", modifiers: [], char: null });
  assert.throws(() => parseChord(""), /empty/);
  assert.throws(() => parseChord("Control+"), /malformed/);
  assert.throws(() => parseChord("a+b"), /more than one key/);
  assert.throws(() => parseChord("Control+Shift"), /no key/);
});

test("key sequences are space separated and expand to keyDown/char/keyUp", () => {
  const chords = parseKeySequence("Control+a  Backspace Enter");
  assert.deepEqual(chords.map((chord) => chord.keyCode), ["a", "Backspace", "Return"]);
  assert.deepEqual(chordEvents(chords[0]), [
    { type: "keyDown", keyCode: "a", modifiers: ["control"] },
    { type: "keyUp", keyCode: "a", modifiers: ["control"] },
  ]);
  assert.deepEqual(chordEvents(chords[2]), [
    { type: "keyDown", keyCode: "Return", modifiers: undefined },
    { type: "char", keyCode: "\r", modifiers: undefined },
    { type: "keyUp", keyCode: "Return", modifiers: undefined },
  ]);
  assert.deepEqual(parseKeySequence("   "), []);
});
