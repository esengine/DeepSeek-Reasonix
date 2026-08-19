import { useCallback, useEffect, useRef, useState, type RefObject } from "react";
import type {
  KeyboardEvent as ReactKeyboardEvent,
  PointerEvent as ReactPointerEvent,
  TouchEvent as ReactTouchEvent,
  WheelEvent as ReactWheelEvent,
} from "react";
import type { FlatIndexLocationWithAlign, SizeFunction, StateSnapshot, VirtuosoHandle } from "react-virtuoso";
import { isEditableTarget } from "./keyboardShortcuts";
import { findVerticalScrollTarget } from "./nestedScrollHandoff";
import { isNativeVerticalScrollbarPointer, measureTranscriptVirtuosoItem } from "./transcriptNativeScrollbar";
import {
  INITIAL_TRANSCRIPT_SCROLL_STATE,
  isSubstantialTranscriptDisplacement,
  isTranscriptContentShrink,
  isTranscriptSelectionMode,
  reduceTranscriptScroll,
  type TranscriptRecoveryCancelReason,
  type TranscriptScrollCommand,
  type TranscriptScrollEvent,
  type TranscriptScrollMode,
  type TranscriptScrollOwner,
  type TranscriptScrollState,
} from "./transcriptScrollArbiter";
import { noteTranscriptScrollWrite } from "./transcriptScrollProbe";
import { captureTranscriptLayoutAnchor, type TranscriptLayoutAnchor } from "./transcriptVirtuosoRecovery";

const SCROLL_UP_KEYS = new Set(["ArrowUp", "PageUp", "Home"]);
const SCROLL_DOWN_KEYS = new Set(["ArrowDown", "PageDown", "End", " ", "Spacebar"]);
export const TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX = 4;
const TAIL_STAGNANT_FRAME_LIMIT = 2;
// A tail displacement must survive one full frame before the writer acts.
// Layout churn (row remeasurement, content-visibility flips, md hydration)
// alternates off-bottom/at-bottom on consecutive frames; writing against it
// perturbs layout again and sustains the oscillation — the reduced-motion
// flicker of #9028/#9089. Real growth stays off-bottom and converges one
// frame (~16ms) later, which is imperceptible.
const TAIL_CONFIRM_OFF_BOTTOM_FRAMES = 2;
const READER_INTENT_IDLE_MS = 180;
// Anchor restores wait for the anchor row to actually mount. An 8-frame
// budget (~128 ms) expired before heavy rows mounted on WebView2, stranding
// the view at the estimate-based (higher) scrollToIndex landing — the
// scroll-down/snap-up loop. Bound by wall clock instead; on expiry the
// request suspends (no intermediate scrollBy ever lands while the anchor row
// is unmounted) and retries after a bounded quiet window, up to
// RECOVERY_MAX_RETRIES times before going terminally expired. User intent
// still preempts a suspended request instead of letting the retry take over.
const ANCHOR_RESTORE_BUDGET_MS = 1_000;
const RECOVERY_MAX_RETRIES = 2;
const RECOVERY_CORRECTION_TOLERANCE_PX = 1;
const RECOVERY_STABLE_FRAMES = 2;

export function nativeTranscriptDistanceFromBottom(element: {
  scrollHeight: number;
  scrollTop: number;
  clientHeight: number;
}) {
  return element.scrollHeight - element.scrollTop - element.clientHeight;
}

export function nativeTranscriptBottomTop(element: { scrollHeight: number; clientHeight: number }) {
  return Math.max(0, element.scrollHeight - element.clientHeight);
}

export function hasTranscriptScrollableRange(
  element: { scrollHeight: number; clientHeight: number },
  threshold = TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX,
) {
  return nativeTranscriptBottomTop(element) > threshold;
}

/** Terminal state every recovery request reaches; reported to diagnostics. */
export type TranscriptRecoveryTerminal = {
  id: number;
  outcome: "done" | "cancelled" | "expired";
  reason?: TranscriptRecoveryCancelReason;
};

/** One layout-recovery job the arbiter executes on the integrity hook's
 *  behalf. The arbiter owns every scroll write; the spec supplies only
 *  geometry lookups and lifecycle callbacks. */
