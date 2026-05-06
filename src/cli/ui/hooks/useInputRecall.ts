import { useCallback, useRef } from "react";

export interface UseInputRecallResult {
  recallPrev: () => void;
  recallNext: () => void;
  pushHistory: (text: string) => void;
  /** Reset cursor to the "fresh input" position — call after a successful submit. */
  resetCursor: () => void;
}

/** Bash-style ↑/↓ recall over a turn-local prompt history. Cursor is `useRef` so toggles don't re-render. */
export function useInputRecall(setInput: (s: string) => void): UseInputRecallResult {
  const promptHistory = useRef<string[]>([]);
  const historyCursor = useRef<number>(-1);

  const recallPrev = useCallback(() => {
    const hist = promptHistory.current;
    if (hist.length === 0) return;
    const nextCursor = Math.min(historyCursor.current + 1, hist.length - 1);
    historyCursor.current = nextCursor;
    setInput(hist[hist.length - 1 - nextCursor] ?? "");
  }, [setInput]);

  const recallNext = useCallback(() => {
    if (historyCursor.current < 0) return;
    const hist = promptHistory.current;
    const nextCursor = historyCursor.current - 1;
    historyCursor.current = nextCursor;
    setInput(nextCursor < 0 ? "" : (hist[hist.length - 1 - nextCursor] ?? ""));
  }, [setInput]);

  const pushHistory = useCallback((text: string) => {
    promptHistory.current.push(text);
  }, []);

  const resetCursor = useCallback(() => {
    historyCursor.current = -1;
  }, []);

  return { recallPrev, recallNext, pushHistory, resetCursor };
}
