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
import { noteTranscriptRowMeasurement, recordTranscriptScrollDiagnostic, type TranscriptScrollWriteRecord } from "./transcriptScrollProbe";
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
  MIN_REVERSE_JUMP_PX,
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
import { useTranscriptNativeScrollbarOwnership } from "./useTranscriptNativeScrollbarOwnership";
import { createTranscriptScrollWriter } from "./transcriptScrollWriter";
import { createTranscriptQuestionJumpOwnership } from "./transcriptQuestionJumpOwnership";
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
  const nativeScrollbarOwnershipRef = useRef<ReturnType<typeof useTranscriptNativeScrollbarOwnership> | null>(null);
  const middlePointerScrollRef = useRef(false);
  const deliverScrollRef = useRef<((element?: HTMLDivElement) => void) | null>(null);
  const generationRef = useRef(0);
  const questionJumpOwnershipRef = useRef<ReturnType<typeof createTranscriptQuestionJumpOwnership> | null>(null);
  const geometryRevisionRef = useRef(0);
  const followFrameRef = useRef<number | null>(null);
  // Virtuoso reuses physical row elements. A known size from the previous
  // logical row must not be treated as the current row's geometry contract.
  const measuredRowKeyRef = useRef(new WeakMap<HTMLElement, string>());
  const layoutTransientRef = useRef(false);
  const resizeSettleFrameRef = useRef<number | null>(null);
  const readerIntentTimerRef = useRef<number | null>(null);
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
  const [isAtBottom, setIsAtBottom] = useState(true);
  const [scrollElement, setScrollElement] = useState<HTMLDivElement | null>(null);
  const [userResizeRevision, setUserResizeRevision] = useState(0);
  const writerRef = useRef<ReturnType<typeof createTranscriptScrollWriter> | null>(null);
  writerRef.current ??= createTranscriptScrollWriter({ virtuosoRef, scrollRef, modeRef, generationRef });
  const writer = writerRef.current;
  const writeReaderCorrection = useCallback((write: TranscriptScrollWriteRecord) => {
    if (write.top === undefined) return false;
    return writer.write({
      owner: write.owner,
      operation: "scrollBy",
      top: write.top,
      behavior: "auto",
      source: write.source ?? "reader-stability",
      expectedGeneration: generationRef.current,
      geometryRevision: geometryRevisionRef.current,
    });
  }, [writer]);
  const readerExtent = useTranscriptReaderExtentStability({ generationRef, modeRef, scrollRef, writeCorrection: writeReaderCorrection });

  // The tail writer and its bounded settle loop live in their own controller
  // (file-size budget); all inputs are stable refs, so it is created once.
  const tailSettleRef = useRef<TranscriptTailSettle | null>(null);
  tailSettleRef.current ??= createTranscriptTailSettle({
    writer, scrollRef, modeRef, generationRef, geometryRevisionRef, layoutTransientRef,
  });
  const tailSettle = tailSettleRef.current;

  const invalidateAsyncFrames = useCallback(() => {
    // Generations may advance without replacing the scroller; end the old
    // browser-owned transaction before its frozen geometry can leak across.
    nativeScrollbarOwnershipRef.current?.cancel();
    generationRef.current += 1;
    geometryRevisionRef.current = 0;
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
    const writeSource = source ?? command.type.toLowerCase();
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
        writer.write({ owner: "jump", operation: "scrollToIndex", index: command.index, behavior: command.behavior, source: writeSource, expectedGeneration: generationRef.current, geometryRevision: geometryRevisionRef.current });
        return;
      case "SCROLL_TO_OFFSET":
        writer.write({ owner: command.owner, operation: "scrollTo", top: command.top, behavior: command.behavior, source: writeSource, expectedGeneration: generationRef.current, geometryRevision: geometryRevisionRef.current });
        return;
      case "CANCEL_RECOVERY":
        cancelInFlightRecovery(command.id, command.reason);
    }
  }, [cancelInFlightRecovery, tailSettle, writer]);

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
    if (previousState.mode !== "tail-follow" && result.state.mode === "tail-follow") {
      // Tail ownership is published only after every reader-owned async path
      // is invalidated. Otherwise a queued correction can land on the new
      // owner after the reducer has already committed tail-follow.
      readerExtent.cancel();
      anchorCompensationRef.current?.reset();
      if (readerIntentTimerRef.current !== null) {
        window.clearTimeout(readerIntentTimerRef.current);
        readerIntentTimerRef.current = null;
      }
    }
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
  questionJumpOwnershipRef.current ??= createTranscriptQuestionJumpOwnership({ invalidateAsyncFrames, endReaderIntent, dispatch });
  const questionJumpOwnership = questionJumpOwnershipRef.current;

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
    nativeScrollbarOwnershipRef.current?.observe(element);
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

  const pinLiveTailBeforePaint = useCallback(() => tailSettle.pinLiveTailBeforePaint(), [tailSettle]);

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
          writer.write({
            owner: "recovery",
            operation: "scrollToIndex",
            index: location.index,
            align: location.align,
            behavior: location.behavior,
            source: "recovery-mount",
            expectedGeneration: generationRef.current,
            geometryRevision: geometryRevisionRef.current,
          });
        }
        recovery.frame = requestAnimationFrame(tick);
        return;
      }
      const viewportTop = element.getBoundingClientRect().top;
      const correction = row.getBoundingClientRect().top - viewportTop - anchor.offset;
      if (Math.abs(correction) > RECOVERY_CORRECTION_TOLERANCE_PX) {
        writer.write({
          owner: "recovery",
          operation: "scrollBy",
          top: correction,
          behavior: "auto",
          source: "recovery-anchor",
          expectedGeneration: generationRef.current,
          geometryRevision: geometryRevisionRef.current,
        });
      }
      recovery.stableFrames = Math.abs(correction) <= RECOVERY_CORRECTION_TOLERANCE_PX ? recovery.stableFrames + 1 : 0;
      if (Date.now() < recovery.deadline && recovery.stableFrames < RECOVERY_STABLE_FRAMES) {
        recovery.frame = requestAnimationFrame(tick);
        return;
      }
      finishRecovery(recovery, { outcome: "done" });
    };
    recovery.frame = requestAnimationFrame(tick);
  }, [finishRecovery, scrollToBottom, writer]);

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
    questionJumpOwnership.reset();
    invalidateAsyncFrames();
    endReaderIntent();
    followGeometryRef.current = { contentExtent: null, viewportExtent: null };
    dispatch({ type: "RESET" });
  }, [dispatch, endReaderIntent, invalidateAsyncFrames]);

  const setMode = useCallback((mode: TranscriptScrollMode, _reason?: string) => {
    switch (mode) {
      case "tail-follow": reset(); break;
      case "manual":
        dispatch({ type: "MANUAL_READING" });
        break;
      case "native-thumb": dispatch({ type: "NATIVE_SCROLLBAR_BEGIN" }); break;
      case "user-resize": dispatch({ type: "USER_RESIZE_BEGIN" }); break;
      case "selection": dispatch({ type: "SELECTION_BEGIN" }); break;
      case "restoring": dispatch({ type: "PROGRAMMATIC_BEGIN" }); break;
    }
  }, [dispatch, reset]);

  const nativeScrollbarOwnership = useTranscriptNativeScrollbarOwnership({
    scrollRef, modeRef, readerIntentTimerRef, deliverScroll, dispatch, tailSettle,
  });
  nativeScrollbarOwnershipRef.current = nativeScrollbarOwnership;
  const { begin: beginNativeScrollbarDrag, cancel: cancelNativeScrollbarDrag,
    dragging: nativeScrollbarDragging, finish: finishNativeScrollbarDrag,
    isActive: nativeScrollbarDragIsActive } = nativeScrollbarOwnership;

  const finishPointerIntent = useCallback((event?: PointerEvent) => {
    if (nativeScrollbarDragIsActive()) finishNativeScrollbarDrag(event?.pointerId);
    if (middlePointerScrollRef.current) {
      middlePointerScrollRef.current = false;
      endReaderIntent();
    }
  }, [endReaderIntent, finishNativeScrollbarDrag, nativeScrollbarDragIsActive]);

  const finishAllReaderIntent = useCallback(() => {
    finishNativeScrollbarDrag();
    endReaderIntent();
  }, [endReaderIntent, finishNativeScrollbarDrag]);

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
    cancelNativeScrollbarDrag();
    if (followFrameRef.current !== null) cancelAnimationFrame(followFrameRef.current);
    if (resizeSettleFrameRef.current !== null) cancelAnimationFrame(resizeSettleFrameRef.current);
    if (readerIntentTimerRef.current !== null) window.clearTimeout(readerIntentTimerRef.current);
    if (recoveryRef.current?.frame != null) cancelAnimationFrame(recoveryRef.current.frame);
    tailSettle.cancel();
    anchorCompensationRef.current?.reset();
    generationRef.current += 1;
    recoveryRef.current = null;
  }, [cancelNativeScrollbarDrag, tailSettle]);

  const itemSize = useCallback<SizeFunction>((element, field) => {
    // Native scrollbar dragging may keep the current Virtuoso size tree. Wheel
    // and touch reading measure the current logical row normally; anchor
    // compensation handles the settled layout delta instead of freezing a
    // recycled DOM node to an estimate.
    const frozen = nativeScrollbarDragIsActive() || nativeScrollbarDragging;
    if (CAPTURE_TRANSCRIPT_SCROLL_DIAGNOSTICS && field === "offsetHeight" && stateRef.current.readerIntent) {
      element.dataset.transcriptReaderFreeze = "true";
    }
    const measured = measureTranscriptVirtuosoItem(element, field, frozen);
    const pendingGeometry = field === "offsetHeight" && hasPendingTranscriptGeometry(element);
    if (field === "offsetHeight" && frozen && pendingGeometry) {
      const estimate = Number.parseFloat(element.dataset.knownSize ?? element.dataset.transcriptEstimate ?? element.dataset.staticEstimate ?? "");
      if (Number.isFinite(estimate) && estimate > 0) {
        // Native thumb dragging owns the physical track. Keep the currently
        // measured row box stable for that drag only; wheel/touch reading does
        // not enter this branch and never freezes a recycled row.
        element.style.height = `${estimate}px`;
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
      const estimateSource = rawSource === "exact" || rawSource === "calibrated" || rawSource === "static"
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
    dispatch({ type: "USER_SCROLL_INTENT", canClaimTail: claimPhysicalBottom });
    if (
      claimPhysicalBottom
      && element
      && nativeTranscriptDistanceFromBottom(element) <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX
    ) {
      deliverScroll(element);
    }
    if (readerDeltaY !== undefined) {
      // A downward reader gesture at (or close to) the physical bottom has no
      // extent above it to reverse onto. Arming the guard here would let a
      // later extent rebound snap the viewport back up and fight the wheel.
      const nearBottom = element
        && nativeTranscriptDistanceFromBottom(element) < MIN_REVERSE_JUMP_PX;
      if (readerDeltaY <= 0 || !nearBottom) readerExtent.arm(readerDeltaY);
    }
    armReaderIntentIdle();
  }, [armReaderIntentIdle, deliverScroll, dispatch, readerExtent]);
  const followGrowingTail = useCallback(() => {
    geometryRevisionRef.current += 1;
    tailSettle.noteLayoutTransient();
    readerExtent.observe();
    const pinnedTop = scrollRef.current && pinTranscriptTailAfterViewportShrink(scrollRef.current, followGeometryRef.current, pinnedRef.current);
    if (pinnedTop !== null) tailSettle.scrollToTail("auto");
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
    setUserResizeRevision((revision) => revision + 1);
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
    // Native scrollend can fire after the indexed jump but before the target
    // has survived its two-frame paint commit. Only the matching jump token
    // may release that transaction.
    if (questionJumpOwnership.blocksGenericFinish()) return;
    dispatch({ type: "PROGRAMMATIC_END" });
    endReaderIntent();
  }, [dispatch, endReaderIntent, questionJumpOwnership]);

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
      beginNativeScrollbarDrag(event.nativeEvent.pointerId, element);
      return true;
    }
    if (event.button !== 1 || restoreTailIfNotScrollable()) return false;
    middlePointerScrollRef.current = true;
    releaseTailFollow();
    return true;
  }, [beginNativeScrollbarDrag, releaseTailFollow, restoreTailIfNotScrollable]);

  const onNestedScrollIntent = useCallback((deltaY: number) => {
    if (deltaY === 0 || restoreTailIfNotScrollable()) return false;
    if (deltaY < 0 || !pinnedRef.current) {
      releaseTailFollow(deltaY > 0, deltaY);
      return true;
    }
    return false;
  }, [releaseTailFollow, restoreTailIfNotScrollable]);

  return {
    virtuosoRef, scrollRef, scrollElement, layoutTransientRef,
    itemSize, nativeScrollbarDragging, pinnedRef, isAtBottom, modeRef,
    scrollerRef, setMode, reset, writeOffset,
    scrollToBottom, pinLiveTailBeforePaint, followGrowingTail, scrollToDataIndex,
    beginQuestionJump: questionJumpOwnership.begin,
    finishQuestionJump: questionJumpOwnership.finish,
    finishProgrammaticScroll, releaseTailFollow, beginUserResize, userResizeRevision,
    atBottomStateChange, deliverScroll, onWheelIntent,
    onTouchStartIntent, onTouchMoveIntent, onTouchEndIntent,
    onKeyScrollIntent, onPointerDownIntent, onNestedScrollIntent,
    submitRecoveryRequest, retryRecoveryRequest,
    lastGoodAnchorRef, captureStateSnapshot,
  };
}