export type TranscriptRecoveryRequestSpec = {
  anchor: TranscriptLayoutAnchor;
  /** Absolute Virtuoso location for the current anchor, recomputed per re-aim. */
  locate: (anchor: TranscriptLayoutAnchor) => FlatIndexLocationWithAlign | undefined;
  /** The user's resting viewport anchor, sampled at takeover/retry time. */
  captureUserAnchor: () => TranscriptLayoutAnchor | undefined;
  onSettle?: (anchor: TranscriptLayoutAnchor) => void;
  onCancel?: (reason: TranscriptRecoveryCancelReason) => void;
  onSuspend?: (id: number) => void;
  onExpired?: (id: number) => void;
};

/** The recovery lane of the arbiter, consumed by useTranscriptLayoutIntegrity. */
export type TranscriptScrollArbiterRecoveryApi = {
  submitRecoveryRequest: (spec: TranscriptRecoveryRequestSpec) => number;
  retryRecoveryRequest: (id: number) => void;
  lastGoodAnchorRef: RefObject<TranscriptLayoutAnchor | null>;
  /** Synchronous getState read on the live Virtuoso handle; null when the
   *  handle is unmounted. Used to snapshot the measured tree + scrollTop
   *  before a keyed remount. */
  captureStateSnapshot: () => StateSnapshot | null;
};

type ActiveTranscriptRecovery = {
  id: number;
  spec: TranscriptRecoveryRequestSpec;
  anchor: TranscriptLayoutAnchor;
  retries: number;
  status: "active" | "suspended";
  stableFrames: number;
  deadline: number;
  frame: number | null;
};

/**
 * One scroll coordinator around React Virtuoso. No native scrollTop writes.
 * This is the single writer on the Virtuoso handle: tail-follow, jumps,
 * selection edge scrolls, and recovery restores all dispatch through the
 * reducer, which arbitrates preemption (selection > user intent >
 * programmatic > recovery > tail-follow).
 */
