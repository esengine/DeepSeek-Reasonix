import { useCallback, type Dispatch, type MutableRefObject, type SetStateAction } from "react";
import { app } from "./bridge";
import type { TabMeta } from "./types";

type RemoteTabSwitchOptions = {
  activeTabIdRef: MutableRefObject<string | undefined>;
  setActiveTabId: Dispatch<SetStateAction<string | undefined>>;
  beginNavigation: () => number;
  navigationCanComplete: (seq: number, kind: string, tabId: string) => boolean;
  navigationIsCurrent: (seq: number) => boolean;
  confirmBackendActiveTab: (tabId: string) => void;
  reassertVisibleTab: (kind: string, staleTabId: string) => Promise<void>;
};

// Remote surfaces hydrate from Serve events, not the local session history API.
export function useRemoteTabSwitch(options: RemoteTabSwitchOptions) {
  const {
    activeTabIdRef, setActiveTabId, beginNavigation, navigationCanComplete,
    navigationIsCurrent, confirmBackendActiveTab, reassertVisibleTab,
  } = options;
  return useCallback(async (meta: TabMeta, navigationIntentSeq?: number): Promise<void> => {
    const tabId = meta.id;
    const navigationSeq = navigationIntentSeq ?? beginNavigation();
    if (!meta.remote || !navigationCanComplete(navigationSeq, "tab.switch-remote", tabId)) return;
    const previousTabId = activeTabIdRef.current;
    setActiveTabId(tabId);
    activeTabIdRef.current = tabId;
    try {
      await app.SetActiveTab(tabId);
      if (!navigationIsCurrent(navigationSeq) || activeTabIdRef.current !== tabId) {
        await reassertVisibleTab("tab.switch-remote", tabId);
        return;
      }
      confirmBackendActiveTab(tabId);
    } catch (error) {
      if (navigationIsCurrent(navigationSeq) && activeTabIdRef.current === tabId && previousTabId) {
        setActiveTabId(previousTabId);
        activeTabIdRef.current = previousTabId;
      }
      throw error;
    }
  }, [activeTabIdRef, beginNavigation, confirmBackendActiveTab, navigationCanComplete, navigationIsCurrent, reassertVisibleTab, setActiveTabId]);
}
