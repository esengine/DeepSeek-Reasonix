import { useCallback, useRef, useState } from "react";

export interface UseInputRecallResult {
  recallPrev: () => void;
  recallNext: () => void;
  pushHistory: (text: string) => void;
  /** Reset cursor to the "fresh input" position — call after a successful submit. */
  resetCursor: () => void;
  /** Newest-last snapshot. */
  history: readonly string[];
  /** True when viewing a historical input (cursor > -1). */
  isHistoryMode: boolean;
}

const HISTORY_MAX = 100;

export function useInputRecall(setInput: (s: string) => void): UseInputRecallResult {
  const [history, setHistory] = useState<readonly string[]>([]);
  const historyCursor = useRef<number>(-1);
  const [isHistoryMode, setIsHistoryMode] = useState(false);

  const recallPrev = useCallback(() => {
    if (history.length === 0) return;
    const nextCursor = Math.min(historyCursor.current + 1, history.length - 1);
    historyCursor.current = nextCursor;
    setIsHistoryMode(nextCursor >= 0);
    setInput(history[history.length - 1 - nextCursor] ?? "");
  }, [setInput, history]);

  const recallNext = useCallback(() => {
    if (historyCursor.current < 0) return;
    const nextCursor = historyCursor.current - 1;
    historyCursor.current = nextCursor;
    setIsHistoryMode(nextCursor >= 0);
    setInput(nextCursor < 0 ? "" : (history[history.length - 1 - nextCursor] ?? ""));
  }, [setInput, history]);

  const pushHistory = useCallback((text: string) => {
    if (!text) return;
    setHistory((prev) => {
      const next = prev.length >= HISTORY_MAX ? prev.slice(prev.length - HISTORY_MAX + 1) : prev;
      return [...next, text];
    });
  }, []);

  const resetCursor = useCallback(() => {
    historyCursor.current = -1;
    setIsHistoryMode(false);
  }, []);

  return { recallPrev, recallNext, pushHistory, resetCursor, history, isHistoryMode };
}
