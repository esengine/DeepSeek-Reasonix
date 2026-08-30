import { useCallback, useEffect, useMemo, useRef, useState, type RefObject } from "react";
import type { TranscriptScrollMode } from "./transcriptScrollArbiter";
import { MIN_REVERSE_JUMP_PX, TRANSCRIPT_READER_IDLE_MS, TRANSCRIPT_READER_SETTLE_MS, transcriptReaderDirection } from "./transcriptReaderExtentStability";
import { nativeTranscriptBottomTop, nativeTranscriptDistanceFromBottom, TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX } from "./transcriptScrollGeometry";
import { recordTranscriptScrollDiagnostic, type TranscriptScrollWriteRecord } from "./transcriptScrollProbe";
import { transcriptElementViewportIsBlank } from "./transcriptVirtuosoRecovery";

const STABLE_FRAMES_REQUIRED = 2;
const GEOMETRY_EPSILON_PX = 1;
const POST_CORRECTION_SETTLE_MS = 320;

export type TranscriptReaderTransaction = {
  id: number;
  surfaceGeneration: number;
  ownershipEpoch: number;
  direction: -1 | 1;
  phase: "active" | "settling" | "handoff-pending";
  deadline: number;
  settleDeadline: number;
  baselineTop: number;
  baselineHeight: number;
  minimumHeight: number;
  lastAcceptedTop: number;
  expectedTop: number;
  anchor?: { index: number; offset: number; key?: string };
  stableFrames: number;
  lastGeometryRevision: number;
  canClaimTail: boolean;
};

type ActiveReaderTransaction = TranscriptReaderTransaction & {
  element: HTMLDivElement;
  frame: number | null;
  idleDelivered: boolean;
  correctionWritten: boolean;
  mountAnchorWritten: boolean;
  collapseObserved: boolean;
  /** Accepted direction-consistent native travel accumulated across the
   *  continuous gesture (inherited by same-direction follow-up epochs). The
   *  proof that the reader genuinely navigated this range; a collapse clamp
   *  or in-place wheel at a fabricated bottom adds nothing. */
  directionalTravelPx: number;
  anchorDisplacementObserved: boolean;
  transientCandidateHeight: number;
  transientStableFrames: number;
  lastHeight: number;
  lastBottomDistance: number;
  transient: boolean;
  visualOffset: number;
  postCorrectionSettleDeadline: number;
  /** Last tick's native height: a rebound correction spends its budget only
   *  after one unchanged-height interval with mounted viewport coverage. */
  correctionHeightSample: number;
  /** A blank rebound scroll delivery spends the single correction budget
   *  synchronously before the next paint instead of waiting for a frame. */
  prepaint: boolean;
  /** The scheduled tick, exposed so a blank rebound delivery can run it
   *  synchronously before paint. */
  tick?: () => void;
};

function collapseThreshold(element: HTMLDivElement): number {
  return Math.max(MIN_REVERSE_JUMP_PX, element.clientHeight * 0.5);
}

function captureLogicalAnchor(element: HTMLDivElement): { index: number; offset: number; key?: string } | undefined {
  const viewportTop = element.getBoundingClientRect().top;
  const viewportBottom = viewportTop + element.clientHeight;
  let intersecting: { index: number; offset: number; key?: string } | undefined;
  for (const row of element.querySelectorAll<HTMLElement>(".transcript__row[data-index]")) {
    const rect = row.getBoundingClientRect();
    const index = Number.parseInt(row.dataset.index ?? "", 10);
    if (Number.isInteger(index) && rect.bottom > viewportTop && rect.top < viewportBottom) {
      const candidate = { index, offset: rect.top - viewportTop, key: row.dataset.rowKey };
      if (rect.top >= viewportTop) return candidate;
      intersecting ??= candidate;
    }
  }
  return intersecting;
}

function rowForAnchor(element: HTMLDivElement, anchor: TranscriptReaderTransaction["anchor"]): HTMLElement | undefined {
  if (!anchor) return undefined;
  return element.querySelector<HTMLElement>(`.transcript__row[data-index="${anchor.index}"]`) ?? undefined;
}

