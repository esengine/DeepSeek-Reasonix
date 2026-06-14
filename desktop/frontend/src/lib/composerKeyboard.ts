export type PromptHistoryDirection = "up" | "down";

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
