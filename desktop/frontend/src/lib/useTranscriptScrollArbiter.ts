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
  isTranscriptSelectionMode,
  reduceTranscriptScroll, transcriptReaderBufferingForMode,
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
import { transcriptScrollEventCancelsReaderExtentGuard, transcriptKeyboardScrollDelta } from "./transcriptReaderExtentStability";
import { hasTranscriptScrollableRange, nativeTranscriptBottomTop, nativeTranscriptDistanceFromBottom, TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX } from "./transcriptScrollGeometry";
import type { TranscriptRow } from "./transcriptRows";
import { isTranscriptRowLayoutVariant, type TranscriptEstimateSource, type TranscriptRowLayoutVariant } from "./transcriptRowGeometry";
import { captureTranscriptVirtuosoState } from "./transcriptStateSnapshot";
import { captureTranscriptLayoutAnchor, type TranscriptLayoutAnchor } from "./transcriptVirtuosoRecovery";
import { createTranscriptAnchorCompensation, type TranscriptAnchorCompensation } from "./transcriptAnchorCompensation";
import { createTranscriptTailSettle, type TranscriptTailSettle } from "./transcriptTailSettle";
import { useTranscriptReaderExtentStability } from "./useTranscriptReaderExtentStability";
import { createTranscriptScrollWriter, writeTranscriptReaderCorrection } from "./transcriptScrollWriter";
import { createTranscriptReaderBottomHold } from "./transcriptReaderBottomHold";
import { createTranscriptReaderIntentIdle } from "./transcriptReaderIntentIdle";
import { createTranscriptGeometryRevision, type TranscriptGeometryChangeSource } from "./transcriptGeometryRevision";
import { shouldClaimTranscriptTailFromWheel } from "./transcriptWheelTailClaim";
import { createTranscriptNativeScrollbarBottomProof } from "./transcriptNativeScrollbarBottomProof";
export type { TranscriptRecoveryRequestSpec, TranscriptRecoveryTerminal, TranscriptScrollArbiterRecoveryApi } from "./transcriptScrollRecovery";
export { hasTranscriptScrollableRange, nativeTranscriptBottomTop, nativeTranscriptDistanceFromBottom, TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX };
export type { TranscriptGeometryChangeSource } from "./transcriptGeometryRevision";

