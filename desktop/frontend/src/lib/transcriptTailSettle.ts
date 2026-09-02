import type { RefObject } from "react";
import {
  type TranscriptScrollDiagnosticSource,
  type TranscriptTailWriteDiagnostic,
} from "./transcriptScrollDiagnosticProbe";
import type { TranscriptScrollMode } from "./transcriptScrollArbiter";
import { nativeTranscriptDistanceFromBottom, observeNativeTranscriptTailClamp, tailTop, TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX } from "./transcriptScrollGeometry";
import type { TranscriptScrollWriter } from "./transcriptScrollWriter";
import type { TranscriptGeometryChangeSource } from "./transcriptGeometryRevision";

const TAIL_STAGNANT_FRAME_LIMIT = 2;
const TAIL_SETTLE_MAX_ATTEMPTS = 8;
// Ignore sub-row measurement jitter after the physical tail is already
// pinned. Real content growth still re-arms the settle transaction.
export const TRANSCRIPT_TAIL_REARM_MIN_HEIGHT_PX = 24;
// Ignore one-frame extent oscillation; real growth remains displaced and
// converges on the following frame (#9028/#9089).
const TAIL_CONFIRM_OFF_BOTTOM_FRAMES = 2;
const JUMP_TAIL_TRANSACTION_MS = 240;
const LAYOUT_TRANSIENT_IDLE_MS = 160;

export function transcriptTailSettleBudgetExhausted(attempts: number): boolean {
  return attempts >= TAIL_SETTLE_MAX_ATTEMPTS;
}

export function transcriptTailShouldReaim(previousBottomHeight: number | null, currentHeight: number): boolean {
  if (previousBottomHeight == null) return true;
  return currentHeight - previousBottomHeight >= TRANSCRIPT_TAIL_REARM_MIN_HEIGHT_PX;
}

export function transcriptTailIsStranded(mode: TranscriptScrollMode, distance: number, exhausted: boolean): boolean {
  return exhausted && mode === "tail-follow" && distance > TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX;
}

export type TranscriptTailSettle = {
  /** Native-extent tail write. Converges against real DOM geometry and avoids
   *  scrollToIndex("LAST") retries against a stale Virtuoso size tree (#9028). */
  scrollToTail: (behavior: "auto" | "smooth", diagnostic?: TranscriptTailWriteDiagnostic) => void;
  /** Re-aim across a bounded number of frames after a tail write or layout
   *  growth notification. */
  schedule: (jump: boolean, source?: TranscriptScrollDiagnosticSource) => void;
  /** End the settle loop and its idle/jump timers (also marks the layout
   *  transient over). */
  cancel: () => void;
  /** Mark a layout-transient window and arm its bounded idle expiry. */
  noteLayoutTransient: (source?: TranscriptGeometryChangeSource) => void;
  /** Repair an already-owned live tail before a structural Footer commit paints. */
  pinLiveTailBeforePaint: () => boolean;
};

/**
 * The tail-follow writer and its bounded settle loop, extracted from the
 * scroll arbiter hook (file-size budget). Behavior is identical: it closes
 * over the same refs and uses only the global requestAnimationFrame /
 * window.setTimeout clocks, so the fake-clock race harness drives it.
 */
