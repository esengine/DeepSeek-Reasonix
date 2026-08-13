import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
  type RefObject,
} from "react";
import { observeScrollContentSize } from "./scrollContentObserver";
import type { TranscriptScrollMode, TranscriptScrollOwner } from "./transcriptScrollController";

const HOT_ZONE_PX = 18;
const MIN_THUMB_PX = 28;

type ScrollbarState = {
  visible: boolean;
  hot: boolean;
  thumbTop: number;
  thumbHeight: number;
};

type DragGeometry = {
  pointerId: number;
  startY: number;
  startThumbTop: number;
  overflow: number;
  maxThumbTop: number;
  thumbHeight: number;
};

const HIDDEN_STATE: ScrollbarState = { visible: false, hot: false, thumbTop: 0, thumbHeight: 0 };

export function readCreationScrollbarGeometry(clientHeight: number, scrollHeight: number) {
  const overflow = scrollHeight - clientHeight;
  if (overflow <= 1 || clientHeight <= 0) return null;
  const thumbHeight = Math.max(MIN_THUMB_PX, Math.round((clientHeight / scrollHeight) * clientHeight));
  const maxThumbTop = Math.max(0, clientHeight - thumbHeight);
  return { overflow, thumbHeight, maxThumbTop };
}

function readGeometry(element: HTMLElement) {
  return readCreationScrollbarGeometry(element.clientHeight, element.scrollHeight);
}

export function mapFrozenScrollbarDrag(
  drag: Pick<DragGeometry, "startY" | "startThumbTop" | "overflow" | "maxThumbTop">,
  clientY: number,
) {
  const thumbTop = Math.min(drag.maxThumbTop, Math.max(0, drag.startThumbTop + clientY - drag.startY));
  const scrollTop = drag.maxThumbTop > 0 ? (thumbTop / drag.maxThumbTop) * drag.overflow : 0;
  return { thumbTop, scrollTop };
}

