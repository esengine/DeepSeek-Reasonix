import { useLayoutEffect, useMemo, useRef } from "react";
import { useCommittedSlot, type CommittedSlot } from "../lib/useCommittedSlot";
import { CommandCancelled, executeCapturedCommand, type CommandOutcome } from "../lib/commandOutcome";
import { createOperationOwner, operationTargetsEqual, type OperationTarget } from "./operationOwner";
import { createSessionSurfaceFence, type SessionSurfaceOwnership } from "./sessionTarget";
import { trackAppOperation } from "./appLifecycleProbe";

export type SessionResource = Readonly<{ tabId: string; sessionKey: string }>;
export type SessionOperationAuthority = {
  checkpoint(): void;
  ownsUI(): boolean;
};
type Input = { visible: SessionResource; resources?: readonly OperationTarget[] };
type State = {
  owner: ReturnType<typeof createOperationOwner>;
  surface: ReturnType<typeof createSessionSurfaceFence>;
  epoch: number;
};

function authorityFor(state: State, slot: CommittedSlot<Input>, target: OperationTarget, channel: string) {
  const epoch = slot.epoch;
  state.epoch = state.owner.mount();
  const identity = state.owner.begin(target, undefined, JSON.stringify([target, channel]));
  const surface: SessionSurfaceOwnership | undefined = state.surface.capture();
  const authority: SessionOperationAuthority = {
    checkpoint() {
      if (slot.phase !== "ready" || epoch !== slot.epoch) throw new CommandCancelled("disposed");
      if (!state.owner.owns(identity) || (slot.value?.resources && !slot.value.resources.some(resource => operationTargetsEqual(resource, target)))) {
        throw new CommandCancelled("superseded");
      }
    },
    ownsUI() {
      try { this.checkpoint(); } catch { return false; }
      return Boolean(surface && state.surface.owns(surface) && (target.kind !== "session" || (surface.tabId === target.tabId && surface.sessionKey === target.sessionKey)));
    },
  };
  return { identity, authority };
}

// Stable entry is created outside render. The executor receives no capture callback.
function bindOperations(state: State, slot: CommittedSlot<Input>) {
  return async <Input, Result>(
    target: OperationTarget, channel: string, input: Input,
    execute: (input: Input, authority: SessionOperationAuthority) => Result,
  ): Promise<CommandOutcome<Awaited<Result>>> => {
    if (slot.phase !== "ready" || (target.kind === "session" && !target.tabId)) return { status: "cancelled", reason: slot.phase === "disposed" ? "disposed" : "not-ready" };
    const { identity, authority } = authorityFor(state, slot, Object.freeze({ ...target }), channel);
    const result = await executeCapturedCommand(input, execute, authority);
    const uiOwned = authority.ownsUI();
    state.owner.finish(identity, result.status);
    return result.status === "failed" && !uiOwned ? { status: "cancelled", reason: "superseded" } : result;
  };
}

/** A source request may finish on A while B is visible; only its UI rights expire. */
export function useResourceOperations(input: Input) {
  const slot = useCommittedSlot(input);
  const stateRef = useRef<State | null>(null);
  if (!stateRef.current) stateRef.current = { owner: createOperationOwner(trackAppOperation), surface: createSessionSurfaceFence(), epoch: 0 };
  const state = stateRef.current;
  useLayoutEffect(() => { state.surface.commit(input.visible.tabId, input.visible.sessionKey); }, [input.visible.tabId, input.visible.sessionKey, state]);
  useLayoutEffect(() => () => {
    state.surface.dispose();
    state.owner.unmount(state.epoch);
  }, [state]);
  return useMemo(() => bindOperations(state, slot), [state, slot]);
}