export function createTranscriptTailSettle({
  writer,
  scrollRef,
  modeRef,
  generationRef,
  ownershipEpochRef,
  geometryRevisionRef,
  layoutTransientRef,
  onStranded,
}: {
  writer: TranscriptScrollWriter;
  scrollRef: RefObject<HTMLDivElement | null>;
  modeRef: RefObject<TranscriptScrollMode>;
  generationRef: RefObject<number>;
  ownershipEpochRef: RefObject<number>;
  geometryRevisionRef: RefObject<number>;
  layoutTransientRef: RefObject<boolean>;
  /** Releases exhausted tail ownership so the jump-bottom recovery stays reachable. */
  onStranded?: () => void;
}): TranscriptTailSettle {
  let tailSettleFrame: number | null = null;
  let tailSettleProgress: {
    distance: number;
    stagnantFrames: number;
    offBottomFrames: number;
    attempts: number;
  } | null = null;
  let jumpTailTimer: number | null = null;
  let jumpTailFollowupTimer: number | null = null;
  let layoutTransientIdleTimer: number | null = null;
  let lastBottomHeight: number | null = null;
  let lastBottomViewport: number | null = null;
  let tailPinned = false;
  let ineffectivePin = false;
  let settleExhausted = false;
  let pendingPin: { top: number; height: number; previousTop: number } | null = null;
  let lastPinAttempt: { element: HTMLDivElement; top: number; height: number; clientHeight: number; previousTop: number } | null = null;
  // 0=fresh, 1=awaiting LAST commit, 2=LAST committed, 3=quiet retry spent.
  let fallbackState = 0;
  let fallbackEpoch = -1;

  const requestTailMount = () => {
    if (fallbackState !== 0) return;
    if (writer.write({
      owner: "tail-follow",
      operation: "scrollToIndex",
      index: "LAST",
      align: "end",
      reason: "tail-range-mount",
      phase: "mount-anchor",
      expectedSurfaceGeneration: generationRef.current,
      expectedOwnershipEpoch: ownershipEpochRef.current,
      expectedGeometryRevision: geometryRevisionRef.current,
    })) fallbackState = 1;
  };

  const scrollToTail = (
    behavior: "auto" | "smooth",
    diagnostic?: TranscriptTailWriteDiagnostic,
  ) => {
    const element = scrollRef.current;
    if (!element) return;
    const previousAttempt = lastPinAttempt;
    if (
      previousAttempt?.element === element
      && Math.abs(previousAttempt.height - element.scrollHeight) <= 1
      && Math.abs(previousAttempt.clientHeight - element.clientHeight) <= 1
      && previousAttempt.top >= element.scrollHeight - element.clientHeight - TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX
    ) observeNativeTranscriptTailClamp(element, previousAttempt.previousTop);
    const top = tailTop(element);
    const beforeTop = element.scrollTop;
    if (fallbackEpoch !== ownershipEpochRef.current) {
      fallbackEpoch = ownershipEpochRef.current;
      fallbackState = 0;
    }
    lastBottomHeight = element.scrollHeight;
    lastBottomViewport = element.clientHeight;
    tailPinned = false;
    if (!writer.write({
      owner: "tail-follow",
      operation: "pinTail",
      top,
      behavior,
      reason: diagnostic?.source ?? "tail-follow",
      phase: diagnostic?.phase,
      expectedSurfaceGeneration: generationRef.current,
      expectedOwnershipEpoch: ownershipEpochRef.current,
      expectedGeometryRevision: geometryRevisionRef.current,
      settleFrame: diagnostic?.settle?.frame,
      offBottomFrames: diagnostic?.settle?.offBottomFrames,
      stagnantFrames: diagnostic?.settle?.stagnantFrames,
    })) return;
    lastPinAttempt = {
      element,
      top,
      height: element.scrollHeight,
      clientHeight: element.clientHeight,
      previousTop: beforeTop,
    };
    // WebKit/Virtuoso can accept a physical tail target while its size tree
    // still clamps the native scroller to the previous mounted range. Quarantine
    // that no-op until the quiet window instead of retrying every revision.
    const observedClamp = behavior === "auto"
      && observeNativeTranscriptTailClamp(element, beforeTop);
    ineffectivePin = !observedClamp
      && nativeTranscriptDistanceFromBottom(element) > TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX
      && Math.abs(element.scrollTop - beforeTop) <= 0.5;
    pendingPin = ineffectivePin || observedClamp ? null : { top, height: element.scrollHeight, previousTop: beforeTop };
    if (observedClamp) {
      tailPinned = true;
    }
    if (ineffectivePin) requestTailMount();
  };

  const cancel = () => {
    if (tailSettleFrame !== null) cancelAnimationFrame(tailSettleFrame);
    tailSettleFrame = null;
    tailSettleProgress = null;
    lastBottomHeight = null;
    lastBottomViewport = null;
    tailPinned = false;
    ineffectivePin = false;
    settleExhausted = false;
    pendingPin = null;
    fallbackState = 0;
    if (jumpTailTimer !== null) window.clearTimeout(jumpTailTimer);
    jumpTailTimer = null;
    if (jumpTailFollowupTimer !== null) window.clearTimeout(jumpTailFollowupTimer);
    jumpTailFollowupTimer = null;
    if (layoutTransientIdleTimer !== null) window.clearTimeout(layoutTransientIdleTimer);
    layoutTransientIdleTimer = null;
    layoutTransientRef.current = false;
  };

  const armLayoutTransientIdle = () => {
    if (layoutTransientIdleTimer !== null) window.clearTimeout(layoutTransientIdleTimer);
    layoutTransientIdleTimer = window.setTimeout(() => {
      layoutTransientIdleTimer = null;
      if (tailSettleFrame !== null || jumpTailTimer !== null || jumpTailFollowupTimer !== null) return;
      layoutTransientRef.current = false;
      tailSettleProgress = null;
      if (ineffectivePin && modeRef.current === "tail-follow" && fallbackState !== 3) {
        fallbackState = 3;
        ineffectivePin = false;
        schedule(false, "layout-height-changed");
        return;
      }
      const element = scrollRef.current;
      const exhausted = settleExhausted || (ineffectivePin && fallbackState === 3);
      settleExhausted = false;
      if (element && transcriptTailIsStranded(modeRef.current, nativeTranscriptDistanceFromBottom(element), exhausted)) onStranded?.();
    }, LAYOUT_TRANSIENT_IDLE_MS);
  };

  const noteLayoutTransient = (source?: TranscriptGeometryChangeSource) => {
    // A failed physical pin may be retried only for a new product-level
    // geometry input. Virtuoso's own row/range observations are often emitted
    // by the failed write itself and must not create a feedback loop.
    if (ineffectivePin && fallbackState === 1 && source === "items-rendered") {
      ineffectivePin = false;
      fallbackState = 2;
    } else if (ineffectivePin && (source === "data-change" || source === "footer-resize")) {
      ineffectivePin = false;
      fallbackState = 0;
    }
    layoutTransientRef.current = true;
    armLayoutTransientIdle();
  };

  const pinLiveTailBeforePaint = () => {
    if (!scrollRef.current || modeRef.current !== "tail-follow") return false;
    geometryRevisionRef.current += 1;
    noteLayoutTransient();
    scrollToTail("auto", { source: "tail-content-changed", phase: "initial" });
    schedule(false, "tail-content-changed");
    return true;
  };

  const schedule = (jump: boolean, source?: TranscriptScrollDiagnosticSource) => {
    const scrollElement = scrollRef.current;
    if (!scrollElement) {
      layoutTransientRef.current = false;
      tailSettleProgress = null;
      return;
    }
    layoutTransientRef.current = true;
    if (layoutTransientIdleTimer !== null) {
      window.clearTimeout(layoutTransientIdleTimer);
      layoutTransientIdleTimer = null;
    }
    if (jump) {
      if (jumpTailTimer !== null) window.clearTimeout(jumpTailTimer);
      if (jumpTailFollowupTimer !== null) window.clearTimeout(jumpTailFollowupTimer);
      jumpTailFollowupTimer = null;
      const transactionElement = scrollElement;
      const confirmJumpTail = (settleFrame: number, final: boolean) => {
        const element = scrollRef.current;
        if (element && element === transactionElement && modeRef.current === "tail-follow") {
          const physicalDistance = tailTop(element) - element.scrollTop;
          if (physicalDistance > TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX) {
            const geometryChanged = lastBottomHeight !== null && (
              Math.abs(element.scrollHeight - lastBottomHeight) > 1
              || (lastBottomViewport !== null && Math.abs(element.clientHeight - lastBottomViewport) > 1)
            );
            if (geometryChanged) geometryRevisionRef.current += 1;
            scrollToTail("auto", {
              source,
              phase: "settle",
              settle: { frame: settleFrame },
            });
          }
          if (!final) {
            jumpTailFollowupTimer = window.setTimeout(() => {
              jumpTailFollowupTimer = null;
              confirmJumpTail(2, true);
            }, JUMP_TAIL_TRANSACTION_MS);
          } else {
            armLayoutTransientIdle();
          }
        } else {
          jumpTailFollowupTimer = null;
          layoutTransientRef.current = false;
        }
      };
      jumpTailTimer = window.setTimeout(() => {
        jumpTailTimer = null;
        confirmJumpTail(1, false);
      }, JUMP_TAIL_TRANSACTION_MS);
    }
    if (jumpTailTimer !== null) return;
    if (tailSettleFrame !== null) return;
    if (ineffectivePin) {
      if (nativeTranscriptDistanceFromBottom(scrollElement) <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX) {
        tailPinned = true;
      }
      armLayoutTransientIdle();
      return;
    }
    // Virtuoso emits small layout updates while a mounted row settles. Once
    // the tail has been pinned, those updates must not start another writer;
    // only a real height revision is allowed to re-arm it.
    const distance = nativeTranscriptDistanceFromBottom(scrollElement);
    if (
      !jump
      && tailPinned
      && distance <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX
      && !transcriptTailShouldReaim(lastBottomHeight, scrollElement.scrollHeight)
    ) {
      armLayoutTransientIdle();
      return;
    }
    const generation = generationRef.current;
    settleExhausted = false;
    const tick = () => {
      tailSettleFrame = null;
      if (
        generationRef.current !== generation
        || scrollRef.current !== scrollElement
        || modeRef.current !== "tail-follow"
      ) {
        tailSettleProgress = null;
        layoutTransientRef.current = false;
        return;
      }
      const element = scrollRef.current;
      if (!element) return;
      const distance = nativeTranscriptDistanceFromBottom(element);
      if (pendingPin) {
        const collapseThreshold = Math.max(96, element.clientHeight * 0.5);
        const collapsedAfterPin = pendingPin.height - element.scrollHeight >= collapseThreshold;
        const held = distance <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX
          || Math.abs(element.scrollTop - pendingPin.top) <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX;
        const sameExtent = Math.abs(element.scrollHeight - pendingPin.height) <= 1;
        if (collapsedAfterPin) {
          // The write landed only by collapsing Virtuoso's intermediate
          // extent. Treat the smaller physical bottom as safe, but keep the
          // write quarantined so the subsequent size-tree rebound cannot
          // start another pin/measure cycle.
          pendingPin = null;
          ineffectivePin = true;
          requestTailMount();
          tailPinned = distance <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX;
          tailSettleProgress = null;
          armLayoutTransientIdle();
          return;
        }
        if (held) pendingPin = null;
        else if (sameExtent) {
          if (observeNativeTranscriptTailClamp(element, pendingPin.previousTop)) {
            pendingPin = null;
            tailPinned = true;
            tailSettleProgress = null;
            armLayoutTransientIdle();
            return;
          }
          // The browser accepted the write synchronously, then Virtuoso
          // restored the previous range on the next frame. Quarantine that
          // revision rather than chasing it with another write.
          pendingPin = null;
          ineffectivePin = true;
          requestTailMount();
          tailSettleProgress = null;
          armLayoutTransientIdle();
          return;
        } else {
          pendingPin = null;
        }
      }
      if (ineffectivePin) {
        tailSettleProgress = null;
        armLayoutTransientIdle();
        return;
      }
      if (distance <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX) {
        tailPinned = true;
        tailSettleProgress = null;
        armLayoutTransientIdle();
        return;
      }
      const previous = tailSettleProgress;
      const offBottomFrames = (previous?.offBottomFrames ?? 0) + 1;
      if (offBottomFrames < TAIL_CONFIRM_OFF_BOTTOM_FRAMES) {
        tailSettleProgress = { distance, stagnantFrames: 0, offBottomFrames, attempts: offBottomFrames };
        tailSettleFrame = requestAnimationFrame(tick);
        return;
      }
      const attempts = (previous?.attempts ?? 0) + 1;
      if (transcriptTailSettleBudgetExhausted(attempts)) {
        settleExhausted = true;
        tailSettleProgress = null;
        armLayoutTransientIdle();
        return;
      }
      const stagnantFrames = previous && Math.abs(previous.distance - distance) <= 0.5
        ? previous.stagnantFrames + 1
        : 0;
      scrollToTail("auto", {
        source,
        phase: "settle",
        settle: { frame: offBottomFrames, offBottomFrames, stagnantFrames },
      });
      tailSettleProgress = { distance, stagnantFrames, offBottomFrames, attempts };
      if (stagnantFrames < TAIL_STAGNANT_FRAME_LIMIT && !transcriptTailSettleBudgetExhausted(attempts)) tailSettleFrame = requestAnimationFrame(tick);
      else {
        settleExhausted = true;
        armLayoutTransientIdle();
      }
    };
    tailSettleFrame = requestAnimationFrame(tick);
  };

  return { scrollToTail, schedule, cancel, noteLayoutTransient, pinLiveTailBeforePaint };
}
