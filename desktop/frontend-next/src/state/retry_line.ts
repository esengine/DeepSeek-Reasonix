import type { Waiting } from "./session_types";

export type RetryState = NonNullable<Waiting["retry"]>;

export type RetryPhase = { kind: "backoff"; nextSecs: number } | { kind: "waiting"; secs: number };

/** The kernel's notice fires when an attempt fails, so the first delayMs of it
 *  is the backoff sleep and the attempt's own wait begins after that. */
export function retryPhase(retry: RetryState, elapsedMs: number): RetryPhase {
  const delay = retry.delayMs ?? 0;
  if (elapsedMs < delay) return { kind: "backoff", nextSecs: Math.ceil((delay - elapsedMs) / 1000) };
  return { kind: "waiting", secs: (elapsedMs - delay) / 1000 };
}
