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

// Bounds mirror the arbiter's recovery loop: 1px correction tolerance, two
// stable frames, or a shared 1000ms wall-clock budget, whichever comes first.
const ANCHOR_COMPENSATION_BUDGET_MS = 1_000;
const ANCHOR_COMPENSATION_STABLE_FRAMES = 2;
const ANCHOR_COMPENSATION_TOLERANCE_PX = 1;

type ManualAnchor = Extract<TranscriptLayoutAnchor, { mode: "manual" }>;

type ActiveAnchorCompensation = {
  anchor: ManualAnchor;
  frame: number | null;
  stableFrames: number;
  deadline: number;
};

export type TranscriptAnchorCompensation = {
  /** Feed every dispatched arbiter event: SCROLL_DELIVERED re-samples the
   *  anchor from live geometry, ownership-changing events end an in-flight
   *  loop. The steady-state offset owners (the loop's own writes) are exempt. */
  noteEvent: (event: TranscriptScrollEvent) => void;
  /** Arm the bounded correction loop after a height-change notification. */
  schedule: () => void;
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

  const cancel = () => {
    const compensation = active;
    active = null;
    if (compensation?.frame !== null && compensation?.frame !== undefined) {
      cancelAnimationFrame(compensation.frame);
    }
  };

  const sample = (element: HTMLDivElement) => {
    // Skipped while a loop owns the reference so its own writes cannot move
    // the goalposts mid-flight.
    if (stateRef.current.mode !== "manual" || active !== null) return;
    anchor = captureVisibleTranscriptLayoutAnchor(element) ?? null;
  };

  const schedule = () => {
    if (active !== null) return;
    const element = scrollRef.current;
    if (!element) return;
    if (modeRef.current !== "manual") return;
    // Never fight an active gesture, an in-flight recovery, or an armed
    // reader-extent guard: those already own viewport corrections.
    if (stateRef.current.readerIntent || stateRef.current.recoveryId !== null || readerExtentIsActive()) return;
    if (!anchor) return;
    const generation = generationRef.current;
    const compensation: ActiveAnchorCompensation = {
      anchor,
      frame: null,
      stableFrames: 0,
      deadline: Date.now() + ANCHOR_COMPENSATION_BUDGET_MS,
    };
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
        active = null;
        return;
      }
      const current = scrollRef.current;
      if (!current) {
        active = null;
        return;
      }
      const row = Array.from(current.querySelectorAll<HTMLElement>(".transcript__row[data-row-key]"))
        .find((candidate) => candidate.dataset.rowKey === compensation.anchor.rowKey);
      if (!row) {
        // The anchor row is unmounted: without a measurement there is no
        // trustworthy correction, so the compensation simply stops.
        active = null;
        return;
      }
      const correction = row.getBoundingClientRect().top - current.getBoundingClientRect().top - compensation.anchor.offset;
      if (Math.abs(correction) > ANCHOR_COMPENSATION_TOLERANCE_PX) {
        compensation.stableFrames = 0;
        dispatch({ type: "SCROLL_TO_OFFSET", owner: "anchor-compensation", top: current.scrollTop + correction, behavior: "auto" });
      } else {
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
    if (event.type === "SCROLL_DELIVERED") {
      const element = scrollRef.current;
      if (element) sample(element);
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
  };

  return { noteEvent, schedule, reset };
}
