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
  // Trailing-edge coalesce target for pinned-mode shrinks (issue #653).
  // While a burst of card-collapse re-measurements arrives, we hold the latest
  // target here and apply it once on a microtask flush, so subscribers see one
  // settled transition instead of N oscillating snaps.
  // Grows still apply immediately — streaming content should auto-scroll without
  // latency. Growth oscillation is prevented upstream by the monotonic height
  // lock in CardStream.MeasuredCard: card heights only increase during streaming,
  // so maxScroll growth is naturally monotonic.
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
    jumpToBottom() {
      pendingDelta = 0;
      if (flushTimer !== null) {
        clearTimeout(flushTimer);
        flushTimer = null;
      }
      // Drop any deferred shrink so an explicit jump isn't undone by a stale target.
      pendingMaxShrink = null;
      if (shrinkTimer !== null) {
        clearTimeout(shrinkTimer);
        shrinkTimer = null;
      }
      set({ pinned: true });
    },
    setMaxScroll(rows: number) {
      const m = rows < 0 ? 0 : rows;
      // Coalesce shrinks while pinned (issue #653): a burst of card-teardown
      // re-measurements during an Esc-abort would otherwise snap scrollRows N
      // times, producing a visible flicker. Grows still apply immediately so
      // normal streaming output keeps the viewport pinned without latency.
      // Growth oscillation is prevented upstream by the monotonic height lock
      // in CardStream.MeasuredCard — card heights only increase during streaming,
      // so maxScroll growth is naturally monotonic without coalescing.
      const currentMax = pendingMaxShrink ?? state.maxScroll;
      if (state.pinned && m < currentMax) {
        pendingMaxShrink = m;
        if (shrinkTimer === null) {
          shrinkTimer = setTimeout(() => {
            shrinkTimer = null;
            flushShrink();
          }, COALESCE_MS);
        }
        return;
      }
      // Non-shrink path: flush any deferred shrink first so its trailing state
      // doesn't clobber the value we're about to set.
      if (pendingMaxShrink !== null) flushShrink();
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
