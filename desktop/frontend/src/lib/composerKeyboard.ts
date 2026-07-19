import { loadCustomShortcuts, matchesShortcut, type ShortcutPlatform } from "./keyboardShortcuts";
import { replaceInvocationTextRange, type ComposerInvocation } from "./invocationDisplay";

export type PromptHistoryDirection = "up" | "down";

export interface ComposerEnterKeyLike {
  key: string;
  ctrlKey?: boolean;
  metaKey?: boolean;
  altKey?: boolean;
  shiftKey?: boolean;
}

// "newline-native" keeps the browser's own line-break insertion (only the
// plain Shift+Enter chord, today's proven path); "newline-insert" means the
// chord has no native insertion (e.g. Ctrl+Enter) so the composer must insert
// the "\n" itself. "none" swallows the chord: an Enter combo matching neither
// the send nor the newline shortcut must not fall through to the input, or
// native insertion would resurrect the unbound default.
export type ComposerEnterAction = "send" | "newline-native" | "newline-insert" | "none";

export function composerEnterAction(event: ComposerEnterKeyLike, platform: ShortcutPlatform): ComposerEnterAction | null {
  if (event.key !== "Enter") return null;
  if (matchesShortcut(event, "composer.newline", platform)) {
    const nativeInserts = Boolean(event.shiftKey) && !event.ctrlKey && !event.metaKey && !event.altKey;
    return nativeInserts ? "newline-native" : "newline-insert";
  }
  if (matchesShortcut(event, "composer.send", platform)) return "send";
  // Before composer shortcuts were configurable, every non-Shift Enter chord
  // submitted. Preserve that default compatibility, but stop applying it as
  // soon as the user explicitly chooses a send chord.
  if (!loadCustomShortcuts()["composer.send"] && !event.shiftKey) return "send";
  // Plain Enter bound to neither chord (send moved to e.g. Ctrl+Enter) still
  // breaks the line — the WeChat/DingTalk-style layout users expect there.
  if (!event.ctrlKey && !event.metaKey && !event.altKey && !event.shiftKey) return "newline-insert";
  return "none";
}

export interface ComposerSelectionLike {
  start: number;
  end: number;
  afterInvocationId?: string;
}

export function insertComposerNewline(
  text: string,
  invocations: ComposerInvocation[],
  selection: ComposerSelectionLike,
): { text: string; invocations: ComposerInvocation[] } {
  return replaceInvocationTextRange(
    text,
    invocations,
    selection.start,
    selection.end,
    "\n",
    selection.afterInvocationId,
  );
}

export interface PromptHistoryKeyLike {
  key?: string;
  code?: string;
  keyCode?: number;
  which?: number;
  getModifierState?: (keyArg: string) => boolean;
}

export interface PromptHistoryEligibility {
  direction: PromptHistoryDirection | null;
  menuOpen: boolean;
  composing: boolean;
  altKey: boolean;
  ctrlKey: boolean;
  metaKey: boolean;
  shiftKey: boolean;
  fnKey: boolean;
  value: string;
  selectionStart: number | null;
  selectionEnd: number | null;
  historyIndex: number;
}

export function isFnKeyEvent(event: PromptHistoryKeyLike): boolean {
  return event.key === "Fn" || event.code === "Fn" || event.getModifierState?.("Fn") === true;
}

export function promptHistoryDirectionFromEvent(event: PromptHistoryKeyLike): PromptHistoryDirection | null {
  const key = event.key ?? "";
  const code = event.code ?? "";
  const keyCode = event.keyCode ?? event.which ?? 0;
  const useLegacyCode = key === "" || key === "Unidentified";

  if (key === "ArrowUp" || key === "Up" || code === "ArrowUp" || (useLegacyCode && keyCode === 38)) {
    return "up";
  }
  if (key === "ArrowDown" || key === "Down" || code === "ArrowDown" || (useLegacyCode && keyCode === 40)) {
    return "down";
  }
  return null;
}

export function canUsePromptHistory(options: PromptHistoryEligibility): boolean {
  const {
    direction,
    menuOpen,
    composing,
    altKey,
    ctrlKey,
    metaKey,
    shiftKey,
    fnKey,
    value,
    selectionStart,
    selectionEnd,
    historyIndex,
  } = options;
  if (!direction || menuOpen || composing || fnKey || altKey || ctrlKey || metaKey || shiftKey) return false;
  if (selectionStart === null || selectionEnd === null) return false;
  if (selectionStart !== selectionEnd) return false;

  if (direction === "up") {
    return historyIndex >= 0 || selectionStart === 0;
  }

  return historyIndex >= 0 && selectionEnd === value.length;
}
