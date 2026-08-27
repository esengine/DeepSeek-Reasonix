import { useCallback, useEffect, useMemo, useRef, type RefObject } from "react";
import {
  acceptTranscriptReaderExtentCorrection,
  createTranscriptReaderExtentGuard,
  extendTranscriptReaderExtentGuard,
  MIN_REVERSE_JUMP_PX,
  observeTranscriptReaderExtent,
  retainTranscriptReaderPaintedBaseline,
  resolveTranscriptReaderPaintedReverse,
  resolveTranscriptReaderExtentCorrection,
  transcriptReaderDirectionHandoffBaseline,
  transcriptReaderPaintedRangeReplaced,
  transcriptReaderPaintedRangesShareRow,
  transcriptReaderPaintedSlideIsAdjacent,
  transcriptReaderBlankForwardDelta,
  transcriptReaderAnchorReverseDelta,
  transcriptReaderExtentCanCorrect,
  transcriptReaderExtentHasCollapsed,
  transcriptReaderExtentReverseDelta,
  type TranscriptExtentSnapshot, type TranscriptReaderPaintedReverse,
  type TranscriptReaderExtentGuard,
} from "./transcriptReaderExtentStability";
import type { TranscriptScrollMode } from "./transcriptScrollArbiter";
import { nativeTranscriptDistanceFromBottom } from "./transcriptScrollGeometry";
import { recordTranscriptScrollDiagnostic, type TranscriptScrollWriteRecord } from "./transcriptScrollProbe";
import { shouldBridgeTranscriptReaderCorrection } from "./transcriptScrollWriter";
import { captureLeadingTranscriptLayoutAnchor, transcriptElementViewportIsBlank } from "./transcriptVirtuosoRecovery";

const READER_EXTENT_ACTIVE_MS = 180;
// A frozen native offset proves an anchor break is layout-only; anything past
// this jitter is real input and must never be overridden.
// Real scrolling motion may accompany a measured burst on slow engines; the
// painted slide must dominate it by this ratio to earn a repair, and slides
// beyond one and a half viewports belong to mounted-range replacement paths.
const READER_PIN_INPUT_DOMINANCE_RATIO = 4;
// Inside one viewport of the physical tail the pinned-tail handoff owns the
// geometry: reader anchoring there fights Virtuoso's own clamp and wedges
// manual-mode scrolling (#transcript-selection field replay).
const READER_PIN_MIN_TAIL_DISTANCE_VIEWPORTS = 1.0;
// A correction whose native offset was displaced by a coalesced range swap
// can never reach acknowledgement; bound its single-flight lifetime so later
// reading-position repairs are not wedged behind it (same lease pattern as
// READER_EXTENT_RETENTION_MS).
const READER_PENDING_RELEASE_MS = 600;
// WebView2 can coalesce a sustained native wheel burst and commit Virtuoso's
// replacement range after the reader-intent idle timer has fired. Retain the
// last accepted logical row passively across that bounded compositor delay;
// ownership changes still cancel immediately and ordinary sub-96px layout
// jitter never earns a correction.
// Native WebViews may commit a queued Virtuoso range several seconds after
// the wheel event that selected it (notably while the host process is also
// measuring streamed rows). Keep this lease passive after 180ms: it owns no
// frame loop or scroll position, but a pre-paint mutation/resize observation
// can still reject a late replacement range. Explicit ownership changes
// cancel it immediately.
const READER_EXTENT_RETENTION_MS = 5_000;

type ActiveReaderExtentGuard = TranscriptReaderExtentGuard & {
  element: HTMLDivElement;
  generation: number;
  deadline: number;
  activeFrameDeadline: number;
  frame: number | null;
  paintFrame: number | null;
  paintTimer: number | null;
  expiryTimer: number | null;
  pendingCorrectionTop?: number;
  pendingCorrectionForward?: boolean;
  pendingCorrectionAcknowledged?: boolean;
  pendingAnchor?: readonly [rowKey: string, offsetAtTarget: number];
  paintedRows: ReadonlyMap<string, number>;
  paintedHistory: readonly ReadonlyMap<string, number>[];
  /** Scroll offset captured beside the painted baseline. Equality across an
   * observation proves an anchor break is pure layout shift, not input. */
  baselineScrollTop?: number;
  /** Bounded lifetime for the outstanding single-flight correction so a
   * displaced target cannot wedge later repairs. */
  pendingReleaseTimer?: number | null;
  /** Extent seen by the previous pin probe; measures per-observation layout
   * step size independently of the shared accepted-extent bookkeeping. */
  pinLastHeight?: number;
};

