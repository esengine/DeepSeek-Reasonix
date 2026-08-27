import type { RefObject } from "react";
import {
  type TranscriptScrollDiagnosticSource,
  type TranscriptTailWriteDiagnostic,
} from "./transcriptScrollDiagnosticProbe";
import type { TranscriptScrollMode } from "./transcriptScrollArbiter";
import { nativeTranscriptBottomTop, nativeTranscriptDistanceFromBottom } from "./transcriptScrollGeometry";
import type { TranscriptScrollWriter } from "./transcriptScrollWriter";

const TAIL_SETTLE_MAX_WRITES_PER_REVISION = 1;
const TAIL_COLLAPSE_STABLE_FRAMES = 2;
// Row measurements can precede Virtuoso's final mount-window anchor update by
// several paints. Combine a short frame window with the 80ms quiet threshold
// so the one permitted revision write lands after that internal adjustment.
const TAIL_ROW_MEASURE_STABLE_FRAMES = 5;
const TAIL_GEOMETRY_STABLE_MS = 80;
export const TRANSCRIPT_TAIL_REARM_MIN_HEIGHT_PX = 24;
// Identical no-op settle resends stay bounded so observer noise cannot loop
// per revision; the post-measure jump path uses the same three-correction
// ceiling for its own measured-tail retries.
const TAIL_SETTLE_MAX_STAGNANT_RESENDS = 2;
// Virtuoso keeps an auto scrollToIndex request alive for up to 1.2s while its
// size tree refreshes. Do not start the native-extent confirmation until that
// bounded mount/measurement pass has finished.
const JUMP_TAIL_TRANSACTION_MS = 1_400;
const JUMP_TAIL_MAX_WAIT_MS = 12_000;
const JUMP_TAIL_SAMPLE_MS = 40;
const JUMP_TAIL_POST_MEASURE_MAX_WAIT_MS = 12_000;
const JUMP_TAIL_POST_MEASURE_STABLE_FRAMES = 2;
const JUMP_TAIL_POST_MEASURE_MAX_CORRECTIONS = 3;
const JUMP_TAIL_NATIVE_THRESHOLD_PX = 1;
const JUMP_TAIL_STABLE_EPSILON_PX = 0.5;
const JUMP_TAIL_STABLE_MS = 320;
const LAYOUT_TRANSIENT_IDLE_MS = 160;
const RESIDUAL_VERIFICATION_DELAY_MS = 160;
export const TRANSCRIPT_TRANSIENT_EXTENT_MIN_PX = 96;

export function transcriptTailSettleBudgetExhausted(attempts: number): boolean {
  return attempts >= TAIL_SETTLE_MAX_WRITES_PER_REVISION;
}

export function transcriptTailShouldReaim(previousBottomHeight: number | null, currentHeight: number): boolean {
  if (previousBottomHeight == null) return true;
  return currentHeight - previousBottomHeight >= TRANSCRIPT_TAIL_REARM_MIN_HEIGHT_PX;
}

export function transcriptTailExtentCollapsed(previousHeight: number | null, currentHeight: number, clientHeight: number): boolean {
  if (previousHeight == null || clientHeight <= 0) return false;
  return previousHeight - currentHeight >= Math.max(TRANSCRIPT_TRANSIENT_EXTENT_MIN_PX, clientHeight * 0.5);
}

function transcriptTailMountIsMeasured(element: HTMLElement): boolean {
  const rowCount = Number(element.dataset.transcriptRowCount);
  const firstItemIndex = Number(element.dataset.transcriptFirstItemIndex);
  // Unit-sized controller fixtures do not render Virtuoso's item metadata.
  if (!Number.isInteger(rowCount) || !Number.isInteger(firstItemIndex) || rowCount <= 0) {
    return element.querySelector(".transcript__row") !== null;
  }
  const lastItemIndex = firstItemIndex + rowCount - 1;
  const mountedRows = Array.from(element.querySelectorAll<HTMLElement>(".transcript__row[data-item-index][data-known-size]"));
  if (!mountedRows.some((row) => Number(row.dataset.itemIndex) === lastItemIndex)) return false;
  return mountedRows.every((row) => {
    const knownSize = Number(row.dataset.knownSize);
    const realSize = row.getBoundingClientRect().height;
    return Number.isFinite(knownSize)
      && realSize > 0
      && Math.abs(knownSize - realSize) <= JUMP_TAIL_STABLE_EPSILON_PX;
  });
}

