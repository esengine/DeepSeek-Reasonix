import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { SessionInfo } from "../src/cli/ui/state/state.js";
import { createStore } from "../src/cli/ui/state/store.js";

const session: SessionInfo = {
  id: "test-batch",
  branch: "main",
  workspace: "/tmp/repo",
  model: "deepseek-chat",
};

describe("store notification batching (setTimeout(0))", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("coalesces N synchronous dispatches into a single state-listener call", () => {
    const store = createStore(session);
    let notifyCount = 0;
    store.subscribe(() => {
      notifyCount++;
    });

    // Dispatch 3 events synchronously in one tick
    store.dispatch({ type: "user.submit", text: "hello" });
    store.dispatch({ type: "turn.start", turnId: "t1" });
    store.dispatch({ type: "turn.thinking" });

    // Listener should NOT have fired yet (deferred via setTimeout(0))
    expect(notifyCount).toBe(0);

    // getState() must already reflect all 3 dispatches synchronously
    expect(store.getState().cards).toHaveLength(2); // user card + thinking live card
    expect(store.getState().turnInProgress).toBe(true);

    // Advance past the macrotask — listener fires exactly once
    vi.advanceTimersByTime(0);
    expect(notifyCount).toBe(1);
  });

  it("coalesces rapid streaming chunks into one notification", () => {
    const store = createStore(session);
    let notifyCount = 0;
    store.subscribe(() => {
      notifyCount++;
    });

    store.dispatch({ type: "reasoning.start", id: "r1" });
    for (let i = 0; i < 50; i++) {
      store.dispatch({ type: "reasoning.chunk", id: "r1", text: `tok${i} ` });
    }

    expect(notifyCount).toBe(0);
    expect((store.getState().cards[0] as any).text).toContain("tok49");

    vi.advanceTimersByTime(0);
    expect(notifyCount).toBe(1);
  });

  it("event listeners fire synchronously per-dispatch (not batched)", () => {
    const store = createStore(session);
    const received: string[] = [];
    store.onEvent((ev) => {
      received.push(ev.type);
    });

    store.dispatch({ type: "user.submit", text: "a" });
    expect(received).toEqual(["user.submit"]); // already fired synchronously

    store.dispatch({ type: "turn.start", turnId: "t1" });
    expect(received).toEqual(["user.submit", "turn.start"]);

    store.dispatch({ type: "turn.thinking" });
    expect(received).toEqual(["user.submit", "turn.start", "turn.thinking"]);
  });

  it("second macrotask batch fires another notification for new dispatches", () => {
    const store = createStore(session);
    let notifyCount = 0;
    store.subscribe(() => {
      notifyCount++;
    });

    // First batch
    store.dispatch({ type: "user.submit", text: "a" });
    store.dispatch({ type: "user.submit", text: "b" });
    vi.advanceTimersByTime(0);
    expect(notifyCount).toBe(1);

    // Second batch — a new macrotask should be scheduled
    store.dispatch({ type: "user.submit", text: "c" });
    expect(notifyCount).toBe(1); // not yet
    vi.advanceTimersByTime(0);
    expect(notifyCount).toBe(2);
  });
});
