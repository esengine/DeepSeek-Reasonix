export function guidanceNeedsRetry(state?: string): boolean {
  return state === "uncertain" || state === "blocked";
}

export function guidanceIsInFlight(state?: string): boolean {
	return state === "steer_accepted";
}

export function guidanceHasKnownPendingState(state?: string): boolean {
	return state === undefined || state === "" || state === "queued" || state === "blocked" || state === "uncertain" || state === "steer_accepted";
}

export function guidanceIsEditable(item: { id?: string; state?: string; source?: string; paused?: boolean }): boolean {
  if (!item.id || item.id.startsWith("local-")) return false;
  if (item.paused || guidanceIsInFlight(item.state) || guidanceNeedsRetry(item.state)) return false;
  return item.state === "queued" || item.state === undefined || item.state === "";
}

export function markGuidanceQueued<T extends { id: string; state?: string; paused?: boolean }>(items: T[], id: string): T[] {
  return items.map((item) => item.id === id ? { ...item, state: "queued", paused: false } : item);
}

// Resuming an already-unpaused inbox is an idempotent Controller drain kick.
export async function kickIdleGuidance(
  setPaused: (tabId: string, paused: boolean) => Promise<void>,
  tabId: string,
  refresh: () => void,
): Promise<void> {
  await setPaused(tabId, false);
  refresh();
}

// Exact equality avoids consuming a different queued item whose text merely
// contains the accepted steer (#6238).
export function guidanceTextMatches(queued: string, consumed: string): boolean {
  const left = queued.trim();
  const right = consumed.trim();
  return Boolean(left && right && left === right);
}
