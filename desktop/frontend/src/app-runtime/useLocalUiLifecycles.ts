import { useEffect, useRef, useState, type Dispatch, type SetStateAction } from "react";
export type TopicTimeFilter = "all" | "10" | "20" | "1h" | "3h" | "5h" | "1d";

export function useTopicTimeFilter(): [TopicTimeFilter, Dispatch<SetStateAction<TopicTimeFilter>>] {
  const [value, setValue] = useState<TopicTimeFilter>(() => {
    try {
      const saved = localStorage.getItem("projectTree:timeFilter");
      if (saved === "all" || saved === "10" || saved === "20" || saved === "1h" || saved === "3h" || saved === "5h" || saved === "1d") return saved;
    } catch { /* localStorage unavailable */ }
    return "all";
  });
  useEffect(() => {
    try { localStorage.setItem("projectTree:timeFilter", value); } catch { /* ignore */ }
  }, [value]);
  return [value, setValue];
}

export function useDecisionSurfaceFocus(input: {
  surface: string | null;
  activeTabId?: string | null;
  closeOverlays: () => void;
  activeTabRef: { current: string | null | undefined };
}) {
  const { surface, activeTabId, closeOverlays, activeTabRef } = input;
  const previous = useRef<string | null>(null);
  const surfaceRef = useRef<string | null>(surface);
  surfaceRef.current = surface;
  useEffect(() => {
    if (surface) {
      closeOverlays();
      previous.current = surface;
      return;
    }
    const hadSurface = previous.current !== null;
    previous.current = null;
    if (!hadSurface) return;
    const tabAtRelease = activeTabId;
    const frame = requestAnimationFrame(() => {
      if (surfaceRef.current !== null || activeTabRef.current !== tabAtRelease) return;
      (document.getElementById("composer-input") as HTMLTextAreaElement | null)?.focus({ preventScroll: true });
    });
    return () => cancelAnimationFrame(frame);
  }, [activeTabId, activeTabRef, closeOverlays, surface]);
}

export function useActiveTabUiReset(input: {
  activeTabId?: string | null;
  setClearPending: (value: boolean) => void;
  setInsertTarget: (value: "composer") => void;
  activeTabRef: { current: string | null | undefined };
}) {
  const { activeTabId, activeTabRef, setClearPending, setInsertTarget } = input;
  useEffect(() => {
    activeTabRef.current = activeTabId;
    setClearPending(false);
    setInsertTarget("composer");
  }, [activeTabId, activeTabRef, setClearPending, setInsertTarget]);
}
