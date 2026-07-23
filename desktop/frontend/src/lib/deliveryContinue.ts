// Delivery recovery ("continue checks") flow for final_readiness notices.
//
// Ordinary Delivery turns carry no Goal, so the recovery prompt is submitted
// directly. A tab with a Goal must resume it first so its delivery scope and
// persisted checkpoint survive the continuation; a Goal that refuses to resume
// (completed, or the tab vanished) is not poked further. The active tab is
// re-checked after the async resume so a mid-flight tab switch cannot deliver
// the continuation into the wrong session.
export interface DeliveryContinueOptions {
  tabId: string | null | undefined;
  ready: boolean;
  goal: string | undefined;
  activeTabId: () => string | null | undefined;
  resumeGoal: (tabId: string) => Promise<boolean>;
  send: (tabId: string) => Promise<void>;
}

export async function continueDelivery(opts: DeliveryContinueOptions): Promise<void> {
  const { tabId, ready, goal } = opts;
  if (!tabId || !ready) return;
  if ((goal ?? "").trim()) {
    const resumed = await opts.resumeGoal(tabId);
    if (!resumed) return;
    if (opts.activeTabId() !== tabId) return;
  }
  await opts.send(tabId);
}
