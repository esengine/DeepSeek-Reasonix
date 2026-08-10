// IME guard for the many small Enter-submitting inputs (ask card, command
// palette, rule/note/rename/add). WebKit fires compositionend before the
// confirming Enter keydown, so a shared compositionend timestamp plus the
// 229-keyCode check covers every input without per-input listeners.
import { isImeKeyEvent } from "./composerKeyboard";

let lastGlobalCompositionEndAt = 0;
let globalListenerTarget: Document | null = null;

function onGlobalCompositionEnd(): void {
  lastGlobalCompositionEndAt = Date.now();
}

function ensureGlobalCompositionListener(): void {
  if (typeof document === "undefined") return;
  if (globalListenerTarget === document) return;
  // Tests swap the jsdom document between cases; never leave the listener on
  // a closed document.
  globalListenerTarget?.removeEventListener("compositionend", onGlobalCompositionEnd);
  globalListenerTarget = document;
  document.addEventListener("compositionend", onGlobalCompositionEnd);
}

export function isImeEvent(e: { nativeEvent: { isComposing?: boolean; keyCode?: number } }): boolean {
  ensureGlobalCompositionListener();
  return isImeKeyEvent(e.nativeEvent, false, lastGlobalCompositionEndAt);
}
