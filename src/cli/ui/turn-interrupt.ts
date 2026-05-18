export type TurnInterruptOutcome = "aborted" | "already-aborted" | "stopped-loop" | "idle";

export interface TurnInterruptController {
  turnActiveRef: { readonly current: boolean };
  abortedThisTurn: { current: boolean };
  resetPendingModals: () => void;
  isLoopActive: () => boolean;
  stopLoop: () => void;
  loop: { abort: () => void };
}

export function interruptCurrentTurn({
  turnActiveRef,
  abortedThisTurn,
  resetPendingModals,
  isLoopActive,
  stopLoop,
  loop,
}: TurnInterruptController): TurnInterruptOutcome {
  if (turnActiveRef.current) {
    if (abortedThisTurn.current) return "already-aborted";
    abortedThisTurn.current = true;
    resetPendingModals();
    if (isLoopActive()) stopLoop();
    loop.abort();
    return "aborted";
  }

  if (isLoopActive()) {
    stopLoop();
    return "stopped-loop";
  }

  return "idle";
}
