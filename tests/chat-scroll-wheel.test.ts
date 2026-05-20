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
});
