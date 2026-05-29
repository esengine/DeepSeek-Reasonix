export type TurnInterruptKey = "escape" | "ctrl-c";
export type TurnInterruptOutcome =
  | "aborted"
  | "already-aborted"
  | "stopped-loop"
  | "idle"
  | "quit-armed"
  | "quit";

/** 1.5-second window for double-Ctrl+C confirmation. */
const DOUBLE_PRESS_WINDOW_MS = 1500;

export interface TurnInterruptController {
  turnActiveRef: { readonly current: boolean };
  abortedThisTurn: { current: boolean };
  resetPendingModals: () => void;
  isLoopActive: () => boolean;
  stopLoop: () => void;
  loop: { abort: () => void };
  quitProcess: () => void;
  /** Timestamp of the last Ctrl+C press while idle; null if none or expired. */
  lastCtrlCAt: { current: number | null };
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
    lastCtrlCAt,
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
    if (lastCtrlCAt.current !== null && now - lastCtrlCAt.current < DOUBLE_PRESS_WINDOW_MS) {
      quitProcess();
      return "quit";
    }
    lastCtrlCAt.current = now;
    return "quit-armed";
  }

  return "idle";
}
