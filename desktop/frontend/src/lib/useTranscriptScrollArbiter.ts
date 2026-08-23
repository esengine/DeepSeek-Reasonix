import { useCallback, useEffect, useRef, useState } from "react";
import type {
  KeyboardEvent as ReactKeyboardEvent,
  PointerEvent as ReactPointerEvent,
  TouchEvent as ReactTouchEvent,
  WheelEvent as ReactWheelEvent,
} from "react";
import type { SizeFunction, VirtuosoHandle } from "react-virtuoso";
import { isEditableTarget } from "./keyboardShortcuts";
import { findVerticalScrollTarget, normalizeWheelDelta } from "./nestedScrollHandoff";
import { hasPendingTranscriptGeometry, isNativeVerticalScrollbarPointer, measureTranscriptVirtuosoItem } from "./transcriptNativeScrollbar";
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
import { noteTranscriptRowMeasurement, noteTranscriptScrollWrite, recordTranscriptScrollDiagnostic, type TranscriptScrollWriteRecord } from "./transcriptScrollProbe";
import {
  CAPTURE_TRANSCRIPT_SCROLL_DIAGNOSTICS,
  recordTranscriptScrollTransition,
  type TranscriptScrollDiagnosticSource,
} from "./transcriptScrollDiagnosticProbe";
import type {
  ActiveTranscriptRecovery,
  TranscriptRecoveryRequestSpec,
  TranscriptRecoveryTerminal,
} from "./transcriptScrollRecovery";
import {
  transcriptScrollEventCancelsReaderExtentGuard,
  transcriptKeyboardScrollDelta,
} from "./transcriptReaderExtentStability";
import { hasTranscriptScrollableRange, nativeTranscriptBottomTop, nativeTranscriptDistanceFromBottom, pinTranscriptTailAfterViewportShrink, TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX, type TranscriptFollowGeometry } from "./transcriptScrollGeometry";
import type { TranscriptRow } from "./transcriptRows";
import { isTranscriptRowLayoutVariant, type TranscriptEstimateSource, type TranscriptRowLayoutVariant } from "./transcriptRowGeometry";
import { captureTranscriptVirtuosoState } from "./transcriptStateSnapshot";
import { captureTranscriptLayoutAnchor, type TranscriptLayoutAnchor } from "./transcriptVirtuosoRecovery";
import { createTranscriptAnchorCompensation, type TranscriptAnchorCompensation } from "./transcriptAnchorCompensation";
import { createTranscriptTailSettle, type TranscriptTailSettle } from "./transcriptTailSettle";
import { useTranscriptReaderExtentStability } from "./useTranscriptReaderExtentStability";
export type {
  TranscriptRecoveryRequestSpec,
  TranscriptRecoveryTerminal,
  TranscriptScrollArbiterRecoveryApi,
} from "./transcriptScrollRecovery";
export { hasTranscriptScrollableRange, nativeTranscriptBottomTop, nativeTranscriptDistanceFromBottom, TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX };

const READER_INTENT_IDLE_MS = 180;
// Slow WebView2 rows need a wall-clock mount budget. Expiry suspends without
// an intermediate scrollBy, then retries after a bounded quiet window.
const ANCHOR_RESTORE_BUDGET_MS = 1_000;
const RECOVERY_MAX_RETRIES = 2;
const RECOVERY_CORRECTION_TOLERANCE_PX = 1;
const RECOVERY_STABLE_FRAMES = 2;
/** Single Virtuoso writer for tail-follow, jumps, selection, and recovery.
 * The reducer arbitrates selection > user > programmatic > recovery > tail. */
