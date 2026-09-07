import { useEffect } from "react";

export type ActiveTabMirror = Readonly<{ current: string | undefined }>;

const mirror: { current: string | undefined } = { current: undefined };

/**
 * Layout-committed active tab mirror. The single AppRuntime host writes it
 * through useActiveTabMirrorCommit after each committed layout; readers are
 * event handlers and async continuations in app-runtime owners that must
 * never capture a stale render value. It is not a render input — presentation
 * keeps reading the reactive activeTabId.
 */
export function activeTabMirror(): ActiveTabMirror {
  return mirror;
}

export function useActiveTabMirrorCommit(activeTabId: string | undefined): void {
  useEffect(() => {
    mirror.current = activeTabId;
  }, [activeTabId]);
  useEffect(() => () => {
    mirror.current = undefined;
  }, []);
}
