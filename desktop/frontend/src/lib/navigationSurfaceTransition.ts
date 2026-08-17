export type NavigationSurfaceIntent = number | null;

/** Older completions must never release the latest navigation surface mask. */
export function settleNavigationSurfaceIntent(
  current: NavigationSurfaceIntent,
  completedIntent: number,
): NavigationSurfaceIntent {
  return current === completedIntent ? null : current;
}

type BackendNavigationResultGuard = {
  intent: number;
  targetTabId: string;
  kind: string;
  isIntentCurrent: (intent: number) => boolean;
  reassert: (kind: string, staleTabId: string) => Promise<void>;
};

/**
 * Backend reveal calls activate their returned tab before resolving. A stale
 * frontend result therefore needs an active repair, not just an ignored value.
 */
export async function guardBackendNavigationResult({
  intent,
  targetTabId,
  kind,
  isIntentCurrent,
  reassert,
}: BackendNavigationResultGuard): Promise<boolean> {
  if (isIntentCurrent(intent)) return true;
  await reassert(kind, targetTabId);
  return false;
}