function capturePaintedReaderRows(element: HTMLDivElement): ReadonlyMap<string, number> {
  const viewport = element.getBoundingClientRect();
  const rows = new Map<string, number>();
  for (const row of element.querySelectorAll<HTMLElement>(".transcript__row[data-row-key]")) {
    const rowKey = row.dataset.rowKey;
    const rect = row.getBoundingClientRect();
    if (rowKey && rect.bottom > viewport.top && rect.top < viewport.bottom) {
      rows.set(rowKey, rect.top - viewport.top);
    }
  }
  return rows;
}

function promotePaintedReaderRows(
  guard: ActiveReaderExtentGuard,
  next: ReadonlyMap<string, number>,
) {
  // Mutation, resize, scroll, and rAF observers can all promote the same
  // mounted Virtuoso window before the native smoke sampler reaches its next
  // painted frame. Refresh that window's offsets without rotating away the
  // preceding logical range. Only a majority range replacement advances the
  // bounded three-generation history.
  if (transcriptReaderPaintedRangeReplaced(guard.paintedRows, next)) {
    guard.paintedHistory = [guard.paintedRows, ...guard.paintedHistory].slice(0, 3);
  }
  guard.paintedRows = next;
}

function readerAnchorOffset(element: HTMLDivElement, rowKey: string): number | undefined {
  const row = Array.from(element.querySelectorAll<HTMLElement>(".transcript__row[data-row-key]"))
    .find((candidate) => candidate.dataset.rowKey === rowKey);
  return row ? row.getBoundingClientRect().top - element.getBoundingClientRect().top : undefined;
}

