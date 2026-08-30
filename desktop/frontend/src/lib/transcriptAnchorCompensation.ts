import type { RefObject } from "react";
import type {
  TranscriptScrollEvent,
  TranscriptScrollMode,
  TranscriptScrollState,
} from "./transcriptScrollArbiter";
import {
  captureVisibleTranscriptLayoutAnchor,
  type TranscriptLayoutAnchor,
} from "./transcriptVirtuosoRecovery";
import { MIN_REVERSE_JUMP_PX } from "./transcriptReaderExtentStability";

// Fractional row metrics can shift by 1-2px while Virtuoso's estimate tree is
// converging during ordinary traversal. Treat that as layout noise so a
// completed reader transaction cannot turn it into a stream of tiny writes.
// Larger drift still uses two stable frames and the shared 1000ms budget.
const ANCHOR_COMPENSATION_BUDGET_MS = 1_000;
const ANCHOR_COMPENSATION_STABLE_FRAMES = 2;
const ANCHOR_COMPENSATION_TOLERANCE_PX = 8;

type ManualAnchor = Extract<TranscriptLayoutAnchor, { mode: "manual" }>;

type ActiveAnchorCompensation = {
  anchor: ManualAnchor;
  element: HTMLDivElement;
  frame: number | null;
  stableFrames: number;
  deadline: number;
  visualOffset: number;
};

export type TranscriptAnchorCompensation = {
  /** Feed every dispatched arbiter event: SCROLL_DELIVERED re-samples the
   *  anchor from live geometry, ownership-changing events end an in-flight
   *  loop. The steady-state offset owners (the loop's own writes) are exempt. */
  noteEvent: (event: TranscriptScrollEvent) => void;
  /** Arm the bounded correction loop after a height-change notification. */
  schedule: () => void;
  /** Continue a deferred geometry reconciliation from the transaction's last
   * accepted logical anchor, not from a browser clamp delivery. */
  adoptReaderAnchor: (anchor: { rowKey: string; offset: number } | undefined) => void;
  /** Cancel the loop and drop the sampled anchor (reset / scroller change /
   *  unmount). */
  reset: () => void;
};

/**
 * Steady-state viewport anchoring for manual reading (#8438/#8488/#8897).
 * While the user owns the viewport, the topmost visible row is re-sampled as
 * the anchor on every delivered scroll. When content ABOVE the viewport then
 * changes height (fold auto-collapse, history patch), the anchor row's
 * measured drift is compensated through the arbiter's SCROLL_TO_OFFSET
 * channel (owner "anchor-compensation") instead of pushing the viewport.
 * Changes below the viewport (streaming footer growth) leave the anchor row
 * put, so they measure zero drift and earn no write.
 *
 * Deterministic-clock discipline: global requestAnimationFrame / Date.now
 * only, same as the arbiter's recovery loop, so the fake-clock race harness
 * drives it. All inputs are stable refs, so the controller is created once
 * per arbiter hook instance and never re-created.
 */
