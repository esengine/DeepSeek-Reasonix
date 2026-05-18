import { describe, expect, it, vi } from "vitest";
import { interruptCurrentTurn } from "../src/cli/ui/turn-interrupt.js";

describe("interruptCurrentTurn", () => {
  it("aborts an active turn and clears pending gates once", () => {
    const resetPendingModals = vi.fn();
    const stopLoop = vi.fn();
    const abort = vi.fn();
    const controller = {
      turnActiveRef: { current: true },
      abortedThisTurn: { current: false },
      resetPendingModals,
      isLoopActive: () => true,
      stopLoop,
      loop: { abort },
    };

    expect(interruptCurrentTurn(controller)).toBe("aborted");
    expect(interruptCurrentTurn(controller)).toBe("already-aborted");
    expect(resetPendingModals).toHaveBeenCalledTimes(1);
    expect(stopLoop).toHaveBeenCalledTimes(1);
    expect(abort).toHaveBeenCalledTimes(1);
  });

  it("stops an idle auto-loop without aborting the model loop", () => {
    const stopLoop = vi.fn();
    const abort = vi.fn();

    const outcome = interruptCurrentTurn({
      turnActiveRef: { current: false },
      abortedThisTurn: { current: false },
      resetPendingModals: vi.fn(),
      isLoopActive: () => true,
      stopLoop,
      loop: { abort },
    });

    expect(outcome).toBe("stopped-loop");
    expect(stopLoop).toHaveBeenCalledTimes(1);
    expect(abort).not.toHaveBeenCalled();
  });

  it("is a no-op when idle and no auto-loop is active", () => {
    const outcome = interruptCurrentTurn({
      turnActiveRef: { current: false },
      abortedThisTurn: { current: false },
      resetPendingModals: vi.fn(),
      isLoopActive: () => false,
      stopLoop: vi.fn(),
      loop: { abort: vi.fn() },
    });

    expect(outcome).toBe("idle");
  });

  it("does not abort the model loop when the UI is busy with unrelated work", () => {
    const resetPendingModals = vi.fn();
    const stopLoop = vi.fn();
    const abort = vi.fn();

    const outcome = interruptCurrentTurn({
      turnActiveRef: { current: false },
      abortedThisTurn: { current: false },
      resetPendingModals,
      isLoopActive: () => false,
      stopLoop,
      loop: { abort },
    });

    expect(outcome).toBe("idle");
    expect(resetPendingModals).not.toHaveBeenCalled();
    expect(stopLoop).not.toHaveBeenCalled();
    expect(abort).not.toHaveBeenCalled();
  });
});
