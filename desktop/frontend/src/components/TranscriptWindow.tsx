import { useVirtualizer } from "@tanstack/react-virtual";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import type { TranscriptKernel } from "../lib/transcriptKernel";
import type { ProjectionViewProps } from "./TranscriptProjectionView";
import { TranscriptMeasurementLedger } from "../lib/transcriptMeasurementLedger";
import type { TimelineBlock, TimelineProjection } from "../lib/transcriptTimeline";
import { extractTranscriptWindowIndexes, type TranscriptWindowDirection } from "../lib/transcriptWindowRange";
import { commitTranscriptWindowGeometry, MAX_MOUNTED_COMPLETED_BLOCKS, type TranscriptWindowGeometry } from "../lib/transcriptWindowGeometry";

const ANCHOR_MEASUREMENT_RADIUS = 4;
// Keep enough mounted runway for native engines whose scroll event can arrive
// ahead of TanStack's next range calculation. The browser fixtures enforce the
// corresponding 40-block upper bound.

type NativeViewportSnapshot = {
  scrollTop: number;
  clientHeight: number;
  scrollHeight: number;
  direction: TranscriptWindowDirection;
};

function useNativeViewportSnapshot(element: HTMLElement | null, kernel: Pick<TranscriptKernel, "generation">): NativeViewportSnapshot {
  const cachedRef = useRef<NativeViewportSnapshot>({ scrollTop: 0, clientHeight: 0, scrollHeight: 0, direction: null });
  const getSnapshot = useCallback(() => {
    const scrollTop = element?.scrollTop ?? 0;
    const clientHeight = element?.clientHeight ?? 0;
    const scrollHeight = element?.scrollHeight ?? 0;
    const cached = cachedRef.current;
    if (Object.is(cached.scrollTop, scrollTop) && Object.is(cached.clientHeight, clientHeight) && Object.is(cached.scrollHeight, scrollHeight)) return cached;
    const direction = scrollTop > cached.scrollTop ? "forward" : scrollTop < cached.scrollTop ? "backward" : cached.direction;
    cachedRef.current = { scrollTop, clientHeight, scrollHeight, direction };
    return cachedRef.current;
  }, [element]);
  const subscribe = useCallback((notify: () => void) => {
    if (!element) return () => {};
    const generation = kernel.generation;
    let active = true;
    const handleChange = () => { if (active && generation === kernel.generation) notify(); };
    element.addEventListener("scroll", handleChange, { passive: true });
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(handleChange);
    observer?.observe(element);
    return () => {
      active = false;
      element.removeEventListener("scroll", handleChange);
      observer?.disconnect();
    };
  }, [element, kernel]);
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}

