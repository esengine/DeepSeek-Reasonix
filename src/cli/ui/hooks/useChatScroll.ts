import { useCallback, useEffect, useRef, useState } from "react";

/** Arrow-key step: small for fine-grained navigation. */
export const SCROLL_ARROW_ROWS = 3;
/** Wheel / PgUp / PgDn step: chunkier so each tick covers meaningful ground. */
export const SCROLL_PAGE_ROWS = 8;
const COALESCE_MS = 16;

export interface ChatScrollState {
  /** How many rows of content are above the visible viewport. */
  scrollRows: number;
  /** True when the user is following the bottom (auto-advances on new content). */
  pinned: boolean;
  /** Arrow-key step — fine-grained. */
  scrollUp: () => void;
  scrollDown: () => void;
  /** PgUp / PgDn / wheel step — chunkier. */
  scrollPageUp: () => void;
  scrollPageDown: () => void;
  /** Jump straight to the latest content and resume auto-follow (End key). */
  jumpToBottom: () => void;
  /** CardStream calls this once it has measured inner/outer Box heights. */
  setMaxScroll: (rows: number) => void;
}

/** Row-precision scroll state. CardStream reports maxScroll back via setMaxScroll once Ink has laid out the inner column. */
export function useChatScroll(): ChatScrollState {
  const [scrollRows, setScrollRows] = useState(0);
  const [pinned, setPinned] = useState(true);
  const [maxScroll, setMaxScrollState] = useState(0);
  const maxScrollRef = useRef(0);

  const pendingDelta = useRef(0);
  const flushTimer = useRef<NodeJS.Timeout | null>(null);

  const applyDelta = useCallback(() => {
    const d = pendingDelta.current;
    pendingDelta.current = 0;
    if (d === 0) return;
    if (d < 0) setPinned(false);
    setScrollRows((o) => {
      const next = Math.max(0, Math.min(maxScrollRef.current, o + d));
      if (next >= maxScrollRef.current) setPinned(true);
      return next;
    });
  }, []);

  /** Leading-edge schedule: first call applies immediately so the user
   * sees the wheel respond on the first tick (no 16 ms wait before any
   * visual feedback). Further calls inside the COALESCE_MS window batch
   * into pendingDelta and land in one trailing flush, so a fast scroll
   * still produces only one re-render per window. */
  const schedule = useCallback(
    (delta: number) => {
      if (flushTimer.current === null) {
        pendingDelta.current = delta;
        applyDelta(); // leading-edge flush — instant feedback
        flushTimer.current = setTimeout(() => {
          flushTimer.current = null;
          if (pendingDelta.current !== 0) applyDelta();
        }, COALESCE_MS);
      } else {
        pendingDelta.current += delta;
      }
    },
    [applyDelta],
  );

  useEffect(() => {
    return () => {
      if (flushTimer.current !== null) {
        clearTimeout(flushTimer.current);
        flushTimer.current = null;
      }
    };
  }, []);

  useEffect(() => {
    if (pinned) setScrollRows(maxScroll);
  }, [pinned, maxScroll]);

  useEffect(() => {
    if (scrollRows > maxScroll) setScrollRows(maxScroll);
  }, [scrollRows, maxScroll]);

  const scrollUp = useCallback(() => schedule(-SCROLL_ARROW_ROWS), [schedule]);
  const scrollDown = useCallback(() => schedule(SCROLL_ARROW_ROWS), [schedule]);
  const scrollPageUp = useCallback(() => schedule(-SCROLL_PAGE_ROWS), [schedule]);
  const scrollPageDown = useCallback(() => schedule(SCROLL_PAGE_ROWS), [schedule]);

  const jumpToBottom = useCallback(() => {
    pendingDelta.current = 0;
    if (flushTimer.current !== null) {
      clearTimeout(flushTimer.current);
      flushTimer.current = null;
    }
    setPinned(true);
  }, []);

  const setMaxScroll = useCallback((rows: number) => {
    maxScrollRef.current = rows;
    setMaxScrollState(rows);
  }, []);

  return {
    scrollRows,
    pinned,
    scrollUp,
    scrollDown,
    scrollPageUp,
    scrollPageDown,
    jumpToBottom,
    setMaxScroll,
  };
}