export function useTranscriptReaderExtentStability({
  generationRef,
  modeRef,
  scrollRef,
  writeCorrection,
  lastWriteOwner,
}: {
  generationRef: RefObject<number>;
  modeRef: RefObject<TranscriptScrollMode>;
  scrollRef: RefObject<HTMLDivElement | null>;
  writeCorrection: (write: TranscriptScrollWriteRecord & { virtuosoSync?: boolean }) => boolean;
  lastWriteOwner: () => string | undefined;
}) {
  const guardRef = useRef<ActiveReaderExtentGuard | null>(null);
  const lastNativeDeliveryRef = useRef<{
    element: HTMLDivElement;
    generation: number;
    height: number;
    top: number;
  } | null>(null);

  const clearActive = useCallback((guard: ActiveReaderExtentGuard) => {
    if (guardRef.current === guard) guardRef.current = null;
    if (guard.frame != null) cancelAnimationFrame(guard.frame);
    if (guard.paintFrame != null) cancelAnimationFrame(guard.paintFrame);
    if (guard.paintTimer != null) window.clearTimeout(guard.paintTimer);
    if (guard.expiryTimer != null) window.clearTimeout(guard.expiryTimer);
    if (guard.pendingReleaseTimer != null) window.clearTimeout(guard.pendingReleaseTimer);
    guard.frame = null;
    guard.paintFrame = null;
    guard.paintTimer = null;
    guard.expiryTimer = null;
    guard.pendingReleaseTimer = null;
  }, []);

  const cancel = useCallback(() => {
    const guard = guardRef.current;
    if (guard) clearActive(guard);
  }, [clearActive]);

  const renewLease = useCallback((guard: ActiveReaderExtentGuard) => {
    const now = Date.now();
    guard.activeFrameDeadline = now + READER_EXTENT_ACTIVE_MS;
    guard.deadline = now + READER_EXTENT_RETENTION_MS;
    if (guard.expiryTimer != null) window.clearTimeout(guard.expiryTimer);
    guard.expiryTimer = window.setTimeout(() => clearActive(guard), READER_EXTENT_RETENTION_MS);
  }, [clearActive]);

  const isActive = useCallback(() => guardRef.current !== null, []);

  const anchorOffset = useCallback((guard: ActiveReaderExtentGuard, element: HTMLDivElement) => {
    return guard.anchor ? readerAnchorOffset(element, guard.anchor.rowKey) : undefined;
  }, []);

  const armPendingRelease = useCallback((guard: ActiveReaderExtentGuard) => {
    if (guard.pendingReleaseTimer != null) window.clearTimeout(guard.pendingReleaseTimer);
    guard.pendingReleaseTimer = window.setTimeout(() => {
      guard.pendingReleaseTimer = null;
      if (guardRef.current !== guard || guard.pendingCorrectionTop === undefined) return;
      guard.pendingCorrectionTop = undefined;
      guard.pendingCorrectionForward = undefined;
      guard.pendingCorrectionAcknowledged = undefined;
      guard.pendingAnchor = undefined;
    }, READER_PENDING_RELEASE_MS);
  }, []);

  const commitPaintedRowsAfterPaint = useCallback((guard: ActiveReaderExtentGuard, element: HTMLDivElement) => {
    if (guard.paintFrame !== null || guard.paintTimer !== null) return;
    guard.paintFrame = requestAnimationFrame(() => {
      guard.paintFrame = null;
      // A Virtuoso range replacement can pass through multiple mutation
      // records in one rendering opportunity. Promote the candidate only
      // after that opportunity has painted; otherwise a transient range with
      // no common rows can erase the last user-visible baseline before the
      // final range restores one boundary row.
      guard.paintTimer = window.setTimeout(() => {
        guard.paintTimer = null;
        if (
          guardRef.current !== guard
          || generationRef.current !== guard.generation
          || scrollRef.current !== element
          || Date.now() >= guard.deadline
          || transcriptElementViewportIsBlank(element)
        ) return;
        const next = capturePaintedReaderRows(element);
        // One native offset can pass through several no-common Virtuoso
        // ranges before the host paints the final translated window. None of
        // those layout-only candidates may rotate away the last range the
        // reader actually saw; the final window may retain only its boundary
        // row, which is still enough to prove and repair the full slide.
        if (retainTranscriptReaderPaintedBaseline(
          guard.paintedRows, next, guard.baselineScrollTop, element.scrollTop,
        )) return;
        promotePaintedReaderRows(guard, next);
        guard.baselineScrollTop = element.scrollTop;
      }, 0);
    });
  }, [generationRef, scrollRef]);

  const acknowledgeCorrection = useCallback((guard: ActiveReaderExtentGuard, element: HTMLDivElement, snapshot: TranscriptExtentSnapshot) => {
    if (guard.pendingCorrectionTop === undefined) return;
    const pendingAnchor = guard.pendingAnchor;
    const progressPastTarget = guard.direction * (snapshot.scrollTop - guard.pendingCorrectionTop);
    const passedForwardCorrection = progressPastTarget > guard.clientHeight
      && guard.pendingCorrectionForward === true;
    if (progressPastTarget < -2 || (progressPastTarget > guard.clientHeight && !passedForwardCorrection)) return;
    if (!passedForwardCorrection && pendingAnchor) {
      const currentOffset = readerAnchorOffset(element, pendingAnchor[0]);
      // A native offset can acknowledge before Virtuoso remounts the logical
      // range that owns it. Keep the single-flight correction pending until
      // its anchor is present; otherwise the replacement range is blessed and
      // starts a staircase of mutually incompatible pixel targets.
      if (currentOffset === undefined) return;
      const expectedOffset = pendingAnchor[1] - (snapshot.scrollTop - guard.pendingCorrectionTop);
      if (guard.direction * (currentOffset - expectedOffset) >= MIN_REVERSE_JUMP_PX) {
        // The native offset acknowledged the write in the same delivery
        // that committed another older Virtuoso range. Keep the correction
        // anchor long enough for the ordinary anomaly path below to reject
        // that range; blessing its leading row here would make the visual
        // reversal the next transaction's baseline.
        guard.anchor = { mode: "manual", rowKey: pendingAnchor[0], offset: pendingAnchor[1] };
        guard.anchorScrollTop = guard.pendingCorrectionTop;
        guard.targetAnchorOffset = pendingAnchor[1];
        guard.pendingCorrectionAcknowledged = true;
        return;
      }
    }
    guard.pendingCorrectionTop = undefined;
    guard.pendingCorrectionForward = undefined;
    guard.pendingCorrectionAcknowledged = undefined;
    guard.pendingAnchor = undefined;
    if (passedForwardCorrection && !transcriptElementViewportIsBlank(element)) {
      // Once real native input has moved more than a viewport beyond a
      // stalled forward correction, the rows captured before that correction
      // are no longer an adjacent painted frame. Comparing a newly mounted
      // range with that stale map creates a correction staircase on WebViews.
      // Rebase the passive visual guard here; a later mutation is still
      // compared with this current occupied range before it can paint.
      guard.paintedRows = capturePaintedReaderRows(element);
      guard.paintedHistory = [];
      guard.baselineScrollTop = element.scrollTop;
    }
    // A correction intentionally drops the stale pre-swap anchor. Re-anchor
    // as soon as the native offset reaches or passes that correction in the
    // gesture direction. Native hosts can coalesce the next wheel delta with
    // the acknowledgement (for example target+24), and exact equality would
    // leave the following visual range replacement protected by scrollTop alone.
    const anchor = captureLeadingTranscriptLayoutAnchor(element);
    if (!anchor) return;
    guard.anchor = anchor;
    guard.anchorScrollTop = snapshot.scrollTop;
    guard.targetAnchorOffset = anchor.offset;
  }, []);


  /**
   * Layout-only reading-position breaks slip past the direction-gated reverse
   * jump path when the accepted extent already matches the live snapshot and
   * the painted median slides against gesture direction. When the native
   * offset is frozen at its baseline, a painted median slide beyond the jump
   * threshold is pure measured-extent drift: re-pin scrollTop by the screen
   * displacement so the rows hold their viewport positions.
   */
  const pinReadingAnchor = useCallback((
    active: ActiveReaderExtentGuard,
    element: HTMLDivElement,
    snapshot: TranscriptExtentSnapshot,
    paintedReverse?: TranscriptReaderPaintedReverse,
    viewportBlank = false,
  ): boolean => {
    const heightDelta = active.pinLastHeight === undefined
      ? 0
      : snapshot.scrollHeight - active.pinLastHeight;
    active.pinLastHeight = snapshot.scrollHeight;
    if (
      viewportBlank
      || !paintedReverse
      || active.baselineScrollTop === undefined
      || active.pendingCorrectionTop !== undefined
      || active.collapsed
      || Math.abs(paintedReverse.screenDelta) < MIN_REVERSE_JUMP_PX
      || Math.abs(snapshot.scrollHeight - snapshot.scrollTop - snapshot.clientHeight)
        < snapshot.clientHeight * READER_PIN_MIN_TAIL_DISTANCE_VIEWPORTS
      || !transcriptReaderPaintedSlideIsAdjacent(paintedReverse.screenDelta, snapshot.clientHeight)
      || Math.abs(heightDelta) < MIN_REVERSE_JUMP_PX
      || Math.abs(paintedReverse.screenDelta)
        > Math.abs(heightDelta) + Math.max(8, snapshot.clientHeight * 0.1)
      || Math.abs(paintedReverse.screenDelta)
        < READER_PIN_INPUT_DOMINANCE_RATIO * Math.abs(snapshot.scrollTop - active.baselineScrollTop)
    ) return false;
    const maxTop = Math.max(0, snapshot.scrollHeight - snapshot.clientHeight);
    const pendingTarget = Math.max(0, Math.min(maxTop, snapshot.scrollTop + paintedReverse.screenDelta));
    const correction = pendingTarget - snapshot.scrollTop;
    if (!writeCorrection({
      owner: "reader-stability",
      kind: "scrollBy",
      top: paintedReverse.screenDelta,
      source: "layout-height-changed",
      scrollTop: element.scrollTop,
      scrollHeight: element.scrollHeight,
      clientHeight: element.clientHeight,
      bottomDistance: nativeTranscriptDistanceFromBottom(element),
      mode: modeRef.current,
      virtuosoSync: true,
    })) return false;
    active.pendingCorrectionTop = pendingTarget;
    armPendingRelease(active);
    active.pendingCorrectionForward = correction > 0;
    active.pendingCorrectionAcknowledged = false;
    active.pendingAnchor = [paintedReverse.rowKey, paintedReverse.currentOffset - correction];
    acceptTranscriptReaderExtentCorrection(active, snapshot, correction);
    active.paintedRows = capturePaintedReaderRows(element);
    active.paintedHistory = [];
    active.baselineScrollTop = element.scrollTop;
    recordTranscriptScrollDiagnostic("scroll-anomaly", {
      source: "reader-gesture",
      mode: modeRef.current,
      owner: lastWriteOwner(),
      direction: active.direction > 0 ? "down" : "up",
      reverseDelta: Math.abs(correction),
      extentDelta: snapshot.scrollHeight - active.acceptedHeight,
      scrollTop: element.scrollTop,
      scrollHeight: snapshot.scrollHeight,
      clientHeight: snapshot.clientHeight,
      bottomDistance: nativeTranscriptDistanceFromBottom(element),
      waiting: false,
      corrected: true,
    });
    return true;
  }, [armPendingRelease, lastWriteOwner, modeRef, writeCorrection]);

  const reportAnomaly = useCallback((
    guard: ActiveReaderExtentGuard,
    element: HTMLDivElement,
    currentAnchorOffset?: number,
    paintedReverse?: TranscriptReaderPaintedReverse,
  ) => {
    const reverseDelta = Math.max(
      transcriptReaderExtentReverseDelta(guard, element),
      transcriptReaderAnchorReverseDelta(guard, element, currentAnchorOffset),
      transcriptReaderBlankForwardDelta(guard),
      paintedReverse?.delta ?? 0,
    );
    if (reverseDelta < MIN_REVERSE_JUMP_PX || guard.anomalyReported) return;
    guard.anomalyReported = true;
    recordTranscriptScrollDiagnostic("scroll-anomaly", {
      source: "reader-gesture",
      mode: modeRef.current,
      owner: lastWriteOwner(),
      direction: guard.direction > 0 ? "down" : "up",
      reverseDelta,
      extentDelta: element.scrollHeight - guard.acceptedHeight,
      scrollTop: element.scrollTop,
      scrollHeight: element.scrollHeight,
      clientHeight: element.clientHeight,
      bottomDistance: nativeTranscriptDistanceFromBottom(element),
      waiting: true,
      corrected: false,
    });
  }, [lastWriteOwner, modeRef]);

  const correctAnomaly = useCallback((
    active: ActiveReaderExtentGuard,
    element: HTMLDivElement,
    snapshot: TranscriptExtentSnapshot,
    currentAnchorOffset?: number,
    paintedReverse?: TranscriptReaderPaintedReverse,
    requiresVirtuosoSync = false,
  ) => {
    reportAnomaly(active, element, currentAnchorOffset, paintedReverse);
    if (
      active.pendingCorrectionTop !== undefined
      && active.pendingCorrectionAcknowledged !== true
    ) {
      // WebView2 may defer a native correction even though the writer accepted
      // it. Until a delivery reaches that target, a different target derived
      // from another transient Virtuoso range is not new user intent. Keep the
      // first write single-flight instead of alternating between range estimates.
      return true;
    }
    // Extent collapse/rebound owns the logical target. A smaller displacement
    // from rows painted while the native range was clamped must not replace
    // the pre-collapse accepted position with that transient viewport.
    const paintedCorrection = !active.collapsed
      && paintedReverse
      && paintedReverse.delta >= MIN_REVERSE_JUMP_PX
      && transcriptReaderPaintedSlideIsAdjacent(paintedReverse.screenDelta, snapshot.clientHeight)
      ? active.direction * paintedReverse.delta
      : undefined;
    if (
      paintedCorrection === undefined
      && !transcriptReaderExtentCanCorrect(active, snapshot, currentAnchorOffset)
    ) return false;
    const rawCorrection = paintedCorrection
      ?? (requiresVirtuosoSync && currentAnchorOffset === undefined
        ? active.acceptedTop - snapshot.scrollTop
        : resolveTranscriptReaderExtentCorrection(active, snapshot, currentAnchorOffset));
    const maxTop = Math.max(0, snapshot.scrollHeight - snapshot.clientHeight);
    const correctionTarget = rawCorrection === undefined
      ? undefined
      : Math.max(0, Math.min(maxTop, snapshot.scrollTop + rawCorrection));
    const correction = correctionTarget === undefined ? undefined : correctionTarget - snapshot.scrollTop;
    const mode = modeRef.current;
    if (
      correctionTarget !== undefined
      && active.pendingCorrectionTop !== undefined
      && Math.abs(correctionTarget - active.pendingCorrectionTop) <= 2
    ) return true;
    if (correction === undefined || !writeCorrection({
      owner: "reader-stability",
      kind: "scrollBy",
      top: correction,
      source: "layout-height-changed",
      scrollTop: element.scrollTop,
      scrollHeight: element.scrollHeight,
      clientHeight: element.clientHeight,
      bottomDistance: nativeTranscriptDistanceFromBottom(element),
      mode,
      virtuosoSync: requiresVirtuosoSync,
    })) return false;
    active.pendingCorrectionTop = correctionTarget;
    armPendingRelease(active);
    active.pendingCorrectionForward = active.direction * correction > 0;
    active.pendingCorrectionAcknowledged = false;
    active.pendingAnchor = paintedReverse
      ? [paintedReverse.rowKey, paintedReverse.currentOffset - correction]
      : active.anchor && currentAnchorOffset !== undefined
        ? [active.anchor.rowKey, currentAnchorOffset - correction]
        : active.anchor
          ? [active.anchor.rowKey, active.targetAnchorOffset ?? active.anchor.offset]
          : undefined;
    const extentDelta = snapshot.scrollHeight - active.acceptedHeight;
    acceptTranscriptReaderExtentCorrection(active, snapshot, correction);
    recordTranscriptScrollDiagnostic("scroll-anomaly", {
      source: "reader-gesture",
      mode,
      owner: lastWriteOwner(),
      direction: active.direction > 0 ? "down" : "up",
      reverseDelta: Math.abs(correction),
      extentDelta,
      scrollTop: snapshot.scrollTop,
      scrollHeight: snapshot.scrollHeight,
      clientHeight: snapshot.clientHeight,
      bottomDistance: nativeTranscriptDistanceFromBottom(element),
      waiting: false,
      corrected: true,
    });
    return true;
  }, [lastWriteOwner, modeRef, reportAnomaly, writeCorrection]);

  const observe = useCallback((
    element = scrollRef.current,
    promoteAcceptedNativeFrame = false,
  ) => {
    const guard = guardRef.current;
    if (!element || guard?.element !== element) return false;
    const now = Date.now();
    const mode = modeRef.current;
    if (
      now >= guard.deadline
      || (
        mode !== "reader-gesture"
        && mode !== "manual"
        && (mode !== "tail-follow" || now >= guard.activeFrameDeadline)
      )
    ) {
      clearActive(guard);
      return false;
    }
    const snapshot = {
      scrollTop: element.scrollTop,
      scrollHeight: element.scrollHeight,
      clientHeight: element.clientHeight,
    };
    acknowledgeCorrection(guard, element, snapshot);
    const viewportBlank = transcriptElementViewportIsBlank(element);
    const currentAnchorOffset = anchorOffset(guard, element);
    const previousAcceptedTop = guard.acceptedTop;
    const currentPaintedRows = viewportBlank ? undefined : capturePaintedReaderRows(element);
    const paintedReverse = currentPaintedRows ? resolveTranscriptReaderPaintedReverse(
      [guard.paintedRows, ...guard.paintedHistory], currentPaintedRows, guard.direction,
    ) : undefined;
    const requiresVirtuosoSync = currentPaintedRows !== undefined
      && currentPaintedRows.size > 0
      && guard.paintedRows.size > 0
      && !transcriptReaderPaintedRangesShareRow(guard.paintedRows, currentPaintedRows);
    let corrected = paintedReverse && paintedReverse.delta >= MIN_REVERSE_JUMP_PX
      ? correctAnomaly(guard, element, snapshot, currentAnchorOffset, paintedReverse, requiresVirtuosoSync)
      : false;
    const accepted = corrected
      ? false
      : observeTranscriptReaderExtent(guard, snapshot, currentAnchorOffset, viewportBlank);
    // An unpainted Virtuoso range cannot supply a trustworthy visual anchor,
    // but a native extent reversal/collapse can still be corrected from the
    // last accepted logical position.
    corrected = corrected || correctAnomaly(
      guard, element, snapshot, viewportBlank ? undefined : currentAnchorOffset, undefined, requiresVirtuosoSync,
    );
    corrected = corrected || pinReadingAnchor(guard, element, snapshot, viewportBlank ? undefined : paintedReverse, viewportBlank);
    if (
      accepted
      && (
        guard.direction * (snapshot.scrollTop - previousAcceptedTop) > 2
        || !guard.anchor
        || currentAnchorOffset === undefined
      )
      && !corrected
      && guard.pendingCorrectionTop === undefined
    ) {
      const anchor = captureLeadingTranscriptLayoutAnchor(element);
      if (anchor) {
        // Commit the row that was actually painted for this accepted frame.
        // The next Virtuoso range swap is compared with this row rather than
        // a possibly unmounted row from the beginning of a long gesture.
        guard.anchor = anchor;
        guard.anchorScrollTop = snapshot.scrollTop;
        guard.targetAnchorOffset = anchor.offset;
      }
    }
    if (accepted && !corrected && !viewportBlank) {
      if (promoteAcceptedNativeFrame) {
        // A native scroll event is the last synchronous observation of the
        // range actually delivered by the host. Preserve it immediately while
        // retaining the prior window: WKWebView can replace the range before
        // the deferred paint timer runs.
        const paintedRows = capturePaintedReaderRows(element);
        if (paintedRows.size > 0) {
          promotePaintedReaderRows(guard, paintedRows);
          guard.baselineScrollTop = element.scrollTop;
        }
      } else {
        commitPaintedRowsAfterPaint(guard, element);
      }
    }
    return transcriptReaderExtentHasCollapsed(guard);
  }, [acknowledgeCorrection, armPendingRelease, anchorOffset, clearActive, commitPaintedRowsAfterPaint, correctAnomaly, modeRef, pinReadingAnchor, scrollRef]);

  const schedule = useCallback((active: ActiveReaderExtentGuard) => {
    if (active.frame !== null) return;
    const tick = () => {
      active.frame = null;
      const mode = modeRef.current;
      if (
        guardRef.current !== active
        || generationRef.current !== active.generation
        || scrollRef.current !== active.element
        || (
          mode !== "reader-gesture"
          && mode !== "manual"
          && (mode !== "tail-follow" || Date.now() >= active.activeFrameDeadline)
        )
      ) {
        if (guardRef.current === active) clearActive(active);
        return;
      }
      const element = active.element;
      const snapshot = {
        scrollTop: element.scrollTop,
        scrollHeight: element.scrollHeight,
        clientHeight: element.clientHeight,
      };
      acknowledgeCorrection(active, element, snapshot);
      const viewportBlank = transcriptElementViewportIsBlank(element);
      const currentAnchorOffset = anchorOffset(active, element);
      const previousAcceptedTop = active.acceptedTop;
      const currentPaintedRows = viewportBlank ? undefined : capturePaintedReaderRows(element);
      const paintedReverse = currentPaintedRows ? resolveTranscriptReaderPaintedReverse(
        [active.paintedRows, ...active.paintedHistory], currentPaintedRows, active.direction,
      ) : undefined;
      const requiresVirtuosoSync = currentPaintedRows !== undefined
        && currentPaintedRows.size > 0
        && active.paintedRows.size > 0
        && !transcriptReaderPaintedRangesShareRow(active.paintedRows, currentPaintedRows);
      let corrected = paintedReverse && paintedReverse.delta >= MIN_REVERSE_JUMP_PX
        ? correctAnomaly(active, element, snapshot, currentAnchorOffset, paintedReverse, requiresVirtuosoSync)
        : false;
      const accepted = corrected
        ? false
        : observeTranscriptReaderExtent(active, snapshot, currentAnchorOffset, viewportBlank);
      corrected = corrected || correctAnomaly(
        active, element, snapshot, viewportBlank ? undefined : currentAnchorOffset, undefined, requiresVirtuosoSync,
      );
      corrected = corrected || pinReadingAnchor(active, element, snapshot, viewportBlank ? undefined : paintedReverse, viewportBlank);
      if (
        accepted
        && (
          active.direction * (snapshot.scrollTop - previousAcceptedTop) > 2
          || !active.anchor
          || currentAnchorOffset === undefined
        )
        && !corrected
        && active.pendingCorrectionTop === undefined
      ) {
        const anchor = captureLeadingTranscriptLayoutAnchor(element);
        if (anchor) {
          active.anchor = anchor;
          active.anchorScrollTop = snapshot.scrollTop;
          active.targetAnchorOffset = anchor.offset;
        }
      }
      if (accepted && !corrected && !viewportBlank) commitPaintedRowsAfterPaint(active, element);
      // After the ordinary 180ms active sampling window, keep the accepted
      // anchor as a passive lease. Mutation/resize/native-scroll observers can
      // still reject a late WebView range swap without spinning a frame loop.
      if (Date.now() >= active.activeFrameDeadline) return;
      active.frame = requestAnimationFrame(tick);
    };
    active.frame = requestAnimationFrame(tick);
  }, [acknowledgeCorrection, armPendingRelease, anchorOffset, clearActive, commitPaintedRowsAfterPaint, correctAnomaly, generationRef, modeRef, pinReadingAnchor, scrollRef]);

  const arm = useCallback((deltaY: number) => {
    const element = scrollRef.current;
    if (!element) return;
    const anchor = captureLeadingTranscriptLayoutAnchor(element);
    const current = guardRef.current;
    const extensionAnchor = current?.anchor
      && anchorOffset(current, element) !== undefined
      && anchor?.mode === "manual"
      && anchor.rowKey !== current.anchor.rowKey
      ? undefined
      : anchor;
    if (
      current
      && current.element === element
      && current.generation === generationRef.current
      && extendTranscriptReaderExtentGuard(current, element, extensionAnchor, deltaY)
    ) {
      renewLease(current);
      schedule(current);
      return;
    }

    cancel();
    const guard = createTranscriptReaderExtentGuard(element, anchor, deltaY);
    if (!guard) return;
    const active: ActiveReaderExtentGuard = {
      ...guard,
      element,
      generation: generationRef.current,
      deadline: 0,
      activeFrameDeadline: 0,
      frame: null,
      paintFrame: null,
      paintTimer: null,
      expiryTimer: null,
      paintedRows: capturePaintedReaderRows(element),
      paintedHistory: [],
      baselineScrollTop: element.scrollTop,
      pinLastHeight: element.scrollHeight,
    };
    guardRef.current = active;
    renewLease(active);
    schedule(active);
  }, [anchorOffset, cancel, generationRef, renewLease, schedule, scrollRef]);
  const syncNativeDirection = useCallback((deltaY: number) => {
    const current = guardRef.current;
    if (
      current
      && current.element === scrollRef.current
      && current.generation === generationRef.current
      && current.direction === (deltaY < 0 ? -1 : 1)
    ) {
      renewLease(current);
      schedule(current);
      return;
    }
    const incomingPaintedRows = current?.element === scrollRef.current
      ? capturePaintedReaderRows(current.element) : undefined;
    const priorPaintedRows = current && current.generation === generationRef.current && incomingPaintedRows ? transcriptReaderDirectionHandoffBaseline(
      [current.paintedRows, ...current.paintedHistory], incomingPaintedRows, deltaY,
    ) : undefined;
    const priorPaint = current && priorPaintedRows
      ? [priorPaintedRows, current.baselineScrollTop, current.pinLastHeight] as const : undefined;
    arm(deltaY);
    const next = guardRef.current;
    if (next) {
      // The first real native direction can arrive with its range swap.
      if (priorPaint) {
        [next.paintedRows, next.baselineScrollTop, next.pinLastHeight] = priorPaint;
        next.paintedHistory = []; // Drop the replaced ranges.
      }
      // This path observes a delivery that has already moved the native
      // scroller. Unlike a pre-scroll wheel intent, its current top/anchor is
      // the expectation; adding deltaY again would create an overshoot.
      next.expectedTop = next.element.scrollTop;
      next.anchorScrollTop = next.anchor ? next.element.scrollTop : undefined;
      next.targetAnchorOffset = next.anchor?.offset;
    }
  }, [arm, generationRef, renewLease, schedule, scrollRef]);

  const observeNativeDelivery = useCallback((element: HTMLDivElement) => {
    const generation = generationRef.current;
    const previous = lastNativeDeliveryRef.current;
    const deliveredTop = element.scrollTop;
    const pendingBeforeObservation = guardRef.current?.pendingCorrectionTop !== undefined;
    const sameSurface = previous?.element === element && previous.generation === generation;
    const nativeDelta = sameSurface ? deliveredTop - previous.top : 0;
    const extentDelta = sameSurface ? element.scrollHeight - previous.height : 0;
    const activeBeforeDelivery = guardRef.current;
    const visualReverseBeforeDelivery = activeBeforeDelivery?.element === element
      && activeBeforeDelivery.generation === generation
      ? resolveTranscriptReaderPaintedReverse(
        [activeBeforeDelivery.paintedRows, ...activeBeforeDelivery.paintedHistory],
        capturePaintedReaderRows(element), activeBeforeDelivery.direction,
      )
      : undefined;
    // A measured range replacement can lower native scrollTop while moving
    // every shared row down-screen. That is layout-owned reverse motion, not
    // an upward user gesture. Real direction changes have already armed the
    // guard from wheel/touch/key intent before the host delivers scroll.
    const retainsArmedDirection = sameSurface
      && activeBeforeDelivery !== null
      && activeBeforeDelivery !== undefined
      // Only a live pre-scroll intent can overrule the native delta. The
      // passive five-second lease protects late layout commits, but its
      // direction may belong to an earlier gesture; retaining that stale
      // direction turns the next real gesture into a multi-screen repair.
      && Date.now() < activeBeforeDelivery.activeFrameDeadline
      && activeBeforeDelivery.direction * nativeDelta < -2
      && Math.abs(extentDelta) > 2
      && (visualReverseBeforeDelivery?.delta ?? 0) >= MIN_REVERSE_JUMP_PX;
    const layoutAnchored = Math.abs(extentDelta) > 2
      && Math.abs(nativeDelta - extentDelta) <= Math.max(8, element.clientHeight * 0.1);
    const view = element.ownerDocument.defaultView;
    const nativeInput = Math.abs(nativeDelta) > 2
      && Math.abs(nativeDelta) <= element.clientHeight
      && !layoutAnchored
      && (modeRef.current === "reader-gesture" || modeRef.current === "manual")
      && view
      && shouldBridgeTranscriptReaderCorrection(view)
      && !pendingBeforeObservation
      && !retainsArmedDirection;
    // Reconcile a coalesced delivery before observing it. Otherwise an
    // opposite setup direction can misclassify the first >96px native move as
    // a reverse layout anomaly and write against real user input.
    if (nativeInput) syncNativeDirection(nativeDelta);
    observe(element, true);
    const active = guardRef.current;
    const pendingAfterObservation = active?.pendingCorrectionTop !== undefined;
    if (
      nativeInput
      && !pendingAfterObservation
      && active?.element === element
      && active.direction === (nativeDelta < 0 ? -1 : 1)
      && Math.abs(active.acceptedTop - element.scrollTop) <= 2
    ) {
      active.expectedTop = element.scrollTop;
    }
    lastNativeDeliveryRef.current = {
      element,
      generation,
      height: element.scrollHeight,
      top: element.scrollTop,
    };
  }, [generationRef, modeRef, observe, syncNativeDirection]);

  useEffect(() => cancel, [cancel]);

  return useMemo(
    () => ({ arm, cancel, observe, observeNativeDelivery, isActive }),
    [arm, cancel, observe, observeNativeDelivery, isActive],
  );
}
