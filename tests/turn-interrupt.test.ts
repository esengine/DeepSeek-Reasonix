import { describe, expect, it, vi } from "vitest";
import { QUIT_ARM_WINDOW_MS, handleTurnInterrupt } from "../src/cli/ui/turn-interrupt.js";

describe("handleTurnInterrupt", () => {
  it("aborts an active Ctrl+C turn without quitting the process", () => {
    const resetPendingModals = vi.fn();
    const stopLoop = vi.fn();
    const abort = vi.fn();
    const quitProcess = vi.fn();
    const controller = {
      turnActiveRef: { current: true },
      abortedThisTurn: { current: false },
      resetPendingModals,
      isLoopActive: () => true,
      stopLoop,
      loop: { abort },
      quitProcess,
      quitArmedAt: { current: null as number | null },
    };

    expect(handleTurnInterrupt("ctrl-c", controller)).toBe("aborted");
    expect(handleTurnInterrupt("ctrl-c", controller)).toBe("already-aborted");
    expect(resetPendingModals).toHaveBeenCalledTimes(1);
    expect(stopLoop).toHaveBeenCalledTimes(1);
    expect(abort).toHaveBeenCalledTimes(1);
    expect(quitProcess).not.toHaveBeenCalled();
  });

  it("arms quit on first idle Ctrl+C and exits on second within the window", () => {
    const quitProcess = vi.fn();
    const quitArmedAt = { current: null as number | null };
    const base = {
      turnActiveRef: { current: false },
      abortedThisTurn: { current: false },
      resetPendingModals: vi.fn(),
      isLoopActive: () => false,
      stopLoop: vi.fn(),
      loop: { abort: vi.fn() },
      quitProcess,
      quitArmedAt,
    };

    // First press — arms, does NOT quit
    expect(handleTurnInterrupt("ctrl-c", base)).toBe("quit-armed");
    expect(quitProcess).not.toHaveBeenCalled();
    expect(quitArmedAt.current).not.toBeNull();

    // Second press within window — quits
    expect(handleTurnInterrupt("ctrl-c", base)).toBe("quit");
    expect(quitProcess).toHaveBeenCalledTimes(1);
  });

  it("re-arms if second Ctrl+C arrives after the window expires", () => {
    const quitProcess = vi.fn();
    const quitArmedAt = { current: null as number | null };
    const base = {
      turnActiveRef: { current: false },
      abortedThisTurn: { current: false },
      resetPendingModals: vi.fn(),
      isLoopActive: () => false,
      stopLoop: vi.fn(),
      loop: { abort: vi.fn() },
      quitProcess,
      quitArmedAt,
    };

    handleTurnInterrupt("ctrl-c", base);
    // Expire the window by backdating armedAt
    quitArmedAt.current = Date.now() - QUIT_ARM_WINDOW_MS - 1;

    // Second press after expiry → re-arms, does not quit
    expect(handleTurnInterrupt("ctrl-c", base)).toBe("quit-armed");
    expect(quitProcess).not.toHaveBeenCalled();
  });

  it("stops an idle auto-loop on Esc without aborting the next turn", () => {
    const resetPendingModals = vi.fn();
    const stopLoop = vi.fn();
    const abort = vi.fn();
    const quitProcess = vi.fn();

    const outcome = handleTurnInterrupt("escape", {
      turnActiveRef: { current: false },
      abortedThisTurn: { current: false },
      resetPendingModals,
      isLoopActive: () => true,
      stopLoop,
      loop: { abort },
      quitProcess,
      quitArmedAt: { current: null },
    });

    expect(outcome).toBe("stopped-loop");
    expect(stopLoop).toHaveBeenCalledTimes(1);
    expect(resetPendingModals).not.toHaveBeenCalled();
    expect(abort).not.toHaveBeenCalled();
    expect(quitProcess).not.toHaveBeenCalled();
  });

  it("ignores Esc during unrelated UI busy work", () => {
    const resetPendingModals = vi.fn();
    const abort = vi.fn();
    const quitProcess = vi.fn();

    const outcome = handleTurnInterrupt("escape", {
      turnActiveRef: { current: false },
      abortedThisTurn: { current: false },
      resetPendingModals,
      isLoopActive: () => false,
      stopLoop: vi.fn(),
      loop: { abort },
      quitProcess,
      quitArmedAt: { current: null },
    });

    expect(outcome).toBe("idle");
    expect(resetPendingModals).not.toHaveBeenCalled();
    expect(abort).not.toHaveBeenCalled();
    expect(quitProcess).not.toHaveBeenCalled();
  });
});
