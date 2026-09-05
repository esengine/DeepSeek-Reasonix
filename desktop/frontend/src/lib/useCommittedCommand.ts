import { useEffect, useInsertionEffect, useMemo, useRef } from "react";

export type CommandCancellationReason = "not-ready" | "superseded" | "disposed";

export type CommandOutcome<T> =
  | { status: "completed"; value: T }
  | { status: "cancelled"; reason: CommandCancellationReason }
  | { status: "failed"; error: unknown };

type AnyCommand = (...args: never[]) => unknown;

type CommittedCommandSlot<Command extends AnyCommand> = {
  command?: Command;
  lifecycle: number;
  mounted: boolean;
};

function bindCommittedCommand<Command extends AnyCommand>(slot: CommittedCommandSlot<Command>) {
  return (...args: Parameters<Command>): ReturnType<Command> | undefined => (
    slot.command?.(...args) as ReturnType<Command> | undefined
  );
}

function useCommittedCommandSlot<Command extends AnyCommand>(command: Command): CommittedCommandSlot<Command> {
  const slotRef = useRef<CommittedCommandSlot<Command>>({ lifecycle: 0, mounted: false });
  const slot = slotRef.current;

  // Ref callbacks run during React's mutation phase, before layout effects.
  // Insertion effects are the earliest committed phase and therefore publish
  // authority in time for ref-backed adapters without exposing render input.
  useInsertionEffect(() => {
    slot.command = command;
    slot.mounted = true;
  });
  useEffect(() => {
    slot.mounted = true;
    slot.lifecycle += 1;
    return () => {
      slot.mounted = false;
      const lifecycle = ++slot.lifecycle;
      queueMicrotask(() => {
        if (!slot.mounted && slot.lifecycle === lifecycle) slot.command = undefined;
      });
    };
  }, [slot]);
  return slot;
}

/**
 * Stable command entry point backed only by the latest committed input.
 *
 * The binding is created outside the component render scope so a long-lived
 * DOM listener cannot retain obsolete React presentation contexts. Before the
 * first commit, and after unmount, the command is deliberately inert.
 */
export function useCommittedCommand<Command extends AnyCommand>(command: Command): Command {
  const slot = useCommittedCommandSlot(command);
  // Existing DOM/component adapters keep their exact callback type. The slot
  // still returns no value at runtime after disposal, when no mounted consumer
  // is permitted to call it.
  return useMemo(() => bindCommittedCommand(slot), [slot]) as Command;
}
