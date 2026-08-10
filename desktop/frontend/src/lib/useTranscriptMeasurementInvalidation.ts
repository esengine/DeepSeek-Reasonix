import { useEffect, useRef, type MutableRefObject, type RefObject } from "react";
import type { Virtualizer } from "@tanstack/react-virtual";
import { readTranscriptLayoutSnapshot, type TranscriptLayoutSnapshot } from "./transcriptHeightCache";

/**
 * Invalidates TanStack's in-memory measurements when transcript width or root
 * typography changes. Active native selections defer the reset so a layout
 * refresh cannot compensate scrollTop in the middle of a drag.
 */
export function useTranscriptMeasurementInvalidation({
  scrollRef,
  layoutSnapshotRef,
  virtualizer,
  selectionActive,
}: {
  scrollRef: RefObject<HTMLDivElement | null>;
  layoutSnapshotRef: MutableRefObject<TranscriptLayoutSnapshot>;
  virtualizer: Virtualizer<HTMLDivElement, HTMLDivElement>;
  selectionActive: boolean;
}) {
  const activeRef = useRef(selectionActive);
  const pendingRef = useRef(false);
  activeRef.current = selectionActive;

  useEffect(() => {
    if (selectionActive || !pendingRef.current) return;
    pendingRef.current = false;
    virtualizer.measure();
  }, [selectionActive, virtualizer]);

  useEffect(() => {
    const element = scrollRef.current;
    if (!element) return;
    const initial = readTranscriptLayoutSnapshot(element);
    const initialChanged = initial.signature !== layoutSnapshotRef.current.signature;
    layoutSnapshotRef.current = initial;
    if (initialChanged) {
      if (activeRef.current) pendingRef.current = true;
      else virtualizer.measure();
    }
    const invalidateIfChanged = () => {
      const next = readTranscriptLayoutSnapshot(element);
      if (next.signature === layoutSnapshotRef.current.signature) return;
      layoutSnapshotRef.current = next;
      if (activeRef.current) {
        pendingRef.current = true;
        return;
      }
      virtualizer.measure();
    };
    const resizeObserver = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(invalidateIfChanged);
    resizeObserver?.observe(element);
    const mutationObserver = typeof MutationObserver === "undefined" ? null : new MutationObserver(invalidateIfChanged);
    if (document.documentElement) mutationObserver?.observe(document.documentElement, { attributes: true, attributeFilter: ["class", "style"] });
    if (document.body) mutationObserver?.observe(document.body, { attributes: true, attributeFilter: ["class", "style"] });
    window.addEventListener("resize", invalidateIfChanged);
    return () => {
      resizeObserver?.disconnect();
      mutationObserver?.disconnect();
      window.removeEventListener("resize", invalidateIfChanged);
    };
  }, [layoutSnapshotRef, scrollRef, virtualizer]);
}