function tailNodesMounted(element: HTMLDivElement): boolean {
  if (!element.querySelector('[data-transcript-last-row="true"]')) return false;
  const live = element.querySelector<HTMLElement>('[data-live-region="true"]');
  return !live || live.childElementCount > 0;
}

function clearVisualGuard(transaction: ActiveReaderTransaction): void {
  if (transaction.visualOffset === 0) return;
  transaction.visualOffset = 0;
  delete transaction.element.dataset.transcriptReaderVisualGuard;
  transaction.element.style.removeProperty("--transcript-reader-visual-offset");
}

export function useTranscriptReaderExtentStability({
  generationRef,
  ownershipEpochRef,
  geometryRevisionRef,
  modeRef,
  scrollRef,
  writeCorrection,
  onStart,
  onIdleDeadline,
  onStabilitySample,
  onTailHandoff,
  onEnd,
}: {
  generationRef: RefObject<number>;
  ownershipEpochRef: RefObject<number>;
  geometryRevisionRef: RefObject<number>;
  modeRef: RefObject<TranscriptScrollMode>;
  scrollRef: RefObject<HTMLDivElement | null>;
  writeCorrection: (write: TranscriptScrollWriteRecord) => boolean;
  onStart: (transaction: TranscriptReaderTransaction) => void;
  onIdleDeadline: (transaction: TranscriptReaderTransaction) => void;
  onStabilitySample: (transaction: TranscriptReaderTransaction, stable: boolean, tailEligible: boolean) => void;
  onTailHandoff: (transaction: TranscriptReaderTransaction) => void;
  onEnd: (transaction: TranscriptReaderTransaction, reason: "stable-manual" | "timeout" | "cancelled") => void;
}) {
  const transactionRef = useRef<ActiveReaderTransaction | null>(null);
  const nextIdRef = useRef(0);
  const mountedRef = useRef(true);
  /** Guards the blank-rebound synchronous tick against re-entering itself
   *  through the tick's own observe() call. */
  const syncTickInFlightRef = useRef(false);
  const [active, setActive] = useState(false);
  // Reader writer ownership is bounded, but its measured mount corridor is
  // not timer-owned. WKWebView can commit a delayed Virtuoso range replacement
  // after the 1s observation window; contracting overscan at that deadline
  // replaces measured rows with estimates and can manufacture a false extent.
  // Keep the layout-only lease until an explicit owner or surface reset calls
  // cancel(). A new reader epoch must inherit the same lease without toggling
  // the Virtuoso range in between.
  const [readerLayoutLease, setReaderLayoutLease] = useState(false);
  const callbacksRef = useRef({ onStart, onIdleDeadline, onStabilitySample, onTailHandoff, onEnd });
  callbacksRef.current = { onStart, onIdleDeadline, onStabilitySample, onTailHandoff, onEnd };
  const finish = useCallback((transaction: ActiveReaderTransaction, reason: "stable-manual" | "timeout" | "cancelled", notify = true) => {
    if (transactionRef.current !== transaction) return;
    transactionRef.current = null;
    if (transaction.frame !== null) cancelAnimationFrame(transaction.frame);
    transaction.frame = null;
    transaction.tick = undefined;
    clearVisualGuard(transaction);
    recordTranscriptScrollDiagnostic("reader-transaction", { transactionId: transaction.id, phase: "end", result: reason });
    if (notify) callbacksRef.current.onEnd(transaction, reason);
    // Keep the transaction overscan through the first deferred-anchor frame.
    // Geometry may have moved the logical row outside the ordinary window;
    // dropping overscan before compensation runs would unmount the only safe
    // measurement target and create a blank intermediate frame.
    requestAnimationFrame(() => {
      if (mountedRef.current && transactionRef.current === null) setActive(false);
    });
  }, []);

  const cancel = useCallback((notify = true) => {
    setReaderLayoutLease(false);
    const transaction = transactionRef.current;
    if (transaction) finish(transaction, "cancelled", notify);
  }, [finish]);

  const isActive = useCallback(() => transactionRef.current !== null, []);

  const observe = useCallback((element = scrollRef.current) => {
    const transaction = transactionRef.current;
    if (!element || transaction?.element !== element) return false;
    transaction.minimumHeight = Math.min(transaction.minimumHeight, element.scrollHeight);
    const threshold = collapseThreshold(element);
    const extentCollapsed = transaction.baselineHeight - transaction.minimumHeight >= threshold;
    transaction.collapseObserved = transaction.collapseObserved || extentCollapsed;
    const reverse = transaction.direction > 0
      ? transaction.lastAcceptedTop - element.scrollTop
      : element.scrollTop - transaction.lastAcceptedTop;
    const viewport = element.getBoundingClientRect();
    let anchorRow = rowForAnchor(element, transaction.anchor);
    if (anchorRow && transaction.visualOffset === 0 && !extentCollapsed && reverse < threshold) {
      const rect = anchorRow.getBoundingClientRect();
      if (rect.bottom <= viewport.top || rect.top >= viewport.top + element.clientHeight) {
        // A harmless Virtuoso range replacement can leave the transaction's
        // old row mounted only in overscan. It is no longer a viewport anchor:
        // guarding its drift would move rows that are already visually stable.
        transaction.anchor = captureLogicalAnchor(element) ?? transaction.anchor;
        anchorRow = rowForAnchor(element, transaction.anchor);
      }
    }
    const renderedAnchorDrift = anchorRow && transaction.anchor
      ? anchorRow.getBoundingClientRect().top - viewport.top - transaction.anchor.offset
      : 0;
    // The row rect includes the current visual guard. Remove it so a stable
    // guard does not look like a fresh displacement on every observation.
    const physicalAnchorDrift = renderedAnchorDrift - transaction.visualOffset;
    const reverseAnchorDisplacement = transaction.direction * physicalAnchorDrift;
    // Extent collapse needs the half-viewport transient threshold, but the
    // user-visible screen anchor has the stricter 96px acceptance contract.
    const anchorDisplaced = reverseAnchorDisplacement >= MIN_REVERSE_JUMP_PX;
    if (anchorRow) transaction.anchorDisplacementObserved = anchorDisplaced;
    const rejected = (extentCollapsed && reverse >= threshold) || anchorDisplaced;
    const remainsCollapsed = extentCollapsed
      && element.scrollHeight < transaction.baselineHeight - Math.max(8, element.clientHeight * 0.5);
    if (remainsCollapsed) {
      if (Math.abs(element.scrollHeight - transaction.transientCandidateHeight) <= GEOMETRY_EPSILON_PX) {
        transaction.transientStableFrames += 1;
      } else {
        transaction.transientCandidateHeight = element.scrollHeight;
        transaction.transientStableFrames = 0;
      }
    } else {
      transaction.transientCandidateHeight = element.scrollHeight;
      transaction.transientStableFrames = 0;
    }
    // A collapse is provisional for two painted samples. If the smaller
    // extent persists, treat it as real geometry and restore the logical
    // anchor against that range instead of waiting for the old extent forever.
    transaction.transient = remainsCollapsed && transaction.transientStableFrames < STABLE_FRAMES_REQUIRED;
    if (rejected) {
      if (transaction.correctionWritten && anchorDisplaced) {
        transaction.postCorrectionSettleDeadline = Math.min(
          transaction.settleDeadline,
          Date.now() + POST_CORRECTION_SETTLE_MS,
        );
      }
      if (anchorRow && transaction.anchor) {
        // DOM geometry includes the transform already applied by a previous
        // observation. Subtract it before deriving the next absolute guard so
        // repeated scroll events cannot compound the visual compensation.
        transaction.visualOffset = -physicalAnchorDrift;
        element.dataset.transcriptReaderVisualGuard = "true";
        element.style.setProperty("--transcript-reader-visual-offset", `${transaction.visualOffset}px`);
      }
      recordTranscriptScrollDiagnostic("scroll-anomaly", {
        transactionId: transaction.id,
        direction: transaction.direction,
        reverseDisplacement: Math.max(reverse, reverseAnchorDisplacement),
        extentDelta: element.scrollHeight - transaction.baselineHeight,
        result: transaction.transient ? "wait-extent" : "restore-anchor",
      });
      // A rebound delivery landing on a blank viewport cannot wait for the
      // next animation frame: it would paint the transient empty range. Spend
      // the single correction budget synchronously before paint; the tick's
      // prepaint lane shares the same budget as the covered-frame fallback.
      if (
        !transaction.correctionWritten
        && transaction.collapseObserved
        && !transaction.transient
        && reverse >= threshold
        && element.scrollHeight >= transaction.baselineHeight - threshold
        && transcriptElementViewportIsBlank(element)
        && !syncTickInFlightRef.current
      ) {
        transaction.prepaint = true;
        if (transaction.frame !== null) cancelAnimationFrame(transaction.frame);
        transaction.frame = null;
        syncTickInFlightRef.current = true;
        try {
          transaction.tick?.();
        } finally {
          syncTickInFlightRef.current = false;
        }
      }
      return true;
    }
    if (!extentCollapsed && !anchorDisplaced && transaction.visualOffset !== 0) clearVisualGuard(transaction);
    const directionConsistent = transaction.direction > 0
      ? element.scrollTop >= transaction.lastAcceptedTop - 1
      : element.scrollTop <= transaction.lastAcceptedTop + 1;
    const movedInDirection = transaction.direction > 0
      ? element.scrollTop > transaction.lastAcceptedTop + 1
      : element.scrollTop < transaction.lastAcceptedTop - 1;
    if (directionConsistent && movedInDirection && !transaction.mountAnchorWritten) {
      // Accumulate the gesture's accepted directional travel: the proof that
      // the reader genuinely navigated this range. A collapse clamp or an
      // in-place wheel at a fabricated bottom adds nothing.
      transaction.directionalTravelPx += transaction.direction * (element.scrollTop - transaction.lastAcceptedTop);
      transaction.lastAcceptedTop = element.scrollTop;
      transaction.anchor = captureLogicalAnchor(element) ?? transaction.anchor;
      // A continuous same-direction gesture can outlive many streaming
      // revisions. Advance the accepted native extent when it grows; keeping
      // the transaction's mount-time height would make a later 1k collapse
      // invisible merely because it remains above that stale initial value.
      if (element.scrollHeight > transaction.baselineHeight + GEOMETRY_EPSILON_PX) {
        transaction.baselineHeight = element.scrollHeight;
        transaction.minimumHeight = element.scrollHeight;
        transaction.collapseObserved = false;
        transaction.transientCandidateHeight = element.scrollHeight;
        transaction.transientStableFrames = 0;
        transaction.transient = false;
      }
    }
    // A shrunken extent that holds for two painted samples is real geometry,
    // not a transient fault. Once the continuous gesture has also produced
    // real directional travel, accept the smaller range as the new baseline so
    // the physical tail of the shrunken transcript remains claimable; the
    // stale high-water would otherwise veto every tail handoff forever — even
    // for a reader the collapse clamped onto the bottom, who cannot produce
    // further downward displacement. Without any travel proof the high-water
    // stays, so an unread or still-transient range cannot be laundered into a
    // false tail claim.
    if (
      transaction.collapseObserved
      && remainsCollapsed
      && !transaction.transient
      && transaction.directionalTravelPx >= MIN_REVERSE_JUMP_PX
    ) {
      transaction.baselineHeight = element.scrollHeight;
      transaction.minimumHeight = element.scrollHeight;
      transaction.collapseObserved = false;
      transaction.transientCandidateHeight = element.scrollHeight;
      transaction.transientStableFrames = 0;
      recordTranscriptScrollDiagnostic("reader-transaction", {
        transactionId: transaction.id,
        ownershipEpoch: transaction.ownershipEpoch,
        direction: transaction.direction,
        phase: transaction.phase,
        result: "extent-accepted",
      });
    }
    return transaction.transient;
  }, [scrollRef]);

  const schedule = useCallback((transaction: ActiveReaderTransaction) => {
    const tick = () => {
      transaction.frame = null;
      const prepaint = transaction.prepaint;
      transaction.prepaint = false;
      if (
        transactionRef.current !== transaction
        || generationRef.current !== transaction.surfaceGeneration
        || ownershipEpochRef.current !== transaction.ownershipEpoch
        || scrollRef.current !== transaction.element
        || modeRef.current !== "manual"
      ) {
        if (transactionRef.current === transaction) finish(transaction, "cancelled", false);
        return;
      }
      const element = transaction.element;
      observe(element);
      // One unchanged-height interval per tick: a restored native extent can
      // surface one or two paints before Virtuoso mounts rows for it, so the
      // correction below must not spend its budget on the appearance frame.
      const correctionHeightStable = transaction.correctionHeightSample === element.scrollHeight;
      transaction.correctionHeightSample = element.scrollHeight;
      const now = Date.now();
      const beforeIdleDeadline = now < transaction.deadline;
      if (!beforeIdleDeadline && !transaction.idleDelivered) {
        transaction.idleDelivered = true;
        transaction.phase = "settling";
        callbacksRef.current.onIdleDeadline(transaction);
      }

      const threshold = collapseThreshold(element);
      const collapseReady = transaction.collapseObserved && !transaction.transient;
      const reverse = transaction.direction > 0
        ? transaction.lastAcceptedTop - element.scrollTop
        : element.scrollTop - transaction.lastAcceptedTop;
      const correctionReady = (collapseReady && reverse >= threshold)
        || transaction.anchorDisplacementObserved;
      const extentStillCollapsed = element.scrollHeight < transaction.baselineHeight - threshold;
      let correctionWrittenThisFrame = false;
      // Two quiet frames can fit inside a short native extent rebound. Keep
      // the visual guard during the active gesture and only commit geometry
      // from a still-collapsed range once the reader idle deadline has passed.
      // A recovered extent or a row-only displacement remains immediately
      // correctable, so ordinary measurement drift does not linger onscreen.
      if (!transaction.correctionWritten && correctionReady && (!extentStillCollapsed || !beforeIdleDeadline)) {
        const anchorRow = rowForAnchor(element, transaction.anchor);
        if (!anchorRow && transaction.anchor && !transaction.mountAnchorWritten) {
          transaction.mountAnchorWritten = writeCorrection({
            owner: "reader-stability",
            kind: "scrollToIndex",
            index: transaction.anchor.index,
            source: "extent-rebound",
            phase: "mount-anchor",
            transactionId: transaction.id,
            scrollTop: element.scrollTop,
            scrollHeight: element.scrollHeight,
            clientHeight: element.clientHeight,
            bottomDistance: nativeTranscriptDistanceFromBottom(element),
            mode: modeRef.current,
          });
          transaction.frame = requestAnimationFrame(tick);
          return;
        }
        // A recovered extent can publish its restored native height one or two
        // paints before Virtuoso mounts rows for it. Writing during that gap
        // moves the viewport into an empty size tree: a blank rebound scroll
        // delivery spends the correction synchronously before paint (the
        // prepaint lane), while the frame fallback waits for mounted coverage
        // and one unchanged-height interval. The mount-anchor step above is
        // exempt: it creates the coverage instead of moving into it.
        if (
          transaction.collapseObserved
          && !extentStillCollapsed
          && (prepaint
            ? !transcriptElementViewportIsBlank(element)
            : transcriptElementViewportIsBlank(element) || !correctionHeightStable)
        ) {
          transaction.frame = requestAnimationFrame(tick);
          return;
        }
        const viewportTop = element.getBoundingClientRect().top;
        const targetTop = anchorRow && transaction.anchor
          // getBoundingClientRect includes the temporary list transform. Use
          // the unguarded row position to derive the physical scrollTop that
          // can replace that transform without a visual jump.
          ? element.scrollTop + anchorRow.getBoundingClientRect().top - transaction.visualOffset - viewportTop - transaction.anchor.offset
          : transaction.expectedTop;
        const correction = Math.max(0, Math.min(nativeTranscriptBottomTop(element), targetTop)) - element.scrollTop;
        if ((transaction.mountAnchorWritten ? Math.abs(correction) : transaction.direction * correction) > 1) {
          correctionWrittenThisFrame = writeCorrection({
            owner: "reader-stability",
            kind: "scrollBy",
            top: correction,
            source: "extent-rebound",
            phase: "correct-offset",
            transactionId: transaction.id,
            scrollTop: element.scrollTop,
            scrollHeight: element.scrollHeight,
            clientHeight: element.clientHeight,
            bottomDistance: nativeTranscriptDistanceFromBottom(element),
            mode: modeRef.current,
          });
          transaction.correctionWritten = correctionWrittenThisFrame;
          if (correctionWrittenThisFrame) {
            transaction.postCorrectionSettleDeadline = Math.min(
              transaction.settleDeadline,
              now + POST_CORRECTION_SETTLE_MS,
            );
          }
        }
      }
      // correctionWritten is transaction-scoped. A later range replacement in
      // the same continuous gesture must not mistake the old write for a write
      // completed in this frame and immediately expose the new displacement.
      if (correctionWrittenThisFrame) {
        transaction.anchorDisplacementObserved = false;
        clearVisualGuard(transaction);
      } else if (collapseReady && reverse < threshold && !transaction.anchorDisplacementObserved) {
        clearVisualGuard(transaction);
      }

      if (beforeIdleDeadline) {
        transaction.frame = requestAnimationFrame(tick);
        return;
      }

      let bottomDistance = nativeTranscriptDistanceFromBottom(element);
      const revision = geometryRevisionRef.current;
      const stable = !transaction.transient
        && revision === transaction.lastGeometryRevision
        && Math.abs(element.scrollHeight - transaction.lastHeight) <= GEOMETRY_EPSILON_PX
        && Math.abs(bottomDistance - transaction.lastBottomDistance) <= GEOMETRY_EPSILON_PX;
      transaction.stableFrames = stable ? transaction.stableFrames + 1 : 0;
      const tailEligible = transaction.direction > 0
        && transaction.canClaimTail
        && !transaction.transient
        && (!transaction.collapseObserved
          || element.scrollHeight >= transaction.baselineHeight - collapseThreshold(element))
        && bottomDistance <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX
        && tailNodesMounted(element);
      callbacksRef.current.onStabilitySample(transaction, stable, tailEligible);
      transaction.lastGeometryRevision = revision;
      transaction.lastHeight = element.scrollHeight;
      transaction.lastBottomDistance = bottomDistance;

      if (transaction.stableFrames >= STABLE_FRAMES_REQUIRED) {
        // A corrective scroll can make Virtuoso commit a replacement range
        // well after the first two quiet animation frames. Keep observing (but
        // never reopen the writer budget) through one bounded quiet window so
        // a delayed commit is visually guarded before paint.
        if (now < transaction.postCorrectionSettleDeadline) {
          transaction.frame = requestAnimationFrame(tick);
          return;
        }
        if (tailEligible) {
          transaction.phase = "handoff-pending";
          callbacksRef.current.onTailHandoff(transaction);
          finish(transaction, "stable-manual", false);
          return;
        }
        // Two quiet frames prove only that this paint is stable. Virtuoso can
        // still commit a delayed measurement/range replacement later in the
        // same bounded settle window (especially in WKWebView). Keep the
        // transaction observationally alive so its logical anchor and visual
        // guard survive that commit; the writer budget remains closed after
        // its first correction.
        if (now < transaction.settleDeadline) {
          transaction.frame = requestAnimationFrame(tick);
          return;
        }
        finish(transaction, "stable-manual");
        return;
      }
      if (now >= transaction.settleDeadline) {
        recordTranscriptScrollDiagnostic("scroll-anomaly", {
          transactionId: transaction.id,
          direction: transaction.direction,
          reverseDisplacement: Math.max(0, reverse),
          extentDelta: element.scrollHeight - transaction.baselineHeight,
          result: "timeout-manual",
        });
        finish(transaction, "timeout");
        return;
      }
      transaction.frame = requestAnimationFrame(tick);
    };
    // Exposed so a blank rebound delivery can run the correction before paint.
    transaction.tick = tick;
    if (transaction.frame === null) transaction.frame = requestAnimationFrame(tick);
  }, [finish, generationRef, geometryRevisionRef, modeRef, observe, ownershipEpochRef, scrollRef, writeCorrection]);

  const arm = useCallback((deltaY: number, canClaimTail: boolean) => {
    const element = scrollRef.current;
    if (!element || !Number.isFinite(deltaY) || deltaY === 0) return { started: false as const };
    const direction = transcriptReaderDirection(deltaY);
    if (direction === undefined) return { started: false as const };
    setReaderLayoutLease(true);
    const current = transactionRef.current;
    const now = Date.now();
    if (
      current
      && current.element === element
      && current.surfaceGeneration === generationRef.current
      && now <= current.deadline
      && current.direction === direction
    ) {
      current.deadline = now + TRANSCRIPT_READER_IDLE_MS;
      current.settleDeadline = current.deadline + TRANSCRIPT_READER_SETTLE_MS;
      current.phase = "active";
      current.idleDelivered = false;
      current.stableFrames = 0;
      current.canClaimTail = current.canClaimTail || canClaimTail;
      current.expectedTop = Math.max(0, Math.min(nativeTranscriptBottomTop(element), current.expectedTop + deltaY));
      recordTranscriptScrollDiagnostic("reader-transaction", {
        transactionId: current.id,
        ownershipEpoch: current.ownershipEpoch,
        direction: current.direction,
        phase: "active",
        result: "extended",
      });
      observe(element);
      schedule(current);
      return { started: false as const, transactionId: current.id };
    }
    const inheritsGesture = current
      && current.element === element
      && current.surfaceGeneration === generationRef.current
      && current.direction === direction
      && now <= current.settleDeadline;
    const inheritedHeight = inheritsGesture
      ? Math.max(element.scrollHeight, current.baselineHeight)
      : element.scrollHeight;
    if (current) finish(current, "cancelled");
    const inheritedCollapse = inheritedHeight - element.scrollHeight >= collapseThreshold(element);
    nextIdRef.current += 1;
    ownershipEpochRef.current += 1;
    const transaction: ActiveReaderTransaction = {
      id: nextIdRef.current,
      surfaceGeneration: generationRef.current,
      ownershipEpoch: ownershipEpochRef.current,
      direction,
      phase: "active",
      deadline: now + TRANSCRIPT_READER_IDLE_MS,
      settleDeadline: now + TRANSCRIPT_READER_IDLE_MS + TRANSCRIPT_READER_SETTLE_MS,
      baselineTop: element.scrollTop,
      baselineHeight: inheritedHeight,
      minimumHeight: element.scrollHeight,
      lastAcceptedTop: element.scrollTop,
      expectedTop: Math.max(0, Math.min(nativeTranscriptBottomTop(element), element.scrollTop + deltaY)),
      anchor: captureLogicalAnchor(element),
      stableFrames: 0,
      lastGeometryRevision: geometryRevisionRef.current,
      canClaimTail,
      element,
      frame: null,
      idleDelivered: false,
      correctionWritten: false,
      mountAnchorWritten: false,
      collapseObserved: inheritedCollapse,
      // A follow-up epoch of one continuous same-direction gesture keeps the
      // travel proof the gesture already earned; a fresh gesture starts at 0.
      directionalTravelPx: inheritsGesture ? current.directionalTravelPx : 0,
      anchorDisplacementObserved: false,
      transientCandidateHeight: element.scrollHeight,
      transientStableFrames: 0,
      lastHeight: element.scrollHeight,
      lastBottomDistance: nativeTranscriptDistanceFromBottom(element),
      transient: inheritedCollapse,
      visualOffset: 0,
      postCorrectionSettleDeadline: 0,
      correctionHeightSample: element.scrollHeight,
      prepaint: false,
    };
    transactionRef.current = transaction;
    setActive(true);
    recordTranscriptScrollDiagnostic("reader-transaction", {
      transactionId: transaction.id,
      ownershipEpoch: transaction.ownershipEpoch,
      direction: transaction.direction,
      phase: "active",
      result: "started",
    });
    callbacksRef.current.onStart(transaction);
    schedule(transaction);
    return { started: true as const, transactionId: transaction.id };
  }, [finish, generationRef, geometryRevisionRef, observe, ownershipEpochRef, schedule, scrollRef]);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      cancel(false);
    };
  }, [cancel]);

  return useMemo(() => ({
    arm,
    cancel,
    observe,
    isActive,
    active: active || readerLayoutLease,
  }), [active, arm, cancel, readerLayoutLease, observe, isActive]);
}
