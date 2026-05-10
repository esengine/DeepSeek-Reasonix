/** Chat-scroll state in its own store so wheel/arrow ticks don't dirty App.tsx. */

export interface ChatScrollState {
  /** Rows of content above the visible viewport. */
  scrollRows: number;
  /** True while following the bottom — auto-advances on new content. */
  pinned: boolean;
  /** Total scrollable rows; CardStream reports this once Yoga has measured. */
  maxScroll: number;
  /** Bumped on every applied scroll delta — consumers can flash an indicator. */
  scrollVersion: number;
  /** Per-card row height, populated as cards mount and re-measured on streaming changes. */
  cardHeights: ReadonlyMap<string, number>;
}

export type ScrollListener = () => void;

export interface ChatScrollStore {
  getState(): ChatScrollState;
  subscribe(listener: ScrollListener): () => void;
  scrollUp(): void;
  scrollDown(): void;
  scrollPageUp(): void;
  scrollPageDown(): void;
  jumpToBottom(): void;
  setMaxScroll(rows: number): void;
  /** Reports a card's measured height. No-op if value matches the cache. */
  setCardHeight(id: string, rows: number): void;
  /** Drops heights for cards no longer in the visible list. Called by CardStream when cards change. */
  pruneCardHeights(liveIds: ReadonlySet<string>): void;
}

export const SCROLL_ARROW_ROWS = 3;
export const SCROLL_PAGE_ROWS = 5;
const COALESCE_MS = 16;

const EMPTY_HEIGHTS: ReadonlyMap<string, number> = new Map();

const initial: ChatScrollState = {
  scrollRows: 0,
  pinned: true,
  maxScroll: 0,
  scrollVersion: 0,
  cardHeights: EMPTY_HEIGHTS,
};

export function createChatScrollStore(): ChatScrollStore {
  let state = initial;
  const listeners = new Set<ScrollListener>();
  let pendingDelta = 0;
  let flushTimer: NodeJS.Timeout | null = null;

  function set(next: Partial<ChatScrollState>): void {
    const merged = { ...state, ...next };
    if (
      merged.scrollRows === state.scrollRows &&
      merged.pinned === state.pinned &&
      merged.maxScroll === state.maxScroll &&
      merged.scrollVersion === state.scrollVersion &&
      merged.cardHeights === state.cardHeights
    ) {
      return;
    }
    state = merged;
    for (const l of listeners) l();
  }

  function applyDelta(): void {
    const d = pendingDelta;
    pendingDelta = 0;
    if (d === 0) return;
    const next = Math.max(0, Math.min(state.maxScroll, state.scrollRows + d));
    set({
      scrollRows: next,
      pinned: d < 0 ? false : next >= state.maxScroll ? true : state.pinned,
      scrollVersion: state.scrollVersion + 1,
    });
  }

  /** Leading-edge: first tick flushes immediately, rest coalesce into one trailing flush. */
  function schedule(delta: number): void {
    if (flushTimer === null) {
      pendingDelta = delta;
      applyDelta();
      flushTimer = setTimeout(() => {
        flushTimer = null;
        if (pendingDelta !== 0) applyDelta();
      }, COALESCE_MS);
    } else {
      pendingDelta += delta;
    }
  }

  return {
    getState() {
      return state;
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    scrollUp: () => schedule(-SCROLL_ARROW_ROWS),
    scrollDown: () => schedule(SCROLL_ARROW_ROWS),
    scrollPageUp: () => schedule(-SCROLL_PAGE_ROWS),
    scrollPageDown: () => schedule(SCROLL_PAGE_ROWS),
    jumpToBottom() {
      pendingDelta = 0;
      if (flushTimer !== null) {
        clearTimeout(flushTimer);
        flushTimer = null;
      }
      set({ pinned: true });
    },
    setMaxScroll(rows: number) {
      const m = rows < 0 ? 0 : rows;
      // Pinned-mode invariant: scrollRows tracks maxScroll exactly.
      const nextScrollRows = state.pinned ? m : Math.min(state.scrollRows, m);
      set({ maxScroll: m, scrollRows: nextScrollRows });
    },
    setCardHeight(id: string, rows: number) {
      if (state.cardHeights.get(id) === rows) return;
      const next = new Map(state.cardHeights);
      next.set(id, rows);
      set({ cardHeights: next });
    },
    pruneCardHeights(liveIds: ReadonlySet<string>) {
      let drop = 0;
      for (const id of state.cardHeights.keys()) {
        if (!liveIds.has(id)) drop++;
      }
      if (drop === 0) return;
      const next = new Map<string, number>();
      for (const [id, h] of state.cardHeights) {
        if (liveIds.has(id)) next.set(id, h);
      }
      set({ cardHeights: next });
    },
  };
}
