export type QQPendingInteraction = {
  gateId: number;
  kind: string;
  payload: unknown;
};

export type QQTurnRoutingState = {
  replyTabs: Set<string>;
  pendingByTab: Map<string, QQPendingInteraction>;
};

export function createQQTurnRoutingState(): QQTurnRoutingState {
  return {
    replyTabs: new Set<string>(),
    pendingByTab: new Map<string, QQPendingInteraction>(),
  };
}

export function markQQTurnStarted(state: QQTurnRoutingState, tabId: string): void {
  state.replyTabs.add(tabId);
}

export function markQQTurnFinished(state: QQTurnRoutingState, tabId: string): void {
  state.replyTabs.delete(tabId);
  state.pendingByTab.delete(tabId);
}

export function shouldRouteQQForTab(state: QQTurnRoutingState, tabId: string): boolean {
  return state.replyTabs.has(tabId);
}

export function setQQPendingInteraction(
  state: QQTurnRoutingState,
  tabId: string,
  gateId: number,
  kind: string,
  payload: unknown,
): void {
  if (!shouldRouteQQForTab(state, tabId)) return;
  state.pendingByTab.set(tabId, { gateId, kind, payload });
}

export function takeQQPendingInteraction(
  state: QQTurnRoutingState,
  tabId: string,
): QQPendingInteraction | null {
  const hit = state.pendingByTab.get(tabId);
  if (!hit) return null;
  state.pendingByTab.delete(tabId);
  return hit;
}

export function clearQQTurnRouting(state: QQTurnRoutingState): void {
  state.replyTabs.clear();
  state.pendingByTab.clear();
}