/** Creation-mode scrollbar with a pointerdown-frozen drag mapping. */
export function useCreationTranscriptScrollbar({
  enabled,
  contentRevision,
  scrollRef,
  onScroll,
  setScrollMode,
  writeOffset,
  finishProgrammaticScroll,
}: {
  enabled: boolean;
  contentRevision: number;
  scrollRef: RefObject<HTMLDivElement | null>;
  onScroll: () => void;
  setScrollMode: (mode: TranscriptScrollMode, reason?: string) => void;
  writeOffset: (owner: TranscriptScrollOwner, top: number, behavior?: ScrollBehavior) => boolean;
  finishProgrammaticScroll: () => void;
}) {
  const [state, setState] = useState<ScrollbarState>(HIDDEN_STATE);
  const hotRef = useRef(false);
  const dragRef = useRef<DragGeometry | null>(null);
  const settleFrameRef = useRef<number | null>(null);

  const setHot = useCallback((hot: boolean) => {
    if (hotRef.current === hot) return;
    hotRef.current = hot;
    setState((previous) => previous.hot === hot ? previous : { ...previous, hot });
  }, []);

  const syncMetrics = useCallback(() => {
    if (!enabled) return;
    const element = scrollRef.current;
    const geometry = element ? readGeometry(element) : null;
    if (!element || !geometry) {
      setState((previous) => previous.visible || previous.hot ? HIDDEN_STATE : previous);
      return;
    }
    const drag = dragRef.current;
    const overflow = drag?.overflow ?? geometry.overflow;
    const maxThumbTop = drag?.maxThumbTop ?? geometry.maxThumbTop;
    const thumbHeight = drag?.thumbHeight ?? geometry.thumbHeight;
    const thumbTop = Math.round(Math.min(maxThumbTop, Math.max(0, (element.scrollTop / overflow) * maxThumbTop)));
    setState((previous) => (
      previous.visible
      && previous.hot === hotRef.current
      && previous.thumbTop === thumbTop
      && previous.thumbHeight === thumbHeight
    ) ? previous : { visible: true, hot: hotRef.current, thumbTop, thumbHeight });
  }, [enabled, scrollRef]);

  const finishDrag = useCallback((event?: PointerEvent) => {
    const drag = dragRef.current;
    if (!drag || (event && event.pointerId !== drag.pointerId)) return;
    dragRef.current = null;
    finishProgrammaticScroll();
    syncMetrics();
    const element = scrollRef.current;
    if (!element || !event) {
      setHot(false);
      return;
    }
    const rect = element.getBoundingClientRect();
    const fromRight = rect.right - event.clientX;
    setHot(event.clientY >= rect.top && event.clientY <= rect.bottom && fromRight >= -2 && fromRight <= HOT_ZONE_PX);
  }, [finishProgrammaticScroll, scrollRef, setHot, syncMetrics]);

  useEffect(() => {
    if (!enabled) {
      dragRef.current = null;
      hotRef.current = false;
      setState(HIDDEN_STATE);
      return;
    }
    const onPointerMove = (event: PointerEvent) => {
      const drag = dragRef.current;
      const element = scrollRef.current;
      if (drag && element && event.pointerId === drag.pointerId) {
        const { thumbTop, scrollTop } = mapFrozenScrollbarDrag(drag, event.clientY);
        writeOffset("custom-scrollbar", scrollTop);
        setState({ visible: true, hot: true, thumbTop: Math.round(thumbTop), thumbHeight: drag.thumbHeight });
        setHot(true);
        return;
      }
      if (!element || !readGeometry(element)) {
        setHot(false);
        return;
      }
      const rect = element.getBoundingClientRect();
      const fromRight = rect.right - event.clientX;
      setHot(event.clientY >= rect.top && event.clientY <= rect.bottom && fromRight >= -2 && fromRight <= HOT_ZONE_PX);
    };
    const onPointerUp = (event: PointerEvent) => finishDrag(event);
    const onBlur = () => finishDrag();
    syncMetrics();
    window.addEventListener("pointermove", onPointerMove, { passive: true });
    window.addEventListener("pointerup", onPointerUp, { passive: true });
    window.addEventListener("pointercancel", onPointerUp, { passive: true });
    window.addEventListener("blur", onBlur);
    window.addEventListener("resize", syncMetrics);
    return () => {
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("pointerup", onPointerUp);
      window.removeEventListener("pointercancel", onPointerUp);
      window.removeEventListener("blur", onBlur);
      window.removeEventListener("resize", syncMetrics);
      if (settleFrameRef.current !== null) cancelAnimationFrame(settleFrameRef.current);
      settleFrameRef.current = null;
      dragRef.current = null;
      hotRef.current = false;
    };
  }, [enabled, finishDrag, scrollRef, setHot, syncMetrics, writeOffset]);

  const handleScroll = useCallback(() => {
    onScroll();
    if (enabled) syncMetrics();
  }, [enabled, onScroll, syncMetrics]);

  useLayoutEffect(() => {
    if (enabled) syncMetrics();
  }, [contentRevision, enabled, syncMetrics]);

  useEffect(() => {
    const element = scrollRef.current;
    if (!enabled || !element) return;
    return observeScrollContentSize(element, syncMetrics);
  }, [enabled, scrollRef, syncMetrics]);

  const onThumbPointerDown = useCallback((event: ReactPointerEvent<HTMLDivElement>) => {
    if (!enabled) return;
    const element = scrollRef.current;
    const geometry = element ? readGeometry(element) : null;
    if (!element || !geometry) return;
    event.preventDefault();
    event.stopPropagation();
    const startThumbTop = (element.scrollTop / geometry.overflow) * geometry.maxThumbTop;
    dragRef.current = { pointerId: event.pointerId, startY: event.clientY, startThumbTop, ...geometry };
    setScrollMode("programmatic", "custom-scrollbar-drag");
    event.currentTarget.setPointerCapture(event.pointerId);
    setHot(true);
  }, [enabled, scrollRef, setHot, setScrollMode]);

  const onRailPointerDown = useCallback((event: ReactPointerEvent<HTMLDivElement>) => {
    if (!enabled || (event.target as HTMLElement | null)?.closest?.(".transcript__scrollbar-thumb")) return;
    const element = scrollRef.current;
    const geometry = element ? readGeometry(element) : null;
    if (!element || !geometry) return;
    const rect = element.getBoundingClientRect();
    const thumbTop = Math.min(geometry.maxThumbTop, Math.max(0, event.clientY - rect.top - geometry.thumbHeight / 2));
    setScrollMode("programmatic", "custom-scrollbar-rail");
    writeOffset("custom-scrollbar", geometry.maxThumbTop > 0 ? (thumbTop / geometry.maxThumbTop) * geometry.overflow : 0);
    setState({ visible: true, hot: true, thumbTop: Math.round(thumbTop), thumbHeight: geometry.thumbHeight });
    setHot(true);
    if (settleFrameRef.current !== null) cancelAnimationFrame(settleFrameRef.current);
    settleFrameRef.current = requestAnimationFrame(() => {
      settleFrameRef.current = null;
      finishProgrammaticScroll();
      syncMetrics();
    });
  }, [enabled, finishProgrammaticScroll, scrollRef, setHot, setScrollMode, syncMetrics, writeOffset]);

  return { state, handleScroll, onThumbPointerDown, onRailPointerDown };
}