export default function TranscriptWindow({
  projection,
  scrollElement,
  onGeometryChange,
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
  onGeometryChange: (covered?: boolean) => void;
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
  const candidateItems = virtualizer.getVirtualItems();
  const committedGeometryRef = useRef<TranscriptWindowGeometry<(typeof candidateItems)[number]> | undefined>(undefined);
  const structureRevision = `${split.cold.length}:${split.cold[0]?.key ?? ""}:${split.cold[split.cold.length - 1]?.key ?? ""}`;
  const geometry = commitTranscriptWindowGeometry({
    candidate: candidateItems,
    measurements: virtualizer.measurementsCache,
    retainedIndexes,
    previous: committedGeometryRef.current,
    residentCount: split.resident.length,
    forceFull,
    structureRevision,
    scrollTop: nativeViewport.scrollTop,
    clientHeight: nativeViewport.clientHeight,
    scrollHeight: nativeViewport.scrollHeight,
    scrollMargin,
    totalSize,
    maxItems: coldMountBudget,
    direction: nativeViewport.direction,
    gestureActive: kernel.userGestureActive,
  });
  const committedRange = geometry.range;
  const virtualItems = committedRange.items;
  const fullDOMFallback = geometry.mode === "full";
  const logicalAnchorIndex = kernel.anchor.kind === "block"
    ? coldIndexByKey.get(kernel.anchor.blockKey)
    : undefined;
  const rangeRevision = `${committedRange.scrollMargin}:${committedRange.totalSize}|${virtualItems.map((item) => `${String(item.key)}:${item.start}:${item.size}`).join("|")}`;

  useLayoutEffect(() => {
    committedGeometryRef.current = geometry;
    onGeometryChange(geometry.covered);
  }, [geometry, onGeometryChange]);
  useLayoutEffect(() => {
    if (!kernel.userGestureActive) measurementLedger.endGesture();
  }, [kernel.generation, kernel.userGestureActive, measurementLedger]);
  useEffect(() => {
    if (!scrollElement) return;
    const observeWheel = (event: WheelEvent) => measurementLedger.observeWheel(event.deltaY, event.deltaMode, scrollElement.clientHeight);
    const beginUnbounded = () => measurementLedger.beginUnboundedGesture();
    const observeKey = (event: KeyboardEvent) => {
      if (["ArrowUp", "ArrowDown", "PageUp", "PageDown", "Home", "End", " "].includes(event.key)) beginUnbounded();
    };
    const endUnownedMouse = () => {
      if (!kernel.userGestureActive) measurementLedger.endGesture();
    };
    const pointerStartEvents = ["pointerdown", "mousedown"] as const;
    scrollElement.addEventListener("wheel", observeWheel, { capture: true, passive: true });
    pointerStartEvents.forEach((type) => scrollElement.addEventListener(type, beginUnbounded, true));
    scrollElement.addEventListener("touchstart", beginUnbounded, { capture: true, passive: true });
    scrollElement.addEventListener("keydown", observeKey, true);
    window.addEventListener("mouseup", endUnownedMouse, true);
    return () => {
      scrollElement.removeEventListener("wheel", observeWheel, true);
      pointerStartEvents.forEach((type) => scrollElement.removeEventListener(type, beginUnbounded, true));
      scrollElement.removeEventListener("touchstart", beginUnbounded, true);
      scrollElement.removeEventListener("keydown", observeKey, true);
      window.removeEventListener("mouseup", endUnownedMouse, true);
    };
  }, [kernel, measurementLedger, scrollElement]);
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
  useLayoutEffect(() => {
    if (fullDOMFallback) return;
    const container = residentTailRef.current;
    const changes: Array<{ key: string; size: number }> = [];
    const viewportBottom = scrollElement?.getBoundingClientRect().bottom;
    // Read the native lease at the publication boundary, not from the render
    // that scheduled this effect. A native capture listener can claim scroll
    // ownership before React commits its kernel snapshot, especially on
    // WebKitGTK. A bounded wheel lease protects the accumulated compositor
    // travel plus one viewport; unbounded gestures keep every measurement
    // staged until ownership ends.
    const publicationLeadPx = measurementLedger.publicationLead(kernel.userGestureActive);
    const paintedSafeIndex = virtualItems.find((item) => (
      item.start >= nativeViewport.scrollTop + nativeViewport.clientHeight + publicationLeadPx - 0.5
    ))?.index;
    let domSafeIndex: number | undefined;
    if (container) {
      for (const item of virtualItems) {
        const element = container.querySelector<HTMLElement>(`.transcript__window-item[data-index="${item.index}"]`);
        if (!element) continue;
        const rect = element.getBoundingClientRect();
        if (domSafeIndex == null && viewportBottom != null && rect.top >= viewportBottom + publicationLeadPx - 0.5) domSafeIndex = item.index;
        const size = Math.max(64, rect.height || element.offsetHeight);
        if (Math.abs(size - item.size) > 0.5) changes.push({ key: String(item.key), size });
      }
    }
    measurementLedger.stage(changes);
    // Freeze every block in the reader's painted viewport, not only its first
    // anchor. Prefix estimates and mounted DOM can disagree in either
    // direction, so both must identify a post-viewport block before any
    // measurement may publish. The logical anchor can only make that boundary
    // more conservative when native listeners lag behind the compositor.
    const postViewportIndex = paintedSafeIndex == null || domSafeIndex == null
      ? undefined
      : Math.max(paintedSafeIndex, domSafeIndex);
    const measurementBoundaryIndex = postViewportIndex == null
      ? undefined
      : Math.max(postViewportIndex, logicalAnchorIndex ?? postViewportIndex);
    const published = measurementLedger.publishStaged((key) => {
      const index = coldIndexByKey.get(key);
      // Only a size after the whole painted viewport can publish without
      // changing geometry the reader already sees. Earlier sizes remain
      // staged until the reader passes them. Tail intent has no cold-history
      // boundary and does not need invisible prefix refinement; its native
      // geometry comes from the exact resident tail. This makes publication
      // independent of platform wheel-event timing and prevents cold
      // refinement from adding extra tail writes.
      return kernel.intent === "reader"
        && measurementBoundaryIndex != null
        && index != null
        && index >= measurementBoundaryIndex;
    });
    if (published.length > 0) {
      // Feed only the atomically published suffix into TanStack's keyed size
      // cache. `measure()` is intentionally forbidden here: it clears that
      // cache and rebuilds the entire prefix, allowing previously committed
      // off-screen measurements to reflow the current native viewport. These
      // synchronous resize notifications complete in one browser task, so
      // React can expose only the final prefix snapshot to paint.
      for (const change of published) {
        const index = coldIndexByKey.get(change.key);
        if (index != null) virtualizer.resizeItem(index, change.size);
      }
      return;
    }
  }, [coldIndexByKey, fullDOMFallback, kernel.intent, kernel.userGestureActive, logicalAnchorIndex, measurementLedger, nativeViewport.clientHeight, nativeViewport.scrollTop, onGeometryChange, projection.activeBlock?.measurementRevision, rangeRevision, scrollElement, split.resident, virtualItems, virtualizer]);

  // Safety disables range eviction, not the last trustworthy prefix. Reflowing
  // every cold estimate into natural DOM would move a held reader without any
  // writer command. Keep those coordinates and mount ALL cold blocks instead.
  const prefix = fullDOMFallback ? geometry.prefix
    : { items: virtualItems, extent: committedRange.totalSize, margin: committedRange.scrollMargin };
  const mounted = fullDOMFallback ? projection.completedBlocks
    : [...virtualItems.map((item) => split.cold[item.index]), ...split.resident];
  const placements = new Map(prefix.items.map((item) => [String(item.key),
    { index: item.index, top: item.start - prefix.margin }]));
  return renderProjection({ blocks: [...mounted, ...(projection.activeBlock ? [projection.activeBlock] : [])],
    placements, extent: prefix.extent,
    spacerRef: coldContainerRef, tailRef: residentTailRef, mode: fullDOMFallback ? "full" : "windowed",
    safety: fullDOMFallback, completedCount: projection.completedBlocks.length,
    revision: `${fullDOMFallback}:${rangeRevision}:${projection.activeBlock?.measurementRevision}` });
}
