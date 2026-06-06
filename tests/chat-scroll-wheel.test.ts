import { describe, expect, it, vi } from "vitest";
import { resolveHistoryScrollMode } from "../src/cli/ui/history-scroll-mode.js";
import { createChatScrollStore } from "../src/cli/ui/state/chat-scroll-store.js";

describe("chat history scroll store", () => {
  it("scrolls wheel reports one row at a time and pins again at the bottom", () => {
    vi.useFakeTimers();
    const store = createChatScrollStore();
    try {
      store.setMaxScroll(20);

      store.scrollWheelUp();
      expect(store.getState()).toMatchObject({ scrollRows: 19, pinned: false, maxScroll: 20 });

      vi.advanceTimersByTime(16);
      store.scrollWheelDown();
      expect(store.getState()).toMatchObject({ scrollRows: 20, pinned: true, maxScroll: 20 });
    } finally {
      vi.useRealTimers();
    }
  });

  it("supports PageUp, PageDown, and End semantics", () => {
    vi.useFakeTimers();
    const store = createChatScrollStore();
    try {
      store.setMaxScroll(20);

      store.scrollPageUp();
      expect(store.getState()).toMatchObject({ scrollRows: 15, pinned: false });

      vi.advanceTimersByTime(16);
      store.scrollPageDown();
      expect(store.getState()).toMatchObject({ scrollRows: 20, pinned: true });

      vi.advanceTimersByTime(16);
      store.scrollPageUp();
      store.jumpToBottom();
      expect(store.getState()).toMatchObject({ scrollRows: 20, pinned: true });
    } finally {
      vi.useRealTimers();
    }
  });

  it("clamps configured wheel rows to a conservative range", () => {
    const tooLarge = createChatScrollStore({ wheelRows: 99 });
    tooLarge.setMaxScroll(20);
    tooLarge.scrollWheelUp();
    expect(tooLarge.getState().scrollRows).toBe(10);

    const invalid = createChatScrollStore({ wheelRows: 0 });
    invalid.setMaxScroll(20);
    invalid.scrollWheelUp();
    expect(invalid.getState().scrollRows).toBe(19);
  });

  it("coalesces wheel bursts without losing the trailing delta", () => {
    vi.useFakeTimers();
    try {
      const store = createChatScrollStore();
      store.setMaxScroll(20);

      store.scrollWheelUp();
      store.scrollWheelUp();
      store.scrollWheelUp();
      expect(store.getState().scrollRows).toBe(19);

      vi.advanceTimersByTime(16);
      expect(store.getState().scrollRows).toBe(17);
    } finally {
      vi.useRealTimers();
    }
  });
});

describe("history scroll mode resolution", () => {
  it("honors explicit native/app config", () => {
    expect(resolveHistoryScrollMode({ configured: "native", env: {} })).toBe("native");
    expect(resolveHistoryScrollMode({ configured: "app", env: {} })).toBe("app");
  });

  it("auto-enables app-managed scrolling in terminals with known native jump issues", () => {
    expect(resolveHistoryScrollMode({ configured: "auto", env: { TERM_PROGRAM: "vscode" } })).toBe(
      "app",
    );
    expect(resolveHistoryScrollMode({ configured: "auto", env: { MSYSTEM: "MINGW64" } })).toBe(
      "app",
    );
    expect(resolveHistoryScrollMode({ configured: "auto", env: { WT_SESSION: "abc" } })).toBe(
      "app",
    );
    expect(resolveHistoryScrollMode({ configured: "auto", env: { TERM_PROGRAM: "ghostty" } })).toBe(
      "app",
    );
  });

  it("keeps native scrollback for unknown terminals in auto mode", () => {
    expect(
      resolveHistoryScrollMode({
        configured: "auto",
        env: { TERM: "xterm-256color" },
        platform: "linux",
      }),
    ).toBe("native");
  });
});
