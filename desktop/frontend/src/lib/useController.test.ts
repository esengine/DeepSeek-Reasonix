import { describe, expect, it } from "vitest";
import { testInitialState, testReducer } from "./useController";

describe("useController optimistic user message state", () => {
  it("renders a sent user message immediately as pending", () => {
    const state = testReducer(testInitialState, { type: "user", text: "你好" });

    expect(state.items).toEqual([{ kind: "user", id: "u0", text: "你好", pending: true }]);
    expect(state.pendingUser).toEqual({ id: "u0", text: "你好" });
    expect(state.running).toBe(true);
  });

  it("confirms the pending user message on the first real event without duplicating it", () => {
    const sent = testReducer(testInitialState, { type: "user", text: "你好" });
    const confirmed = testReducer(sent, { type: "event", e: { kind: "text", text: "hi" } });

    expect(confirmed.items.filter((item) => item.kind === "user")).toEqual([
      { kind: "user", id: "u0", text: "你好", pending: false },
    ]);
    expect(confirmed.pendingUser).toBeUndefined();
  });

  it("removes the pending user message when unsending before the first real event", () => {
    const sent = testReducer(testInitialState, { type: "user", text: "你好" });
    const unsent = testReducer(sent, { type: "unsend" });

    expect(unsent.items).toEqual([]);
    expect(unsent.pendingUser).toBeUndefined();
    expect(unsent.discardTurn).toBe(true);
  });

  it("marks an optimistic user message failed when submit rejects before confirmation", () => {
    const sent = testReducer(testInitialState, { type: "user", text: "你好" });
    const failed = testReducer(sent, { type: "send_failed", error: "bridge unavailable" });

    expect(failed.items).toEqual([
      { kind: "user", id: "u0", text: "你好", pending: false, failed: true },
      { kind: "notice", id: "n1", level: "warn", text: "Send failed: bridge unavailable" },
    ]);
    expect(failed.pendingUser).toBeUndefined();
    expect(failed.running).toBe(false);
  });

  it("ignores a late submit rejection after the user message is confirmed", () => {
    const sent = testReducer(testInitialState, { type: "user", text: "你好" });
    const started = testReducer(sent, { type: "event", e: { kind: "turn_started" } });
    const inFlight = testReducer(started, { type: "event", e: { kind: "text", text: "hi" } });
    const failed = testReducer(inFlight, { type: "send_failed", error: "late bridge rejection" });

    expect(failed).toBe(inFlight);
    expect(failed.running).toBe(true);
    expect(failed.turnActive).toBe(true);
  });
});
