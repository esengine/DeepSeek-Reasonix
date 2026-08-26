import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  type TouchEvent as ReactTouchEvent,
  type WheelEvent as ReactWheelEvent,
} from "react";
import { recordFrontendDiagnostic } from "./frontendDiagnosticBridge";
import { advanceSurfacePaintCommit, type SurfacePaintProgress } from "./navigationSurfaceTransition";
import { hasTranscriptScrollableRange, TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX } from "./useTranscriptScrollArbiter";
import type { HistoryLoadTrigger } from "./useController";

type ScrollRef = { current: HTMLDivElement | null };

export function grantsNativeScrollbarPagePermit(previous: number | null, current: number, tracking: boolean, alreadyGranted: boolean): boolean {
  return tracking && !alreadyGranted && previous !== null && current < previous;
}

// State: 0=idle, 1=permitted, 2=request active. Action: 0=grant,
// 1=consume, 2=complete.
export function advanceViewportPagePermit(state: number, action: 0 | 1 | 2): number {
  if (action === 2) return 0;
  if (action === 0) return state === 0 ? 1 : state;
  return state === 1 ? 2 : state;
}

export function useTranscriptPagingAuthorization(input: {
  layoutSurfaceKey: string;
  nativeScrollbarDragging: boolean;
  scrollRef: ScrollRef;
  noteUserScrollIntent: () => void;
  onWheelIntent: (event: ReactWheelEvent<HTMLElement>) => boolean;
  onWheelAccepted?: (deltaY: number) => void;
  onTouchStartIntent: (event: ReactTouchEvent<HTMLElement>) => void;
  onTouchMoveIntent: (event: ReactTouchEvent<HTMLElement>) => boolean;
  onKeyScrollIntent: (event: ReactKeyboardEvent<HTMLElement>) => boolean;
  onPointerDownIntent: (event: ReactPointerEvent<HTMLElement>) => boolean;
}) {
  const { layoutSurfaceKey, nativeScrollbarDragging, scrollRef, noteUserScrollIntent } = input;
  const permitRef = useRef(0);
  const touchStartYRef = useRef<number | null>(null);
  const previousScrollTopRef = useRef<number | null>(null);
  const nativeTrackingRef = useRef(false);
  const nativePermitGrantedRef = useRef(false);
  useEffect(() => {
    permitRef.current = 0;
    touchStartYRef.current = null;
    previousScrollTopRef.current = null;
    nativeTrackingRef.current = false;
    nativePermitGrantedRef.current = false;
  }, [layoutSurfaceKey]);
  useEffect(() => {
    if (nativeScrollbarDragging) return;
    nativeTrackingRef.current = false;
    nativePermitGrantedRef.current = false;
  }, [nativeScrollbarDragging]);
  const grant = useCallback(() => {
    // Wheel/touch bursts that continue while a page is loading belong to the
    // same viewport request. Do not let them leave a permit behind for the
    // prepend-triggered startReached callback.
    const next = advanceViewportPagePermit(permitRef.current, 0);
    if (next !== permitRef.current) recordFrontendDiagnostic("history", "history.viewport-permit");
    permitRef.current = next;
  }, []);
  const onWheelIntent = useCallback((event: ReactWheelEvent<HTMLElement>) => {
    const accepted = input.onWheelIntent(event);
    if (accepted) {
      input.onWheelAccepted?.(event.deltaY);
      noteUserScrollIntent();
      if (event.deltaY < 0) grant();
    }
    return accepted;
  }, [grant, input.onWheelAccepted, input.onWheelIntent, noteUserScrollIntent]);
  const onTouchStartIntent = useCallback((event: ReactTouchEvent<HTMLElement>) => {
    touchStartYRef.current = event.touches[0]?.clientY ?? null;
    input.onTouchStartIntent(event);
  }, [input.onTouchStartIntent]);
  const onTouchMoveIntent = useCallback((event: ReactTouchEvent<HTMLElement>) => {
    const accepted = input.onTouchMoveIntent(event);
    if (accepted) {
      noteUserScrollIntent();
      const currentY = event.touches[0]?.clientY;
      if (currentY !== undefined && touchStartYRef.current !== null && currentY > touchStartYRef.current) grant();
      if (currentY !== undefined) touchStartYRef.current = currentY;
    }
    return accepted;
  }, [grant, input.onTouchMoveIntent, noteUserScrollIntent]);
  const onKeyScrollIntent = useCallback((event: ReactKeyboardEvent<HTMLElement>) => {
    const accepted = input.onKeyScrollIntent(event);
    if (accepted) {
      noteUserScrollIntent();
      if (event.key === "ArrowUp" || event.key === "PageUp" || event.key === "Home") grant();
    }
    return accepted;
  }, [grant, input.onKeyScrollIntent, noteUserScrollIntent]);
  const onPointerDownIntent = useCallback((event: ReactPointerEvent<HTMLElement>) => {
    const accepted = input.onPointerDownIntent(event);
    if (accepted) {
      noteUserScrollIntent();
      if (event.button === 0) {
        nativeTrackingRef.current = true;
        nativePermitGrantedRef.current = false;
        previousScrollTopRef.current = scrollRef.current?.scrollTop ?? null;
      }
    }
    return accepted;
  }, [input.onPointerDownIntent, noteUserScrollIntent, scrollRef]);
  const noteScrollPosition = useCallback(() => {
    const scrollTop = scrollRef.current?.scrollTop;
    if (scrollTop === undefined) return;
    const previous = previousScrollTopRef.current;
    if (grantsNativeScrollbarPagePermit(previous, scrollTop, nativeTrackingRef.current, nativePermitGrantedRef.current)) {
      nativePermitGrantedRef.current = true;
      grant();
    }
    previousScrollTopRef.current = scrollTop;
  }, [grant, scrollRef]);
  const consume = useCallback(() => {
    const next = advanceViewportPagePermit(permitRef.current, 1);
    const consumed = next !== permitRef.current;
    permitRef.current = next;
    return consumed;
  }, []);
  const complete = useCallback(() => {
    permitRef.current = advanceViewportPagePermit(permitRef.current, 2);
  }, []);
  return { onWheelIntent, onTouchStartIntent, onTouchMoveIntent, onKeyScrollIntent, onPointerDownIntent, noteScrollPosition, consume, complete };
}