export function createTranscriptAnchorCompensation({
  scrollRef,
  modeRef,
  stateRef,
  generationRef,
  readerExtentIsActive,
  dispatch,
}: {
  scrollRef: RefObject<HTMLDivElement | null>;
  modeRef: RefObject<TranscriptScrollMode>;
  stateRef: RefObject<TranscriptScrollState>;
  generationRef: RefObject<number>;
  /** An armed reader-extent guard owns post-gesture extent corrections; the
   *  compensation loop must stay out of its way. */
  readerExtentIsActive: () => boolean;
  dispatch: (event: TranscriptScrollEvent) => void;
}): TranscriptAnchorCompensation {
  let anchor: ManualAnchor | null = null;
  let active: ActiveAnchorCompensation | null = null;
  let pendingAfterReader = false;

  const clearVisualGuard = (compensation: ActiveAnchorCompensation) => {
    compensation.visualOffset = 0;
    delete compensation.element.dataset.transcriptReaderVisualGuard;
    compensation.element.style.removeProperty("--transcript-reader-visual-offset");
  };

  const anchorRow = (compensation: ActiveAnchorCompensation, element: HTMLDivElement) => (
    Array.from(element.querySelectorAll<HTMLElement>(".transcript__row[data-row-key]"))
      .find((candidate) => candidate.dataset.rowKey === compensation.anchor.rowKey)
  );

  const physicalCorrection = (compensation: ActiveAnchorCompensation, element: HTMLDivElement) => {
    const row = anchorRow(compensation, element);
    if (!row) return null;
    const rendered = row.getBoundingClientRect().top - element.getBoundingClientRect().top - compensation.anchor.offset;
    return rendered - compensation.visualOffset;
  };

  const guardLargeDrift = (compensation: ActiveAnchorCompensation, element: HTMLDivElement) => {
    const correction = physicalCorrection(compensation, element);
    if (correction === null || Math.abs(correction) < MIN_REVERSE_JUMP_PX) return;
    compensation.visualOffset = -correction;
    element.dataset.transcriptReaderVisualGuard = "true";
    element.style.setProperty("--transcript-reader-visual-offset", `${compensation.visualOffset}px`);
  };

  const cancel = () => {
    const compensation = active;
    active = null;
    if (compensation?.frame !== null && compensation?.frame !== undefined) {
      cancelAnimationFrame(compensation.frame);
    }
    if (compensation) clearVisualGuard(compensation);
  };

  const sample = (element: HTMLDivElement) => {
    // Skipped while a loop owns the reference so its own writes cannot move
    // the goalposts mid-flight.
    if (stateRef.current.mode !== "manual" || active !== null) return;
    anchor = captureVisibleTranscriptLayoutAnchor(element) ?? null;
  };

  const schedule = () => {
    const element = scrollRef.current;
    if (!element) return;
    if (modeRef.current !== "manual") return;
    // Never fight an active gesture, an in-flight recovery, or an armed
    // reader-extent guard: those already own viewport corrections.
    if (stateRef.current.readerIntent || readerExtentIsActive()) {
      pendingAfterReader = true;
      return;
    }
    if (stateRef.current.recoveryId !== null) return;
    if (active !== null) {
      if (active.element === element) {
        guardLargeDrift(active, element);
        return;
      }
      cancel();
    }
    if (!anchor) return;
    const generation = generationRef.current;
    const compensation: ActiveAnchorCompensation = {
      anchor,
      element,
      frame: null,
      stableFrames: 0,
      deadline: Date.now() + ANCHOR_COMPENSATION_BUDGET_MS,
      visualOffset: 0,
    };
    guardLargeDrift(compensation, element);
    pendingAfterReader = false;
    const tick = () => {
      compensation.frame = null;
      if (active !== compensation) return;
      if (
        generationRef.current !== generation
        || modeRef.current !== "manual"
        || stateRef.current.readerIntent
        || stateRef.current.recoveryId !== null
        || readerExtentIsActive()
      ) {
        clearVisualGuard(compensation);
        active = null;
        return;
      }
      const current = scrollRef.current;
      if (!current) {
        clearVisualGuard(compensation);
        active = null;
        return;
      }
      const correction = physicalCorrection(compensation, current);
      if (correction === null) {
        // The anchor row is unmounted: without a measurement there is no
        // trustworthy correction, so the compensation simply stops.
        clearVisualGuard(compensation);
        active = null;
        return;
      }
      if (Math.abs(correction) > ANCHOR_COMPENSATION_TOLERANCE_PX) {
        compensation.stableFrames = 0;
        dispatch({ type: "SCROLL_TO_OFFSET", owner: "anchor-compensation", top: current.scrollTop + correction, behavior: "auto" });
        clearVisualGuard(compensation);
      } else {
        clearVisualGuard(compensation);
        compensation.stableFrames += 1;
      }
      if (compensation.stableFrames >= ANCHOR_COMPENSATION_STABLE_FRAMES || Date.now() >= compensation.deadline) {
        active = null;
        return;
      }
      compensation.frame = requestAnimationFrame(tick);
    };
    active = compensation;
    compensation.frame = requestAnimationFrame(tick);
  };

  const noteEvent = (event: TranscriptScrollEvent) => {
    if (event.type === "READER_TRANSACTION_END" && pendingAfterReader) {
      schedule();
      return;
    }
    if (event.type === "SCROLL_DELIVERED") {
      const element = scrollRef.current;
      // Scroll deliveries caused by a geometry commit must not replace the
      // reader transaction's last accepted logical anchor.
      if (element && !readerExtentIsActive()) sample(element);
      return;
    }
    if (
      event.type === "RESET"
      || event.type === "USER_SCROLL_INTENT"
      || event.type === "MANUAL_READING"
      || event.type === "USER_RESIZE_BEGIN"
      || event.type === "SELECTION_BEGIN"
      || event.type === "PROGRAMMATIC_BEGIN"
      || event.type === "JUMP_TO_BOTTOM"
      || event.type === "JUMP_TO_INDEX"
      || event.type === "RECOVERY_BEGIN"
      || (event.type === "SCROLL_TO_OFFSET" && event.owner !== "anchor-compensation" && event.owner !== "block-window-prepend")
    ) {
      cancel();
    }
    if (event.type === "RESET") anchor = null;
  };

  const reset = () => {
    cancel();
    anchor = null;
    pendingAfterReader = false;
  };

  const adoptReaderAnchor = (next: { rowKey: string; offset: number } | undefined) => {
    if (next) anchor = { mode: "manual", ...next };
  };

  return { noteEvent, schedule, reset, adoptReaderAnchor };
}
