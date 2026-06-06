import { describe, expect, it } from "vitest";
import {
  clearQQTurnRouting,
  createQQTurnRoutingState,
  markQQTurnFinished,
  markQQTurnStarted,
  setQQPendingInteraction,
  shouldRouteQQForTab,
  takeQQPendingInteraction,
} from "../src/desktop/qq-turn-routing.js";

describe("desktop QQ turn routing", () => {
  it("keeps a QQ-started tab routable even after a local turn starts elsewhere", () => {
    const routing = createQQTurnRoutingState();

    markQQTurnStarted(routing, "tab-1");

    expect(shouldRouteQQForTab(routing, "tab-1")).toBe(true);
    expect(shouldRouteQQForTab(routing, "tab-2")).toBe(false);

    // A local turn in another tab should not cancel the older QQ turn.
    expect(shouldRouteQQForTab(routing, "tab-1")).toBe(true);
    expect(shouldRouteQQForTab(routing, "tab-2")).toBe(false);

    markQQTurnFinished(routing, "tab-1");
    expect(shouldRouteQQForTab(routing, "tab-1")).toBe(false);
  });

  it("stores follow-up prompts only for tabs that are actively QQ-routed", () => {
    const routing = createQQTurnRoutingState();

    markQQTurnStarted(routing, "tab-1");
    setQQPendingInteraction(routing, "tab-1", 7, "choice", { question: "Pick one" });
    setQQPendingInteraction(routing, "tab-2", 9, "choice", { question: "Should stay local" });

    expect(takeQQPendingInteraction(routing, "tab-2")).toBeNull();
    expect(takeQQPendingInteraction(routing, "tab-1")).toEqual({
      gateId: 7,
      kind: "choice",
      payload: { question: "Pick one" },
    });
  });

  it("clears all tab routing state on disconnect", () => {
    const routing = createQQTurnRoutingState();

    markQQTurnStarted(routing, "tab-1");
    setQQPendingInteraction(routing, "tab-1", 3, "run_command", { command: "dir" });

    clearQQTurnRouting(routing);

    expect(shouldRouteQQForTab(routing, "tab-1")).toBe(false);
    expect(takeQQPendingInteraction(routing, "tab-1")).toBeNull();
  });
});
