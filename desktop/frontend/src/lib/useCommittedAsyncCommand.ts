import { useMemo } from "react";
import { CommandCancelled, executeCapturedCommand, type CommandAuthority, type CommandOutcome } from "./commandOutcome";
import { useCommittedSlot, type CommittedSlot } from "./useCommittedSlot";

type CommandDefinition<Args extends unknown[], Input, Result> = {
  capture: (...args: Args) => Input;
  execute: (input: Input, authority: CommandAuthority) => Result;
};

function captureAuthority(slot: CommittedSlot<unknown>): CommandAuthority {
  const epoch = slot.epoch;
  const requestId = ++slot.requestId;
  return {
    checkpoint() {
      if (slot.phase !== "ready" || slot.epoch !== epoch) throw new CommandCancelled("disposed");
      if (slot.requestId !== requestId) throw new CommandCancelled("superseded");
    },
  };
}

function bindCommittedAsync<Args extends unknown[], Input, Result>(
  slot: CommittedSlot<CommandDefinition<Args, Input, Result>>,
) {
  return (...args: Args): Promise<CommandOutcome<Awaited<Result>>> => {
    if (!slot.value || slot.phase !== "ready") {
      return Promise.resolve({ status: "cancelled", reason: slot.phase === "disposed" ? "disposed" : "not-ready" });
    }
    const authority = captureAuthority(slot);
    try {
      const { capture, execute } = slot.value;
      return executeCapturedCommand(capture(...args), execute, authority);
    } catch (error) {
      return Promise.resolve(error instanceof CommandCancelled
        ? { status: "cancelled", reason: error.reason }
        : { status: "failed", error });
    }
  };
}

/** One command lane. Use independent owners for unrelated resource operations. */
export function useCommittedAsyncCommand<Args extends unknown[], Input, Result>(
  capture: (...args: Args) => Input,
  execute: (input: Input, authority: CommandAuthority) => Result,
) {
  const slot = useCommittedSlot({ capture, execute });
  return useMemo(() => bindCommittedAsync(slot), [slot]);
}
