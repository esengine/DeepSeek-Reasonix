import { useEffect } from "react";
import { onRemoteTabOpened, onRemoteTabUpdated } from "./bridge";
import type { TabMeta } from "./types";
import { createSubscriptionScope } from "./subscriptionScope";

export function useRemoteTabOpened(
  registerTabMeta: (tab: TabMeta) => void,
  updateTabMeta: (tab: TabMeta) => void,
) {
  useEffect(() => {
    const scope = createSubscriptionScope();
    scope.listen(onRemoteTabOpened, (meta) => {
      if (!meta?.id || !meta.remote) return;
      // Events are resource notifications. Only a request-owned navigation
      // completion may adopt the surface, even if this event arrives first.
      registerTabMeta(meta);
    });
    scope.listen(onRemoteTabUpdated, (meta) => {
      if (!meta?.id || !meta.remote) return;
      updateTabMeta(meta);
    });
    return () => scope.dispose();
  }, [registerTabMeta, updateTabMeta]);
}
