import { memo, useCallback, useLayoutEffect, useRef, useState, useSyncExternalStore } from "react";
import { domRangeForTranscriptOffsets, projectTranscriptSelectableDom } from "../lib/transcriptSelectionDom";
import { transcriptSelectionStore } from "../lib/transcriptSelectionStore";

type SelectionRect = { left: number; top: number; width: number; height: number };

function sameRects(left: readonly SelectionRect[], right: readonly SelectionRect[]): boolean {
  return left.length === right.length && left.every((rect, index) => {
    const other = right[index];
    return other
      && Math.abs(rect.left - other.left) < 0.25
      && Math.abs(rect.top - other.top) < 0.25
      && Math.abs(rect.width - other.width) < 0.25
      && Math.abs(rect.height - other.height) < 0.25;
  });
}

export const TranscriptSelectionOverlay = memo(function TranscriptSelectionOverlay({
  tabId,
  scrollElement,
  virtualRevision,
}: {
  tabId: string;
  scrollElement: HTMLElement | null;
  virtualRevision: string;
}) {
  const snapshot = useSyncExternalStore(
    transcriptSelectionStore.subscribe,
    transcriptSelectionStore.getSnapshot,
    transcriptSelectionStore.getSnapshot,
  );
  const overlayRef = useRef<HTMLDivElement>(null);
  const frameRef = useRef<number | null>(null);
  const retryRef = useRef(0);
  const measureRef = useRef<() => void>(() => {});
  const [rects, setRects] = useState<SelectionRect[]>([]);

  const measure = useCallback(() => {
    frameRef.current = null;
    const overlay = overlayRef.current;
    const sizer = overlay?.parentElement;
    const current = transcriptSelectionStore.getSnapshot();
    if (
      !overlay
      || !sizer
      || current.tabId !== tabId
      || (current.mode !== "logical-dragging" && current.mode !== "logical-settled")
    ) {
      retryRef.current = 0;
      setRects((current) => current.length === 0 ? current : []);
      return;
    }
    const sizerRect = sizer.getBoundingClientRect();
    const next: SelectionRect[] = [];
    const roots = sizer.querySelectorAll<HTMLElement>(".transcript__row[data-row-key] [data-transcript-selectable]");
    for (const root of roots) {
      const rowKey = root.closest<HTMLElement>(".transcript__row[data-row-key]")?.dataset.rowKey;
      if (!rowKey) continue;
      const projection = projectTranscriptSelectableDom(root);
      const bounds = transcriptSelectionStore.rowBounds(current.id, rowKey, projection.text.length);
      if (!bounds || bounds.start === bounds.end) continue;
      const range = domRangeForTranscriptOffsets(root, bounds.start, bounds.end);
      if (!range || typeof range.getClientRects !== "function") continue;
      for (const rect of Array.from(range.getClientRects())) {
        if (rect.width <= 0 || rect.height <= 0) continue;
        next.push({
          left: rect.left - sizerRect.left,
          top: rect.top - sizerRect.top,
          width: rect.width,
          height: rect.height,
        });
      }
    }
    setRects((current) => sameRects(current, next) ? current : next);
    // Direct virtualizer DOM updates and browser layout can land after the RAF.
    // A bounded retry prevents a logical selection from staying invisible.
    if (next.length === 0 && retryRef.current < 4) {
      retryRef.current += 1;
      frameRef.current = requestAnimationFrame(() => measureRef.current());
      return;
    }
    retryRef.current = 0;
  }, [tabId]);
  measureRef.current = measure;

  const schedule = useCallback(() => {
    if (frameRef.current !== null) return;
    retryRef.current = 0;
    frameRef.current = requestAnimationFrame(measure);
  }, [measure]);

  useLayoutEffect(() => {
    schedule();
  }, [schedule, snapshot, virtualRevision]);

  useLayoutEffect(() => {
    const overlay = overlayRef.current;
    const sizer = overlay?.parentElement;
    if (!overlay || !sizer) return;
    const resize = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(schedule);
    resize?.observe(sizer);
    if (scrollElement) resize?.observe(scrollElement);
    const mutation = typeof MutationObserver === "undefined" ? null : new MutationObserver(schedule);
    mutation?.observe(sizer, { childList: true, subtree: true, characterData: true });
    scrollElement?.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule);
    return () => {
      resize?.disconnect();
      mutation?.disconnect();
      scrollElement?.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", schedule);
      if (frameRef.current !== null) cancelAnimationFrame(frameRef.current);
      frameRef.current = null;
    };
  }, [schedule, scrollElement]);

  return (
    <div ref={overlayRef} className="transcript-selection-overlay" aria-hidden="true">
      {rects.map((rect, index) => (
        <span
          key={`${index}:${rect.left}:${rect.top}`}
          className="transcript-selection-overlay__rect"
          style={rect}
        />
      ))}
    </div>
  );
});
