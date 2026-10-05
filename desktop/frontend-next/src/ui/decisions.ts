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
  // A restored transcript says nothing about how its last turn ended, and
  // nothing is running or waiting on the reader: amber there told every
  // reopened session it was waiting on someone, with nothing to answer.
  if (f.terminal?.kind === "unread") return "idle";
  return f.terminal?.kind === "completed" ? "done" : "halt";
}

/** Which of the two readings the inspector is for.
 *
 *  Two postures and no more: a rail that reorders itself on every change of
 *  fact is not lifecycle-aware, it is unstable, and a reader who has to find
 *  the same panel twice in one turn has lost more than the ordering gave.
 *
 *  Derived from the run, never from what the panels happen to hold. "halt"
 *  answers two different questions — a turn waiting on a decision has not
 *  finished, and a turn that ended badly has — so blocked is asked separately
 *  rather than read back out of the state it already collapsed. */
export type Posture = "working" | "review";

export const posture = (run: RunState, blocked: boolean): Posture =>
  run === "running" || blocked ? "working" : "review";