export function useTranscriptScrollArbiter({
  onRecoveryTerminal,
}: {
  /** Receives the terminal state of every recovery request (done /
   *  cancelled / expired); wired into session diagnostics by the caller. */
  onRecoveryTerminal?: (terminal: TranscriptRecoveryTerminal) => void;
} = {}) {
  const virtuosoRef = useRef<VirtuosoHandle>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const stateRef = useRef<TranscriptScrollState>(INITIAL_TRANSCRIPT_SCROLL_STATE);
  const pinnedRef = useRef(true);
  const modeRef = useRef<TranscriptScrollMode>("tail-follow");
  const touchStartYRef = useRef<number | null>(null);
  const nativeScrollbarDragRef = useRef(false);
  const middlePointerScrollRef = useRef(false);
  const generationRef = useRef(0);
  const followFrameRef = useRef<number | null>(null);
  const tailSettleFrameRef = useRef<number | null>(null);
  const tailSettleProgressRef = useRef<{ distance: number; stagnantFrames: number; offBottomFrames: number } | null>(null);
  const resizeSettleFrameRef = useRef<number | null>(null);
  const readerIntentTimerRef = useRef<number | null>(null);
  const lastFollowExtentRef = useRef<number | null>(null);
  const recoveryRef = useRef<ActiveTranscriptRecovery | null>(null);
  const nextRecoveryIdRef = useRef(0);
  // Last known-good viewport anchor: updated on every completed recovery, on
  // every user-takeover, and sampled on user scroll intent. The blank
  // watchdog restores from it instead of a nearest-mounted-row guess (#8657).
  const lastGoodAnchorRef = useRef<TranscriptLayoutAnchor | null>(null);
  const onRecoveryTerminalRef = useRef(onRecoveryTerminal);
  onRecoveryTerminalRef.current = onRecoveryTerminal;
  const [nativeScrollbarDragging, setNativeScrollbarDragging] = useState(false);
  const [isAtBottom, setIsAtBottom] = useState(true);
  const [scrollElement, setScrollElement] = useState<HTMLDivElement | null>(null);

  // Tail writes aim at the scroller's native extent, never at Virtuoso's size
  // tree. scrollToIndex("LAST") resolves against the estimated tree, which can
  // already believe it is at the bottom while the DOM extent sits hundreds of
  // pixels lower; the write then lands nowhere, the arbiter still reads a
  // non-bottom distance, and the settle loop re-arms every frame (#9028: 340
  // no-op tail writes in 11s with scrollTop frozen — the reduced-motion
  // flicker loop). The browser clamps scrollTo against the real extent, so a
  // native-extent write always converges and is idempotent at the bottom.
  const scrollToTail = useCallback((behavior: "auto" | "smooth") => {
    const element = scrollRef.current;
    if (!element) return;
    const top = element.scrollHeight;
    noteTranscriptScrollWrite({ owner: "tail-follow", kind: "scrollTo", top });
    virtuosoRef.current?.scrollTo({ top, behavior });
  }, []);

  const cancelTailSettle = useCallback(() => {
    if (tailSettleFrameRef.current !== null) cancelAnimationFrame(tailSettleFrameRef.current);
    tailSettleFrameRef.current = null;
    tailSettleProgressRef.current = null;
  }, []);

  const scheduleTailSettle = useCallback(() => {
    if (tailSettleFrameRef.current !== null) return;
    const generation = generationRef.current;
    const scrollElement = scrollRef.current;
    const tick = () => {
      tailSettleFrameRef.current = null;
      if (
        generationRef.current !== generation
        || scrollRef.current !== scrollElement
        || modeRef.current !== "tail-follow"
      ) {
        tailSettleProgressRef.current = null;
        return;
      }
      const element = scrollRef.current;
      if (!element) return;
      const distance = nativeTranscriptDistanceFromBottom(element);
      if (distance <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX) {
        // Already converged: a redundant write would still be a visible
        // programmatic scrollTop touch for an idle tail.
        tailSettleProgressRef.current = null;
        return;
      }
      const previous = tailSettleProgressRef.current;
      const offBottomFrames = (previous?.offBottomFrames ?? 0) + 1;
      if (offBottomFrames < TAIL_CONFIRM_OFF_BOTTOM_FRAMES) {
        tailSettleProgressRef.current = { distance, stagnantFrames: 0, offBottomFrames };
        tailSettleFrameRef.current = requestAnimationFrame(tick);
        return;
      }
      scrollToTail("auto");
      const stagnantFrames = previous && Math.abs(previous.distance - distance) <= 0.5
        ? previous.stagnantFrames + 1
        : 0;
      tailSettleProgressRef.current = { distance, stagnantFrames, offBottomFrames };
      if (stagnantFrames < TAIL_STAGNANT_FRAME_LIMIT) tailSettleFrameRef.current = requestAnimationFrame(tick);
    };
    tailSettleFrameRef.current = requestAnimationFrame(tick);
  }, [scrollToTail]);

  const invalidateAsyncFrames = useCallback(() => {
    generationRef.current += 1;
    if (followFrameRef.current !== null) cancelAnimationFrame(followFrameRef.current);
    if (resizeSettleFrameRef.current !== null) cancelAnimationFrame(resizeSettleFrameRef.current);
    followFrameRef.current = null;
    resizeSettleFrameRef.current = null;
    cancelTailSettle();
  }, [cancelTailSettle]);

  // Executes the reducer's CANCEL_RECOVERY command. The cancelling event
  // already cleared recoveryId in the published state, so no RECOVERY_END
  // dispatch is needed here; this only runs the explicit onCancel transition.
  const cancelInFlightRecovery = useCallback((id: number, reason: TranscriptRecoveryCancelReason) => {
    const recovery = recoveryRef.current;
    if (!recovery || recovery.id !== id) return;
    recoveryRef.current = null;
    if (recovery.frame !== null) cancelAnimationFrame(recovery.frame);
    recovery.frame = null;
    if (reason === "user-takeover") {
      // The user is the consistency source: their resting anchor becomes the
      // last known-good position.
      const anchor = recovery.spec.captureUserAnchor();
      if (anchor) lastGoodAnchorRef.current = anchor;
    }
    recovery.spec.onCancel?.(reason);
    onRecoveryTerminalRef.current?.({ id, outcome: "cancelled", reason });
  }, []);

  const publishState = useCallback((state: TranscriptScrollState) => {
    stateRef.current = state;
    modeRef.current = state.mode;
    pinnedRef.current = state.mode === "tail-follow";
    setIsAtBottom(state.atBottom);
    if (scrollRef.current) scrollRef.current.dataset.scrollMode = state.mode;
  }, []);

  const runCommand = useCallback((command: TranscriptScrollCommand) => {
    const handle = virtuosoRef.current;
    switch (command.type) {
      case "AUTOSCROLL_TO_BOTTOM":
        // Virtuoso's autoscrollToBottom() is inert without the followOutput
        // prop (never passed here), so the rAF settle loop is the real
        // follow mechanism.
        scheduleTailSettle();
        return;
      case "SCROLL_TO_LAST":
        scrollToTail(command.behavior);
        // Re-aim across a bounded number of frames: the first LAST request
        // can use Virtuoso's pre-measurement size tree, and late tail-row
        // measurements would otherwise park the view above the real bottom.
        scheduleTailSettle();
        return;
      case "SCROLL_TO_INDEX":
        noteTranscriptScrollWrite({ owner: "jump", kind: "scrollToIndex", index: command.index });
        handle?.scrollToIndex({ index: command.index, align: "start", behavior: command.behavior });
        return;
      case "SCROLL_TO_OFFSET":
        noteTranscriptScrollWrite({ owner: command.owner, kind: "scrollTo", top: command.top });
        handle?.scrollTo({ top: command.top, behavior: command.behavior });
        return;
      case "CANCEL_RECOVERY":
        cancelInFlightRecovery(command.id, command.reason);
    }
  }, [cancelInFlightRecovery, scheduleTailSettle, scrollToTail]);

  const dispatch = useCallback((event: TranscriptScrollEvent) => {
    if (
      event.type === "USER_SCROLL_INTENT"
      || event.type === "MANUAL_READING"
      || event.type === "USER_RESIZE_BEGIN"
      || event.type === "SELECTION_BEGIN"
      || event.type === "PROGRAMMATIC_BEGIN"
      || event.type === "JUMP_TO_INDEX"
      || event.type === "SCROLL_TO_OFFSET"
      || event.type === "CONTENT_SHRANK"
    ) {
      cancelTailSettle();
    }
    if (event.type === "RESET") lastGoodAnchorRef.current = null;
    if (event.type === "USER_SCROLL_INTENT") {
      const element = scrollRef.current;
      const anchor = element ? captureTranscriptLayoutAnchor(element, false) : undefined;
      if (anchor) lastGoodAnchorRef.current = anchor;
    }
    const result = reduceTranscriptScroll(stateRef.current, event);
    publishState(result.state);
    for (const command of result.commands) runCommand(command);
    return result;
  }, [cancelTailSettle, publishState, runCommand]);

  const endReaderIntent = useCallback(() => {
    if (readerIntentTimerRef.current !== null) window.clearTimeout(readerIntentTimerRef.current);
    readerIntentTimerRef.current = null;
    dispatch({ type: "READER_INTENT_ENDED" });
  }, [dispatch]);

  const armReaderIntentIdle = useCallback(() => {
    if (readerIntentTimerRef.current !== null) window.clearTimeout(readerIntentTimerRef.current);
    readerIntentTimerRef.current = window.setTimeout(() => {
      readerIntentTimerRef.current = null;
      dispatch({ type: "READER_INTENT_ENDED" });
    }, READER_INTENT_IDLE_MS);
  }, [dispatch]);

  const deliverScroll = useCallback((element = scrollRef.current) => {
    if (!element) return;
    const distance = nativeTranscriptDistanceFromBottom(element);
    dispatch({
      type: "SCROLL_DELIVERED",
      atBottom: distance <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX,
      scrollable: hasTranscriptScrollableRange(element),
      substantial: isSubstantialTranscriptDisplacement(distance),
    });
    if (stateRef.current.readerIntent) armReaderIntentIdle();
  }, [armReaderIntentIdle, dispatch]);

  const scrollToBottom = useCallback((behavior: ScrollBehavior = "auto") => {
    if (isTranscriptSelectionMode(modeRef.current)) return;
    dispatch({ type: "JUMP_TO_BOTTOM", behavior });
  }, [dispatch]);

  // Reaches a terminal state for a recovery the arbiter itself ends (done /
  // expired / scroller gone). Preemption cancels go through
  // cancelInFlightRecovery instead, driven by the reducer's CANCEL command.
  const finishRecovery = useCallback((
    recovery: ActiveTranscriptRecovery,
    terminal: { outcome: "done" } | { outcome: "expired" } | { outcome: "cancelled"; reason: TranscriptRecoveryCancelReason },
  ) => {
    if (recoveryRef.current !== recovery) return;
    recoveryRef.current = null;
    if (recovery.frame !== null) cancelAnimationFrame(recovery.frame);
    recovery.frame = null;
    dispatch({ type: "RECOVERY_END", id: recovery.id });
    if (terminal.outcome === "done") {
      lastGoodAnchorRef.current = recovery.anchor;
      recovery.spec.onSettle?.(recovery.anchor);
    } else if (terminal.outcome === "expired") {
      recovery.spec.onExpired?.(recovery.id);
    } else {
      recovery.spec.onCancel?.(terminal.reason);
    }
    onRecoveryTerminalRef.current?.({ id: recovery.id, ...terminal });
  }, [dispatch]);

  const launchRecovery = useCallback((recovery: ActiveTranscriptRecovery) => {
    const tick = () => {
      recovery.frame = null;
      if (recoveryRef.current !== recovery || recovery.status !== "active") return;
      const element = scrollRef.current;
      if (!element) {
        finishRecovery(recovery, { outcome: "cancelled", reason: "surface-switch" });
        return;
      }
      const anchor = recovery.anchor;
      if (anchor.mode === "tail") {
        finishRecovery(recovery, { outcome: "done" });
        scrollToBottom();
        return;
      }
      const row = Array.from(element.querySelectorAll<HTMLElement>(".transcript__row[data-row-key]"))
        .find((candidate) => candidate.dataset.rowKey === anchor.rowKey);
      if (!row) {
        // Heavy rows can take far longer than a few frames to mount after a
        // rebuild on slow renderers. Keep re-aiming until the wall-clock
        // budget expires — re-aims only, never an intermediate scrollBy into
        // the estimate-based void. On expiry the request suspends; the
        // integrity owner schedules a bounded retry unless user intent
        // explicitly cancels it (#8657/#8688).
        if (Date.now() >= recovery.deadline) {
          recovery.status = "suspended";
          recovery.spec.onSuspend?.(recovery.id);
          return;
        }
        const location = recovery.spec.locate(anchor);
        if (location) {
          noteTranscriptScrollWrite({ owner: "recovery", kind: "scrollToIndex", index: location.index });
          virtuosoRef.current?.scrollToIndex(location);
        }
        recovery.frame = requestAnimationFrame(tick);
        return;
      }
      const viewportTop = element.getBoundingClientRect().top;
      const correction = row.getBoundingClientRect().top - viewportTop - anchor.offset;
      if (Math.abs(correction) > RECOVERY_CORRECTION_TOLERANCE_PX) {
        noteTranscriptScrollWrite({ owner: "recovery", kind: "scrollBy", top: correction });
        virtuosoRef.current?.scrollBy({ top: correction, behavior: "auto" });
      }
      recovery.stableFrames = Math.abs(correction) <= RECOVERY_CORRECTION_TOLERANCE_PX ? recovery.stableFrames + 1 : 0;
      if (Date.now() < recovery.deadline && recovery.stableFrames < RECOVERY_STABLE_FRAMES) {
        recovery.frame = requestAnimationFrame(tick);
        return;
      }
      finishRecovery(recovery, { outcome: "done" });
    };
    recovery.frame = requestAnimationFrame(tick);
  }, [finishRecovery, scrollToBottom]);

  const submitRecoveryRequest = useCallback((spec: TranscriptRecoveryRequestSpec): number => {
    nextRecoveryIdRef.current += 1;
    const id = nextRecoveryIdRef.current;
    const recovery: ActiveTranscriptRecovery = {
      id,
      spec,
      anchor: spec.anchor,
      retries: 0,
      status: "active",
      stableFrames: 0,
      deadline: Date.now() + ANCHOR_RESTORE_BUDGET_MS,
      frame: null,
    };
    // The reducer preempts any older in-flight request ("superseded") before
    // this one becomes active, keeping at most one recovery writer.
    dispatch({ type: "RECOVERY_BEGIN", id, settleMode: spec.anchor.mode === "tail" ? "tail-follow" : "manual" });
    recoveryRef.current = recovery;
    launchRecovery(recovery);
    return id;
  }, [dispatch, launchRecovery]);

  // Retries a budget-suspended request after the integrity owner's quiet
  // window. The current viewport is the consistency source, so the retry
  // re-anchors on it.
  const retryRecoveryRequest = useCallback((id: number) => {
    const recovery = recoveryRef.current;
    if (!recovery || recovery.id !== id || recovery.status !== "suspended") return;
    if (recovery.retries >= RECOVERY_MAX_RETRIES) {
      finishRecovery(recovery, { outcome: "expired" });
      return;
    }
    recovery.retries += 1;
    recovery.anchor = recovery.spec.captureUserAnchor() ?? recovery.anchor;
    recovery.status = "active";
    recovery.stableFrames = 0;
    recovery.deadline = Date.now() + ANCHOR_RESTORE_BUDGET_MS;
    launchRecovery(recovery);
  }, [finishRecovery, launchRecovery]);

  const reset = useCallback(() => {
    invalidateAsyncFrames();
    endReaderIntent();
    lastFollowExtentRef.current = null;
    dispatch({ type: "RESET" });
  }, [dispatch, endReaderIntent, invalidateAsyncFrames]);

  const setMode = useCallback((mode: TranscriptScrollMode, _reason?: string) => {
    switch (mode) {
      case "tail-follow": reset(); break;
      case "manual": dispatch({ type: "MANUAL_READING" }); break;
      case "user-resize": dispatch({ type: "USER_RESIZE_BEGIN" }); break;
      case "selection": dispatch({ type: "SELECTION_BEGIN" }); break;
      case "restoring": dispatch({ type: "PROGRAMMATIC_BEGIN" }); break;
    }
  }, [dispatch, reset]);

  const finishNativeScrollbarDrag = useCallback(() => {
    if (!nativeScrollbarDragRef.current) return;
    const element = scrollRef.current;
    if (element) {
      dispatch({ type: "USER_SCROLL_INTENT", canClaimTail: true });
      deliverScroll(element);
      delete element.dataset.nativeScrollbarDrag;
    }
    endReaderIntent();
    nativeScrollbarDragRef.current = false;
    setNativeScrollbarDragging(false);
    if (modeRef.current === "tail-follow") scheduleTailSettle();
  }, [deliverScroll, dispatch, endReaderIntent, scheduleTailSettle]);

  const finishPointerIntent = useCallback(() => {
    if (nativeScrollbarDragRef.current) finishNativeScrollbarDrag();
    if (middlePointerScrollRef.current) {
      middlePointerScrollRef.current = false;
      endReaderIntent();
    }
  }, [endReaderIntent, finishNativeScrollbarDrag]);

  const finishAllReaderIntent = useCallback(() => {
    finishPointerIntent();
    endReaderIntent();
  }, [endReaderIntent, finishPointerIntent]);

  useEffect(() => {
    window.addEventListener("pointerup", finishPointerIntent, true);
    window.addEventListener("pointercancel", finishPointerIntent, true);
    window.addEventListener("blur", finishAllReaderIntent);
    return () => {
      window.removeEventListener("pointerup", finishPointerIntent, true);
      window.removeEventListener("pointercancel", finishPointerIntent, true);
      window.removeEventListener("blur", finishAllReaderIntent);
    };
  }, [finishAllReaderIntent, finishPointerIntent]);

  useEffect(() => () => {
    if (followFrameRef.current !== null) cancelAnimationFrame(followFrameRef.current);
    if (tailSettleFrameRef.current !== null) cancelAnimationFrame(tailSettleFrameRef.current);
    if (resizeSettleFrameRef.current !== null) cancelAnimationFrame(resizeSettleFrameRef.current);
    if (readerIntentTimerRef.current !== null) window.clearTimeout(readerIntentTimerRef.current);
    if (recoveryRef.current?.frame != null) cancelAnimationFrame(recoveryRef.current.frame);
    generationRef.current += 1;
    recoveryRef.current = null;
  }, []);

  const itemSize = useCallback<SizeFunction>((element, field) => {
    return measureTranscriptVirtuosoItem(element, field, nativeScrollbarDragRef.current || nativeScrollbarDragging);
  }, [nativeScrollbarDragging]);

  const scrollerRef = useCallback((node: HTMLElement | Window | null) => {
    const element = node instanceof HTMLElement ? node as HTMLDivElement : null;
    if (scrollRef.current !== element) {
      finishNativeScrollbarDrag();
      invalidateAsyncFrames();
    }
    scrollRef.current = element;
    if (element) {
      element.dataset.scrollMode = stateRef.current.mode;
      deliverScroll(element);
    }
    setScrollElement((current) => current === element ? current : element);
  }, [deliverScroll, finishNativeScrollbarDrag, invalidateAsyncFrames]);

  const releaseTailFollow = useCallback((claimPhysicalBottom = false) => {
    if (isTranscriptSelectionMode(modeRef.current)) return;
    const element = scrollRef.current;
    if (element && !stateRef.current.scrollable && hasTranscriptScrollableRange(element)) {
      deliverScroll(element);
    }
    dispatch({ type: "USER_SCROLL_INTENT", canClaimTail: claimPhysicalBottom });
    if (
      claimPhysicalBottom
      && element
      && nativeTranscriptDistanceFromBottom(element) <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX
    ) {
      deliverScroll(element);
    }
    armReaderIntentIdle();
  }, [armReaderIntentIdle, deliverScroll, dispatch]);

  const followGrowingTail = useCallback(() => {
    if (followFrameRef.current !== null) return;
    const generation = generationRef.current;
    const scrollElement = scrollRef.current;
    followFrameRef.current = requestAnimationFrame(() => {
      followFrameRef.current = null;
      if (generationRef.current !== generation || scrollRef.current !== scrollElement) return;
      const element = scrollRef.current;
      if (element) {
        const scrollHeight = element.scrollHeight;
        const previous = lastFollowExtentRef.current;
        lastFollowExtentRef.current = scrollHeight;
        if (previous != null && isTranscriptContentShrink(scrollHeight - previous)) {
          dispatch({ type: "CONTENT_SHRANK" });
          return;
        }
      }
      dispatch({ type: "LAYOUT_HEIGHT_CHANGED" });
    });
  }, [dispatch]);

  const beginUserResize = useCallback(() => {
    dispatch({ type: "USER_RESIZE_BEGIN" });
    if (resizeSettleFrameRef.current !== null) cancelAnimationFrame(resizeSettleFrameRef.current);
    const generation = generationRef.current;
    const scrollElement = scrollRef.current;
    resizeSettleFrameRef.current = requestAnimationFrame(() => {
      if (generationRef.current !== generation || scrollRef.current !== scrollElement) {
        resizeSettleFrameRef.current = null;
        return;
      }
      resizeSettleFrameRef.current = requestAnimationFrame(() => {
        resizeSettleFrameRef.current = null;
        if (generationRef.current !== generation || scrollRef.current !== scrollElement) return;
        dispatch({ type: "USER_RESIZE_END" });
      });
    });
  }, [dispatch]);

  const atBottomStateChange = useCallback((_atBottom: boolean) => deliverScroll(), [deliverScroll]);

  const writeOffset = useCallback((owner: TranscriptScrollOwner, top: number, behavior: ScrollBehavior = "auto") => {
    if (isTranscriptSelectionMode(modeRef.current) && owner !== "selection-edge-scroll") return false;
    if (!scrollRef.current) return false;
    dispatch({ type: "SCROLL_TO_OFFSET", owner, top, behavior });
    return true;
  }, [dispatch]);

  const scrollToDataIndex = useCallback((dataIndex: number, behavior: "auto" | "smooth" = "auto") => {
    if (isTranscriptSelectionMode(modeRef.current)) return;
    dispatch({ type: "JUMP_TO_INDEX", index: dataIndex, behavior });
  }, [dispatch]);

  const finishProgrammaticScroll = useCallback(() => {
    dispatch({ type: "PROGRAMMATIC_END" });
    endReaderIntent();
  }, [dispatch, endReaderIntent]);

  // getState invokes its callback synchronously with the live measured tree
  // and scrollTop (header height excluded).
  const captureStateSnapshot = useCallback((): StateSnapshot | null => {
    const handle = virtuosoRef.current;
    if (!handle) return null;
    let state: StateSnapshot | null = null;
    handle.getState((snapshot) => { state = snapshot; });
    return state;
  }, []);

  const restoreTailIfNotScrollable = useCallback(() => {
    const element = scrollRef.current;
    if (!element || hasTranscriptScrollableRange(element)) return false;
    deliverScroll(element);
    return true;
  }, [deliverScroll]);

  const onWheelIntent = useCallback((event: ReactWheelEvent<HTMLElement>) => {
    if (event.ctrlKey || event.deltaY === 0 || Math.abs(event.deltaX) > Math.abs(event.deltaY)) return false;
    const element = scrollRef.current;
    if (element && findVerticalScrollTarget(event.target, element, event.deltaY)) return false;
    if (restoreTailIfNotScrollable()) return false;
    if (event.deltaY < 0 || !pinnedRef.current) {
      releaseTailFollow(event.deltaY > 0);
      return true;
    }
    return false;
  }, [releaseTailFollow, restoreTailIfNotScrollable]);

  const onTouchStartIntent = useCallback((event: ReactTouchEvent<HTMLElement>) => {
    touchStartYRef.current = event.touches[0]?.clientY ?? null;
  }, []);

  const onTouchMoveIntent = useCallback((event: ReactTouchEvent<HTMLElement>) => {
    const start = touchStartYRef.current;
    const current = event.touches[0]?.clientY;
    if (start == null || current == null || Math.abs(current - start) < 2) return false;
    if (restoreTailIfNotScrollable()) return false;
    if (current > start || !pinnedRef.current) {
      releaseTailFollow(current < start);
      return true;
    }
    return false;
  }, [releaseTailFollow, restoreTailIfNotScrollable]);

  const onTouchEndIntent = useCallback(() => {
    touchStartYRef.current = null;
    if (stateRef.current.readerIntent) armReaderIntentIdle();
  }, [armReaderIntentIdle]);

  const onKeyScrollIntent = useCallback((event: ReactKeyboardEvent<HTMLElement>) => {
    if (isEditableTarget(event.target)) return false;
    if (!SCROLL_UP_KEYS.has(event.key) && !SCROLL_DOWN_KEYS.has(event.key)) return false;
    if (restoreTailIfNotScrollable()) return false;
    if (SCROLL_UP_KEYS.has(event.key) || !pinnedRef.current) {
      releaseTailFollow(SCROLL_DOWN_KEYS.has(event.key));
      return true;
    }
    return false;
  }, [releaseTailFollow, restoreTailIfNotScrollable]);

  const onPointerDownIntent = useCallback((event: ReactPointerEvent<HTMLElement>) => {
    const element = scrollRef.current;
    if (element && isNativeVerticalScrollbarPointer(element, event.nativeEvent)) {
      if (!nativeScrollbarDragRef.current) {
        nativeScrollbarDragRef.current = true;
        element.dataset.nativeScrollbarDrag = "true";
        setNativeScrollbarDragging(true);
      }
      releaseTailFollow();
      return true;
    }
    if (event.button !== 1 || restoreTailIfNotScrollable()) return false;
    middlePointerScrollRef.current = true;
    releaseTailFollow();
    return true;
  }, [releaseTailFollow, restoreTailIfNotScrollable]);

  const onNestedScrollIntent = useCallback((deltaY: number) => {
    if (deltaY === 0 || restoreTailIfNotScrollable()) return false;
    if (deltaY < 0 || !pinnedRef.current) {
      releaseTailFollow(deltaY > 0);
      return true;
    }
    return false;
  }, [releaseTailFollow, restoreTailIfNotScrollable]);

  return {
    virtuosoRef,
    scrollRef,
    scrollElement,
    itemSize,
    nativeScrollbarDragging,
    pinnedRef,
    isAtBottom,
    modeRef,
    scrollerRef,
    setMode,
    reset,
    writeOffset,
    scrollToBottom,
    followGrowingTail,
    scrollToDataIndex,
    finishProgrammaticScroll,
    releaseTailFollow,
    beginUserResize,
    atBottomStateChange,
    deliverScroll,
    onWheelIntent,
    onTouchStartIntent,
    onTouchMoveIntent,
    onTouchEndIntent,
    onKeyScrollIntent,
    onPointerDownIntent,
    onNestedScrollIntent,
    submitRecoveryRequest,
    retryRecoveryRequest,
    lastGoodAnchorRef,
    captureStateSnapshot,
  };
}
