import { useCallback, useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import type { Range } from "@tanstack/react-virtual";
import { createSelectionRangeExtractor, type TranscriptSelectionRowRange } from "./transcriptSelectionRange";
import type { TranscriptScrollMode } from "./transcriptScrollController";
import type { TranscriptViewportAnchor } from "./transcriptScrollController";

const SELECTABLE_SELECTOR = "[data-transcript-selectable]";
const ROW_SELECTOR = ".transcript__row[data-row-key]";

function elementForNode(node: Node | null): Element | null {
  if (!node) return null;
  return node.nodeType === Node.ELEMENT_NODE ? node as Element : node.parentElement;
}

function rowKeyForNode(node: Node | null): string | null {
  return elementForNode(node)?.closest<HTMLElement>(ROW_SELECTOR)?.dataset.rowKey ?? null;
}

export function useTranscriptSelectionRetention({
  tabId,
  revealSignal,
  rowIndexByKey,
  setScrollMode,
  cancelStreamingScroll,
  captureViewportAnchor,
  reconcileViewportAnchor,
}: {
  tabId?: string;
  revealSignal: number;
  rowIndexByKey: ReadonlyMap<string, number>;
  setScrollMode: (mode: TranscriptScrollMode, reason?: string) => void;
  cancelStreamingScroll: () => void;
  captureViewportAnchor: () => TranscriptViewportAnchor | null;
  reconcileViewportAnchor: (snapshot: TranscriptViewportAnchor | null) => boolean;
}) {
  const selectionRef = useRef<{ anchorKey: string; focusKey: string; dragging: boolean } | null>(null);
  const [, setRevision] = useState(0);
  const viewportAnchorRef = useRef<TranscriptViewportAnchor | null>(null);
  const lifecycleGenerationRef = useRef(0);
  const settleFramesRef = useRef(new Set<number>());

  const publish = useCallback(() => setRevision((value) => value + 1), []);
  const cancelSettleFrames = useCallback(() => {
    for (const frame of settleFramesRef.current) cancelAnimationFrame(frame);
    settleFramesRef.current.clear();
  }, []);
  const clear = useCallback((reason = "clear") => {
    if (!selectionRef.current) return;
    lifecycleGenerationRef.current += 1;
    cancelSettleFrames();
    selectionRef.current = null;
    viewportAnchorRef.current = null;
    setScrollMode("manual", reason);
    publish();
  }, [cancelSettleFrames, publish, setScrollMode]);

  const onPointerDownCapture = useCallback((event: ReactPointerEvent<HTMLElement>) => {
    if (event.button !== 0) return;
    const target = event.target instanceof Element ? event.target : null;
    const selectable = target?.closest(SELECTABLE_SELECTOR);
    if (!selectable) {
      clear("new-pointer-outside-selection");
      return;
    }
    const anchorKey = selectable.closest<HTMLElement>(ROW_SELECTOR)?.dataset.rowKey;
    if (!anchorKey) return;
    lifecycleGenerationRef.current += 1;
    cancelSettleFrames();
    cancelStreamingScroll();
    viewportAnchorRef.current = captureViewportAnchor();
    selectionRef.current = { anchorKey, focusKey: anchorKey, dragging: true };
    setScrollMode("native-selecting", "pointerdown");
    publish();
  }, [cancelSettleFrames, cancelStreamingScroll, captureViewportAnchor, clear, publish, setScrollMode]);

  useEffect(() => {
    const onSelectionChange = () => {
      const tracked = selectionRef.current;
      if (!tracked) return;
      const selection = document.getSelection();
      if (!selection || selection.isCollapsed) {
        if (!tracked.dragging) clear("selection-collapsed");
        return;
      }
      const anchorKey = rowKeyForNode(selection.anchorNode);
      const focusKey = rowKeyForNode(selection.focusNode);
      if (!anchorKey || !focusKey) return;
      if (tracked.anchorKey === anchorKey && tracked.focusKey === focusKey) return;
      selectionRef.current = { ...tracked, anchorKey, focusKey };
      publish();
    };
    const finish = (event: PointerEvent) => {
      if (event.button !== 0 || !selectionRef.current?.dragging) return;
      const selection = document.getSelection();
      if (!selection || selection.isCollapsed || selection.toString().trim() === "") {
        clear("empty-pointerup");
        return;
      }
      const settledSelection = { ...selectionRef.current, dragging: false };
      const generation = lifecycleGenerationRef.current;
      selectionRef.current = settledSelection;
      publish();
      const outerFrame = requestAnimationFrame(() => {
        settleFramesRef.current.delete(outerFrame);
        const innerFrame = requestAnimationFrame(() => {
          settleFramesRef.current.delete(innerFrame);
          if (generation !== lifecycleGenerationRef.current || selectionRef.current !== settledSelection) return;
          reconcileViewportAnchor(viewportAnchorRef.current);
          viewportAnchorRef.current = null;
          setScrollMode("manual", "native-selection-settled");
        });
        settleFramesRef.current.add(innerFrame);
      });
      settleFramesRef.current.add(outerFrame);
    };
    const onCopy = () => {
      const tracked = selectionRef.current;
      const selection = document.getSelection();
      if (!tracked || tracked.dragging || !selection || selection.isCollapsed) return;
      const generation = lifecycleGenerationRef.current;
      const anchorNode = selection.anchorNode;
      const anchorOffset = selection.anchorOffset;
      const focusNode = selection.focusNode;
      const focusOffset = selection.focusOffset;
      const frame = requestAnimationFrame(() => {
        settleFramesRef.current.delete(frame);
        const current = document.getSelection();
        if (
          generation !== lifecycleGenerationRef.current
          || !current
          || current.isCollapsed
          || current.anchorNode !== anchorNode
          || current.anchorOffset !== anchorOffset
          || current.focusNode !== focusNode
          || current.focusOffset !== focusOffset
        ) return;
        current.removeAllRanges();
        clear("copy");
      });
      settleFramesRef.current.add(frame);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      document.getSelection()?.removeAllRanges();
      clear("escape");
    };
    document.addEventListener("selectionchange", onSelectionChange);
    document.addEventListener("pointerup", finish);
    document.addEventListener("pointercancel", finish);
    document.addEventListener("copy", onCopy);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("selectionchange", onSelectionChange);
      document.removeEventListener("pointerup", finish);
      document.removeEventListener("pointercancel", finish);
      document.removeEventListener("copy", onCopy);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [clear, publish, reconcileViewportAnchor, setScrollMode]);

  useEffect(() => {
    lifecycleGenerationRef.current += 1;
    cancelSettleFrames();
    const hadSelection = selectionRef.current !== null;
    selectionRef.current = null;
    viewportAnchorRef.current = null;
    document.getSelection()?.removeAllRanges();
    if (hadSelection) publish();
  }, [cancelSettleFrames, publish, revealSignal, tabId]);

  useEffect(() => cancelSettleFrames, [cancelSettleFrames]);

  useEffect(() => {
    const tracked = selectionRef.current;
    if (!tracked) return;
    if (!rowIndexByKey.has(tracked.anchorKey) || !rowIndexByKey.has(tracked.focusKey)) {
      document.getSelection()?.removeAllRanges();
      clear("selection-endpoint-removed");
    }
  }, [clear, rowIndexByKey]);

  const rangeExtractor = useMemo(() => createSelectionRangeExtractor((): TranscriptSelectionRowRange | null => {
    const tracked = selectionRef.current;
    if (!tracked) return null;
    const anchorIndex = rowIndexByKey.get(tracked.anchorKey);
    const focusIndex = rowIndexByKey.get(tracked.focusKey);
    return anchorIndex == null || focusIndex == null ? null : { anchorIndex, focusIndex };
  }), [rowIndexByKey]);

  return {
    clear,
    active: selectionRef.current !== null,
    onPointerDownCapture,
    rangeExtractor: (range: Range) => rangeExtractor(range),
  };
}
