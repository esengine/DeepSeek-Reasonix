export type TranscriptScrollMode =
  | "tail-follow"
  | "manual"
  | "native-thumb"
  | "user-resize"
  | "selection"
  | "restoring";

export type TranscriptScrollOwner =
  | "jump"
  | "rewind"
  | "jump-bottom"
  | "custom-scrollbar"
  | "selection-edge-scroll"
  | "anchor-compensation"
  | "block-window-prepend";

/** Why an in-flight recovery request was cancelled. "user-takeover" is the
 *  only preemption path user gestures take; the other two are lifecycle
 *  bookkeeping (surface remount, a newer request superseding). */
export type TranscriptRecoveryCancelReason =
  | "user-takeover"
  | "surface-switch"
  | "superseded";

export type TranscriptScrollState = {
  mode: TranscriptScrollMode;
  atBottom: boolean;
  scrollable: boolean;
  readerIntent: boolean;
  readerIntentCanClaimTail: boolean;
  readerPhase: "inactive" | "active" | "settling" | "handoff-pending";
  /** Consecutive stable geometry frames accepted by the reader transaction.
   *  Scroll event counts are deliberately not used because native WebViews
   *  may coalesce or omit deliveries at the physical tail. */
  readerStableFrames: number;
  settleMode: "tail-follow" | "manual";
  /** Id of the in-flight recovery request; at most one at a time. */
  recoveryId: number | null;
};

export type TranscriptScrollEvent =
  | { type: "RESET" }
  | { type: "USER_SCROLL_INTENT"; canClaimTail: boolean }
  | { type: "MANUAL_READING" }
  | { type: "READER_IDLE_DEADLINE" }
  | { type: "READER_STABILITY_SAMPLE"; stable: boolean; tailEligible: boolean }
  | { type: "READER_TAIL_HANDOFF" }
  | { type: "READER_TRANSACTION_END" }
  | { type: "NATIVE_SCROLLBAR_BEGIN" }
  | { type: "NATIVE_SCROLLBAR_END"; claimTail: boolean }
  | { type: "SCROLL_DELIVERED"; atBottom: boolean; scrollable: boolean; substantial?: boolean }
  | { type: "TAIL_CONTENT_CHANGED" }
  | { type: "TAIL_SETTLE_EXHAUSTED" }
  | { type: "CONTENT_SHRANK" }
  | { type: "LAYOUT_HEIGHT_CHANGED" }
  | { type: "VIEWPORT_RESIZED" }
  | { type: "USER_RESIZE_BEGIN" }
  | { type: "USER_RESIZE_END" }
  | { type: "SELECTION_BEGIN" }
  | { type: "SELECTION_END" }
  | { type: "PROGRAMMATIC_BEGIN"; settleMode?: "tail-follow" | "manual" }
  | { type: "PROGRAMMATIC_END" }
  | { type: "JUMP_TO_BOTTOM"; behavior?: ScrollBehavior }
  | { type: "JUMP_TO_INDEX"; index: number; behavior?: "auto" | "smooth" }
  | { type: "SCROLL_TO_OFFSET"; owner: TranscriptScrollOwner; top: number; behavior?: ScrollBehavior }
  | { type: "RECOVERY_BEGIN"; id: number; settleMode?: "tail-follow" | "manual" }
  | { type: "RECOVERY_END"; id: number };

export type TranscriptScrollCommand =
  | { type: "AUTOSCROLL_TO_BOTTOM" }
  | { type: "SCROLL_TO_LAST"; behavior: "auto" | "smooth" }
  | { type: "SCROLL_TO_INDEX"; index: number; behavior: "auto" | "smooth" }
  | { type: "SCROLL_TO_OFFSET"; owner: TranscriptScrollOwner; top: number; behavior: ScrollBehavior }
  | { type: "CANCEL_RECOVERY"; id: number; reason: TranscriptRecoveryCancelReason };

export type TranscriptScrollTransition = {
  state: TranscriptScrollState;
  commands: readonly TranscriptScrollCommand[];
};

