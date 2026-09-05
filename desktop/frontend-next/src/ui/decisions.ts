import type { SessionStatus } from "../port/session";
import type { TurnTerminal } from "../state/session_types";

/** hasPendingDecision answers whether this turn is waiting on the person.
 *
 *  The condition is the list's length, not what is in it: Decisions() is
 *  defined as what is still unanswered, and all four kinds need a person. Which
 *  kind it is says who answers and is what an explanation would render — this
 *  only says that someone has to.
 *
 *  Absent is false, never a fallback. A kernel that does not send the field
 *  leaves this window unable to say anyone is waiting, which is the honest
 *  shape; quietly reading the old display label instead would put the
 *  presentation back in charge of the verdict with nothing saying so. */
export function hasPendingDecision(status: SessionStatus | null | undefined): boolean {
  return (status?.decisions?.length ?? 0) > 0;
}

/** RunState is what the window says this session is doing, as the chrome and
 *  the composer both read it. */
export type RunState = "idle" | "running" | "halt" | "done";

/** runState takes facts, not labels. A turn waiting on a person is not a turn
 *  in motion — the glow says "it is moving" and has to go out, and the action
 *  slot goes back to send so you can talk.
 *
 *  Only a delivery claims the tick. A turn that failed, that was stopped, or
 *  that ended because its obligations were not met all stay amber: the label
 *  this replaced read the last two as finished, because it only ever asked
 *  whether an error had been attached. */
export function runState(f: { blocked: boolean; running: boolean; hasItems: boolean; terminal: TurnTerminal }): RunState {
  if (f.blocked) return "halt";
  if (f.running) return "running";
  if (!f.hasItems) return "idle";
  return f.terminal?.kind === "completed" ? "done" : "halt";
}
