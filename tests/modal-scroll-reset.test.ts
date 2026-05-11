import { describe, expect, it } from "vitest";
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
