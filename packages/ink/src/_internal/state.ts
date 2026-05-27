/** Per-process "last user activity" timestamp. */

let lastInteractionTime = 0;

/** Mark "real user interaction just happened". */
export function updateLastInteractionTime(): void {
  lastInteractionTime = Date.now();
}

/** Reset the timestamp. */
export function flushInteractionTime(): void {
  lastInteractionTime = 0;
}

/** Mark scroll input as user activity. */
export function markScrollActivity(): void {
  lastInteractionTime = Date.now();
}

export function getLastInteractionTime(): number {
  return lastInteractionTime;
}
