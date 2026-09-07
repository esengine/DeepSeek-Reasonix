import { app } from "./bridge";
import type { BalanceInfo } from "./types";

type Ref<T> = { current: T };
type Ports = {
  statesRef: Ref<ReadonlyMap<string, { balance?: BalanceInfo }>>;
  modelSwitchSeqByTab: Ref<Map<string, number>>;
  modelSwitchSuccessVersionByTab: Ref<Map<string, number>>;
  modelSwitchQueueByTab: Ref<ReadonlyMap<string, { fallbackBalance?: BalanceInfo }>>;
  enqueueModelSwitch: (tabId: string, name: string, balance?: BalanceInfo) => Promise<"applied" | "superseded">;
  clearBalanceForTab: (tabId: string) => void;
  dispatchTo: (tabId: string, action: { type: "local_notice"; level: "warn"; text: string } | { type: "balance"; balance: BalanceInfo }) => void;
  refreshBalanceForTab: (tabId: string) => Promise<unknown>;
  refreshMetaForTab: (tabId: string) => Promise<void>;
};

/** Uses Controller-owned queues and stores; never resolves an active tab after await. */
export function createControllerModelCommands(ports: Ports) {
  const { statesRef, modelSwitchSeqByTab, modelSwitchSuccessVersionByTab, modelSwitchQueueByTab,
    enqueueModelSwitch, clearBalanceForTab, dispatchTo, refreshBalanceForTab, refreshMetaForTab } = ports;
  const setModelForTab = async (tabId: string, name: string) => {
    if (!tabId) return false;
    const switchSeq = (modelSwitchSeqByTab.current.get(tabId) ?? 0) + 1;
    const successVersion = modelSwitchSuccessVersionByTab.current.get(tabId) ?? 0;
    const existingQueue = modelSwitchQueueByTab.current.get(tabId);
    // Every attempt in one queued burst shares the balance that was visible
    // before the first switch cleared it. Otherwise a later queued failure
    // captures the placeholder and cannot restore the outgoing provider.
    const fallbackBalance = existingQueue
      ? existingQueue.fallbackBalance
      : statesRef.current.get(tabId)?.balance;
    modelSwitchSeqByTab.current.set(tabId, switchSeq);
    // Hide the outgoing provider's wallet as soon as the user starts a hot
    // switch. If the rebuild fails, the catch path re-queries the still-active
    // provider and restores its balance.
    clearBalanceForTab(tabId);
    try {
      const result = await enqueueModelSwitch(tabId, name, fallbackBalance);
      if (result === "superseded") return false;
      modelSwitchSuccessVersionByTab.current.set(
        tabId,
        (modelSwitchSuccessVersionByTab.current.get(tabId) ?? 0) + 1,
      );
    } catch (err) {
      if (modelSwitchSeqByTab.current.get(tabId) !== switchSeq) return false;
      const { modelSwitchNoticeText } = await import("./controllerSwitchNotices");
      if (modelSwitchSeqByTab.current.get(tabId) !== switchSeq) return false;
      dispatchTo(tabId, { type: "local_notice", level: "warn", text: modelSwitchNoticeText(err) });
      const olderSwitchSucceeded =
        (modelSwitchSuccessVersionByTab.current.get(tabId) ?? 0) !== successVersion;
      // Restore the known balance only when no older overlapping switch
      // completed after this attempt began. Otherwise the backend now owns a
      // different provider and the refresh below must establish its balance.
      if (fallbackBalance && !olderSwitchSucceeded) {
        dispatchTo(tabId, { type: "balance", balance: fallbackBalance });
      }
      void refreshBalanceForTab(tabId);
      // A superseded success deliberately skips its own UI reconciliation.
      // If this latest queued switch then fails, reconcile the model metadata
      // to the provider that actually became active in the backend.
      if (olderSwitchSucceeded) await refreshMetaForTab(tabId);
      return false;
    }
    if (modelSwitchSeqByTab.current.get(tabId) !== switchSeq) return false;
    void refreshBalanceForTab(tabId);
    await refreshMetaForTab(tabId);
    return modelSwitchSeqByTab.current.get(tabId) === switchSeq;
  };

  const setEffortForTab = async (tabId: string, level: string) => {
    if (!tabId) return;
    try {
      await app.SetEffortForTab(tabId, level);
    } catch (err) {
      const { effortSwitchNoticeText } = await import("./controllerSwitchNotices");
      dispatchTo(tabId, { type: "local_notice", level: "warn", text: effortSwitchNoticeText(err) });
      return;
    }
    await refreshMetaForTab(tabId);
  };


  return { setModelForTab, setEffortForTab };
}
