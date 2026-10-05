// Which modifier this platform spells its shortcuts with.
//
// Read from the agent string, not from the shell's `data-platform`: that one is
// written after a promise resolves, and a label rendered before it would keep
// saying ⌘ on Windows for the rest of the session. The browser build has no
// shell to ask at all.
const mac = /mac|iphone|ipad|ipod/i.test(
  (navigator as { userAgentData?: { platform?: string } }).userAgentData?.platform ?? navigator.userAgent,
);

// chord renders one shortcut the way its platform writes it. macOS sets the
// glyph tight against the key; every other platform spells the word and needs
// the space to stay readable.
export function chord(key: string): string {
  return mac ? `⌘${key}` : `Ctrl ${key}`;
}

// asksDelete reads the key that deletes whatever holds focus: Delete everywhere,
// and ⌘⌫ on macOS, whose main key sends Backspace.
export function asksDelete(ev: { key: string; metaKey: boolean; ctrlKey: boolean; altKey: boolean; shiftKey: boolean }): boolean {
  if (ev.ctrlKey || ev.altKey || ev.shiftKey) return false;
  if (ev.key === "Delete") return !ev.metaKey;
  return mac && ev.metaKey && ev.key === "Backspace";
}

// ariaChord is chord for aria-keyshortcuts, which names modifiers in words.
export function ariaChord(key: string): string {
  return `${mac ? "Meta" : "Control"}+${key.toUpperCase()}`;
}

/** Whether a press is the platform's modifier chord for `key`. A letter is also
 *  read from the key's position when the layout did not report a Latin letter
 *  (Cyrillic, or an input method that answers "Process"), and AltGr is not the
 *  modifier: on many layouts it types a character. */
export function pressedChord(
  ev: { key: string; code: string; metaKey: boolean; ctrlKey: boolean; altKey: boolean; shiftKey: boolean },
  key: string,
  shift = false,
): boolean {
  const mod = mac ? ev.metaKey && !ev.ctrlKey : ev.ctrlKey && !ev.metaKey;
  if (!mod || ev.altKey || ev.shiftKey !== shift) return false;
  if (ev.key.toLowerCase() === key.toLowerCase()) return true;
  const letter = /^[a-z]$/i.test(key);
  return letter && !/^[\x20-\x7e]$/.test(ev.key) && ev.code === `Key${key.toUpperCase()}`;
}

const TEXT_FIELD = "textarea, select, input:not([type=checkbox], [type=radio], [type=button], [type=submit], [type=reset], [type=range], [type=color], [type=file])";

/** Whether focus sits in a field that keeps its own keys. A field that hands
 *  the window's chords through says so with `data-window-keys`. */
export function typingElsewhere(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const field = target.isContentEditable || target.matches(TEXT_FIELD);
  return field && !target.closest("[data-window-keys]");
}
