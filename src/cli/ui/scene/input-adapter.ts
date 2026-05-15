import type { KeystrokeHandler, KeystrokeReader } from "../keystroke-context.js";
import { type KeyEvent, sanitizePasteText } from "../stdin-reader.js";
import {
  type KeyInputEvent,
  type MouseInputEvent,
  type PasteInputEvent,
  type SpawnInputSourceOptions,
  spawnInputSource,
} from "./input-source.js";

export type RustKeystrokeReader = KeystrokeReader & {
  /** Send SIGINT to the input child and await exit. */
  close(): Promise<number | null>;
  /** Await the child's natural exit without signaling. */
  wait(): Promise<number | null>;
};

export function createRustKeystrokeReader(opts: SpawnInputSourceOptions = {}): RustKeystrokeReader {
  const source = spawnInputSource(opts);
  const handlers = new Set<KeystrokeHandler>();

  source.onKey((rust) => {
    const ev = translate(rust);
    if (!ev) return;
    for (const h of handlers) h(ev);
  });
  source.onPaste((rust) => {
    const ev = translatePaste(rust);
    for (const h of handlers) h(ev);
  });
  source.onMouse((rust) => {
    const ev = translateMouse(rust);
    for (const h of handlers) h(ev);
  });

  return {
    start() {
      // The child is already running from spawn — no-op so KeystrokeProvider's
      // existing call site stays unchanged.
    },
    subscribe(handler) {
      handlers.add(handler);
      return () => {
        handlers.delete(handler);
      };
    },
    close(): Promise<number | null> {
      return source.close();
    },
    wait(): Promise<number | null> {
      return source.wait();
    },
  };
}

export function translate(rust: KeyInputEvent): KeyEvent | null {
  const mods = new Set(rust.modifiers ?? []);
  const ctrl = mods.has("ctrl");
  const shift = mods.has("shift");
  const alt = mods.has("alt");
  switch (rust.code) {
    case "Char": {
      if (rust.char === undefined) return null;
      const ev: KeyEvent = { input: rust.char };
      if (ctrl) ev.ctrl = true;
      if (alt) ev.meta = true;
      if (shift) ev.shift = true;
      return ev;
    }
    case "Enter":
      return withMods({ input: "", return: true }, ctrl, shift, alt);
    case "Esc":
      return { input: "", escape: true };
    case "Up":
      return { input: "", upArrow: true };
    case "Down":
      return { input: "", downArrow: true };
    case "Left":
      return { input: "", leftArrow: true };
    case "Right":
      return { input: "", rightArrow: true };
    case "Backspace":
      return { input: "", backspace: true };
    case "Tab":
      return withMods({ input: "", tab: true }, ctrl, shift, alt);
    case "BackTab":
      return { input: "", tab: true, shift: true };
    case "Home":
      return { input: "", home: true };
    case "End":
      return { input: "", end: true };
    case "PageUp":
      return { input: "", pageUp: true };
    case "PageDown":
      return { input: "", pageDown: true };
    case "Delete":
      return { input: "", delete: true };
    default:
      // F1-F12 and anything unrecognized — KeyEvent has no slot for them.
      // Better to drop than to fake-fill a different field.
      return null;
  }
}

function withMods(ev: KeyEvent, ctrl: boolean, shift: boolean, alt: boolean): KeyEvent {
  if (ctrl) ev.ctrl = true;
  if (shift) ev.shift = true;
  if (alt) ev.meta = true;
  return ev;
}

export function translatePaste(rust: PasteInputEvent): KeyEvent {
  return { input: sanitizePasteText(rust.text), paste: true };
}

export function translateMouse(rust: MouseInputEvent): KeyEvent {
  const ev: KeyEvent = { input: "", mouseRow: rust.row, mouseCol: rust.col };
  switch (rust.kind) {
    case "click":
      ev.mouseClick = true;
      break;
    case "drag":
      ev.mouseDrag = true;
      break;
    case "release":
      ev.mouseRelease = true;
      break;
    case "scroll-up":
      ev.mouseScrollUp = true;
      break;
    case "scroll-down":
      ev.mouseScrollDown = true;
      break;
  }
  return ev;
}
