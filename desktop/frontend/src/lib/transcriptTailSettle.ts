import type { RefObject } from "react";
import type { VirtuosoHandle } from "react-virtuoso";
import { noteTranscriptScrollWrite } from "./transcriptScrollProbe";
import {
  CAPTURE_TRANSCRIPT_SCROLL_DIAGNOSTICS,
  type TranscriptScrollDiagnosticSource,
  type TranscriptTailWriteDiagnostic,
} from "./transcriptScrollDiagnosticProbe";
import type { TranscriptScrollMode } from "./transcriptScrollArbiter";
import { nativeTranscriptDistanceFromBottom, TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX } from "./transcriptScrollGeometry";

const TAIL_STAGNANT_FRAME_LIMIT = 2;
// Ignore one-frame extent oscillation; real growth remains displaced and
// converges on the following frame (#9028/#9089).
const TAIL_CONFIRM_OFF_BOTTOM_FRAMES = 2;
const JUMP_TAIL_TRANSACTION_MS = 240;
const LAYOUT_TRANSIENT_IDLE_MS = 160;

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
  noteLayoutTransient: () => void;
};

/**
 * The tail-follow writer and its bounded settle loop, extracted from the
 * scroll arbiter hook (file-size budget). Behavior is identical: it closes
 * over the same refs and uses only the global requestAnimationFrame /
 * window.setTimeout clocks, so the fake-clock race harness drives it.
 */
export function createTranscriptTailSettle({
  virtuosoRef,
  scrollRef,
  modeRef,
  generationRef,
  layoutTransientRef,
}: {
  virtuosoRef: RefObject<VirtuosoHandle | null>;
  scrollRef: RefObject<HTMLDivElement | null>;
  modeRef: RefObject<TranscriptScrollMode>;
  generationRef: RefObject<number>;
  layoutTransientRef: RefObject<boolean>;
}): TranscriptTailSettle {
  let tailSettleFrame: number | null = null;
  let tailSettleProgress: {
    distance: number;
    stagnantFrames: number;
    offBottomFrames: number;
  } | null = null;
  let jumpTailTimer: number | null = null;
  let layoutTransientIdleTimer: number | null = null;

  const scrollToTail = (
    behavior: "auto" | "smooth",
    diagnostic?: TranscriptTailWriteDiagnostic,
  ) => {
    const element = scrollRef.current;
    if (!element) return;
    const top = element.scrollHeight;
    if (CAPTURE_TRANSCRIPT_SCROLL_DIAGNOSTICS && diagnostic) {
      noteTranscriptScrollWrite({
        owner: "tail-follow",
        kind: "scrollTo",
        top,
        source: diagnostic.source,
        phase: diagnostic.phase,
        scrollTop: element.scrollTop,
        scrollHeight: element.scrollHeight,
        clientHeight: element.clientHeight,
        bottomDistance: nativeTranscriptDistanceFromBottom(element),
        mode: modeRef.current,
        settleFrame: diagnostic.settle?.frame,
        offBottomFrames: diagnostic.settle?.offBottomFrames,
        stagnantFrames: diagnostic.settle?.stagnantFrames,
      });
    } else {
      noteTranscriptScrollWrite({ owner: "tail-follow", kind: "scrollTo", top });
    }
    virtuosoRef.current?.scrollTo({ top, behavior });
  };

  const cancel = () => {
    if (tailSettleFrame !== null) cancelAnimationFrame(tailSettleFrame);
    tailSettleFrame = null;
    tailSettleProgress = null;
    if (jumpTailTimer !== null) window.clearTimeout(jumpTailTimer);
    jumpTailTimer = null;
    if (layoutTransientIdleTimer !== null) window.clearTimeout(layoutTransientIdleTimer);
    layoutTransientIdleTimer = null;
    layoutTransientRef.current = false;
  };

  const armLayoutTransientIdle = () => {
    if (layoutTransientIdleTimer !== null) window.clearTimeout(layoutTransientIdleTimer);
    layoutTransientIdleTimer = window.setTimeout(() => {
      layoutTransientIdleTimer = null;
      if (tailSettleFrame !== null) return;
      layoutTransientRef.current = false;
      tailSettleProgress = null;
    }, LAYOUT_TRANSIENT_IDLE_MS);
  };

  const noteLayoutTransient = () => {
    layoutTransientRef.current = true;
    armLayoutTransientIdle();
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
      const transactionElement = scrollElement;
      jumpTailTimer = window.setTimeout(() => {
        jumpTailTimer = null;
        const element = scrollRef.current;
        if (element && element === transactionElement && modeRef.current === "tail-follow") {
          const distance = nativeTranscriptDistanceFromBottom(element);
          if (distance > TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX) {
            scrollToTail("auto", CAPTURE_TRANSCRIPT_SCROLL_DIAGNOSTICS && source
              ? { source, phase: "settle", settle: { frame: 0, offBottomFrames: 0, stagnantFrames: 0 } }
              : undefined);
          }
          armLayoutTransientIdle();
        } else {
          layoutTransientRef.current = false;
        }
      }, JUMP_TAIL_TRANSACTION_MS);
    }
    if (jumpTailTimer !== null) return;
    if (tailSettleFrame !== null) return;
    const generation = generationRef.current;
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
      if (distance <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX) {
        tailSettleProgress = null;
        armLayoutTransientIdle();
        return;
      }
      const previous = tailSettleProgress;
      const offBottomFrames = (previous?.offBottomFrames ?? 0) + 1;
      if (offBottomFrames < TAIL_CONFIRM_OFF_BOTTOM_FRAMES) {
        tailSettleProgress = { distance, stagnantFrames: 0, offBottomFrames };
        tailSettleFrame = requestAnimationFrame(tick);
        return;
      }
      const stagnantFrames = previous && Math.abs(previous.distance - distance) <= 0.5
        ? previous.stagnantFrames + 1
        : 0;
      scrollToTail("auto", CAPTURE_TRANSCRIPT_SCROLL_DIAGNOSTICS && source
        ? { source, phase: "settle", settle: { frame: offBottomFrames, offBottomFrames, stagnantFrames } }
        : undefined);
      tailSettleProgress = { distance, stagnantFrames, offBottomFrames };
      if (stagnantFrames < TAIL_STAGNANT_FRAME_LIMIT) tailSettleFrame = requestAnimationFrame(tick);
      else armLayoutTransientIdle();
    };
    tailSettleFrame = requestAnimationFrame(tick);
  };

  return { scrollToTail, schedule, cancel, noteLayoutTransient };
}