export function useTranscriptScrollArbiter({
  onRecoveryTerminal,
  onItemMeasured,
}: {
  /** Receives the terminal state of every recovery request (done /
   *  cancelled / expired); wired into session diagnostics by the caller. */
  onRecoveryTerminal?: (terminal: TranscriptRecoveryTerminal) => void;
  /** Receives real, unfrozen itemSize measurements; data-known-size is ignored. */
  onItemMeasured?: (
    rowKey: string,
    kind: TranscriptRow["kind"],
    layoutVariant: TranscriptRowLayoutVariant,
    height: number,
    width: number,
    measurementVersion: string | undefined,
    estimateSource: TranscriptEstimateSource | undefined,
    staticEstimate: number | undefined,
  ) => void;
} = {}) {
  const virtuosoRef = useRef<VirtuosoHandle>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const stateRef = useRef<TranscriptScrollState>(INITIAL_TRANSCRIPT_SCROLL_STATE);
  const pinnedRef = useRef(true);
  const modeRef = useRef<TranscriptScrollMode>("tail-follow");
  const touchStartYRef = useRef<number | null>(null);
  const nativeScrollbarDragRef = useRef(false);
  const middlePointerScrollRef = useRef(false);
  const deliverScrollRef = useRef<((element?: HTMLDivElement) => void) | null>(null);
  const generationRef = useRef(0);
  const followFrameRef = useRef<number | null>(null);
  // Virtuoso reuses physical row elements. A known size from the previous
  // logical row must not be treated as the current row's geometry contract.
  const measuredRowKeyRef = useRef(new WeakMap<HTMLElement, string>());
  const layoutTransientRef = useRef(false);
  const resizeSettleFrameRef = useRef<number | null>(null);
  const readerIntentTimerRef = useRef<number | null>(null);
  // Last gesture direction controls whether recycled rows may update
  // Virtuoso's size tree. This survives the short reader-intent lease so an
  // upward wheel gesture cannot become a resize race between two deliveries.
  const manualMeasurementFreezeRef = useRef(false);
  const followGeometryRef = useRef<TranscriptFollowGeometry>({ contentExtent: null, viewportExtent: null });
  const recoveryRef = useRef<ActiveTranscriptRecovery | null>(null);
  const nextRecoveryIdRef = useRef(0);
  // Last known-good viewport anchor: updated on every completed recovery, on
  // every user-takeover, and sampled on user scroll intent. The blank
  // watchdog restores from it instead of a nearest-mounted-row guess (#8657).
  const lastGoodAnchorRef = useRef<TranscriptLayoutAnchor | null>(null);
  // Steady-state manual-mode anchor compensation (#8438/#8488/#8897) lives in
  // its own controller; lazy-created once dispatch exists (below).
  const anchorCompensationRef = useRef<TranscriptAnchorCompensation | null>(null);
  const onRecoveryTerminalRef = useRef(onRecoveryTerminal);
  onRecoveryTerminalRef.current = onRecoveryTerminal;
  const onItemMeasuredRef = useRef(onItemMeasured);
  onItemMeasuredRef.current = onItemMeasured;
  const [nativeScrollbarDragging, setNativeScrollbarDragging] = useState(false);
  const [isAtBottom, setIsAtBottom] = useState(true);
  const [scrollElement, setScrollElement] = useState<HTMLDivElement | null>(null);
  const writeReaderCorrection = useCallback((write: TranscriptScrollWriteRecord) => {
    if (!virtuosoRef.current || write.top === undefined) return false;
    noteTranscriptScrollWrite(write);
    virtuosoRef.current.scrollBy({ top: write.top, behavior: "auto" });
    return true;
  }, []);
  const readerExtent = useTranscriptReaderExtentStability({ generationRef, modeRef, scrollRef, writeCorrection: writeReaderCorrection });

  // The tail writer and its bounded settle loop live in their own controller
  // (file-size budget); all inputs are stable refs, so it is created once.
  const tailSettleRef = useRef<TranscriptTailSettle | null>(null);
  tailSettleRef.current ??= createTranscriptTailSettle({
    virtuosoRef, scrollRef, modeRef, generationRef, layoutTransientRef,
  });
  const tailSettle = tailSettleRef.current;

  const invalidateAsyncFrames = useCallback(() => {
    generationRef.current += 1;
    if (followFrameRef.current !== null) cancelAnimationFrame(followFrameRef.current);
    if (resizeSettleFrameRef.current !== null) cancelAnimationFrame(resizeSettleFrameRef.current);
    followFrameRef.current = null;
    resizeSettleFrameRef.current = null;
    tailSettle.cancel();
    anchorCompensationRef.current?.reset();
    readerExtent.cancel();
  }, [readerExtent, tailSettle]);

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
    if (CAPTURE_TRANSCRIPT_SCROLL_DIAGNOSTICS) recordTranscriptScrollDiagnostic("recovery", { state: "cancelled", reason });
    onRecoveryTerminalRef.current?.({ id, outcome: "cancelled", reason });
  }, []);

  const publishState = useCallback((state: TranscriptScrollState) => {
    stateRef.current = state;
    modeRef.current = state.mode;
    pinnedRef.current = state.mode === "tail-follow";
    // Keep jump-bottom manual-only while tail-follow repairs footer resize gaps.
    setIsAtBottom(state.atBottom || state.mode === "tail-follow");
    if (scrollRef.current) {
      scrollRef.current.dataset.scrollMode = state.mode;
      scrollRef.current.dataset.transcriptReaderIntent = state.readerIntent ? "true" : "false";
    }
  }, []);

  const runCommand = useCallback((command: TranscriptScrollCommand, source?: TranscriptScrollDiagnosticSource) => {
    const handle = virtuosoRef.current;
    switch (command.type) {
      case "AUTOSCROLL_TO_BOTTOM":
        // Virtuoso's autoscrollToBottom() is inert without the followOutput
        // prop (never passed here), so the rAF settle loop is the real
        // follow mechanism.
        tailSettle.schedule(false, source);
        return;
      case "SCROLL_TO_LAST":
        tailSettle.scrollToTail(command.behavior, CAPTURE_TRANSCRIPT_SCROLL_DIAGNOSTICS && source
          ? { source, phase: "initial" }
          : undefined);
        // Re-aim across a bounded number of frames: the first LAST request
        // can use Virtuoso's pre-measurement size tree, and late tail-row
        // measurements would otherwise park the view above the real bottom.
        tailSettle.schedule(true, source);
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
  }, [cancelInFlightRecovery, tailSettle]);

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
      tailSettle.cancel();
    }
    if (transcriptScrollEventCancelsReaderExtentGuard(event.type)) readerExtent.cancel();
    if (event.type === "RESET") lastGoodAnchorRef.current = null;
    if (event.type === "USER_SCROLL_INTENT") {
      const element = scrollRef.current;
      const anchor = element ? captureTranscriptLayoutAnchor(element, false) : undefined;
      if (anchor) lastGoodAnchorRef.current = anchor;
    }
    const previousState = stateRef.current;
    const result = reduceTranscriptScroll(previousState, event);
    const source = recordTranscriptScrollTransition(event, previousState, result.state, result.commands, scrollRef.current);
    publishState(result.state);
    for (const command of result.commands) runCommand(command, source);
    // Post-publish so the controller's SCROLL_DELIVERED anchor sampling sees
    // the new state.
    anchorCompensationRef.current?.noteEvent(event);
    return result;
  }, [publishState, readerExtent, runCommand, tailSettle]);

  // All controller inputs are stable refs plus dispatch (itself stable: every
  // dep is a ref-closing useCallback), so this runs once per hook instance.
  anchorCompensationRef.current ??= createTranscriptAnchorCompensation({
    scrollRef, modeRef, stateRef, generationRef, dispatch,
    readerExtentIsActive: readerExtent.isActive,
  });
  const anchorCompensation = anchorCompensationRef.current;

  const endReaderIntent = useCallback(() => {
    if (readerIntentTimerRef.current !== null) window.clearTimeout(readerIntentTimerRef.current);
    readerIntentTimerRef.current = null;
    dispatch({ type: "READER_INTENT_ENDED" });
  }, [dispatch]);

  const armReaderIntentIdle = useCallback(() => {
    if (readerIntentTimerRef.current !== null) window.clearTimeout(readerIntentTimerRef.current);
    readerIntentTimerRef.current = window.setTimeout(() => {
      readerIntentTimerRef.current = null;
      // A large wheel/touch gesture can clamp the browser to the physical
      // bottom without emitting a second scroll event. Re-sample once before
      // closing the intent window so the bottom-hold policy can complete on
      // real WebView2/native scrolling as well as on synthetic deliveries.
      deliverScrollRef.current?.(scrollRef.current ?? undefined);
      dispatch({ type: "READER_INTENT_ENDED" });
    }, READER_INTENT_IDLE_MS);
  }, [dispatch]);

  const deliverScroll = useCallback((element = scrollRef.current) => {
    if (!element) return;
    const transientReaderClamp = readerExtent.observe(element);
    const distance = nativeTranscriptDistanceFromBottom(element);
    dispatch({
      type: "SCROLL_DELIVERED",
      atBottom: !transientReaderClamp && distance <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX,
      scrollable: hasTranscriptScrollableRange(element),
      substantial: isSubstantialTranscriptDisplacement(distance),
    });
    if (stateRef.current.readerIntent) armReaderIntentIdle();
  }, [armReaderIntentIdle, dispatch, readerExtent]);
  deliverScrollRef.current = deliverScroll;

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
    if (CAPTURE_TRANSCRIPT_SCROLL_DIAGNOSTICS) {
      recordTranscriptScrollDiagnostic("recovery", {
        state: terminal.outcome === "cancelled" ? "cancelled" : terminal.outcome,
        reason: terminal.outcome === "cancelled" ? terminal.reason : undefined,
      });
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
        // Re-aim until the mount budget expires, without an intermediate
        // scrollBy into estimate-only space (#8657/#8688).
        if (Date.now() >= recovery.deadline) {
          recovery.status = "suspended";
          if (CAPTURE_TRANSCRIPT_SCROLL_DIAGNOSTICS) recordTranscriptScrollDiagnostic("recovery", { state: "suspend" });
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
    if (CAPTURE_TRANSCRIPT_SCROLL_DIAGNOSTICS) {
      recordTranscriptScrollDiagnostic("recovery", { state: "begin", mode: spec.anchor.mode === "tail" ? "tail-follow" : "manual" });
    }
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
    if (CAPTURE_TRANSCRIPT_SCROLL_DIAGNOSTICS) recordTranscriptScrollDiagnostic("recovery", { state: "retry" });
    launchRecovery(recovery);
  }, [finishRecovery, launchRecovery]);

  const reset = useCallback(() => {
    invalidateAsyncFrames();
    endReaderIntent();
    manualMeasurementFreezeRef.current = false;
    followGeometryRef.current = { contentExtent: null, viewportExtent: null };
    dispatch({ type: "RESET" });
  }, [dispatch, endReaderIntent, invalidateAsyncFrames]);

  const setMode = useCallback((mode: TranscriptScrollMode, _reason?: string) => {
    switch (mode) {
      case "tail-follow": reset(); break;
      case "manual":
        manualMeasurementFreezeRef.current = true;
        dispatch({ type: "MANUAL_READING" });
        break;
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
    if (modeRef.current === "tail-follow") tailSettle.schedule(
      false,
      CAPTURE_TRANSCRIPT_SCROLL_DIAGNOSTICS ? "native-scrollbar-release" : undefined,
    );
  }, [deliverScroll, dispatch, endReaderIntent, tailSettle]);

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
    if (resizeSettleFrameRef.current !== null) cancelAnimationFrame(resizeSettleFrameRef.current);
    if (readerIntentTimerRef.current !== null) window.clearTimeout(readerIntentTimerRef.current);
    if (recoveryRef.current?.frame != null) cancelAnimationFrame(recoveryRef.current.frame);
    tailSettle.cancel();
    anchorCompensationRef.current?.reset();
    generationRef.current += 1;
    recoveryRef.current = null;
  }, [tailSettle]);

  const itemSize = useCallback<SizeFunction>((element, field) => {
    // During an active manual gesture, keep Virtuoso's current size tree
    // authoritative. Measuring a newly mounted asynchronous Markdown row in
    // the middle of the gesture would change offsets under the pointer and
    // produce the visible reverse frame this arbiter is meant to prevent.
    // Freeze only an active upward reader gesture. A downward gesture is an
    // explicit tail-claiming action and must keep measuring newly mounted
    // rows so a long hydrated transcript can reach its real physical tail.
    // Once the short reader-intent lease expires, normal measurements resume
    // at the next idle frame without remounting Virtuoso.
    const upwardManualGesture = stateRef.current.mode === "manual" && manualMeasurementFreezeRef.current;
    const frozen = nativeScrollbarDragRef.current || nativeScrollbarDragging || upwardManualGesture;
    if (CAPTURE_TRANSCRIPT_SCROLL_DIAGNOSTICS && field === "offsetHeight" && stateRef.current.readerIntent) {
      element.dataset.transcriptReaderFreeze = "true";
    }
    const pendingGeometry = field === "offsetHeight" && hasPendingTranscriptGeometry(element);
    const measured = measureTranscriptVirtuosoItem(element, field, frozen);
    if (field === "offsetHeight" && frozen) {
      const estimate = Number.parseFloat(element.dataset.transcriptEstimate ?? "");
      if (Number.isFinite(estimate) && estimate > 0) {
        // Match the physical recycled row to the same logical seed returned
        // to Virtuoso. Without this, ResizeObserver can still see the real
        // Markdown height after itemSize returned the seed and Virtuoso's
        // built-in upward compensation writes the delta back to scrollTop.
        element.style.setProperty("height", `${estimate}px`, "important");
        element.dataset.transcriptGeometryFrozen = "true";
      }
    } else if (field === "offsetHeight" && element.dataset.transcriptGeometryFrozen === "true") {
      element.style.removeProperty("height");
      delete element.dataset.transcriptGeometryFrozen;
    }
    if (field === "offsetHeight") {
      const currentRowKey = element.dataset.rowKey;
      if (currentRowKey) {
        const previousRowKey = measuredRowKeyRef.current.get(element);
        if (previousRowKey !== undefined && previousRowKey !== currentRowKey) element.dataset.transcriptRecycled = "true";
        else delete element.dataset.transcriptRecycled;
        measuredRowKeyRef.current.set(element, currentRowKey);
      }
    }
    if (CAPTURE_TRANSCRIPT_SCROLL_DIAGNOSTICS) noteTranscriptRowMeasurement(element, field, measured);
    if (!frozen && !pendingGeometry && field === "offsetHeight") {
      const rowKey = element.dataset.rowKey;
      const kind = element.dataset.rowKind as TranscriptRow["kind"] | undefined;
      const stateElement = element.querySelector<HTMLElement>("[data-transcript-layout-variant]");
      const rawVariant = stateElement?.dataset.transcriptLayoutVariant ?? element.dataset.transcriptLayoutVariant;
      const width = Number.parseFloat(element.dataset.transcriptContentWidth ?? "") || element.getBoundingClientRect().width;
      const rawSource = element.dataset.estimateSource;
      const estimateSource = rawSource === "exact" || rawSource === "compact-median" || rawSource === "calibrated" || rawSource === "static"
        ? rawSource
        : undefined;
      const staticEstimate = Number.parseFloat(element.dataset.staticEstimate ?? "");
      const staticEstimateMatchesState = rawVariant === element.dataset.transcriptLayoutVariant;
      if (rowKey && kind && isTranscriptRowLayoutVariant(rawVariant) && measured > 0 && width > 0) {
        const recycled = element.dataset.transcriptRecycled === "true";
        if (!recycled) {
          onItemMeasuredRef.current?.(
            rowKey,
            kind,
            rawVariant,
            measured,
            width,
            element.dataset.layoutVersion,
            estimateSource,
            staticEstimateMatchesState && Number.isFinite(staticEstimate) ? staticEstimate : undefined,
          );
        }
      }
    }
    return measured;
  }, [nativeScrollbarDragging]);

  const scrollerRef = useCallback((node: HTMLElement | Window | null) => {
    const element = node instanceof HTMLElement ? node as HTMLDivElement : null;
    if (scrollRef.current !== element) {
      finishNativeScrollbarDrag();
      invalidateAsyncFrames();
    }
    scrollRef.current = element;
    followGeometryRef.current.viewportExtent = element?.clientHeight ?? null;
    if (element) {
      element.dataset.scrollMode = stateRef.current.mode;
      deliverScroll(element);
    }
    setScrollElement((current) => current === element ? current : element);
  }, [deliverScroll, finishNativeScrollbarDrag, invalidateAsyncFrames]);

  const releaseTailFollow = useCallback((claimPhysicalBottom = false, readerDeltaY?: number) => {
    if (isTranscriptSelectionMode(modeRef.current)) return;
    const element = scrollRef.current;
    if (element && !stateRef.current.scrollable && hasTranscriptScrollableRange(element)) {
      deliverScroll(element);
    }
    manualMeasurementFreezeRef.current = readerDeltaY !== undefined
      ? readerDeltaY < 0
      : !claimPhysicalBottom;
    dispatch({ type: "USER_SCROLL_INTENT", canClaimTail: claimPhysicalBottom });
    if (
      claimPhysicalBottom
      && element
      && nativeTranscriptDistanceFromBottom(element) <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX
    ) {
      deliverScroll(element);
    }
    if (readerDeltaY !== undefined) readerExtent.arm(readerDeltaY);
    armReaderIntentIdle();
  }, [armReaderIntentIdle, deliverScroll, dispatch, readerExtent]);
  const followGrowingTail = useCallback(() => {
    tailSettle.noteLayoutTransient();
    readerExtent.observe();
    const pinnedTop = scrollRef.current && pinTranscriptTailAfterViewportShrink(scrollRef.current, followGeometryRef.current, pinnedRef.current);
    if (pinnedTop !== null) noteTranscriptScrollWrite({ owner: "tail-follow", kind: "scrollTo", top: pinnedTop });
    if (followFrameRef.current !== null) return;
    const generation = generationRef.current;
    const scrollElement = scrollRef.current;
    followFrameRef.current = requestAnimationFrame(() => {
      followFrameRef.current = null;
      if (generationRef.current !== generation || scrollRef.current !== scrollElement) return;
      const element = scrollRef.current;
      if (element) {
        const scrollHeight = element.scrollHeight;
        const previous = followGeometryRef.current.contentExtent;
        followGeometryRef.current.contentExtent = scrollHeight;
        if (previous != null && isTranscriptContentShrink(scrollHeight - previous)) {
          dispatch({ type: "CONTENT_SHRANK" });
          anchorCompensation.schedule();
          return;
        }
      }
      dispatch({ type: "LAYOUT_HEIGHT_CHANGED" });
      anchorCompensation.schedule();
    });
  }, [anchorCompensation, dispatch, readerExtent, tailSettle]);

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
        // An in-row resize (fold toggle) may have pushed the viewport.
        anchorCompensation.schedule();
      });
    });
  }, [anchorCompensation, dispatch]);

  const atBottomStateChange = useCallback((_atBottom: boolean) => deliverScroll(), [deliverScroll]);

  const writeOffset = useCallback((owner: TranscriptScrollOwner, top: number, behavior: ScrollBehavior = "auto") => {
    // Selection mode accepts only selection-stabilizing writes (its own edge
    // scrolls and the in-row block-window prepend compensation).
    if (isTranscriptSelectionMode(modeRef.current) && owner !== "selection-edge-scroll" && owner !== "block-window-prepend") return false;
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

  const captureStateSnapshot = useCallback(() => captureTranscriptVirtuosoState(virtuosoRef.current), []);

  const restoreTailIfNotScrollable = useCallback(() => {
    const element = scrollRef.current;
    if (!element || hasTranscriptScrollableRange(element)) return false;
    deliverScroll(element);
    return true;
  }, [deliverScroll]);

  const onWheelIntent = useCallback((event: ReactWheelEvent<HTMLElement>) => {
    const element = scrollRef.current;
    if (!element || event.ctrlKey) return false;
    const delta = normalizeWheelDelta(event, element);
    if (delta.y === 0 || Math.abs(delta.x) > Math.abs(delta.y)) return false;
    if (findVerticalScrollTarget(event.target, element, delta.y)) return false;
    if (restoreTailIfNotScrollable()) return false;
    if (delta.y < 0 || !pinnedRef.current) {
      releaseTailFollow(delta.y > 0, delta.y);
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
      const deltaY = start - current;
      touchStartYRef.current = current;
      releaseTailFollow(deltaY > 0, deltaY);
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
    const element = scrollRef.current;
    if (!element) return false;
    const deltaY = transcriptKeyboardScrollDelta(event.key, event.shiftKey, element);
    if (deltaY === undefined || deltaY === 0) return false;
    if (restoreTailIfNotScrollable()) return false;
    if (deltaY < 0 || !pinnedRef.current) {
      releaseTailFollow(deltaY > 0, deltaY);
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
      releaseTailFollow(deltaY > 0, deltaY);
      return true;
    }
    return false;
  }, [releaseTailFollow, restoreTailIfNotScrollable]);

  return {
    virtuosoRef,
    scrollRef,
    scrollElement,
    layoutTransientRef,
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
