import type { SessionState } from "./session_types";

export const IDLE = "空闲";
export const RUNNING = "运行中";

// A client that attached after turn_started has no running flag of its own and
// the status snapshot says the turn is live: print that, not the idle or
// settled word it last held. Anything the stream has said since stays.
export function chipLabel(s: SessionState, running: boolean): string {
  if (running && !s.running && (s.terminal || s.doing === IDLE)) return RUNNING;
  return s.doing || RUNNING;
}
