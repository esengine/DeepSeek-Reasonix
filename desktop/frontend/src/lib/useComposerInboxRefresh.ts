import { useCallback, useEffect, useLayoutEffect, useRef } from "react";
import { app } from "./bridge";
import {
  guidanceFromInboxSnapshot,
  hydrateEmptyGuidancePreviews,
  inboxSnapshotBelongsToScope,
  localGuidanceFallback,
  mergeGuidanceSnapshot,
} from "./composerInboxQueue";
import { onInboxChanged } from "./inboxEvents";
import type { PendingGuidance } from "../components/ComposerGuidanceShelf";

export function useComposerInboxRefresh(
  tabId: string | undefined,
  draftKey: string,
  guidanceDraftKey: string,
  inboxSessionKey: string,
  previewKey: string,
  retryNonce: number,
  running: boolean,
  applyQueue: (items: PendingGuidance[]) => void,
  collapse: () => void,
  bump: () => void,
) {
  const inboxSessionKeyRef = useRef(inboxSessionKey);
  const clearQueue = useCallback(() => applyQueue([]), [applyQueue]);
  useLayoutEffect(() => {
    if (inboxSessionKeyRef.current === inboxSessionKey) return;
    inboxSessionKeyRef.current = inboxSessionKey;
    clearQueue();
  }, [draftKey, inboxSessionKey, clearQueue]);
  useEffect(() => onInboxChanged((changedTab) => {
    if (changedTab && tabId && changedTab !== tabId) return;
    bump();
  }), [tabId, bump]);
  useEffect(() => {
    if (guidanceDraftKey !== draftKey) return;
    let live = true;
    const fallback = localGuidanceFallback(previewKey);
    if (typeof app.InboxSnapshot !== "function") {
      applyQueue(fallback);
      collapse();
      return;
    }
    void app.InboxSnapshot(tabId || "").then((snap) => {
      if (!live || !inboxSnapshotBelongsToScope(snap?.sessionPath, inboxSessionKey)) return;
      const durable = guidanceFromInboxSnapshot(snap);
      applyQueue(mergeGuidanceSnapshot(durable, fallback));
      collapse();
      void hydrateEmptyGuidancePreviews(durable, (id) => app.ReadInboxItem(tabId || "", id)).then((hydrated) => {
        if (live && hydrated.some((item) => item.text.trim())) {
          applyQueue(mergeGuidanceSnapshot(hydrated, fallback));
        }
      });
    }).catch(() => {
      if (live) applyQueue(localGuidanceFallback(previewKey));
    });
    return () => { live = false; };
  }, [draftKey, guidanceDraftKey, previewKey, running, tabId, retryNonce, inboxSessionKey, applyQueue, collapse]);
}