function transcriptTailIsMounted(element: HTMLElement): boolean {
  const rowCount = Number(element.dataset.transcriptRowCount);
  const firstItemIndex = Number(element.dataset.transcriptFirstItemIndex);
  // Controller unit fixtures do not render Virtuoso's item metadata. Their
  // single synthetic row stands in for an already mounted logical tail.
  if (!Number.isInteger(rowCount) || !Number.isInteger(firstItemIndex) || rowCount <= 0) return true;
  const lastItemIndex = firstItemIndex + rowCount - 1;
  return Array.from(element.querySelectorAll<HTMLElement>(".transcript__row[data-item-index]"))
    .some((row) => Number(row.dataset.itemIndex) === lastItemIndex);
}

export type TranscriptTailSettle = {
  scrollToTail: (
    behavior: "auto" | "smooth",
    diagnostic?: TranscriptTailWriteDiagnostic,
    geometryRevision?: number,
  ) => boolean;
  /** A geometry revision can produce at most one tail write. */
  schedule: (geometryRevision: number, jump: boolean, source: TranscriptScrollDiagnosticSource, deferUntilStable?: boolean) => void;
  cancel: () => void;
  noteLayoutTransient: () => void;
};

/**
 * Revision-scoped tail convergence. Layout reports may continue after a write
 * while Virtuoso measures newly mounted rows; those reports cannot turn into
 * another write for the same revision. A large one-frame extent collapse is
 * observed for two frames before it is accepted as real geometry.
 */
