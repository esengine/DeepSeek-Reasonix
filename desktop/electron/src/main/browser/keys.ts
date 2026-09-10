import type { KeyboardInputEvent } from "electron";

type Modifier = "shift" | "control" | "alt" | "meta";

export interface Chord {
  keyCode: string;
  modifiers: Modifier[];
  char: string | null;
}

const MODIFIERS: Record<string, Modifier> = {
  shift: "shift",
  control: "control",
  ctrl: "control",
  alt: "alt",
  option: "alt",
  meta: "meta",
  command: "meta",
  cmd: "meta",
  super: "meta",
};

// DOM key names to Electron accelerator key codes where they differ.
const KEY_CODES: Record<string, string> = {
  enter: "Return",
  return: "Return",
  escape: "Escape",
  esc: "Escape",
  arrowup: "Up",
  arrowdown: "Down",
  arrowleft: "Left",
  arrowright: "Right",
  up: "Up",
  down: "Down",
  left: "Left",
  right: "Right",
  space: "Space",
  " ": "Space",
  tab: "Tab",
  backspace: "Backspace",
  delete: "Delete",
  del: "Delete",
  insert: "Insert",
  home: "Home",
  end: "End",
  pageup: "PageUp",
  pagedown: "PageDown",
  plus: "Plus",
  "+": "Plus",
};

const CHAR_KEYS: Record<string, string> = { Return: "\r", Tab: "\t", Space: " " };

export function parseChord(input: string): Chord {
  const raw = input.trim();
  if (raw === "") throw new Error("empty key chord");
  const parts = raw.split("+").map((part) => part.trim());
  // A literal "+" key ("Shift++", "+") ends the split with an empty element.
  if (parts.length > 1 && parts[parts.length - 1] === "") {
    parts.pop();
    if (parts[parts.length - 1] !== "") throw new Error(`malformed key chord "${input}"`);
    parts[parts.length - 1] = "+";
  }
  const modifiers: Modifier[] = [];
  let key = "";
  for (const part of parts) {
    if (part === "") throw new Error(`malformed key chord "${input}"`);
    const modifier = MODIFIERS[part.toLowerCase()];
    if (modifier && !modifiers.includes(modifier)) {
      modifiers.push(modifier);
      continue;
    }
    if (key !== "") throw new Error(`key chord "${input}" names more than one key`);
    key = part;
  }
  if (key === "") throw new Error(`key chord "${input}" has no key`);
  const keyCode = keyCodeFor(key);
  return { keyCode, modifiers, char: charFor(key, keyCode, modifiers) };
}

function keyCodeFor(key: string): string {
  const mapped = KEY_CODES[key.toLowerCase()];
  if (mapped) return mapped;
  if (/^f([1-9]|1\d|2[0-4])$/i.test(key)) return key.toUpperCase();
  if (key.length === 1) return key;
  return key.charAt(0).toUpperCase() + key.slice(1);
}

function charFor(key: string, keyCode: string, modifiers: Modifier[]): string | null {
  if (modifiers.includes("control") || modifiers.includes("meta") || modifiers.includes("alt")) return null;
  const special = CHAR_KEYS[keyCode];
  if (special) return special;
  if (key.length !== 1) return null;
  return modifiers.includes("shift") ? key.toUpperCase() : key;
}

export function chordEvents(chord: Chord): KeyboardInputEvent[] {
  const modifiers = chord.modifiers.length ? chord.modifiers : undefined;
  const events: KeyboardInputEvent[] = [{ type: "keyDown", keyCode: chord.keyCode, modifiers }];
  if (chord.char !== null) events.push({ type: "char", keyCode: chord.char, modifiers });
  events.push({ type: "keyUp", keyCode: chord.keyCode, modifiers });
  return events;
}

// "Control+a Enter" presses two chords in sequence; a chord never contains
// whitespace, so splitting on it is unambiguous.
export function parseKeySequence(keys: string): Chord[] {
  return keys.split(/\s+/).filter((part) => part !== "").map(parseChord);
}
