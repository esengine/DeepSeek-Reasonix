import { useCallback, useRef } from "react";
import { app } from "./bridge";
import { asArray } from "./array";
import type { HistoryMessage } from "./types";
import type { SessionRef } from "./sessionRef";

export function useSessionCatalogActions(
  requireIntent: (seq: number) => Promise<void>,
  current: (seq: number) => boolean,
  sync: (reset: boolean, guard: boolean, options: { navigationIntentSeq: number; surfacePolicy: "replace-surface" }) => Promise<string | undefined>,
  invalidateCache: () => void,
) {
  const opening = useRef<{ intent: number; sessionId: string } | null>(null);
  const openCanonicalSession = useCallback(async (ref: SessionRef, navigationIntentSeq: number): Promise<void> => {
    await requireIntent(navigationIntentSeq);
    if (!current(navigationIntentSeq)) return;
    const owner = { intent: navigationIntentSeq, sessionId: ref.sessionId };
    opening.current = owner;
    try {
      await app.OpenSession(ref);
      if (!current(navigationIntentSeq)) return;
      // OpenSession publishes rebuilt/ready before its response. This intent
      // owns the single hydration; those notifications must not start a second
      // follower against the surface while its binding is still committing.
      await sync(true, false, { navigationIntentSeq, surfacePolicy: "replace-surface" });
    } finally {
      if (opening.current === owner) opening.current = null;
    }
  }, [requireIntent, current, sync]);
  const previewSession = useCallback((path: string): Promise<HistoryMessage[]> => app.PreviewSession(path).then(asArray<HistoryMessage>, () => []), []);
  const deleteSession = useCallback((path: string) => app.DeleteSession(path).finally(invalidateCache), [invalidateCache]);
  const restoreSession = useCallback((path: string) => app.RestoreSession(path).finally(invalidateCache), [invalidateCache]);
  const purgeTrashedSession = useCallback((path: string) => app.PurgeTrashedSession(path).finally(invalidateCache), [invalidateCache]);
  const renameSession = useCallback((path: string, title: string) => app.RenameSession(path, title).catch(() => {}).finally(invalidateCache), [invalidateCache]);
  return { openCanonicalSession, opening, previewSession, deleteSession, restoreSession, purgeTrashedSession, renameSession };
}
