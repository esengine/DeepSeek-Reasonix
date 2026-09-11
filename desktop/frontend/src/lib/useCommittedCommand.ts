import { useMemo } from "react";
import { useCommittedSlot, type CommittedSlot } from "./useCommittedSlot";

export type { CommandCancellationReason, CommandOutcome } from "./commandOutcome";

type AnyCommand = (...args: never[]) => unknown;

function bindCommittedCommand<Command extends AnyCommand>(slot: CommittedSlot<Command>) {
  return (...args: Parameters<Command>): ReturnType<Command> | undefined => {
    if (slot.phase !== "ready") return undefined;
    return slot.value?.(...args) as ReturnType<Command> | undefined;
  };
}

/** Stable event entry; DOM ref attachment is a separate mutation-phase contract. */
export function useCommittedCommand<Command extends AnyCommand>(command: Command): Command {
  const slot = useCommittedSlot(command);
  return useMemo(() => bindCommittedCommand(slot), [slot]) as Command;
}