const NATIVE_MOUSE_COMPAT_RELEASE_MS = 64;
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
  const pinnedRef = useRef(true); const modeRef = useRef<TranscriptScrollMode>("tail-follow");
  const touchStartYRef = useRef<number | null>(null); const nativeScrollbarDragRef = useRef<[startTop: number, moved: boolean] | null>(null);
  const middlePointerScrollRef = useRef(false);
  const deliverScrollRef = useRef<((element?: HTMLDivElement) => void) | null>(null);
  const dispatchEventRef = useRef<((event: TranscriptScrollEvent) => void) | null>(null);
  const generationRef = useRef(0);
  const geometryRevisionRef = useRef(0);
  const noteGeometryChangeRef = useRef<((source: TranscriptGeometryChangeSource) => void) | null>(null);
  const measuredRowKeyRef = useRef(new WeakMap<HTMLElement, string>());
  const layoutTransientRef = useRef(false);
  const resizeSettleFrameRef = useRef<number | null>(null);
  const recoveryRef = useRef<ActiveTranscriptRecovery | null>(null);
  const nextRecoveryIdRef = useRef(0);
  // Last known-good viewport anchor: updated on every completed recovery, on
  // every user-takeover, and sampled on user scroll intent. The blank
  // watchdog restores from it instead of a nearest-mounted-row guess (#8657).
  const lastGoodAnchorRef = useRef<TranscriptLayoutAnchor | null>(null);
  const anchorCompensationRef = useRef<TranscriptAnchorCompensation | null>(null);
  const onRecoveryTerminalRef = useRef(onRecoveryTerminal);
  onRecoveryTerminalRef.current = onRecoveryTerminal;
  const onItemMeasuredRef = useRef(onItemMeasured);
  onItemMeasuredRef.current = onItemMeasured;
  const [isAtBottom, setIsAtBottom] = useState(true); const [nativeScrollbarDragging, setNativeScrollbarDragging] = useState(false); const [readerBuffering, setReaderBuffering] = useState(false);
  const [scrollElement, setScrollElement] = useState<HTMLDivElement | null>(null);
  const writerRef = useRef<ReturnType<typeof createTranscriptScrollWriter> | null>(null);
  const writer = writerRef.current ??= createTranscriptScrollWriter({
    virtuosoRef,
    scrollRef,
    modeRef,
    generationRef,
  });
  const nativeScrollbarBottomProofRef = useRef<ReturnType<typeof createTranscriptNativeScrollbarBottomProof> | null>(null);
  const nativeScrollbarBottomProof = nativeScrollbarBottomProofRef.current ??= createTranscriptNativeScrollbarBottomProof({ scrollRef });
  const writeReaderCorrection = useCallback(
    (write: TranscriptScrollWriteRecord & { virtuosoSync?: boolean }) => writeTranscriptReaderCorrection(
      writer, write, generationRef.current, geometryRevisionRef.current, scrollRef.current?.scrollTop ?? 0,
    ),
    [writer],
  );
  const readerExtent = useTranscriptReaderExtentStability({
    generationRef,
    modeRef,
    scrollRef,
    writeCorrection: writeReaderCorrection,
    lastWriteOwner: writer.lastOwner,
  });
  // The tail writer and its bounded settle loop live in their own controller
  // (file-size budget); all inputs are stable refs, so it is created once.
  const tailSettleRef = useRef<TranscriptTailSettle | null>(null);
  tailSettleRef.current ??= createTranscriptTailSettle({
    writer, scrollRef, modeRef, generationRef, layoutTransientRef,
    requestResidualGeometry: () => noteGeometryChangeRef.current?.("row-measure"),
  });
  const tailSettle = tailSettleRef.current;
  const bottomHoldRef = useRef<ReturnType<typeof createTranscriptReaderBottomHold> | null>(null);
  bottomHoldRef.current ??= createTranscriptReaderBottomHold({
    scrollRef,
    stateRef,
    generationRef,
    deliverScrollRef,
    dispatch: (event) => dispatchEventRef.current?.(event),
  });
  const bottomHold = bottomHoldRef.current;
  const [cancelBottomHold, deliverBottomHold] = bottomHold;
  const readerIntentIdleRef = useRef<ReturnType<typeof createTranscriptReaderIntentIdle> | null>(null);
  readerIntentIdleRef.current ??= createTranscriptReaderIntentIdle({
    scrollRef,
    stateRef,
    generationRef,
    deliverScrollRef,
    dispatch: (event) => dispatchEventRef.current?.(event),
  });
  const [endReaderIntent, armReaderIntentIdle, cancelReaderIntentIdle] = readerIntentIdleRef.current;
  const geometryRef = useRef<ReturnType<typeof createTranscriptGeometryRevision> | null>(null);
  geometryRef.current ??= createTranscriptGeometryRevision({
    scrollRef,
    modeRef,
    generationRef,
    revisionRef: geometryRevisionRef,
    dispatch: (event) => dispatchEventRef.current?.(event),
    noteLayoutTransient: tailSettle.noteLayoutTransient,
    observeReaderExtent: readerExtent.observe,
    scheduleAnchorCompensation: () => anchorCompensationRef.current?.schedule(),
  });
  const geometry = geometryRef.current;
  const [cancelGeometry, noteGeometryChange, observeListHeight, resetGeometry] = geometry;
  noteGeometryChangeRef.current = noteGeometryChange;
  const invalidateAsyncFrames = useCallback(() => {
    generationRef.current += 1;
    if (resizeSettleFrameRef.current !== null) cancelAnimationFrame(resizeSettleFrameRef.current);
    resizeSettleFrameRef.current = null;
    cancelBottomHold();
    cancelGeometry();
    tailSettle.cancel();
    anchorCompensationRef.current?.reset();
    readerExtent.cancel();
    nativeScrollbarBottomProof.cancel();
  }, [cancelBottomHold, cancelGeometry, nativeScrollbarBottomProof, readerExtent, tailSettle]);

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

  const publishState = useCallback((state: TranscriptScrollState, eventType?: TranscriptScrollEvent["type"]) => {
    stateRef.current = state;
    modeRef.current = state.mode;
    pinnedRef.current = state.mode === "tail-follow";
    setReaderBuffering((active) => transcriptReaderBufferingForMode(active, state.mode, eventType));
    // Publish committed tail ownership; physical coincidence in manual mode
    // must keep the explicit jump-bottom path available.
    setIsAtBottom(state.mode === "tail-follow");
    if (scrollRef.current) {
      scrollRef.current.dataset.scrollMode = state.mode;
      scrollRef.current.dataset.transcriptReaderIntent = state.readerIntent ? "true" : "false";
      scrollRef.current.dataset.transcriptBottomHoldCount = String(state.bottomHoldCount);
      scrollRef.current.dataset.transcriptCanClaimTail = state.readerIntentCanClaimTail ? "true" : "false";
    }
  }, []);
  const runCommand = useCallback((command: TranscriptScrollCommand, source: TranscriptScrollDiagnosticSource) => {
    switch (command.type) {
      case "AUTOSCROLL_TO_BOTTOM":
        // Virtuoso's autoscrollToBottom() is inert without the followOutput
        // prop (never passed here), so the rAF settle loop is the real
        // follow mechanism.
        tailSettle.schedule(command.revision ?? geometryRevisionRef.current, false, source, command.deferUntilStable);
        return;
      case "SCROLL_TO_LAST":
        tailSettle.scrollToTail(command.behavior, { source, phase: "initial" }, geometryRevisionRef.current);
        // One LAST request mounts the real tail through Virtuoso's bounded
        // size-tree retry. The controller then confirms the native extent;
        // geometry revisions cannot start a competing settle transaction.
        tailSettle.schedule(geometryRevisionRef.current, true, source);
        return;
      case "SCROLL_TO_INDEX":
        writer.write({ owner: "jump", operation: "scrollToIndex", index: command.index, behavior: command.behavior, source, expectedGeneration: generationRef.current, geometryRevision: geometryRevisionRef.current });
        return;
      case "SCROLL_TO_OFFSET":
        writer.write(command.anchor
          ? { owner: command.owner, operation: "scrollToIndex", index: command.anchor.index, align: "start", offset: command.anchor.offset, behavior: command.behavior, source, expectedGeneration: generationRef.current, geometryRevision: geometryRevisionRef.current }
          : { owner: command.owner, operation: "scrollTo", top: command.top, behavior: command.behavior, source, expectedGeneration: generationRef.current, geometryRevision: geometryRevisionRef.current });
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
      || event.type === "JUMP_TO_BOTTOM"
      || event.type === "JUMP_TO_INDEX"
      || event.type === "SCROLL_TO_OFFSET"
      || event.type === "NATIVE_SCROLLBAR_BEGIN"
    ) {
      tailSettle.cancel();
    }
    if (transcriptScrollEventCancelsReaderExtentGuard(event.type)) readerExtent.cancel();
    if (
      (event.type === "USER_SCROLL_INTENT" && !event.canClaimTail)
      || event.type === "READER_INTENT_ENDED"
      || event.type === "NATIVE_SCROLLBAR_BEGIN"
      || event.type === "SELECTION_BEGIN"
      || event.type === "PROGRAMMATIC_BEGIN"
      || event.type === "JUMP_TO_BOTTOM"
      || event.type === "JUMP_TO_INDEX"
      || event.type === "SCROLL_TO_OFFSET"
      || event.type === "RECOVERY_BEGIN"
      || event.type === "RESET"
    ) cancelBottomHold();
    if (event.type === "RESET") lastGoodAnchorRef.current = null;
    if (event.type === "USER_SCROLL_INTENT") {
      const element = scrollRef.current;
      const anchor = element ? captureTranscriptLayoutAnchor(element, false) : undefined;
      if (anchor) lastGoodAnchorRef.current = anchor;
    }
    const previousState = stateRef.current;
    const result = reduceTranscriptScroll(previousState, event);
    const source = recordTranscriptScrollTransition(event, previousState, result.state, result.commands, scrollRef.current);
    publishState(result.state, event.type);
    for (const command of result.commands) runCommand(command, source);
    // Post-publish so the controller's SCROLL_DELIVERED anchor sampling sees
    // the new state.
    anchorCompensationRef.current?.noteEvent(event);
    return result;
  }, [cancelBottomHold, publishState, readerExtent, runCommand, tailSettle]);
  dispatchEventRef.current = dispatch;

  // All controller inputs are stable refs plus dispatch (itself stable: every
  // dep is a ref-closing useCallback), so this runs once per hook instance.
  anchorCompensationRef.current ??= createTranscriptAnchorCompensation({
    scrollRef, modeRef, stateRef, generationRef, dispatch,
    readerExtentIsActive: readerExtent.isActive,
  });
  const anchorCompensation = anchorCompensationRef.current;

  const deliverScroll = useCallback((element = scrollRef.current, provedNativeBottom = false, nativeScrollEvent = false) => {
    if (!element) return;
    if (nativeScrollbarDragRef.current) { nativeScrollbarDragRef.current[1] ||= nativeScrollEvent && Math.abs(element.scrollTop - nativeScrollbarDragRef.current[0]) > 1; nativeScrollbarBottomProof.observe(element); }
    readerExtent.observeNativeDelivery(element);
    const retainsNativeBottomIntent = deliverBottomHold(element, provedNativeBottom);
    if (stateRef.current.readerIntent && !retainsNativeBottomIntent) armReaderIntentIdle();
  }, [armReaderIntentIdle, deliverBottomHold, nativeScrollbarBottomProof, readerExtent]);
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
          writer.write({
            owner: "recovery",
            operation: "scrollToIndex",
            index: location.index,
            behavior: location.behavior,
            expectedGeneration: recovery.generation,
            geometryRevision: geometryRevisionRef.current,
            source: "recovery-begin",
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
          expectedGeneration: recovery.generation,
          geometryRevision: geometryRevisionRef.current,
          source: "recovery-end",
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
      generation: generationRef.current,
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
    setReaderBuffering(false); invalidateAsyncFrames();
    endReaderIntent();
    resetGeometry();
    dispatch({ type: "RESET" });
  }, [dispatch, endReaderIntent, invalidateAsyncFrames, resetGeometry]);

  const setMode = useCallback((mode: TranscriptScrollMode, _reason?: string) => {
    switch (mode) {
      case "tail-follow": reset(); break;
      case "reader-gesture":
        dispatch({ type: "USER_SCROLL_INTENT", canClaimTail: false });
        break;
      case "native-thumb": dispatch({ type: "NATIVE_SCROLLBAR_BEGIN" }); break;
      case "manual":
        dispatch({ type: "MANUAL_READING" });
        break;
      case "user-resize": dispatch({ type: "USER_RESIZE_BEGIN" }); break;
      case "selection": dispatch({ type: "SELECTION_BEGIN" }); break;
      case "programmatic": dispatch({ type: "PROGRAMMATIC_BEGIN" }); break;
      // Recovery ownership can only begin with a concrete request id through
      // submitRecoveryRequest; setMode is intentionally not an entry point.
      case "recovery": break;
    }
  }, [dispatch, reset]);

  const finishNativeScrollbarDrag = useCallback((pointerY?: number) => {
    if (!nativeScrollbarDragRef.current) return;
    const element = scrollRef.current;
    if (element && pointerY !== undefined) nativeScrollbarBottomProof.observe(element, pointerY);
    const [provedThumbMoved, reachedBottom] = nativeScrollbarBottomProof.finish(element); const thumbMoved = provedThumbMoved || Boolean(nativeScrollbarDragRef.current?.[1]);
    nativeScrollbarDragRef.current = null; setNativeScrollbarDragging(false);
    dispatch({ type: "NATIVE_SCROLLBAR_END", canClaimTail: thumbMoved });
    if (element) {
      delete element.dataset.nativeScrollbarDrag;
      const generation = generationRef.current;
      requestAnimationFrame(() => {
        if (generationRef.current !== generation || scrollRef.current !== element) return;
        deliverScroll(element, reachedBottom || (thumbMoved && nativeTranscriptDistanceFromBottom(element) <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX));
        requestAnimationFrame(() => {
          if (generationRef.current === generation && scrollRef.current === element) deliverScroll(element);
        });
      });
    }
    if (!reachedBottom) armReaderIntentIdle();
  }, [armReaderIntentIdle, deliverScroll, dispatch, nativeScrollbarBottomProof]);
  const finishPointerIntent = useCallback((event?: PointerEvent) => {
    if (event?.pointerType === "mouse" && nativeScrollbarDragRef.current) {
      const drag = nativeScrollbarDragRef.current;
      const pointerY = event.clientY;
      window.setTimeout(() => {
        if (drag && nativeScrollbarDragRef.current === drag) finishNativeScrollbarDrag(pointerY);
      }, NATIVE_MOUSE_COMPAT_RELEASE_MS);
      return; // Mouseup retains the native gutter coordinate; the timer is a bounded missing-mouseup fallback.
    }
    if (nativeScrollbarDragRef.current) finishNativeScrollbarDrag(event?.clientY);
    if (middlePointerScrollRef.current) { middlePointerScrollRef.current = false; endReaderIntent(); }
  }, [endReaderIntent, finishNativeScrollbarDrag]);
  const finishMouseIntent = useCallback((event: MouseEvent) => { if (nativeScrollbarDragRef.current) finishNativeScrollbarDrag(event.clientY); if (middlePointerScrollRef.current) { middlePointerScrollRef.current = false; endReaderIntent(); } }, [endReaderIntent, finishNativeScrollbarDrag]);
  const finishAllReaderIntent = useCallback(() => {
    finishPointerIntent();
    endReaderIntent();
  }, [endReaderIntent, finishPointerIntent]);

  useEffect(() => {
    const observeNativeThumb = (event: PointerEvent | MouseEvent) => { const element = scrollRef.current; if (nativeScrollbarDragRef.current && element) nativeScrollbarBottomProof.observe(element, event.clientY); };
    window.addEventListener("pointermove", observeNativeThumb, true); window.addEventListener("mousemove", observeNativeThumb, true); window.addEventListener("pointerup", finishPointerIntent, true); window.addEventListener("pointercancel", finishPointerIntent, true); window.addEventListener("mouseup", finishMouseIntent, true); window.addEventListener("blur", finishAllReaderIntent);
    return () => {
      window.removeEventListener("pointermove", observeNativeThumb, true); window.removeEventListener("mousemove", observeNativeThumb, true); window.removeEventListener("pointerup", finishPointerIntent, true); window.removeEventListener("pointercancel", finishPointerIntent, true); window.removeEventListener("mouseup", finishMouseIntent, true); window.removeEventListener("blur", finishAllReaderIntent);
    };
  }, [finishAllReaderIntent, finishMouseIntent, finishPointerIntent, nativeScrollbarBottomProof]);
  useEffect(() => () => {
    if (resizeSettleFrameRef.current !== null) cancelAnimationFrame(resizeSettleFrameRef.current);
    cancelReaderIntentIdle();
    if (recoveryRef.current?.frame != null) cancelAnimationFrame(recoveryRef.current.frame);
    tailSettle.cancel();
    cancelGeometry();
    cancelBottomHold();
    anchorCompensationRef.current?.reset();
    generationRef.current += 1;
    recoveryRef.current = null;
  }, [cancelBottomHold, cancelGeometry, cancelReaderIntentIdle, tailSettle]);

  const itemSize = useCallback<SizeFunction>((element, field) => {
    if (CAPTURE_TRANSCRIPT_SCROLL_DIAGNOSTICS && field === "offsetHeight" && stateRef.current.readerIntent) {
      element.dataset.transcriptReaderFreeze = "true";
    }
    const measured = measureTranscriptVirtuosoItem(element, field);
    const pendingGeometry = field === "offsetHeight" && hasPendingTranscriptGeometry(element);
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
    if (!pendingGeometry && field === "offsetHeight") {
      const knownSize = Number.parseFloat(element.dataset.knownSize ?? "");
      if (!Number.isFinite(knownSize) || Math.abs(knownSize - measured) > 0.5) {
        noteGeometryChangeRef.current?.("row-measure");
      }
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
  }, []);

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
      const generation = generationRef.current;
      requestAnimationFrame(() => {
        if (generationRef.current === generation && scrollRef.current === element && modeRef.current === "tail-follow") noteGeometryChangeRef.current?.("viewport");
      });
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
    // Fence the synthetic delivery from a retained opposite-direction guard.
    if (readerDeltaY !== undefined) {
      readerExtent.arm(readerDeltaY);
    }
    if (
      claimPhysicalBottom
      && element
      && nativeTranscriptDistanceFromBottom(element) <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX
    ) {
      deliverScroll(element);
    }
    armReaderIntentIdle();
  }, [armReaderIntentIdle, deliverScroll, dispatch, readerExtent]);
  // Compatibility alias for existing layout owners. New call sites should
  // name the actual geometry source through noteGeometryChange.
  const followGrowingTail = useCallback(() => noteGeometryChange("row-measure"), [noteGeometryChange]);

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
        noteGeometryChange("user-resize");
        // An in-row resize (fold toggle) may have pushed the viewport.
        anchorCompensation.schedule();
      });
    });
  }, [anchorCompensation, dispatch, noteGeometryChange]);

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
    const bottomDistance = nativeTranscriptDistanceFromBottom(element);
    if (modeRef.current !== "tail-follow" && shouldClaimTranscriptTailFromWheel(bottomDistance, delta.y, layoutTransientRef.current)) {
      event.preventDefault?.();
      scrollToBottom();
      return false;
    }
    if (delta.y > 0 && modeRef.current === "tail-follow") {
      if (!layoutTransientRef.current && bottomDistance <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX) {
        // A stable physical tail has no lower reader position; consume WebKit rubber-band.
        event.preventDefault?.();
        return false;
      }
      // Keep the current tail transaction authoritative while native input consumes its measured remainder.
      return true;
    }
    releaseTailFollow(delta.y > 0, delta.y);
    return true;
  }, [releaseTailFollow, restoreTailIfNotScrollable, scrollToBottom]);

  const onTouchStartIntent = useCallback((event: ReactTouchEvent<HTMLElement>) => {
    touchStartYRef.current = event.touches[0]?.clientY ?? null;
  }, []);

  const onTouchMoveIntent = useCallback((event: ReactTouchEvent<HTMLElement>) => {
    const start = touchStartYRef.current;
    const current = event.touches[0]?.clientY;
    if (start == null || current == null || Math.abs(current - start) < 2) return false;
    if (restoreTailIfNotScrollable()) return false;
    const deltaY = start - current;
    touchStartYRef.current = current;
    releaseTailFollow(deltaY > 0, deltaY);
    return true;
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
    releaseTailFollow(deltaY > 0, deltaY);
    return true;
  }, [releaseTailFollow, restoreTailIfNotScrollable]);

  const onPointerDownIntent = useCallback((event: ReactPointerEvent<HTMLElement>) => {
    const element = scrollRef.current;
    if (element && isNativeVerticalScrollbarPointer(element, event.nativeEvent)) {
      if (!nativeScrollbarDragRef.current) {
        nativeScrollbarDragRef.current = [element.scrollTop, false]; setNativeScrollbarDragging(true); element.dataset.nativeScrollbarDrag = "true";
        nativeScrollbarBottomProof.begin(element, event.clientY);
        dispatch({ type: "NATIVE_SCROLLBAR_BEGIN" });
      }
      return true;
    }
    if (event.button !== 1 || restoreTailIfNotScrollable()) return false;
    middlePointerScrollRef.current = true;
    releaseTailFollow();
    return true;
  }, [dispatch, nativeScrollbarBottomProof, releaseTailFollow, restoreTailIfNotScrollable]);

  const onNestedScrollIntent = useCallback((deltaY: number) => {
    if (deltaY === 0 || restoreTailIfNotScrollable()) return false;
    releaseTailFollow(deltaY > 0, deltaY);
    return true;
  }, [releaseTailFollow, restoreTailIfNotScrollable]);
  return {
    virtuosoRef,
    scrollRef,
    scrollElement,
    layoutTransientRef,
    itemSize,
    nativeScrollbarDragging, readerBuffering,
    pinnedRef,
    isAtBottom,
    modeRef,
    scrollerRef,
    setMode,
    reset,
    writeOffset,
    scrollToBottom,
    followGrowingTail,
    noteGeometryChange,
    observeListHeight,
    scrollToDataIndex,
    finishProgrammaticScroll,
    releaseTailFollow,
    beginUserResize,
    atBottomStateChange,
    deliverScroll,
    observeReaderExtent: readerExtent.observe,
    cancelReaderExtent: readerExtent.cancel,
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
