import { useCallback, useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent, type RefObject } from "react";
import type { Range } from "@tanstack/react-virtual";
import { createSelectionRangeExtractor, type TranscriptSelectionRowRange } from "./transcriptSelectionRange";
import {
  TRANSCRIPT_SELECTABLE_SELECTOR,
  TRANSCRIPT_ROW_SELECTOR,
  transcriptSelectionProjectionReadyForNode,
  transcriptSelectionPointFromClient,
  transcriptSelectionPointFromDom,
} from "./transcriptSelectionDom";
import { transcriptSelectionStore, type TranscriptSelectableRow } from "./transcriptSelectionStore";
import { mergeTranscriptSelectableRows } from "./transcriptSelectionText";
import type { TranscriptScrollMode, TranscriptScrollOwner, TranscriptViewportAnchor } from "./transcriptScrollController";

const EDGE_SCROLL_ZONE_PX = 48;
const EDGE_SCROLL_MIN_PX = 4;
const EDGE_SCROLL_MAX_PX = 24;

type TrackedSelection = {
  anchorKey: string;
  focusKey: string;
  dragging: boolean;
  logical: boolean;
  pointerId: number;
  captureElement: HTMLElement;
};

type CaretDocument = Document & {
  caretPositionFromPoint?: (x: number, y: number) => unknown;
  caretRangeFromPoint?: (x: number, y: number) => globalThis.Range | null;
};

function supportsCaretPoint(doc: Document): boolean {
  const caretDoc = doc as CaretDocument;
  return typeof caretDoc.caretPositionFromPoint === "function" || typeof caretDoc.caretRangeFromPoint === "function";
}