export function transcriptSurfaceGeometryKey(element: HTMLElement): string {
  return `${Math.round(element.clientHeight)}:${Math.round(element.scrollHeight)}:${Math.round(element.scrollTop)}`;
}

export function useTranscriptSurfaceCommit(input: {
  token?: string;
  hydrating: boolean;
  layoutSurfaceKey: string;
  virtualRowCount: number;
  scrollRef: ScrollRef;
  virtuosoReadyRef: { current: boolean };
  layoutTransientRef: { current: boolean };
  scheduleRecovery: () => void;
  onReady?: (token: string, outcome: "ready" | "degraded") => void;
}) {
  const { token, hydrating, layoutSurfaceKey, virtualRowCount, scrollRef, virtuosoReadyRef, layoutTransientRef, scheduleRecovery, onReady } = input;
  const renderedTokenRef = useRef<string | null>(null);
  const terminalTokenRef = useRef<string | null>(null);
  const [renderSeq, setRenderSeq] = useState(0);
  const [readySurfaceKey, setReadySurfaceKey] = useState<string | null>(null);
  useEffect(() => setReadySurfaceKey(null), [layoutSurfaceKey]);
  const markItemsRendered = useCallback((count: number) => {
    if (count <= 0 || !token || renderedTokenRef.current === token) return;
    renderedTokenRef.current = token;
    setRenderSeq((value) => value + 1);
  }, [token]);
  useEffect(() => {
    if (!token || hydrating || !onReady || terminalTokenRef.current === token) return;
    let frame: number | null = null;
    let cancelled = false;
    let progress: SurfacePaintProgress = { attempts: 0, stableFrames: 0 };
    const empty = virtualRowCount === 0;
    const finish = (outcome: "ready" | "degraded") => {
      if (cancelled || terminalTokenRef.current === token) return;
      terminalTokenRef.current = token;
      recordFrontendDiagnostic("transcript", "transcript.initial-placement-terminal", {
        intent: Number(/^navigation-(\d+)-/.exec(token)?.[1] ?? 0), outcome,
      });
      setReadySurfaceKey(layoutSurfaceKey);
      onReady(token, outcome);
    };
    const tick = () => {
      frame = null;
      if (cancelled) return;
      const element = scrollRef.current;
      const rendered = empty || renderedTokenRef.current === token;
      const placementReady = empty ? Boolean(element) : Boolean(element && virtuosoReadyRef.current && !layoutTransientRef.current);
      const bottomReady = Boolean(element && element.scrollHeight - element.scrollTop - element.clientHeight <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX);
      const decision = advanceSurfacePaintCommit(progress, {
        rendered, placementReady, geometryReady: bottomReady,
        geometryKey: element ? transcriptSurfaceGeometryKey(element) : undefined,
      });
      progress = decision.progress;
      if (decision.outcome) return finish(decision.outcome);
      if (decision.requestRecovery) scheduleRecovery();
      frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => {
      cancelled = true;
      if (frame !== null) cancelAnimationFrame(frame);
    };
  }, [hydrating, layoutSurfaceKey, layoutTransientRef, onReady, renderSeq, scheduleRecovery, scrollRef, token, virtualRowCount, virtuosoReadyRef]);
  return { readySurfaceKey, markItemsRendered };
}

export function useTranscriptHistoryAutoFill(input: {
  readySurfaceKey: string | null;
  layoutSurfaceKey: string;
  hydrating: boolean;
  hasOlderHistory: boolean;
  loadingOlderHistory: boolean;
  olderHistoryError?: string;
  running: boolean;
  historyStartTurn: number;
  virtualRowCount: number;
  scrollRef: ScrollRef;
  virtuosoReadyRef: { current: boolean };
  layoutTransientRef: { current: boolean };
  onLoadOlderHistory?: (targetTurn?: number, trigger?: HistoryLoadTrigger) => boolean | Promise<boolean>;
}) {
  const {
    readySurfaceKey, layoutSurfaceKey, hydrating, hasOlderHistory, loadingOlderHistory,
    olderHistoryError, running, historyStartTurn, virtualRowCount, scrollRef,
    virtuosoReadyRef, layoutTransientRef, onLoadOlderHistory,
  } = input;
  const pageCountRef = useRef(0);
  const inFlightRef = useRef(false);
  useEffect(() => {
    pageCountRef.current = 0;
    inFlightRef.current = false;
  }, [layoutSurfaceKey]);
  useEffect(() => {
    if (readySurfaceKey !== layoutSurfaceKey || hydrating || !hasOlderHistory ||
      loadingOlderHistory || olderHistoryError || running || !onLoadOlderHistory) return;
    let frame: number | null = null;
    let cancelled = false;
    let attempts = 0;
    const probe = () => {
      frame = null;
      if (cancelled) return;
      attempts += 1;
      const element = scrollRef.current;
      if (!element || !virtuosoReadyRef.current || layoutTransientRef.current) {
        if (attempts < 180) frame = requestAnimationFrame(probe);
        return;
      }
      if (hasTranscriptScrollableRange(element) || inFlightRef.current || pageCountRef.current >= 3) return;
      inFlightRef.current = true;
      pageCountRef.current += 1;
      void Promise.resolve(onLoadOlderHistory(undefined, "auto-fill")).finally(() => { inFlightRef.current = false; });
    };
    frame = requestAnimationFrame(probe);
    return () => {
      cancelled = true;
      if (frame !== null) cancelAnimationFrame(frame);
    };
  }, [hasOlderHistory, historyStartTurn, hydrating, layoutSurfaceKey, layoutTransientRef, loadingOlderHistory, olderHistoryError, onLoadOlderHistory, readySurfaceKey, running, scrollRef, virtualRowCount, virtuosoReadyRef]);
}
