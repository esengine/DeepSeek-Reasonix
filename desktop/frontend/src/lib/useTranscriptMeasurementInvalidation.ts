import { useCallback, useEffect, useRef, type MutableRefObject, type RefObject } from "react";
import type { Virtualizer } from "@tanstack/react-virtual";
import { readTranscriptLayoutSnapshot, type TranscriptLayoutSnapshot } from "./transcriptHeightCache";
import type { TranscriptViewportAnchor } from "./transcriptScrollController";

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
  canMeasure,
  onMeasureIdle,
  captureViewportAnchor,
  reconcileViewportAnchor,
}: {
  scrollRef: RefObject<HTMLDivElement | null>;
  layoutSnapshotRef: MutableRefObject<TranscriptLayoutSnapshot>;
  virtualizer: Virtualizer<HTMLDivElement, HTMLDivElement>;
  selectionActive: boolean;
  canMeasure: () => boolean;
  onMeasureIdle: (listener: () => void) => () => void;
  captureViewportAnchor: () => TranscriptViewportAnchor | null;
  reconcileViewportAnchor: (snapshot: TranscriptViewportAnchor | null) => boolean;
}) {
  const activeRef = useRef(selectionActive);
  const pendingRef = useRef(false);
  const reconcileFrameRef = useRef<number | null>(null);
  activeRef.current = selectionActive;

  const flushPending = useCallback(() => {
    if (!pendingRef.current || activeRef.current || !canMeasure()) return;
    pendingRef.current = false;
    const anchor = captureViewportAnchor();
    virtualizer.measure();
    if (reconcileFrameRef.current !== null) cancelAnimationFrame(reconcileFrameRef.current);
    reconcileFrameRef.current = requestAnimationFrame(() => {
      reconcileFrameRef.current = null;
      if (activeRef.current || !canMeasure()) {
        pendingRef.current = true;
        return;
      }
      reconcileViewportAnchor(anchor);
    });
  }, [canMeasure, captureViewportAnchor, reconcileViewportAnchor, virtualizer]);

  useEffect(() => {
    if (!selectionActive) flushPending();
  }, [flushPending, selectionActive]);

  useEffect(() => onMeasureIdle(flushPending), [flushPending, onMeasureIdle]);

  useEffect(() => () => {
    if (reconcileFrameRef.current !== null) cancelAnimationFrame(reconcileFrameRef.current);
  }, []);

  useEffect(() => {
    const element = scrollRef.current;
    if (!element) return;
    const initial = readTranscriptLayoutSnapshot(element);
    const initialChanged = initial.signature !== layoutSnapshotRef.current.signature;
    layoutSnapshotRef.current = initial;
    if (initialChanged) {
      pendingRef.current = true;
      flushPending();
    }
    const invalidateIfChanged = () => {
      const next = readTranscriptLayoutSnapshot(element);
      if (next.signature === layoutSnapshotRef.current.signature) return;
      layoutSnapshotRef.current = next;
      pendingRef.current = true;
      flushPending();
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
  }, [flushPending, layoutSnapshotRef, scrollRef]);
}