export const INITIAL_TRANSCRIPT_SCROLL_STATE: TranscriptScrollState = {
  mode: "tail-follow",
  atBottom: true,
  scrollable: false,
  readerIntent: false,
  readerIntentCanClaimTail: false,
  readerPhase: "inactive",
  readerStableFrames: 0,
  settleMode: "tail-follow",
  recoveryId: null,
};

// Fold collapse drops at least one process row (~28px). Smaller deltas are
// Virtuoso measurement jitter and must keep following the tail.
export const TRANSCRIPT_CONTENT_SHRINK_THRESHOLD_PX = 24;

// A delivery this far above the bottom is a real physical displacement (thumb
// drop, row remeasure), not bottom-adjacent jitter. Re-converging these is the
// tail writer's job even when the previous delivery was already non-bottom.
export const TRANSCRIPT_SUBSTANTIAL_DISPLACEMENT_PX = 24;

export function isSubstantialTranscriptDisplacement(distance: number): boolean {
  return distance >= TRANSCRIPT_SUBSTANTIAL_DISPLACEMENT_PX;
}

export function isTranscriptContentShrink(delta: number): boolean {
  return delta <= -TRANSCRIPT_CONTENT_SHRINK_THRESHOLD_PX;
}

export const TRANSCRIPT_READER_STABLE_FRAMES = 2;

function transition(state: TranscriptScrollState, commands: readonly TranscriptScrollCommand[] = []): TranscriptScrollTransition {
  return { state, commands };
}

/**
 * Pure product-level scroll policy. Virtuoso remains the only layout/anchor
 * owner; this reducer decides when an imperative Virtuoso command is allowed.
 *
 * Writer preemption priority (highest first): selection > user intent >
 * programmatic jumps/drags > recovery > tail-follow. A recovery request is
 * always ended by an explicit transition — any higher-priority event below
 * emits CANCEL_RECOVERY so the writer hook can run onCancel("user-takeover")
 * and report the terminal state; a recovery never exits silently (#8657).
 */
