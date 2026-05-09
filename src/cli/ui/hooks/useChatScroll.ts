import { useCallback, useEffect, useRef, useState } from "react";

/** Rows advanced per PgUp / PgDn / arrow / wheel — small for smooth feel. */
export const SCROLL_PAGE_ROWS = 3;
const COALESCE_MS = 16;

export interface ChatScrollState {
  /** How many rows of content are above the visible viewport. */
  scrollRows: number;
  /** True when the user is following the bottom (auto-advances on new content). */
  pinned: boolean;
  scrollUp: () => void;
  scrollDown: () => void;
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

  const flush = useCallback(() => {
    flushTimer.current = null;
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

  const schedule = useCallback(
    (delta: number) => {
      pendingDelta.current += delta;
      if (flushTimer.current !== null) return;
      flushTimer.current = setTimeout(flush, COALESCE_MS);
    },
    [flush],
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

  const scrollUp = useCallback(() => schedule(-SCROLL_PAGE_ROWS), [schedule]);
  const scrollDown = useCallback(() => schedule(SCROLL_PAGE_ROWS), [schedule]);

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

  return { scrollRows, pinned, scrollUp, scrollDown, jumpToBottom, setMaxScroll };
}
