/** Chat-history scroll state in its own store so wheel ticks do not dirty App.tsx. */

export interface ChatScrollState {
  /** Rows of content above the visible viewport. */
  scrollRows: number;
  /** True while following the bottom; new content keeps the viewport pinned. */
  pinned: boolean;
  /** Total scrollable rows reported by CardStream after measurement. */
  maxScroll: number;
  /** Bumped on every applied scroll delta so the indicator can flash. */
  scrollVersion: number;
  /** Per-card row height, populated as cards mount and re-measure. */
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
  scrollWheelUp(): void;
  scrollWheelDown(): void;
  jumpToBottom(): void;
  setMaxScroll(rows: number): void;
  setCardHeight(id: string, rows: number): void;
  pruneCardHeights(liveIds: ReadonlySet<string>): void;
}

export const SCROLL_ARROW_ROWS = 3;
export const SCROLL_PAGE_ROWS = 5;
export const SCROLL_WHEEL_ROWS = 1;
const COALESCE_MS = 16;

const EMPTY_HEIGHTS: ReadonlyMap<string, number> = new Map();

const initial: ChatScrollState = {
  scrollRows: 0,
  pinned: true,
  maxScroll: 0,
  scrollVersion: 0,
  cardHeights: EMPTY_HEIGHTS,
};

export interface CreateChatScrollStoreOptions {
  /** Per-SGR-wheel-report step. Defaults to 1; clamped to [1, 10]. */
  wheelRows?: number;
}

export function createChatScrollStore(opts: CreateChatScrollStoreOptions = {}): ChatScrollStore {
  const wheelRows =
    typeof opts.wheelRows === "number" && Number.isInteger(opts.wheelRows) && opts.wheelRows >= 1
      ? Math.min(opts.wheelRows, 10)
      : SCROLL_WHEEL_ROWS;
  let state = initial;
  const listeners = new Set<ScrollListener>();
  let pendingDelta = 0;
  let flushTimer: NodeJS.Timeout | null = null;
  let pendingMaxShrink: number | null = null;
  let shrinkTimer: NodeJS.Timeout | null = null;

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

  function flushShrink(): void {
    if (shrinkTimer !== null) {
      clearTimeout(shrinkTimer);
      shrinkTimer = null;
    }
    const target = pendingMaxShrink;
    pendingMaxShrink = null;
    if (target === null) return;
    const nextScrollRows = state.pinned ? target : Math.min(state.scrollRows, target);
    set({ maxScroll: target, scrollRows: nextScrollRows });
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
    scrollWheelUp: () => schedule(-wheelRows),
    scrollWheelDown: () => schedule(wheelRows),
    jumpToBottom() {
      pendingDelta = 0;
      if (flushTimer !== null) {
        clearTimeout(flushTimer);
        flushTimer = null;
      }
      pendingMaxShrink = null;
      if (shrinkTimer !== null) {
        clearTimeout(shrinkTimer);
        shrinkTimer = null;
      }
      set({ pinned: true, scrollRows: state.maxScroll });
    },
    setMaxScroll(rows: number) {
      const maxScroll = rows < 0 ? 0 : rows;
      const currentMax = pendingMaxShrink ?? state.maxScroll;
      if (state.pinned && maxScroll < currentMax) {
        pendingMaxShrink = maxScroll;
        if (shrinkTimer === null) {
          shrinkTimer = setTimeout(() => {
            shrinkTimer = null;
            flushShrink();
          }, COALESCE_MS);
        }
        return;
      }
      if (pendingMaxShrink !== null) flushShrink();
      const nextScrollRows = state.pinned ? maxScroll : Math.min(state.scrollRows, maxScroll);
      set({ maxScroll, scrollRows: nextScrollRows });
    },
    setCardHeight(id: string, rows: number) {
      if (state.cardHeights.get(id) === rows) return;
      const next = new Map(state.cardHeights);
      next.set(id, rows);
      set({ cardHeights: next });
    },
    pruneCardHeights(liveIds: ReadonlySet<string>) {
      let changed = false;
      const next = new Map<string, number>();
      for (const [id, rows] of state.cardHeights) {
        if (liveIds.has(id)) {
          next.set(id, rows);
        } else {
          changed = true;
        }
      }
      if (changed) set({ cardHeights: next });
    },
  };
}