export function reduceTranscriptScroll(
  state: TranscriptScrollState,
  event: TranscriptScrollEvent,
): TranscriptScrollTransition {
  // A higher-priority event preempts an in-flight recovery: the next state
  // drops recoveryId and the transition carries the explicit cancel command.
  const preempt = (
    next: TranscriptScrollState,
    commands: readonly TranscriptScrollCommand[] = [],
    reason: TranscriptRecoveryCancelReason = "user-takeover",
  ): TranscriptScrollTransition => (
    state.recoveryId === null
      ? transition(next, commands)
      : transition({ ...next, recoveryId: null }, [...commands, { type: "CANCEL_RECOVERY", id: state.recoveryId, reason }])
  );
  switch (event.type) {
    case "RESET":
      return preempt(INITIAL_TRANSCRIPT_SCROLL_STATE, [], "surface-switch");
    case "USER_SCROLL_INTENT":
      if (!state.scrollable) {
        return preempt({ ...state, mode: "tail-follow", atBottom: true, readerIntent: false, readerIntentCanClaimTail: false, readerPhase: "inactive", readerStableFrames: 0, settleMode: "tail-follow" });
      }
      return preempt({ ...state, mode: "manual", readerIntent: true, readerIntentCanClaimTail: event.canClaimTail, readerPhase: "active", readerStableFrames: 0, settleMode: "manual" });
    case "MANUAL_READING":
      return preempt({ ...state, mode: "manual", readerIntent: false, readerIntentCanClaimTail: false, readerPhase: "inactive", readerStableFrames: 0, settleMode: "manual" });
    case "READER_IDLE_DEADLINE":
      return transition(
        state.readerIntent
          ? { ...state, readerPhase: "settling", readerStableFrames: 0 }
          : state,
      );
    case "READER_STABILITY_SAMPLE": {
      if (!state.readerIntent || state.readerPhase === "inactive") return transition(state);
      const readerStableFrames = event.stable ? state.readerStableFrames + 1 : 0;
      return transition({
        ...state,
        readerStableFrames,
        readerPhase: readerStableFrames >= TRANSCRIPT_READER_STABLE_FRAMES && event.tailEligible
          ? "handoff-pending"
          : state.readerPhase === "active" ? "active" : "settling",
      });
    }
    case "READER_TAIL_HANDOFF":
      if (
        !state.readerIntent
        || !state.readerIntentCanClaimTail
        || state.readerPhase !== "handoff-pending"
        || state.readerStableFrames < TRANSCRIPT_READER_STABLE_FRAMES
      ) return transition(state);
      return preempt({
        ...state,
        mode: "tail-follow",
        atBottom: true,
        readerIntent: false,
        readerIntentCanClaimTail: false,
        readerPhase: "inactive",
        readerStableFrames: 0,
        settleMode: "tail-follow",
      });
    case "READER_TRANSACTION_END":
      return transition(state.readerIntent || state.readerPhase !== "inactive"
        ? { ...state, readerIntent: false, readerIntentCanClaimTail: false, readerPhase: "inactive", readerStableFrames: 0 }
        : state);
    case "NATIVE_SCROLLBAR_BEGIN":
      return preempt({
        ...state,
        mode: "native-thumb",
        readerIntent: false,
        readerIntentCanClaimTail: false,
        readerPhase: "inactive",
        readerStableFrames: 0,
        settleMode: "manual",
      });
    case "NATIVE_SCROLLBAR_END":
      if (state.mode !== "native-thumb") return transition(state);
      return transition({
        ...state,
        mode: event.claimTail ? "tail-follow" : "manual",
        atBottom: event.claimTail || state.atBottom,
        readerIntent: false,
        readerIntentCanClaimTail: false,
        readerPhase: "inactive",
        readerStableFrames: 0,
        settleMode: event.claimTail ? "tail-follow" : "manual",
      });
    case "SCROLL_DELIVERED": {
      if (!event.scrollable) {
        return transition({ ...state, mode: "tail-follow", atBottom: true, scrollable: false, readerIntent: false, readerIntentCanClaimTail: false, readerPhase: "inactive", readerStableFrames: 0, settleMode: "tail-follow" });
      }
      if (state.mode === "native-thumb") {
        return transition({
          ...state,
          atBottom: event.atBottom,
          scrollable: true,
          readerIntent: false,
          readerIntentCanClaimTail: false,
          readerPhase: "inactive",
          readerStableFrames: 0,
        });
      }
      // Scroll delivery is observational only. Geometry revisions are the
      // sole lane that may re-aim tail-follow; otherwise a writer delivery can
      // recursively arm another writer while Virtuoso is still measuring.
      return transition({ ...state, atBottom: event.atBottom, scrollable: true });
    }
    case "TAIL_CONTENT_CHANGED":
    case "LAYOUT_HEIGHT_CHANGED":
    case "VIEWPORT_RESIZED":
      return transition(state, state.mode === "tail-follow" ? [{ type: "AUTOSCROLL_TO_BOTTOM" }] : []);
    case "TAIL_SETTLE_EXHAUSTED":
      return state.mode === "tail-follow"
        ? transition({ ...state, atBottom: false })
        : transition(state);
    case "CONTENT_SHRANK":
      // Auto-fold collapse shortens the transcript. Keep tail ownership but
      // let the browser's native scrollTop clamp settle the new bottom; a
      // tail re-aim here visibly tugs the viewport after the collapse.
      return transition(state);
    case "USER_RESIZE_BEGIN":
      return preempt({
        ...state,
        mode: "user-resize",
        readerIntent: false,
        readerIntentCanClaimTail: false,
        readerPhase: "inactive",
        readerStableFrames: 0,
        settleMode: state.mode === "tail-follow" ? "tail-follow" : "manual",
      });
    case "USER_RESIZE_END":
      return transition(
        state.mode === "user-resize" ? { ...state, mode: state.settleMode } : state,
        state.mode === "user-resize" && state.settleMode === "tail-follow" ? [{ type: "AUTOSCROLL_TO_BOTTOM" }] : [],
      );
    case "SELECTION_BEGIN":
      return preempt({ ...state, mode: "selection", readerIntent: false, readerIntentCanClaimTail: false, readerPhase: "inactive", readerStableFrames: 0, settleMode: "manual" });
    case "SELECTION_END":
      return transition(state.mode === "selection" ? { ...state, mode: "manual", settleMode: "manual" } : state);
    case "PROGRAMMATIC_BEGIN":
      return preempt({ ...state, mode: "restoring", readerIntent: false, readerIntentCanClaimTail: false, readerPhase: "inactive", readerStableFrames: 0, settleMode: event.settleMode ?? "manual" });
    case "PROGRAMMATIC_END":
      return transition(state.mode === "restoring" ? { ...state, mode: state.settleMode } : state);
    case "JUMP_TO_BOTTOM":
      return preempt(
        { ...state, mode: "tail-follow", atBottom: true, readerIntent: false, readerIntentCanClaimTail: false, readerPhase: "inactive", readerStableFrames: 0, settleMode: "tail-follow" },
        [{ type: "SCROLL_TO_LAST", behavior: event.behavior === "smooth" ? "smooth" : "auto" }],
      );
    case "JUMP_TO_INDEX":
      return preempt(
        { ...state, mode: "restoring", atBottom: false, readerIntent: false, readerIntentCanClaimTail: false, readerPhase: "inactive", readerStableFrames: 0, settleMode: "manual" },
        [{ type: "SCROLL_TO_INDEX", index: event.index, behavior: event.behavior ?? "auto" }],
      );
    case "SCROLL_TO_OFFSET":
      // Steady-state corrections keep the current ownership: they only re-aim
      // the offset (manual-mode viewport anchor compensation, in-row
      // block-window prepend) without flipping the mode or claiming/releasing
      // the tail, and they never preempt an in-flight recovery.
      if (event.owner === "anchor-compensation" || event.owner === "block-window-prepend") {
        return transition(state, [{ type: "SCROLL_TO_OFFSET", owner: event.owner, top: event.top, behavior: event.behavior ?? "auto" }]);
      }
      return preempt(
        event.owner === "selection-edge-scroll"
          ? { ...state, mode: "selection", readerIntent: false, readerIntentCanClaimTail: false, readerPhase: "inactive", readerStableFrames: 0, settleMode: "manual" }
          : event.owner === "jump-bottom"
            ? { ...state, mode: "tail-follow", atBottom: true, readerIntent: false, readerIntentCanClaimTail: false, readerPhase: "inactive", readerStableFrames: 0, settleMode: "tail-follow" }
            : { ...state, mode: "restoring", atBottom: false, readerIntent: false, readerIntentCanClaimTail: false, readerPhase: "inactive", readerStableFrames: 0, settleMode: "manual" },
        [{ type: "SCROLL_TO_OFFSET", owner: event.owner, top: event.top, behavior: event.behavior ?? "auto" }],
      );
    case "RECOVERY_BEGIN":
      // At most one recovery in flight: a new request supersedes the old one
      // through the same explicit cancel transition a takeover would use.
      if (state.recoveryId !== null && state.recoveryId !== event.id) {
        return transition(
          { ...state, mode: "restoring", readerIntent: false, readerIntentCanClaimTail: false, readerPhase: "inactive", readerStableFrames: 0, settleMode: event.settleMode ?? "manual", recoveryId: event.id },
          [{ type: "CANCEL_RECOVERY", id: state.recoveryId, reason: "superseded" }],
        );
      }
      return transition({ ...state, mode: "restoring", readerIntent: false, readerIntentCanClaimTail: false, readerPhase: "inactive", readerStableFrames: 0, settleMode: event.settleMode ?? "manual", recoveryId: event.id });
    case "RECOVERY_END":
      if (state.recoveryId !== event.id) return transition(state);
      return transition({
        ...state,
        recoveryId: null,
        mode: state.mode === "restoring" ? state.settleMode : state.mode,
      });
  }
}

export function isTranscriptSelectionMode(mode: TranscriptScrollMode): boolean {
  return mode === "selection";
}
