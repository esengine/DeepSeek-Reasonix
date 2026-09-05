import { useEffect, useInsertionEffect, useMemo, useRef } from "react";
import type { CommandOutcome } from "./useCommittedCommand";

type AsyncCommand<Args extends unknown[], Result> = (...args: Args) => Result;
type AsyncCommandSlot<Args extends unknown[], Result> = {
  command?: AsyncCommand<Args, Result>;
  lifecycle: number;
  requestId: number;
  mounted: boolean;
  disposed: boolean;
};

async function invokeCommittedAsync<Args extends unknown[], Result>(
  slot: AsyncCommandSlot<Args, Result>,
  args: Args,
): Promise<CommandOutcome<Awaited<Result>>> {
  const command = slot.command;
  if (!command || !slot.mounted) {
    return { status: "cancelled", reason: slot.disposed ? "disposed" : "not-ready" };
  }
  const lifecycle = slot.lifecycle;
  const requestId = ++slot.requestId;
  try {
    const value = await command(...args);
    if (!slot.mounted || lifecycle !== slot.lifecycle) return { status: "cancelled", reason: "disposed" };
    if (requestId !== slot.requestId) return { status: "cancelled", reason: "superseded" };
    return { status: "completed", value: value as Awaited<Result> };
  } catch (error) {
    if (!slot.mounted || lifecycle !== slot.lifecycle) return { status: "cancelled", reason: "disposed" };
    if (requestId !== slot.requestId) return { status: "cancelled", reason: "superseded" };
    return { status: "failed", error };
  }
}

function bindCommittedAsync<Args extends unknown[], Result>(slot: AsyncCommandSlot<Args, Result>) {
  return (...args: Args) => invokeCommittedAsync(slot, args);
}

/** Async command authority with explicit terminal outcomes and last-call ownership. */
export function useCommittedAsyncCommand<Args extends unknown[], Result>(
  command: AsyncCommand<Args, Result>,
) {
  const slotRef = useRef<AsyncCommandSlot<Args, Result>>({
    lifecycle: 0,
    requestId: 0,
    mounted: false,
    disposed: false,
  });
  const slot = slotRef.current;
  useInsertionEffect(() => {
    slot.command = command;
    slot.mounted = true;
    slot.disposed = false;
  });
  useEffect(() => {
    slot.mounted = true;
    slot.disposed = false;
    slot.lifecycle += 1;
    return () => {
      slot.mounted = false;
      slot.disposed = true;
      slot.lifecycle += 1;
      slot.requestId += 1;
      const lifecycle = slot.lifecycle;
      queueMicrotask(() => {
        if (!slot.mounted && slot.lifecycle === lifecycle) slot.command = undefined;
      });
    };
  }, [slot]);
  return useMemo(() => bindCommittedAsync(slot), [slot]);
}