export function useTranscriptSelectionRetention({
  tabId,
  revealSignal,
  rowIndexByKey,
  selectableRows = [],
  selectableRowOverrides = [],
  scrollRef: providedScrollRef,
  setScrollMode,
  writeOffset = () => false,
  cancelStreamingScroll,
  captureViewportAnchor,
  reconcileViewportAnchor,
}: {
  tabId?: string;
  revealSignal: number;
  rowIndexByKey: ReadonlyMap<string, number>;
  selectableRows?: readonly TranscriptSelectableRow[];
  selectableRowOverrides?: readonly TranscriptSelectableRow[];
  scrollRef?: RefObject<HTMLDivElement | null>;
  setScrollMode: (mode: TranscriptScrollMode, reason?: string) => void;
  writeOffset?: (owner: TranscriptScrollOwner, top: number, behavior?: ScrollBehavior) => boolean;
  cancelStreamingScroll: () => void;
  captureViewportAnchor: () => TranscriptViewportAnchor | null;
  reconcileViewportAnchor: (snapshot: TranscriptViewportAnchor | null) => boolean;
}) {
  const fallbackScrollRef = useRef<HTMLDivElement>(null);
  const scrollRef = providedScrollRef ?? fallbackScrollRef;
  const selectionRef = useRef<TrackedSelection | null>(null);
  const [, setRevision] = useState(0);
  const viewportAnchorRef = useRef<TranscriptViewportAnchor | null>(null);
  const lifecycleGenerationRef = useRef(0);
  const settleFramesRef = useRef(new Set<number>());
  const focusFrameRef = useRef<number | null>(null);
  const edgeFrameRef = useRef<number | null>(null);
  const lastPointerRef = useRef<{ x: number; y: number } | null>(null);
  const rowsRef = useRef(selectableRows);
  const rowOverridesRef = useRef(selectableRowOverrides);
  rowsRef.current = selectableRows;
  rowOverridesRef.current = selectableRowOverrides;

  const publish = useCallback(() => setRevision((value) => value + 1), []);
  const cancelFrames = useCallback(() => {
    for (const frame of settleFramesRef.current) cancelAnimationFrame(frame);
    settleFramesRef.current.clear();
    if (focusFrameRef.current !== null) cancelAnimationFrame(focusFrameRef.current);
    if (edgeFrameRef.current !== null) cancelAnimationFrame(edgeFrameRef.current);
    focusFrameRef.current = null;
    edgeFrameRef.current = null;
  }, []);

  const releasePointerCapture = useCallback((tracked: TrackedSelection | null) => {
    if (!tracked) return;
    try {
      if (tracked.captureElement.hasPointerCapture(tracked.pointerId)) {
        tracked.captureElement.releasePointerCapture(tracked.pointerId);
      }
    } catch {
      // WebKit may drop capture before pointercancel/unmount.
    }
  }, []);

  const clear = useCallback((reason = "clear") => {
    const tracked = selectionRef.current;
    if (!tracked && transcriptSelectionStore.getSnapshot().mode === "none") return;
    lifecycleGenerationRef.current += 1;
    cancelFrames();
    releasePointerCapture(tracked);
    selectionRef.current = null;
    lastPointerRef.current = null;
    viewportAnchorRef.current = null;
    transcriptSelectionStore.clear(reason);
    setScrollMode("manual", reason);
    publish();
  }, [cancelFrames, publish, releasePointerCapture, setScrollMode]);

  const updateLogicalFocus = useCallback((pointer = lastPointerRef.current) => {
    const tracked = selectionRef.current;
    if (!tracked?.logical || !tracked.dragging || !pointer) return;
    const focus = transcriptSelectionPointFromClient(document, pointer.x, pointer.y);
    if (!focus) return;
    transcriptSelectionStore.updateLogicalFocus(focus);
    tracked.focusKey = focus.rowKey;
  }, []);

  const scheduleLogicalFocus = useCallback(() => {
    const tracked = selectionRef.current;
    if (!tracked?.logical || !tracked.dragging || focusFrameRef.current !== null) return;
    focusFrameRef.current = requestAnimationFrame(() => {
      focusFrameRef.current = requestAnimationFrame(() => {
        focusFrameRef.current = null;
        updateLogicalFocus();
      });
    });
  }, [updateLogicalFocus]);

  const edgeScrollTick = useCallback(() => {
    edgeFrameRef.current = null;
    const tracked = selectionRef.current;
    const pointer = lastPointerRef.current;
    const scroll = scrollRef.current;
    if (!tracked?.logical || !tracked.dragging || !pointer || !scroll) return;
    const rect = scroll.getBoundingClientRect();
    let speed = 0;
    if (pointer.y < rect.top + EDGE_SCROLL_ZONE_PX) {
      const ratio = Math.max(0, Math.min(1, (rect.top + EDGE_SCROLL_ZONE_PX - pointer.y) / EDGE_SCROLL_ZONE_PX));
      speed = -(EDGE_SCROLL_MIN_PX + ratio * (EDGE_SCROLL_MAX_PX - EDGE_SCROLL_MIN_PX));
    } else if (pointer.y > rect.bottom - EDGE_SCROLL_ZONE_PX) {
      const ratio = Math.max(0, Math.min(1, (pointer.y - (rect.bottom - EDGE_SCROLL_ZONE_PX)) / EDGE_SCROLL_ZONE_PX));
      speed = EDGE_SCROLL_MIN_PX + ratio * (EDGE_SCROLL_MAX_PX - EDGE_SCROLL_MIN_PX);
    }
    if (speed === 0) return;
    const max = Math.max(0, scroll.scrollHeight - scroll.clientHeight);
    const next = Math.max(0, Math.min(max, scroll.scrollTop + speed));
    if (next === scroll.scrollTop) {
      scheduleLogicalFocus();
      return;
    }
    if (!writeOffset("selection-edge-scroll", next)) return;
    scheduleLogicalFocus();
    edgeFrameRef.current = requestAnimationFrame(edgeScrollTick);
  }, [scheduleLogicalFocus, scrollRef, writeOffset]);

  const scheduleEdgeScroll = useCallback(() => {
    if (edgeFrameRef.current === null) edgeFrameRef.current = requestAnimationFrame(edgeScrollTick);
  }, [edgeScrollTick]);

  const onPointerDownCapture = useCallback((event: ReactPointerEvent<HTMLElement>) => {
    if (event.button !== 0) return;
    const target = event.target instanceof Element ? event.target : null;
    if (target?.closest("input, textarea, select, [contenteditable='true']")) return;
    const selectable = target?.closest(TRANSCRIPT_SELECTABLE_SELECTOR);
    if (!selectable) {
      document.getSelection()?.removeAllRanges();
      clear("new-pointer-outside-selection");
      return;
    }
    const anchorKey = selectable.closest<HTMLElement>(TRANSCRIPT_ROW_SELECTOR)?.dataset.rowKey;
    if (!anchorKey) return;
    clear("new-pointer-selection");
    lifecycleGenerationRef.current += 1;
    cancelStreamingScroll();
    viewportAnchorRef.current = captureViewportAnchor();
    selectionRef.current = {
      anchorKey,
      focusKey: anchorKey,
      dragging: true,
      logical: false,
      pointerId: event.pointerId,
      captureElement: event.currentTarget,
    };
    lastPointerRef.current = { x: event.clientX, y: event.clientY };
    transcriptSelectionStore.beginNative(tabId ?? "");
    setScrollMode("native-selecting", "pointerdown");
    publish();
  }, [cancelStreamingScroll, captureViewportAnchor, clear, publish, setScrollMode, tabId]);

  useEffect(() => {
    const onSelectionChange = () => {
      const tracked = selectionRef.current;
      if (!tracked || tracked.logical) return;
      const selection = document.getSelection();
      if (!selection || selection.isCollapsed) {
        if (!tracked.dragging) clear("selection-collapsed");
        return;
      }
      const anchor = transcriptSelectionPointFromDom(selection.anchorNode, selection.anchorOffset);
      const nativeFocus = transcriptSelectionPointFromDom(selection.focusNode, selection.focusOffset, lastPointerRef.current?.x);
      const pointer = lastPointerRef.current;
      const focus = nativeFocus ?? (pointer ? transcriptSelectionPointFromClient(document, pointer.x, pointer.y) : null);
      if (!anchor || !focus) return;
      tracked.anchorKey = anchor.rowKey;
      tracked.focusKey = focus.rowKey;
      transcriptSelectionStore.updateNativeRange(anchor, focus);
      if (anchor.rowKey === focus.rowKey || !supportsCaretPoint(document)) {
        publish();
        return;
      }
      if (
        !transcriptSelectionProjectionReadyForNode(selection.anchorNode)
        || !transcriptSelectionProjectionReadyForNode(selection.focusNode)
      ) {
        publish();
        return;
      }
      const snapshotId = transcriptSelectionStore.promoteToLogical(
        tabId ?? "",
        anchor,
        focus,
        mergeTranscriptSelectableRows(rowsRef.current, rowOverridesRef.current),
      );
      if (snapshotId == null) return;
      tracked.logical = true;
      selection.removeAllRanges();
      setScrollMode("logical-selecting", "cross-row-selection");
      try {
        tracked.captureElement.setPointerCapture(tracked.pointerId);
      } catch {
        // Native fallback remains available when pointer capture is rejected.
      }
      scheduleEdgeScroll();
      publish();
    };

    const onPointerMove = (event: PointerEvent) => {
      const tracked = selectionRef.current;
      if (!tracked?.dragging || event.pointerId !== tracked.pointerId) return;
      lastPointerRef.current = { x: event.clientX, y: event.clientY };
      if (!tracked.logical) return;
      scheduleLogicalFocus();
      scheduleEdgeScroll();
    };

    const finish = (event: PointerEvent) => {
      const tracked = selectionRef.current;
      if (event.button !== 0 || !tracked?.dragging || (event.pointerId !== tracked.pointerId && event.pointerId !== 0)) return;
      if (tracked.logical) {
        lastPointerRef.current = { x: event.clientX, y: event.clientY };
        if (focusFrameRef.current !== null) cancelAnimationFrame(focusFrameRef.current);
        focusFrameRef.current = null;
        if (edgeFrameRef.current !== null) cancelAnimationFrame(edgeFrameRef.current);
        edgeFrameRef.current = null;
        updateLogicalFocus(lastPointerRef.current);
        tracked.dragging = false;
        releasePointerCapture(tracked);
        transcriptSelectionStore.settleLogical();
        viewportAnchorRef.current = null;
        setScrollMode("manual", "logical-settled");
        publish();
        return;
      }
      tracked.dragging = false;
      releasePointerCapture(tracked);
      const selection = document.getSelection();
      if (!selection || selection.isCollapsed || selection.toString().trim() === "") {
        clear("empty-pointerup");
        return;
      }
      transcriptSelectionStore.settleNative();
      const settledSelection = { ...tracked };
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

    const cancelGesture = (event: PointerEvent) => {
      const tracked = selectionRef.current;
      if (!tracked?.dragging || (event.pointerId !== tracked.pointerId && event.pointerId !== 0)) return;
      document.getSelection()?.removeAllRanges();
      clear("pointercancel");
    };

    const onCopy = () => {
      const tracked = selectionRef.current;
      const selection = document.getSelection();
      if (!tracked || tracked.logical || tracked.dragging || !selection || selection.isCollapsed) return;
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

    const onScroll = () => {
      if (selectionRef.current?.logical) scheduleLogicalFocus();
    };
    const scroll = scrollRef.current;
    document.addEventListener("selectionchange", onSelectionChange);
    document.addEventListener("pointermove", onPointerMove);
    document.addEventListener("pointerup", finish);
    document.addEventListener("pointercancel", cancelGesture);
    document.addEventListener("copy", onCopy);
    document.addEventListener("keydown", onKeyDown);
    scroll?.addEventListener("scroll", onScroll, { passive: true });
    return () => {
      document.removeEventListener("selectionchange", onSelectionChange);
      document.removeEventListener("pointermove", onPointerMove);
      document.removeEventListener("pointerup", finish);
      document.removeEventListener("pointercancel", cancelGesture);
      document.removeEventListener("copy", onCopy);
      document.removeEventListener("keydown", onKeyDown);
      scroll?.removeEventListener("scroll", onScroll);
    };
  }, [clear, publish, reconcileViewportAnchor, releasePointerCapture, scheduleEdgeScroll, scheduleLogicalFocus, scrollRef, setScrollMode, tabId, updateLogicalFocus]);

  useEffect(() => {
    lifecycleGenerationRef.current += 1;
    cancelFrames();
    const tracked = selectionRef.current;
    releasePointerCapture(tracked);
    selectionRef.current = null;
    lastPointerRef.current = null;
    viewportAnchorRef.current = null;
    document.getSelection()?.removeAllRanges();
    transcriptSelectionStore.clear("transcript-generation-reset");
    if (tracked) publish();
  }, [cancelFrames, publish, releasePointerCapture, revealSignal, tabId]);

  useEffect(() => cancelFrames, [cancelFrames]);

  useEffect(() => transcriptSelectionStore.subscribe(() => {
    const tracked = selectionRef.current;
    if (!tracked?.logical || transcriptSelectionStore.getSnapshot().mode !== "none") return;
    lifecycleGenerationRef.current += 1;
    cancelFrames();
    releasePointerCapture(tracked);
    selectionRef.current = null;
    lastPointerRef.current = null;
    viewportAnchorRef.current = null;
    setScrollMode("manual", "logical-selection-cleared");
    publish();
  }), [cancelFrames, publish, releasePointerCapture, setScrollMode]);

  useEffect(() => {
    const tracked = selectionRef.current;
    if (!tracked) return;
    if (!rowIndexByKey.has(tracked.anchorKey) || !rowIndexByKey.has(tracked.focusKey)) {
      document.getSelection()?.removeAllRanges();
      clear("selection-endpoint-removed");
      return;
    }
    transcriptSelectionStore.validateRows(mergeTranscriptSelectableRows(selectableRows, rowOverridesRef.current));
  }, [clear, rowIndexByKey, selectableRows]);

  useEffect(() => {
    if (!selectionRef.current?.logical) return;
    transcriptSelectionStore.validateRowChanges(selectableRowOverrides);
  }, [selectableRowOverrides]);

  const rangeExtractor = useMemo(() => createSelectionRangeExtractor((): TranscriptSelectionRowRange | null => {
    const tracked = selectionRef.current;
    if (!tracked || tracked.logical) return null;
    const anchorIndex = rowIndexByKey.get(tracked.anchorKey);
    const focusIndex = rowIndexByKey.get(tracked.focusKey);
    return anchorIndex == null || focusIndex == null ? null : { anchorIndex, focusIndex };
  }), [rowIndexByKey]);

  return {
    clear,
    active: selectionRef.current !== null,
    logical: selectionRef.current?.logical ?? false,
    reconcileLogicalFocus: scheduleLogicalFocus,
    onPointerDownCapture,
    rangeExtractor: (range: Range) => rangeExtractor(range),
  };
}
