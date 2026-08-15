import { useEffect, type RefObject } from "react";

const STALE_TURN_RECONCILE_MS = 30_000;

export type StaleTurnWatchdogState = {
  running: boolean;
  turnStartAt: number;
};

export function shouldReconcileStaleTurn(
  state: StaleTurnWatchdogState | undefined,
  lastTurnActivityAt: number,
  now = Date.now(),
  timeoutMs = STALE_TURN_RECONCILE_MS,
): boolean {
  const lastEvidenceAt = lastTurnActivityAt > 0 ? lastTurnActivityAt : state?.turnStartAt ?? 0;
  if (!state?.running || lastEvidenceAt <= 0) return false;
  return Math.max(0, now - lastEvidenceAt) >= timeoutMs;
}

export function useStaleTurnWatchdog<T extends StaleTurnWatchdogState>({
  tabId,
  visibleState,
  activeTabIdRef,
  statesRef,
  lastTurnActivityAtByTab,
  reconcile,
}: {
  tabId?: string;
  visibleState: StaleTurnWatchdogState;
  activeTabIdRef: RefObject<string | undefined>;
  statesRef: RefObject<Map<string, T>>;
  lastTurnActivityAtByTab: RefObject<Map<string, number>>;
  reconcile: (tabId: string) => Promise<void>;
}) {
  useEffect(() => {
    if (!tabId) return;
    let cancelled = false;
    let timer: number | undefined;

    const schedule = (delay?: number) => {
      if (cancelled || activeTabIdRef.current !== tabId) return;
      const current = statesRef.current.get(tabId);
      const lastActivityAt = lastTurnActivityAtByTab.current.get(tabId) ?? 0;
      const lastEvidenceAt = lastActivityAt > 0 ? lastActivityAt : current?.turnStartAt ?? 0;
      if (!current?.running || lastEvidenceAt <= 0) return;
      const nextDelay = delay ?? Math.max(0, STALE_TURN_RECONCILE_MS - Math.max(0, Date.now() - lastEvidenceAt));
      timer = window.setTimeout(() => { void tick(); }, nextDelay);
    };

    const tick = async () => {
      if (cancelled || activeTabIdRef.current !== tabId) return;
      const current = statesRef.current.get(tabId);
      const lastActivityAt = lastTurnActivityAtByTab.current.get(tabId) ?? 0;
      if (!shouldReconcileStaleTurn(current, lastActivityAt)) {
        schedule();
        return;
      }
      await reconcile(tabId);
      if (!cancelled && activeTabIdRef.current === tabId && statesRef.current.get(tabId)?.running) {
        schedule(STALE_TURN_RECONCILE_MS);
      }
    };

    schedule();
    return () => {
      cancelled = true;
      if (timer !== undefined) window.clearTimeout(timer);
    };
  }, [activeTabIdRef, lastTurnActivityAtByTab, reconcile, statesRef, tabId, visibleState.running, visibleState.turnStartAt]);
}
