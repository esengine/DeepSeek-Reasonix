import { describe, expect, it, vi } from "vitest";
import {
  SCROLL_PAGE_ROWS,
  SCROLL_WHEEL_ROWS,
  createChatScrollStore,
} from "../src/cli/ui/state/chat-scroll-store.js";

describe("chatScroll wheel step (issue #1419)", () => {
  it("one wheel tick moves a single row, not a full page", () => {
    const store = createChatScrollStore();
    store.setMaxScroll(200);
    store.scrollWheelUp();
    expect(store.getState().scrollRows).toBe(200 - SCROLL_WHEEL_ROWS);
  });

  it("a burst of wheel ticks accumulates rows-per-tick, not pages-per-tick", async () => {
    vi.useFakeTimers();
    try {
      const store = createChatScrollStore();
      store.setMaxScroll(500);
      const before = store.getState().scrollRows;

      for (let i = 0; i < 5; i++) store.scrollWheelUp();
      await vi.advanceTimersByTimeAsync(32);

      const moved = before - store.getState().scrollRows;
      expect(moved).toBe(5 * SCROLL_WHEEL_ROWS);
      expect(moved).toBeLessThan(5 * SCROLL_PAGE_ROWS);
    } finally {
      vi.useRealTimers();
    }
  });

  it("page step stays at SCROLL_PAGE_ROWS for keyboard PgUp / PgDn", () => {
    const store = createChatScrollStore();
    store.setMaxScroll(200);
    store.scrollPageUp();
    expect(store.getState().scrollRows).toBe(200 - SCROLL_PAGE_ROWS);
  });

  it("wheelRows override scales each tick (issue #1494)", () => {
    const store = createChatScrollStore({ wheelRows: 3 });
    store.setMaxScroll(200);
    store.scrollWheelUp();
    expect(store.getState().scrollRows).toBe(200 - 3);
  });

  it("wheelRows clamps to [1, 10] and ignores non-positive / non-integer values", async () => {
    vi.useFakeTimers();
    try {
      for (const [opt, expected] of [
        [{ wheelRows: 0 }, SCROLL_WHEEL_ROWS],
        [{ wheelRows: -5 }, SCROLL_WHEEL_ROWS],
        [{ wheelRows: 2.5 }, SCROLL_WHEEL_ROWS],
        [{ wheelRows: 99 }, 10],
        [{}, SCROLL_WHEEL_ROWS],
      ] as const) {
        const store = createChatScrollStore(opt);
        store.setMaxScroll(500);
        const before = store.getState().scrollRows;
        store.scrollWheelUp();
        await vi.advanceTimersByTimeAsync(32);
        expect(before - store.getState().scrollRows).toBe(expected);
      }
    } finally {
      vi.useRealTimers();
    }
  });
});
