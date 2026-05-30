export type TurnInterruptKey = "escape" | "ctrl-c";
export type TurnInterruptOutcome = "aborted" | "already-aborted" | "stopped-loop" | "idle" | "quit";
export type BusySubmitDecision =
  | "idle"
  | "ignore"
  | "reject-command"
  | "queue-after-abort"
  | "steer";

export interface BusySubmitState {
  busy: boolean;
  submitting: boolean;
  aborted: boolean;
  isCommand: (text: string) => boolean;
}

export interface TurnInterruptController {
  turnActiveRef: { readonly current: boolean };
  abortedThisTurn: { current: boolean };
  resetPendingModals: () => void;
  isLoopActive: () => boolean;
  stopLoop: () => void;
  loop: { abort: () => void };
  quitProcess: () => void;
}

export function handleTurnInterrupt(
  key: TurnInterruptKey,
  {
    turnActiveRef,
    abortedThisTurn,
    resetPendingModals,
    isLoopActive,
    stopLoop,
    loop,
    quitProcess,
  }: TurnInterruptController,
): TurnInterruptOutcome {
  if (turnActiveRef.current) {
    if (abortedThisTurn.current) return "already-aborted";
    abortedThisTurn.current = true;
    resetPendingModals();
    if (isLoopActive()) stopLoop();
    loop.abort();
    return "aborted";
  }

  if (key === "escape" && isLoopActive()) {
    stopLoop();
    return "stopped-loop";
  }

  if (key === "ctrl-c") {
    quitProcess();
    return "quit";
  }

  return "idle";
}

export function decideBusySubmit(text: string, state: BusySubmitState): BusySubmitDecision {
  if (!state.busy && !state.submitting) return "idle";
  const trimmed = text.trim();
  if (!trimmed) return "ignore";
  if (state.isCommand(trimmed)) return "reject-command";
  if (state.aborted) return "queue-after-abort";
  if (!state.busy) return "ignore";
  return "steer";
}
