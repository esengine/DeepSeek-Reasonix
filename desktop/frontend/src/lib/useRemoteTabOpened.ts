import { useEffect, type MutableRefObject } from "react";
import { onRemoteTabOpened, onRemoteTabUpdated } from "./bridge";
import type { TabMeta } from "./types";

export function useRemoteTabOpened(
  activeTabIdRef: MutableRefObject<string | undefined>,
  seedActiveTabMeta: (tab: TabMeta) => void,
  updateTabMeta: (tab: TabMeta) => void,
  switchRemoteTab: (tab: TabMeta) => Promise<unknown>,
) {
  useEffect(() => {
    const off = onRemoteTabOpened((meta) => {
      if (!meta?.id || !meta.remote) return;
      seedActiveTabMeta(meta);
      if (activeTabIdRef.current !== meta.id) void switchRemoteTab(meta);
    });
    const offUpdated = onRemoteTabUpdated((meta) => {
      if (!meta?.id || !meta.remote) return;
      updateTabMeta(meta);
    });
    return () => {
      off();
      offUpdated();
    };
  }, [activeTabIdRef, seedActiveTabMeta, switchRemoteTab, updateTabMeta]);
}
