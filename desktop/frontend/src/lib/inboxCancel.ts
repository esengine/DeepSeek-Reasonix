export type InboxCancelReceipt = {
  discardedItemIds: string[];
  warning?: string;
};

export type CancelOutcome = InboxCancelReceipt & {
  restoredText?: string;
};

type InboxCancelBridge = {
  CancelTab(tabId: string): Promise<void>;
  CancelTabWithInboxItems(tabId: string, itemIds: string[]): Promise<void>;
  CancelTabWithInboxItemsResult?(tabId: string, itemIds: string[]): Promise<InboxCancelReceipt>;
  InterruptTurnForTab?(tabId: string, turnId: string): Promise<void>;
  InterruptTurnWithInboxItemsForTab?(tabId: string, turnId: string, itemIds: string[]): Promise<InboxCancelReceipt>;
};

export async function requestInboxCancel(
  app: InboxCancelBridge,
  tabId: string,
  itemIds: string[],
  turnId?: string,
): Promise<InboxCancelReceipt> {
  if (turnId && itemIds.length > 0 && typeof app.InterruptTurnWithInboxItemsForTab === "function") {
    const receipt = await app.InterruptTurnWithInboxItemsForTab(tabId, turnId, itemIds);
    return {
      discardedItemIds: Array.isArray(receipt?.discardedItemIds) ? receipt.discardedItemIds.map(String) : [],
      warning: receipt?.warning?.trim() || undefined,
    };
  }
  if (turnId && itemIds.length === 0 && typeof app.InterruptTurnForTab === "function") {
    await app.InterruptTurnForTab(tabId, turnId);
    return { discardedItemIds: [] };
  }
  if (itemIds.length > 0 && typeof app.CancelTabWithInboxItemsResult === "function") {
    const receipt = await app.CancelTabWithInboxItemsResult(tabId, itemIds);
    return {
      discardedItemIds: Array.isArray(receipt?.discardedItemIds) ? receipt.discardedItemIds.map(String) : [],
      warning: receipt?.warning?.trim() || undefined,
    };
  }
  if (itemIds.length > 0) {
    // Compatibility fallback: an old backend has no per-item receipt, so
    // durable messages remain in the queue instead of returning to the draft.
    await app.CancelTabWithInboxItems(tabId, itemIds);
  } else {
    await app.CancelTab(tabId);
  }
  return { discardedItemIds: [] };
}
