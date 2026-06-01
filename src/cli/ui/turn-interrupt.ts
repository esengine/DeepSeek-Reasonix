export type TurnInterruptKey = "escape" | "ctrl-c";
export type TurnInterruptOutcome =
  | "aborted"
  | "already-aborted"
  | "stopped-loop"
  | "idle"
  | "quit"
  | "quit-armed";

/** ms window in which a second Ctrl+C confirms the exit intent. */
export const QUIT_ARM_WINDOW_MS = 1500;

export interface TurnInterruptController {
  turnActiveRef: { readonly current: boolean };
  abortedThisTurn: { current: boolean };
  resetPendingModals: () => void;
  isLoopActive: () => boolean;
  stopLoop: () => void;
  loop: { abort: () => void };
  quitProcess: () => void;
  /** Tracks when the quit intent was first armed; null = not armed. */
  quitArmedAt: { current: number | null };
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
    quitArmedAt,
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
    const now = Date.now();
    if (quitArmedAt.current !== null && now - quitArmedAt.current <= QUIT_ARM_WINDOW_MS) {
      quitProcess();
      return "quit";
    }
    quitArmedAt.current = now;
    return "quit-armed";
  }

  return "idle";
}
