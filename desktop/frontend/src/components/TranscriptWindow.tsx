import { TranscriptPresentationProvider } from "./TranscriptPresentationContext";
import { useNativeViewportSnapshot } from "../lib/useTranscriptNativeViewport";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import type { LogicalAnchor, TranscriptKernel } from "../lib/transcriptKernel";
import type { ProjectionViewProps } from "./TranscriptProjectionView";
import { TranscriptMeasurementLedger } from "../lib/transcriptMeasurementLedger";
import type { TimelineBlock, TimelineProjection } from "../lib/transcriptTimeline";
import { extractTranscriptWindowIndexes } from "../lib/transcriptWindowRange";
import { commitTranscriptWindowGeometry, findTranscriptMeasurementPublicationBoundary, MAX_MOUNTED_COMPLETED_BLOCKS, type TranscriptWindowGeometry } from "../lib/transcriptWindowGeometry";

const ANCHOR_MEASUREMENT_RADIUS = 4;
// Keep enough mounted runway for native engines whose scroll event can arrive
// ahead of TanStack's next range calculation. The browser fixtures enforce the
// corresponding 40-block upper bound.

export default function TranscriptWindow({
  projection,
  scrollElement,
  onGeometryChange,
  onGeometryWillChange,
  protectedBlockKeys,
  kernel,
  pinnedJumpBlockKey,
  onPinnedJumpVisible,
  estimateBlock,
  renderProjection,
  forceFull,
}: {
  projection: TimelineProjection;
  scrollElement: HTMLDivElement | null;
  onGeometryChange: (covered?: boolean, beforePaint?: boolean) => void;
  onGeometryWillChange: (anchor?: LogicalAnchor) => unknown;
  protectedBlockKeys: ReadonlySet<string>;
  kernel: Pick<TranscriptKernel, "anchor" | "generation" | "intent" | "userGestureActive" | "afterCurrentGenerationPaint">;
  pinnedJumpBlockKey?: string;
  onPinnedJumpVisible: () => void;
  estimateBlock: (block: TimelineBlock) => number;
  renderProjection: (layout: Pick<ProjectionViewProps, "blocks" | "placements" | "extent" | "spacerRef" | "tailRef" | "mode" | "safety" | "completedCount" | "revision">) => ReactNode;
  forceFull: boolean;
}) {
  const minimumResidentIndex = Math.max(0, projection.completedBlocks.length - 2);
  const minimumResidentKey = projection.completedBlocks[minimumResidentIndex]?.key;
  const [residentStartKey, setResidentStartKey] = useState<string | undefined>(minimumResidentKey);
  const currentResidentIndex = residentStartKey
    ? projection.completedBlocks.findIndex((block) => block.key === residentStartKey)
    : -1;
  const residentStartIndex = currentResidentIndex >= 0 ? Math.min(currentResidentIndex, minimumResidentIndex) : minimumResidentIndex;
  const split = useMemo(() => ({
    cold: projection.completedBlocks.slice(0, residentStartIndex),
    resident: projection.completedBlocks.slice(residentStartIndex),
  }), [projection.completedBlocks, residentStartIndex]);
  const coldMountBudget = Math.max(0, MAX_MOUNTED_COMPLETED_BLOCKS - split.resident.length);
  const coldIndexByKey = useMemo(() => new Map(split.cold.map((block, index) => [block.key, index])), [split.cold]);
  const retainedIndexes = new Set<number>();
  const retainKey = (key: string | undefined, radius = 0) => {
    const index = key ? coldIndexByKey.get(key) : undefined;
    if (index == null) return;
    for (let candidate = Math.max(0, index - radius); candidate <= Math.min(split.cold.length - 1, index + radius); candidate += 1) retainedIndexes.add(candidate);
  };
  protectedBlockKeys.forEach((key) => retainKey(key));
  const focusedBlock = document.activeElement instanceof Element
    ? document.activeElement.closest<HTMLElement>("[data-transcript-block-key]")?.dataset.transcriptBlockKey
    : undefined;
  retainKey(focusedBlock);
  retainKey(kernel.anchor.kind === "block" ? kernel.anchor.blockKey : undefined, ANCHOR_MEASUREMENT_RADIUS);
  retainKey(pinnedJumpBlockKey, ANCHOR_MEASUREMENT_RADIUS);
  const coldContainerRef = useRef<HTMLDivElement>(null);
  const residentTailRef = useRef<HTMLDivElement>(null);
  const measurementLedgerRef = useRef<TranscriptMeasurementLedger | null>(null);
  if (!measurementLedgerRef.current) measurementLedgerRef.current = new TranscriptMeasurementLedger();
  const measurementLedger = measurementLedgerRef.current;
  // Native scrolling is an external store. React verifies this immutable
  // snapshot immediately before commit, so a concurrent render calculated at
  // an old compositor offset cannot replace the currently covering range.
  const nativeViewport = useNativeViewportSnapshot(scrollElement, kernel);
  const coldContainer = coldContainerRef.current;
  const scrollMargin = coldContainer && scrollElement
    ? coldContainer.getBoundingClientRect().top - scrollElement.getBoundingClientRect().top + nativeViewport.scrollTop
    : 0;
  const virtualizer = useVirtualizer({
    count: split.cold.length,
    getScrollElement: () => scrollElement,
    estimateSize: (index) => {
      const block = split.cold[index];
      return measurementLedger.sizeFor(block.key, estimateBlock(block));
    },
    getItemKey: (index) => split.cold[index].key,
    overscan: 0,
    // The window adapter owns the DOM-to-ledger commit below. TanStack still
    // observes stable item identities, but cannot publish ResizeObserver
    // measurements independently of the kernel's native-gesture boundary.
    useCachedMeasurements: true,
    rangeExtractor: (range) => extractTranscriptWindowIndexes(range, retainedIndexes, coldMountBudget, nativeViewport.direction),
    scrollMargin,
    scrollToFn: () => {},
  });
  virtualizer.shouldAdjustScrollPositionOnItemSizeChange = () => false;
  // Materialize TanStack's prefix-size ledger before reading either its
  // asynchronous candidate range or the synchronous recovery input.
  const totalSize = virtualizer.getTotalSize();
  // Newly materialized DOM is measured before its first paint. A single
  // window origin preserves already-visible blocks when those new sizes
  // refine the prefix above them, without writing during native input.
  const materializedElements = useRef(new WeakSet<Element>());
  const windowOrigin = useRef(0);
  const materializationAnchor = useRef<{ generation: number; key?: string; tailOffset?: number; top: number } | null>(null);
  const materializationGeneration = useRef(kernel.generation);
  if (materializationGeneration.current !== kernel.generation) {
    materializationGeneration.current = kernel.generation;
    materializedElements.current = new WeakSet();
    windowOrigin.current = 0;
    materializationAnchor.current = null;
  }
  const pendingAnchor = materializationAnchor.current;
  const anchorStart = pendingAnchor?.key
    ? virtualizer.measurementsCache[coldIndexByKey.get(pendingAnchor.key) ?? -1]?.start
    : pendingAnchor?.tailOffset != null ? scrollMargin + totalSize + pendingAnchor.tailOffset : undefined;
  const refinedOrigin = pendingAnchor?.generation === kernel.generation && anchorStart != null
    ? pendingAnchor.top - anchorStart : windowOrigin.current;
  // Consume the temporary origin continuously as native travel approaches
  // the leading edge. Clearing it only at zero would create a discontinuity;
  // carrying it through zero would make the first content unreachable.
  const origin = Math.max(-nativeViewport.scrollTop, Math.min(nativeViewport.scrollTop, refinedOrigin));
  const candidateItems = virtualizer.getVirtualItems();
  const committedGeometryRef = useRef<TranscriptWindowGeometry<(typeof candidateItems)[number]> | undefined>(undefined);
  const pendingMeasurementCommit = useRef(false);
  const measurementNeedsRender = useRef(false);
  const structureRevision = `${split.cold.length}:${split.cold[0]?.key ?? ""}:${split.cold[split.cold.length - 1]?.key ?? ""}`;
  const geometry = commitTranscriptWindowGeometry({
    candidate: candidateItems,
    measurements: virtualizer.measurementsCache,
    retainedIndexes,
    previous: committedGeometryRef.current,
    residentCount: split.resident.length,
    forceFull,
    structureRevision,
    scrollTop: nativeViewport.scrollTop - origin,
    clientHeight: nativeViewport.clientHeight,
    scrollHeight: nativeViewport.scrollHeight,
    scrollMargin,
    totalSize,
    maxItems: coldMountBudget,
    direction: nativeViewport.direction,
    gestureActive: kernel.userGestureActive,
    measurementCommit: pendingMeasurementCommit.current,
  });
  const committedRange = geometry.range;
  const virtualItems = committedRange.items;
  const fullDOMFallback = geometry.mode === "full";
  const logicalAnchorIndex = kernel.anchor.kind === "block"
    ? coldIndexByKey.get(kernel.anchor.blockKey)
    : undefined;
  const rangeRevision = `${origin}:${committedRange.scrollMargin}:${committedRange.totalSize}|${virtualItems.map((item) => `${String(item.key)}:${item.start}:${item.size}`).join("|")}`;

  useLayoutEffect(() => {
    committedGeometryRef.current = geometry;
    windowOrigin.current = origin;
    materializationAnchor.current = null;
  }, [geometry, origin]);
  useLayoutEffect(() => {
    if (!minimumResidentKey || currentResidentIndex >= 0) return;
    setResidentStartKey(minimumResidentKey);
  }, [currentResidentIndex, minimumResidentKey]);
  useLayoutEffect(() => {
    const validKeys = new Set(projection.completedBlocks.map((block) => block.key));
    measurementLedger.retain(validKeys);
  }, [measurementLedger, projection.completedBlocks]);
  useLayoutEffect(() => {
    if (residentStartIndex >= minimumResidentIndex || !scrollElement) return;
    const viewport = scrollElement.getBoundingClientRect();
    const elements = new Map(Array.from(scrollElement.querySelectorAll<HTMLElement>("[data-transcript-block-key]"))
      .map((element) => [element.dataset.transcriptBlockKey ?? "", element]));
    const residentChanges: Array<{ key: string; size: number }> = [];
    let nextResidentIndex = residentStartIndex;
    while (nextResidentIndex < minimumResidentIndex) {
      const block = projection.completedBlocks[nextResidentIndex];
      const element = elements.get(block.key);
      const ownsAnchor = kernel.anchor.kind === "block" && kernel.anchor.blockKey === block.key;
      if (!element || ownsAnchor || element.contains(document.activeElement) || protectedBlockKeys.has(block.key)) break;
      const rect = element.getBoundingClientRect();
      if (rect.bottom >= viewport.top - scrollElement.clientHeight) break;
      residentChanges.push({ key: block.key, size: Math.max(64, rect.height || element.offsetHeight) });
      nextResidentIndex += 1;
    }
    if (nextResidentIndex === residentStartIndex) return;
    // Resident-to-cold transfer is one identity-preserving geometry commit.
    // Every leaving block has an exact size before React removes its in-flow
    // DOM, so the virtual prefix replaces the resident prefix without a
    // transient extent change for the native scroller.
    measurementLedger.commit(residentChanges);
    setResidentStartKey(projection.completedBlocks[nextResidentIndex]?.key ?? minimumResidentKey);
  }, [kernel.anchor, measurementLedger, minimumResidentIndex, minimumResidentKey, nativeViewport.scrollTop, projection.completedBlocks, protectedBlockKeys, residentStartIndex, scrollElement]);
  useEffect(() => {
    if (!pinnedJumpBlockKey || !scrollElement) return;
    const target = Array.from(scrollElement.querySelectorAll<HTMLElement>("[data-transcript-block-key]"))
      .find((element) => element.dataset.transcriptBlockKey === pinnedJumpBlockKey);
    if (!target) return;
    const viewport = scrollElement.getBoundingClientRect();
    const rect = target.getBoundingClientRect();
    if (rect.bottom >= viewport.top && rect.top <= viewport.bottom) onPinnedJumpVisible();
  }, [onPinnedJumpVisible, pinnedJumpBlockKey, rangeRevision, scrollElement]);
  const surfaceGeneration = kernel.generation;
  const [measurementRevision, setMeasurementRevision] = useState(0);
  const presentationChanged = useCallback(() => setMeasurementRevision(value => value + 1), []);
  const presentation = useMemo(() => ({ gestureActive: kernel.userGestureActive, windowed: true, geometryChanged: presentationChanged }),
    [kernel.userGestureActive, presentationChanged]);
  useLayoutEffect(() => {
    const container = residentTailRef.current;
    if (!container || typeof ResizeObserver === "undefined") return;
    const generation = kernel.generation;
    let disposed = false;
    let cancelFrame: (() => void) | undefined;
    const observer = new ResizeObserver(() => {
      if (disposed || generation !== kernel.generation || cancelFrame) return;
      cancelFrame = kernel.afterCurrentGenerationPaint(() => {
        cancelFrame = undefined;
        if (!disposed && generation === kernel.generation) setMeasurementRevision(revision => revision + 1);
      });
    });
    // Absolute children do not resize the projection root. Observe the actual
    // mounted blocks so local folds and deferred Markdown invalidate geometry.
    container.querySelectorAll(".transcript__window-item").forEach(element => observer.observe(element));
    return () => { disposed = true; observer.disconnect(); cancelFrame?.(); };
  }, [fullDOMFallback, kernel, rangeRevision, surfaceGeneration]);
  const measuredItems = fullDOMFallback ? geometry.prefix.items : virtualItems;
  useLayoutEffect(() => {
    measurementNeedsRender.current = false;
    const container = residentTailRef.current;
    const changes: Array<{ key: string; size: number }> = [];
    const firstMeasurements = new Set<string>();
    const viewport = scrollElement?.getBoundingClientRect();
    const observedTop = scrollElement?.scrollTop ?? nativeViewport.scrollTop;
    const clientHeight = scrollElement?.clientHeight ?? nativeViewport.clientHeight;
    const domItems: Array<{ index: number; top: number }> = [];
    const blocks = Array.from(container?.querySelectorAll<HTMLElement>("[data-transcript-block-key]") ?? []);
    const visible = blocks.filter(element => {
      const rect = element.getBoundingClientRect();
      return viewport && rect.bottom > viewport.top + 0.5 && rect.top < viewport.top + clientHeight;
    });
    const common = visible.find(element => materializedElements.current.has(element));
    const commonKey = common?.dataset.transcriptBlockKey;
    const commonItem = commonKey ? measuredItems.find(item => String(item.key) === commonKey) : undefined;
    // The committed prefix is independent of compositor progress. Rebuilding
    // a cold content coordinate from separately sampled DOMRect/scrollTop
    // would let a native advance between those reads become an origin error.
    const commonTop = commonItem ? commonItem.start + origin
      : common && viewport ? common.getBoundingClientRect().top - viewport.top + (scrollElement?.scrollTop ?? observedTop) : undefined;
    // Origin removal converts the already-painted coordinate system. Choose
    // from its committed prefix, never from DOM heights that have just changed.
    const committedVisible = measuredItems.find(item => item.start + origin + item.size > observedTop + 0.5
      && item.start + origin < observedTop + clientHeight);
    const originAnchor: LogicalAnchor | undefined = committedVisible
      ? { kind: "block", blockKey: String(committedVisible.key), offsetPx: observedTop - (committedVisible.start + origin) }
      : undefined;
    if (container) {
      for (const item of measuredItems) {
        const element = container.querySelector<HTMLElement>(`.transcript__window-item[data-index="${item.index}"]`);
        if (!element) continue;
        if (!materializedElements.current.has(element)) firstMeasurements.add(String(item.key));
        const rect = element.getBoundingClientRect();
        if (viewport) domItems.push({ index: item.index, top: rect.top - viewport.top });
        const size = Math.max(64, rect.height || element.offsetHeight);
        changes.push({ key: String(item.key), size });
      }
    }
    measurementLedger.stage(changes);
    // Input leases own intent and writer exclusion, not a second size queue.
    // Only current painted/DOM geometry can decide which future rows are safe.
    // Re-read native progress at publication; a render's snapshot can be older.
    const publicationTop = Math.max(observedTop, scrollElement?.scrollTop ?? observedTop);
    const measurementBoundaryIndex = findTranscriptMeasurementPublicationBoundary({
      paintedItems: measuredItems.map(item => ({ ...item, start: item.start + origin })), domItems, scrollTop: publicationTop, clientHeight,
      anchorIndex: logicalAnchorIndex,
    });
    const published = measurementLedger.publishStaged((key) => {
      const index = coldIndexByKey.get(key);
      return index != null && (firstMeasurements.has(key) || (kernel.intent === "reader" && (
        !kernel.userGestureActive
        || (measurementBoundaryIndex != null && index >= measurementBoundaryIndex)
      )));
    });
    blocks.forEach(element => materializedElements.current.add(element));
    const releaseOrigin = !kernel.userGestureActive && Math.abs(origin) > 0.5;
    if (published.length > 0 || releaseOrigin) {
      measurementNeedsRender.current = true;
      if (!kernel.userGestureActive) {
        // Ordinary content growth belongs to the input-captured anchor;
        // a newly enlarged preceding DOM block must not replace that owner.
        onGeometryWillChange(releaseOrigin ? originAnchor : undefined);
        windowOrigin.current = 0;
      } else if (common && commonTop != null && published.some(change => firstMeasurements.has(change.key))) {
        const key = common.dataset.transcriptBlockKey!;
        materializationAnchor.current = coldIndexByKey.has(key)
          ? { generation: kernel.generation, key, top: commonTop }
          : { generation: kernel.generation, tailOffset: commonTop - (scrollMargin + totalSize + origin), top: commonTop };
      }
      pendingMeasurementCommit.current = true;
      // Feed only the atomically published batch into TanStack's keyed size
      // cache. `measure()` is intentionally forbidden here: it clears that
      // cache and rebuilds the entire prefix, allowing previously committed
      // off-screen measurements to reflow the current native viewport. These
      // synchronous resize notifications complete in one browser task, so
      // React can expose only the final prefix snapshot to paint.
      for (const change of published) {
        const index = coldIndexByKey.get(change.key);
        if (index != null) virtualizer.resizeItem(index, change.size);
      }
      // A layout-effect state update closes the batch before paint; do not
      // depend on TanStack's asynchronous notification scheduling. Geometry
      // acknowledges this same batch instead of retaining the older prefix.
      setMeasurementRevision(revision => revision + 1);
      return;
    }
  }, [coldIndexByKey, fullDOMFallback, kernel.intent, kernel.userGestureActive, logicalAnchorIndex, measuredItems, measurementLedger, measurementRevision, nativeViewport.clientHeight, nativeViewport.scrollTop, onGeometryChange, onGeometryWillChange, projection.activeBlock?.measurementRevision, rangeRevision, scrollElement, split.resident, virtualItems, virtualizer, origin, scrollMargin, totalSize]);

  useLayoutEffect(() => {
    // Estimates are preparation, not trustworthy painted geometry. Only
    // acknowledge after every first-materialization size has entered the
    // same prefix; otherwise tail correction/health checks see an intermediate
    // extent and can latch safety while this commit is still measuring it.
    if (measurementNeedsRender.current) return;
    const beforePaint = geometry.measurementCommitted;
    if (beforePaint) pendingMeasurementCommit.current = false;
    onGeometryChange(geometry.covered, beforePaint);
  }, [geometry, onGeometryChange]);

  // Safety disables range eviction, not the last trustworthy prefix. Reflowing
  // every cold estimate into natural DOM would move a held reader without any
  // writer command. Keep those coordinates and mount ALL cold blocks instead.
  const prefix = fullDOMFallback ? geometry.prefix
    : { items: virtualItems, extent: committedRange.totalSize, margin: committedRange.scrollMargin };
  const mounted = fullDOMFallback ? projection.completedBlocks
    : [...virtualItems.map((item) => split.cold[item.index]), ...split.resident];
  const placements = new Map(prefix.items.map((item) => [String(item.key),
    { index: item.index, top: item.start - prefix.margin + origin }]));
  return <TranscriptPresentationProvider value={presentation}>{renderProjection({ blocks: [...mounted, ...(projection.activeBlock ? [projection.activeBlock] : [])],
    placements, extent: Math.max(0, prefix.extent + origin),
    spacerRef: coldContainerRef, tailRef: residentTailRef, mode: fullDOMFallback ? "full" : "windowed",
    safety: fullDOMFallback, completedCount: projection.completedBlocks.length,
    revision: `${fullDOMFallback}:${rangeRevision}:${projection.activeBlock?.measurementRevision}` })}</TranscriptPresentationProvider>;
}
