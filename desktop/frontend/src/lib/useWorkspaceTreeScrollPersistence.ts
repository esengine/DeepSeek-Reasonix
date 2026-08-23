import { useCallback, useEffect, type RefObject, type UIEvent } from "react";
import { flushWorkspaceTreeMemory, rememberWorkspaceTreeScroll } from "./workspaceTreeMemory";

export function useWorkspaceTreeScrollPersistence<T extends HTMLElement>({
  memoryKey,
  open,
  scrollRef,
}: {
  memoryKey: string;
  open: boolean;
  scrollRef: RefObject<T | null>;
}) {
  useEffect(() => {
    if (!open) return;
    const element = scrollRef.current;
    const flush = () => flushWorkspaceTreeMemory();
    const flushWhenHidden = () => {
      if (document.visibilityState === "hidden") flush();
    };
    element?.addEventListener("scrollend", flush);
    window.addEventListener("pagehide", flush);
    document.addEventListener("visibilitychange", flushWhenHidden);
    return () => {
      element?.removeEventListener("scrollend", flush);
      window.removeEventListener("pagehide", flush);
      document.removeEventListener("visibilitychange", flushWhenHidden);
      flush();
    };
  }, [memoryKey, open, scrollRef]);

  return useCallback((event: UIEvent<T>) => {
    // Only user-initiated scrolling is persisted: tree rebuilds reset the
    // container to scrollTop 0 via script (isTrusted=false), and persisting
    // that 0 would clobber the saved offset so restoration jumps back to top.
    if (!event.nativeEvent.isTrusted) return;
    rememberWorkspaceTreeScroll(memoryKey, event.currentTarget.scrollTop);
  }, [memoryKey]);
}
