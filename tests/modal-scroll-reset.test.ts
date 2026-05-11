import { describe, expect, it, vi } from "vitest";
import { createChatScrollStore } from "../src/cli/ui/state/chat-scroll-store.js";
import { PauseGate } from "../src/core/pause-gate.js";

describe("chatScroll jumpToBottom (issue #642)", () => {
  it("flips pinned back on and snaps scrollRows on the next setMaxScroll", () => {
    const store = createChatScrollStore();
    store.setMaxScroll(100);
    store.scrollPageUp();
    store.scrollPageUp();
    expect(store.getState().pinned).toBe(false);
    expect(store.getState().scrollRows).toBeLessThan(100);

    store.jumpToBottom();
    expect(store.getState().pinned).toBe(true);

    store.setMaxScroll(120);
    expect(store.getState().scrollRows).toBe(120);
  });

  it("a pauseGate listener that calls jumpToBottom reseats the viewport on every modal kind", () => {
    const store = createChatScrollStore();
    const gate = new PauseGate();
    gate.on(() => {
      store.jumpToBottom();
    });

    const kinds = [
      "run_command",
      "plan_proposed",
      "plan_checkpoint",
      "plan_revision",
      "choice",
    ] as const;
    for (const kind of kinds) {
      store.setMaxScroll(50);
      store.scrollPageUp();
      expect(store.getState().pinned).toBe(false);

      void gate.ask({ kind, payload: payloadFor(kind) } as Parameters<typeof gate.ask>[0]);
      expect(store.getState().pinned).toBe(true);
      gate.cancelAll();
    }
  });
});

describe("chatScroll setMaxScroll coalescing (issue #653)", () => {
  it("coalesces a burst of pinned-mode shrinks into a single trailing transition", async () => {
    vi.useFakeTimers();
    try {
      const store = createChatScrollStore();
      // Fill past one screen so maxScroll > 0; pinned stays true.
      store.setMaxScroll(500);
      expect(store.getState().pinned).toBe(true);
      expect(store.getState().scrollRows).toBe(500);

      const seen: Array<{ scrollRows: number; maxScroll: number }> = [];
      store.subscribe(() => {
        const s = store.getState();
        seen.push({ scrollRows: s.scrollRows, maxScroll: s.maxScroll });
      });

      // Simulate N parallel subagent-card collapses re-measuring maxScroll in
      // rapid succession during an Esc abort. Without coalescing, each call
      // would snap scrollRows and bounce the viewport.
      store.setMaxScroll(440);
      store.setMaxScroll(380);
      store.setMaxScroll(310);
      store.setMaxScroll(260);
      store.setMaxScroll(200);

      // No subscriber notifications yet — shrinks are deferred.
      expect(seen).toEqual([]);
      // State still reflects the pre-burst values until the trailing flush.
      expect(store.getState().scrollRows).toBe(500);
      expect(store.getState().maxScroll).toBe(500);

      await vi.advanceTimersByTimeAsync(32);

      // Exactly one settled transition lands at the final target.
      expect(seen).toEqual([{ scrollRows: 200, maxScroll: 200 }]);
      expect(store.getState().pinned).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  it("applies grows immediately so streaming output stays pinned without latency", () => {
    const store = createChatScrollStore();
    store.setMaxScroll(50);
    expect(store.getState().scrollRows).toBe(50);
    store.setMaxScroll(120);
    expect(store.getState().scrollRows).toBe(120);
    store.setMaxScroll(300);
    expect(store.getState().scrollRows).toBe(300);
  });

  it("does not coalesce when not pinned — shrinks apply immediately so scrollRows clamps", () => {
    const store = createChatScrollStore();
    store.setMaxScroll(500);
    store.scrollPageUp();
    store.scrollPageUp();
    expect(store.getState().pinned).toBe(false);
    const beforeRows = store.getState().scrollRows;

    store.setMaxScroll(200);
    expect(store.getState().maxScroll).toBe(200);
    expect(store.getState().scrollRows).toBe(Math.min(beforeRows, 200));
  });

  it("a follow-up grow during a coalesced shrink flushes the shrink first", async () => {
    vi.useFakeTimers();
    try {
      const store = createChatScrollStore();
      store.setMaxScroll(500);

      store.setMaxScroll(300);
      // Deferred — state still 500.
      expect(store.getState().maxScroll).toBe(500);

      store.setMaxScroll(600);
      // Grow forced an immediate flush; final value is the grow target.
      expect(store.getState().maxScroll).toBe(600);
      expect(store.getState().scrollRows).toBe(600);
      expect(store.getState().pinned).toBe(true);

      // Allow any leftover timer to fire — must not regress the state.
      await vi.advanceTimersByTimeAsync(32);
      expect(store.getState().maxScroll).toBe(600);
      expect(store.getState().scrollRows).toBe(600);
    } finally {
      vi.useRealTimers();
    }
  });

  it("jumpToBottom cancels a pending shrink so the explicit pin wins", async () => {
    vi.useFakeTimers();
    try {
      const store = createChatScrollStore();
      store.setMaxScroll(500);
      store.scrollPageUp();
      store.scrollPageUp();
      expect(store.getState().pinned).toBe(false);
      // Re-pin (this also clears any prior delta flush).
      store.jumpToBottom();
      expect(store.getState().pinned).toBe(true);

      // Now grow then queue a shrink.
      store.setMaxScroll(500);
      expect(store.getState().scrollRows).toBe(500);
      store.setMaxScroll(200);
      // Deferred.
      expect(store.getState().maxScroll).toBe(500);

      store.jumpToBottom();
      await vi.advanceTimersByTimeAsync(32);

      // The deferred shrink was dropped; state is still at 500 from the grow.
      expect(store.getState().maxScroll).toBe(500);
      expect(store.getState().pinned).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });
});

function payloadFor(kind: string): unknown {
  switch (kind) {
    case "run_command":
    case "run_background":
      return { command: "ls" };
    case "plan_proposed":
      return { plan: "do thing" };
    case "plan_checkpoint":
      return { stepId: "s1", result: "ok" };
    case "plan_revision":
      return { reason: "r", remainingSteps: [] };
    case "choice":
      return { question: "?", options: [], allowCustom: false };
    default:
      return {};
  }
}
