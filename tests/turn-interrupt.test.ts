import { describe, expect, it, vi } from "vitest";
import { decideBusySubmit, handleTurnInterrupt } from "../src/cli/ui/turn-interrupt.js";

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
    };

    expect(handleTurnInterrupt("ctrl-c", controller)).toBe("aborted");
    expect(handleTurnInterrupt("ctrl-c", controller)).toBe("already-aborted");
    expect(resetPendingModals).toHaveBeenCalledTimes(1);
    expect(stopLoop).toHaveBeenCalledTimes(1);
    expect(abort).toHaveBeenCalledTimes(1);
    expect(quitProcess).not.toHaveBeenCalled();
  });

  it("quits on Ctrl+C when no model turn is active", () => {
    const resetPendingModals = vi.fn();
    const abort = vi.fn();
    const quitProcess = vi.fn();

    const outcome = handleTurnInterrupt("ctrl-c", {
      turnActiveRef: { current: false },
      abortedThisTurn: { current: false },
      resetPendingModals,
      isLoopActive: () => true,
      stopLoop: vi.fn(),
      loop: { abort },
      quitProcess,
    });

    expect(outcome).toBe("quit");
    expect(quitProcess).toHaveBeenCalledTimes(1);
    expect(resetPendingModals).not.toHaveBeenCalled();
    expect(abort).not.toHaveBeenCalled();
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
    });

    expect(outcome).toBe("idle");
    expect(resetPendingModals).not.toHaveBeenCalled();
    expect(abort).not.toHaveBeenCalled();
    expect(quitProcess).not.toHaveBeenCalled();
  });
});

describe("decideBusySubmit", () => {
  const isCommand = (text: string) => text.startsWith("/");

  it("steers ordinary text into an active non-aborted turn", () => {
    expect(
      decideBusySubmit("please adjust that", {
        busy: true,
        submitting: true,
        aborted: false,
        isCommand,
      }),
    ).toBe("steer");
  });

  it("queues ordinary text once the active turn has already been interrupted", () => {
    expect(
      decideBusySubmit("actually do this instead", {
        busy: true,
        submitting: true,
        aborted: true,
        isCommand,
      }),
    ).toBe("queue-after-abort");
  });

  it("queues interrupted-turn text even if React busy state already dropped", () => {
    expect(
      decideBusySubmit("actually do this instead", {
        busy: false,
        submitting: true,
        aborted: true,
        isCommand,
      }),
    ).toBe("queue-after-abort");
  });

  it("still rejects commands while busy", () => {
    expect(
      decideBusySubmit("/new", {
        busy: true,
        submitting: true,
        aborted: true,
        isCommand,
      }),
    ).toBe("reject-command");
  });
});