export function createTranscriptTailSettle({
  writer,
  scrollRef,
  modeRef,
  generationRef,
  layoutTransientRef,
  requestResidualGeometry,
}: {
  writer: TranscriptScrollWriter;
  scrollRef: RefObject<HTMLDivElement | null>;
  modeRef: RefObject<TranscriptScrollMode>;
  generationRef: RefObject<number>;
  layoutTransientRef: RefObject<boolean>;
  requestResidualGeometry: () => void;
}): TranscriptTailSettle {
  let tailSettleFrame: number | null = null;
  let pending: {
    revision: number;
    source: TranscriptScrollDiagnosticSource;
    element: HTMLDivElement;
    generation: number;
    offBottomFrames: number;
    bottomFrames: number;
    stableFrames: number;
    deferUntilStable: boolean;
    lastObservedHeight: number;
    stableSince: number;
    collapseFrames: number;
    lastCollapsedHeight: number | null;
  } | null = null;
  let writtenRevision = -1;
  let stagnantWrite: {
    generation: number;
    target: number;
    scrollTop: number;
    height: number;
    consecutive: number;
  } | null = null;
  let jumpTailTimer: number | null = null;
  let jumpTailStartedAt: number | null = null;
  let jumpTailRevision = 0;
  let jumpTailRemounts = 0;
  let layoutTransientIdleTimer: number | null = null;
  let residualVerificationTimer: number | null = null;
  let residualCorrectionHeight: number | null = null;
  let lastStableHeight: number | null = null;

  const verifyResidualGeometry = () => {
    if (residualVerificationTimer !== null) window.clearTimeout(residualVerificationTimer);
    // Virtuoso can publish its final mount-window anchor several paints after
    // the native write. A two-rAF check races that update on Chromium/WKWebView;
    // one delayed, height-keyed verification catches the late displacement
    // while residualCorrectionHeight limits a stable extent to one retry.
    residualVerificationTimer = window.setTimeout(() => {
      residualVerificationTimer = null;
      const element = scrollRef.current;
      if (!element || modeRef.current !== "tail-follow" || lastStableHeight === null) return;
      const distance = nativeTranscriptDistanceFromBottom(element);
      const heightChangedAfterWrite = Math.abs(element.scrollHeight - lastStableHeight) > JUMP_TAIL_STABLE_EPSILON_PX;
      if (distance <= JUMP_TAIL_NATIVE_THRESHOLD_PX) {
        residualCorrectionHeight = null;
      } else if (heightChangedAfterWrite || residualCorrectionHeight !== element.scrollHeight) {
        residualCorrectionHeight = element.scrollHeight;
        requestResidualGeometry();
      }
    }, RESIDUAL_VERIFICATION_DELAY_MS);
  };

  const armLayoutTransientIdle = () => {
    if (layoutTransientIdleTimer !== null) window.clearTimeout(layoutTransientIdleTimer);
    layoutTransientIdleTimer = window.setTimeout(() => {
      layoutTransientIdleTimer = null;
      if (tailSettleFrame === null) layoutTransientRef.current = false;
    }, LAYOUT_TRANSIENT_IDLE_MS);
  };

  const noteLayoutTransient = () => {
    layoutTransientRef.current = true;
    armLayoutTransientIdle();
  };

  const scrollToTail = (
    behavior: "auto" | "smooth",
    diagnostic?: TranscriptTailWriteDiagnostic,
    geometryRevision?: number,
  ): boolean => {
    const element = scrollRef.current;
    if (!element) return false;
    const mountingTail = diagnostic?.phase === "initial";
    const wrote = writer.write({
      owner: "tail-follow",
      operation: mountingTail ? "scrollToIndex" : "scrollTo",
      index: mountingTail ? "LAST" : undefined,
      align: mountingTail ? "end" : undefined,
      top: mountingTail ? undefined : nativeTranscriptBottomTop(element),
      behavior,
      source: diagnostic?.source ?? "geometry-changed",
      phase: diagnostic?.phase,
      expectedGeneration: generationRef.current,
      geometryRevision: geometryRevision ?? 0,
      settleFrame: diagnostic?.settle?.frame,
      offBottomFrames: diagnostic?.settle?.offBottomFrames,
      stagnantFrames: diagnostic?.settle?.stagnantFrames,
    });
    if (wrote) {
      if (lastStableHeight === null || Math.abs(element.scrollHeight - lastStableHeight) > JUMP_TAIL_STABLE_EPSILON_PX) {
        residualCorrectionHeight = null;
      }
      lastStableHeight = element.scrollHeight;
      // The LAST mount has its own pending-Markdown and post-measure
      // confirmation transaction below. Residual verification is only for
      // absolute extent writes; running both would let the residual timer
      // preempt a still-pending initial mount.
      if (!mountingTail) verifyResidualGeometry();
    }
    return wrote;
  };

  const remountJumpTail = (element: HTMLElement, source: TranscriptScrollDiagnosticSource, revision: number) => {
    if (jumpTailRemounts >= TAIL_SETTLE_MAX_STAGNANT_RESENDS || transcriptTailIsMounted(element)) return false;
    if (!scrollToTail("auto", { source, phase: "initial" }, revision)) return false;
    jumpTailRemounts += 1;
    return true;
  };

  const cancel = () => {
    if (tailSettleFrame !== null) cancelAnimationFrame(tailSettleFrame);
    tailSettleFrame = null;
    pending = null;
    writtenRevision = -1;
    stagnantWrite = null;
    lastStableHeight = null;
    if (jumpTailTimer !== null) window.clearTimeout(jumpTailTimer);
    jumpTailTimer = null;
    jumpTailStartedAt = null;
    jumpTailRevision = 0;
    if (layoutTransientIdleTimer !== null) window.clearTimeout(layoutTransientIdleTimer);
    layoutTransientIdleTimer = null;
    if (residualVerificationTimer !== null) window.clearTimeout(residualVerificationTimer);
    residualVerificationTimer = null;
    residualCorrectionHeight = null;
    layoutTransientRef.current = false;
  };

  const tick = () => {
    tailSettleFrame = null;
    const transaction = pending;
    if (!transaction) return;
    if (
      generationRef.current !== transaction.generation
      || scrollRef.current !== transaction.element
      || modeRef.current !== "tail-follow"
    ) {
      pending = null;
      layoutTransientRef.current = false;
      return;
    }

    const element = transaction.element;
    const height = element.scrollHeight;
    const distance = nativeTranscriptDistanceFromBottom(element);
    if (Math.abs(transaction.lastObservedHeight - height) > JUMP_TAIL_STABLE_EPSILON_PX) {
      transaction.lastObservedHeight = height;
      transaction.stableSince = Date.now();
      transaction.bottomFrames = 0;
      transaction.stableFrames = 0;
    } else {
      transaction.stableFrames += 1;
    }
    // An empty mounted range is never a valid tail coordinate, even when the
    // browser has already clamped the native offset to its current physical
    // bottom. Mutation/ResizeObserver delivers this tick before paint, so hand
    // the repair to Virtuoso's logical LAST transaction now instead of letting
    // a blank range become the next stable native baseline.
    if (!transcriptTailIsMounted(element)) {
      pending = null;
      stagnantWrite = null;
      scrollToTail("auto", { source: transaction.source, phase: "initial" }, transaction.revision);
      schedule(transaction.revision, true, transaction.source);
      return;
    }
    // WebKit can retain the old scrollTop for the remainder of the current
    // layout opportunity after a virtual range contracts. A negative bottom
    // distance is therefore not a settled tail: it is an invalid native
    // offset that would be clamped only after painting a reverse/blank frame.
    // Correct that overshoot synchronously through the single writer while
    // ResizeObserver/MutationObserver are still in the pre-paint checkpoint.
    if (distance < -JUMP_TAIL_NATIVE_THRESHOLD_PX) {
      const wrote = scrollToTail(
        "auto",
        { source: transaction.source, phase: "settle" },
        transaction.revision,
      );
      if (wrote) writtenRevision = transaction.revision;
    }
    if (distance <= JUMP_TAIL_NATIVE_THRESHOLD_PX) {
      transaction.bottomFrames += 1;
      if (
        transaction.bottomFrames < TAIL_COLLAPSE_STABLE_FRAMES
        || Date.now() - transaction.stableSince < TAIL_GEOMETRY_STABLE_MS
      ) {
        tailSettleFrame = requestAnimationFrame(tick);
        return;
      }
      lastStableHeight = height;
      pending = null;
      armLayoutTransientIdle();
      return;
    }
    transaction.bottomFrames = 0;
    transaction.offBottomFrames += 1;

    if (
      transaction.deferUntilStable
      && (
        transaction.stableFrames < TAIL_ROW_MEASURE_STABLE_FRAMES
        || Date.now() - transaction.stableSince < TAIL_GEOMETRY_STABLE_MS
      )
    ) {
      tailSettleFrame = requestAnimationFrame(tick);
      return;
    }

    if (transcriptTailExtentCollapsed(lastStableHeight, height, element.clientHeight)) {
      const sameCollapsedHeight = transaction.lastCollapsedHeight !== null
        && Math.abs(transaction.lastCollapsedHeight - height) <= 1;
      transaction.collapseFrames = sameCollapsedHeight ? transaction.collapseFrames + 1 : 1;
      transaction.lastCollapsedHeight = height;
      if (transaction.collapseFrames < TAIL_COLLAPSE_STABLE_FRAMES) {
        tailSettleFrame = requestAnimationFrame(tick);
        return;
      }
      lastStableHeight = height;
    }

    if (writtenRevision !== transaction.revision) {
      const target = nativeTranscriptBottomTop(element);
      const attemptedScrollTop = element.scrollTop;
      const attemptedHeight = element.scrollHeight;
      // A settle resend whose target, native offset, and extent all match the
      // previous attempt was already rejected by the engine; replaying it per
      // incoming revision turns observer noise into a doomed write loop.
      // Compare the geometry that requested the write, not the state after
      // writer.write(): Virtuoso may synchronously replace its range/extent
      // while processing the request. Real pre-write movement or extent
      // change still re-arms corrections at once, while a repeating range
      // cycle reaches the bounded logical-LAST handoff.
      const identicalNoOp = stagnantWrite !== null
        && stagnantWrite.generation === generationRef.current
        && Math.abs(stagnantWrite.target - target) <= JUMP_TAIL_NATIVE_THRESHOLD_PX
        && Math.abs(stagnantWrite.scrollTop - attemptedScrollTop) <= JUMP_TAIL_NATIVE_THRESHOLD_PX
        && Math.abs(stagnantWrite.height - attemptedHeight) <= JUMP_TAIL_STABLE_EPSILON_PX;
      const stagnantFrames = identicalNoOp ? stagnantWrite!.consecutive + 1 : 0;
      if (identicalNoOp && stagnantWrite!.consecutive >= TAIL_SETTLE_MAX_STAGNANT_RESENDS) {
        pending = null;
        stagnantWrite = null;
        // The native engine accepted the same absolute destination without
        // retaining it across the virtual range commit. Stop resending that
        // coordinate and hand the bounded recovery to Virtuoso's logical LAST
        // mount transaction through the same writer.
        scrollToTail("auto", { source: transaction.source, phase: "initial" }, transaction.revision);
        schedule(transaction.revision, true, transaction.source);
        return;
      }
      const wrote = scrollToTail(
        "auto",
        { source: transaction.source, phase: "settle", settle: { frame: transaction.collapseFrames, offBottomFrames: transaction.offBottomFrames, stagnantFrames } },
        transaction.revision,
      );
      if (wrote) {
        writtenRevision = transaction.revision;
        stagnantWrite = {
          generation: generationRef.current,
          target,
          scrollTop: attemptedScrollTop,
          height: attemptedHeight,
          consecutive: stagnantFrames,
        };
      }
    }
    pending = null;
    armLayoutTransientIdle();
  };

  const schedule = (geometryRevision: number, jump: boolean, source: TranscriptScrollDiagnosticSource, deferUntilStable = false) => {
    const element = scrollRef.current;
    if (!element) {
      layoutTransientRef.current = false;
      return;
    }
    noteLayoutTransient();
    if (lastStableHeight === null) lastStableHeight = element.scrollHeight;

    if (jump) {
      if (jumpTailTimer !== null) window.clearTimeout(jumpTailTimer);
      const generation = generationRef.current;
      const transactionElement = element;
      const startedAt = Date.now();
      jumpTailStartedAt = startedAt;
      jumpTailRevision = geometryRevision;
      jumpTailRemounts = 0;
      let previousHeight = element.scrollHeight;
      let previousTop = element.scrollTop;
      let stableFrames = 0;
      const confirmJumpTail = () => {
        jumpTailTimer = null;
        if (
          generationRef.current !== generation
          || scrollRef.current !== transactionElement
          || modeRef.current !== "tail-follow"
        ) {
          jumpTailStartedAt = null;
          armLayoutTransientIdle();
          return;
        }
        const height = transactionElement.scrollHeight;
        const top = transactionElement.scrollTop;
        const pendingGeometry = transactionElement.querySelector("[data-transcript-geometry-pending]") !== null;
        const tailMounted = transcriptTailIsMounted(transactionElement);
        // Relinquish the initial jump as soon as the logical tail is mounted
        // and native geometry itself is stable for two samples. Requiring
        // every mounted row's Virtuoso known-size metadata to match here can
        // keep the jump owner alive for seconds even though the physical tail
        // is already settled, suppressing deferred live-size revisions.
        const sampleStable = !pendingGeometry
          && tailMounted
          && Math.abs(height - previousHeight) <= JUMP_TAIL_STABLE_EPSILON_PX
          && Math.abs(top - previousTop) <= JUMP_TAIL_STABLE_EPSILON_PX;
        stableFrames = sampleStable ? stableFrames + 1 : 0;
        previousHeight = height;
        previousTop = top;
        const elapsed = Date.now() - startedAt;
        if (!pendingGeometry && remountJumpTail(transactionElement, source, jumpTailRevision)) {
          previousHeight = transactionElement.scrollHeight;
          previousTop = transactionElement.scrollTop;
          stableFrames = 0;
          jumpTailTimer = window.setTimeout(confirmJumpTail, JUMP_TAIL_SAMPLE_MS);
          return;
        }
        if (
          nativeTranscriptDistanceFromBottom(transactionElement) <= JUMP_TAIL_NATIVE_THRESHOLD_PX
          && tailMounted
          && stableFrames >= TAIL_COLLAPSE_STABLE_FRAMES
        ) {
          jumpTailStartedAt = null;
          armLayoutTransientIdle();
          return;
        }
        if (
          (elapsed >= JUMP_TAIL_TRANSACTION_MS
            && !pendingGeometry
            && tailMounted)
          || elapsed >= JUMP_TAIL_MAX_WAIT_MS
        ) {
          let wrote = false;
          if (nativeTranscriptDistanceFromBottom(transactionElement) > JUMP_TAIL_NATIVE_THRESHOLD_PX) {
            wrote = scrollToTail(
              "auto",
              { source, phase: "settle", settle: { frame: stableFrames, offBottomFrames: 1, stagnantFrames: 0 } },
              jumpTailRevision,
            );
          }
          if (wrote) {
            let remainingCorrections = JUMP_TAIL_POST_MEASURE_MAX_CORRECTIONS;
            let offBottomFrames = 2;
            let verifyPreviousHeight = transactionElement.scrollHeight;
            let verifyPreviousTop = transactionElement.scrollTop;
            let verifyStableFrames = 0;
            let verifyStableSince: number | null = null;
            let verifyStartedAt = Date.now();
            const verifyMeasuredTail = () => {
              jumpTailTimer = null;
              if (
                generationRef.current !== generation
                || scrollRef.current !== transactionElement
                || modeRef.current !== "tail-follow"
              ) {
                jumpTailStartedAt = null;
                armLayoutTransientIdle();
                return;
              }
              const verifyHeight = transactionElement.scrollHeight;
              const verifyTop = transactionElement.scrollTop;
              const verifyPendingGeometry = transactionElement.querySelector("[data-transcript-geometry-pending]") !== null;
              const verifyTailMounted = transcriptTailIsMounted(transactionElement);
              const verifyStable = !verifyPendingGeometry
                && verifyTailMounted
                && transcriptTailMountIsMeasured(transactionElement)
                && Math.abs(verifyHeight - verifyPreviousHeight) <= JUMP_TAIL_STABLE_EPSILON_PX
                && Math.abs(verifyTop - verifyPreviousTop) <= JUMP_TAIL_STABLE_EPSILON_PX;
              verifyStableFrames = verifyStable ? verifyStableFrames + 1 : 0;
              verifyStableSince = verifyStable ? verifyStableSince ?? Date.now() : null;
              verifyPreviousHeight = verifyHeight;
              verifyPreviousTop = verifyTop;
              const verifyStableWindow = verifyStableFrames >= JUMP_TAIL_POST_MEASURE_STABLE_FRAMES
                && verifyStableSince !== null
                && Date.now() - verifyStableSince >= JUMP_TAIL_STABLE_MS;
              if (
                nativeTranscriptDistanceFromBottom(transactionElement) <= JUMP_TAIL_NATIVE_THRESHOLD_PX
                && verifyTailMounted
              ) {
                if (
                  verifyStableWindow
                  || Date.now() - verifyStartedAt >= JUMP_TAIL_POST_MEASURE_MAX_WAIT_MS
                ) {
                  jumpTailStartedAt = null;
                  armLayoutTransientIdle();
                  return;
                }
                jumpTailTimer = window.setTimeout(verifyMeasuredTail, JUMP_TAIL_SAMPLE_MS);
                return;
              }
              const correctionWindowElapsed = Date.now() - verifyStartedAt >= JUMP_TAIL_STABLE_MS;
              if (
                remainingCorrections > 0
                && !verifyPendingGeometry
                && (verifyStableWindow || correctionWindowElapsed)
              ) {
                if (!verifyTailMounted) {
                  scrollToTail("auto", { source, phase: "initial" }, jumpTailRevision);
                } else {
                  scrollToTail(
                    "auto",
                    { source, phase: "settle", settle: { frame: stableFrames, offBottomFrames, stagnantFrames: 0 } },
                    jumpTailRevision,
                  );
                }
                remainingCorrections -= 1;
                offBottomFrames += 1;
                verifyPreviousHeight = transactionElement.scrollHeight;
                verifyPreviousTop = transactionElement.scrollTop;
                verifyStableFrames = 0;
                verifyStableSince = null;
                verifyStartedAt = Date.now();
                jumpTailTimer = window.setTimeout(verifyMeasuredTail, JUMP_TAIL_SAMPLE_MS);
                return;
              }
              if (remainingCorrections > 0) {
                jumpTailTimer = window.setTimeout(verifyMeasuredTail, JUMP_TAIL_SAMPLE_MS);
              } else {
                jumpTailStartedAt = null;
                armLayoutTransientIdle();
              }
            };
            jumpTailTimer = window.setTimeout(verifyMeasuredTail, JUMP_TAIL_SAMPLE_MS);
          } else {
            jumpTailStartedAt = null;
            armLayoutTransientIdle();
          }
          return;
        }
        jumpTailTimer = window.setTimeout(confirmJumpTail, JUMP_TAIL_SAMPLE_MS);
      };
      jumpTailTimer = window.setTimeout(confirmJumpTail, JUMP_TAIL_SAMPLE_MS);
      return;
    }

    // Before LAST mounts, the explicit index jump is the only writer. Once the
    // tail exists, fold streamed geometry into the same transaction so its
    // long measurement confirmation cannot suppress live tail following.
    if (jumpTailStartedAt !== null) {
      jumpTailRevision = Math.max(jumpTailRevision, geometryRevision);
      const tailMounted = transcriptTailIsMounted(element);
      const offBottom = nativeTranscriptDistanceFromBottom(element) > JUMP_TAIL_NATIVE_THRESHOLD_PX;
      if (geometryRevision > writtenRevision && remountJumpTail(element, source, geometryRevision)) {
        writtenRevision = geometryRevision;
        return;
      }
      if (deferUntilStable && tailMounted && offBottom) {
        // A real measured extent has grown after LAST mounted. End the mount
        // transaction before handing this revision to the stable-frame path;
        // otherwise the jump owner can suppress live following until its
        // long fallback timeout even though it no longer has mount work.
        if (jumpTailTimer !== null) window.clearTimeout(jumpTailTimer);
        jumpTailTimer = null;
        jumpTailStartedAt = null;
      } else {
        if (
          !deferUntilStable
          && geometryRevision > writtenRevision
          && tailMounted
          && !transcriptTailExtentCollapsed(lastStableHeight, element.scrollHeight, element.clientHeight)
          && offBottom
        ) {
          const wrote = scrollToTail(
            "auto",
            { source, phase: "settle", settle: { frame: 0, offBottomFrames: 1, stagnantFrames: 0 } },
            geometryRevision,
          );
          if (wrote) writtenRevision = geometryRevision;
        }
        return;
      }
    }

    if (geometryRevision <= writtenRevision) return;
    if (!pending || geometryRevision >= pending.revision) {
      pending = {
        revision: geometryRevision,
        source,
        element,
        generation: generationRef.current,
        offBottomFrames: 0,
        bottomFrames: 0,
        stableFrames: 0,
        lastObservedHeight: element.scrollHeight,
        stableSince: Date.now(),
        collapseFrames: 0,
        lastCollapsedHeight: null,
        deferUntilStable,
      };
    } else if (!deferUntilStable) {
      pending.deferUntilStable = false;
    }
    // Geometry signals can originate in ResizeObserver's pre-paint delivery.
    // Run the first extent check synchronously so a tail correction lands in
    // that same rendering opportunity. Stable-bottom and transient-collapse
    // confirmation still continue through bounded animation frames in tick().
    if (tailSettleFrame === null) tick();
  };

  return { scrollToTail, schedule, cancel, noteLayoutTransient };
}
