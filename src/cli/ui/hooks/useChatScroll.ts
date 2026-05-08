import { useCallback, useEffect, useState } from "react";

/** Rows advanced per PgUp / PgDn / arrow / wheel — small for smooth feel. */
export const SCROLL_PAGE_ROWS = 3;

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

  useEffect(() => {
    if (pinned) setScrollRows(maxScroll);
  }, [pinned, maxScroll]);

  useEffect(() => {
    if (scrollRows > maxScroll) setScrollRows(maxScroll);
  }, [scrollRows, maxScroll]);

  const scrollUp = useCallback(() => {
    setPinned(false);
    setScrollRows((o) => Math.max(0, o - SCROLL_PAGE_ROWS));
  }, []);

  const scrollDown = useCallback(() => {
    setScrollRows((o) => {
      const next = Math.min(maxScroll, o + SCROLL_PAGE_ROWS);
      if (next >= maxScroll) setPinned(true);
      return next;
    });
  }, [maxScroll]);

  const jumpToBottom = useCallback(() => setPinned(true), []);

  const setMaxScroll = useCallback((rows: number) => setMaxScrollState(rows), []);

  return { scrollRows, pinned, scrollUp, scrollDown, jumpToBottom, setMaxScroll };
}
