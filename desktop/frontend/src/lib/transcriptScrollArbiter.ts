export type TranscriptScrollMode =
  | "tail-follow"
  | "manual"
  | "user-resize"
  | "selection"
  | "restoring";

export type TranscriptScrollOwner =
  | "jump"
  | "rewind"
  | "jump-bottom"
  | "custom-scrollbar"
  | "selection-edge-scroll";

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
  settleMode: "tail-follow" | "manual";
  /** Id of the in-flight recovery request; at most one at a time. */
  recoveryId: number | null;
};

export type TranscriptScrollEvent =
  | { type: "RESET" }
  | { type: "USER_SCROLL_INTENT" }
  | { type: "AT_BOTTOM_CHANGED"; atBottom: boolean; scrollable: boolean }
  | { type: "TAIL_CONTENT_CHANGED" }
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
  settleMode: "tail-follow",
  recoveryId: null,
};

// Fold collapse drops at least one process row (~28px). Smaller deltas are
// Virtuoso measurement jitter and must keep following the tail.
export const TRANSCRIPT_CONTENT_SHRINK_THRESHOLD_PX = 24;

export function isTranscriptContentShrink(delta: number): boolean {
  return delta <= -TRANSCRIPT_CONTENT_SHRINK_THRESHOLD_PX;
}

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
        return preempt({ ...state, mode: "tail-follow", atBottom: true, settleMode: "tail-follow" });
      }
      return preempt({ ...state, mode: "manual", atBottom: false, settleMode: "manual" });
    case "AT_BOTTOM_CHANGED": {
      if (!event.scrollable) {
        return transition({ ...state, mode: "tail-follow", atBottom: true, scrollable: false, settleMode: "tail-follow" });
      }
      const next = { ...state, atBottom: event.atBottom, scrollable: true };
      if (event.atBottom && state.mode !== "selection" && state.mode !== "user-resize" && state.mode !== "restoring") {
        next.mode = "tail-follow";
        next.settleMode = "tail-follow";
      }
      // `false` is only a physical report. Dynamic measurement and viewport
      // changes can produce it, so it never revokes tail ownership by itself.
      return transition(next);
    }
    case "TAIL_CONTENT_CHANGED":
    case "LAYOUT_HEIGHT_CHANGED":
    case "VIEWPORT_RESIZED":
      return transition(state, state.mode === "tail-follow" ? [{ type: "AUTOSCROLL_TO_BOTTOM" }] : []);
    case "CONTENT_SHRANK":
      // Auto-fold collapse shortens the transcript. Keep tail ownership but
      // let the browser's native scrollTop clamp settle the new bottom; a
      // tail re-aim here visibly tugs the viewport after the collapse.
      return transition(state);
    case "USER_RESIZE_BEGIN":
      return preempt({ ...state, mode: "user-resize", settleMode: "manual" });
    case "USER_RESIZE_END":
      return transition(state.mode === "user-resize" ? { ...state, mode: "manual", settleMode: "manual" } : state);
    case "SELECTION_BEGIN":
      return preempt({ ...state, mode: "selection", settleMode: "manual" });
    case "SELECTION_END":
      return transition(state.mode === "selection" ? { ...state, mode: "manual", settleMode: "manual" } : state);
    case "PROGRAMMATIC_BEGIN":
      return preempt({ ...state, mode: "restoring", settleMode: event.settleMode ?? "manual" });
    case "PROGRAMMATIC_END":
      return transition(state.mode === "restoring" ? { ...state, mode: state.settleMode } : state);
    case "JUMP_TO_BOTTOM":
      return preempt(
        { ...state, mode: "tail-follow", atBottom: true, settleMode: "tail-follow" },
        [{ type: "SCROLL_TO_LAST", behavior: event.behavior === "smooth" ? "smooth" : "auto" }],
      );
    case "JUMP_TO_INDEX":
      return preempt(
        { ...state, mode: "restoring", atBottom: false, settleMode: "manual" },
        [{ type: "SCROLL_TO_INDEX", index: event.index, behavior: event.behavior ?? "auto" }],
      );
    case "SCROLL_TO_OFFSET":
      return preempt(
        event.owner === "selection-edge-scroll"
          ? { ...state, mode: "selection", settleMode: "manual" }
          : event.owner === "jump-bottom"
            ? { ...state, mode: "tail-follow", atBottom: true, settleMode: "tail-follow" }
            : { ...state, mode: "restoring", atBottom: false, settleMode: "manual" },
        [{ type: "SCROLL_TO_OFFSET", owner: event.owner, top: event.top, behavior: event.behavior ?? "auto" }],
      );
    case "RECOVERY_BEGIN":
      // At most one recovery in flight: a new request supersedes the old one
      // through the same explicit cancel transition a takeover would use.
      if (state.recoveryId !== null && state.recoveryId !== event.id) {
        return transition(
          { ...state, mode: "restoring", settleMode: event.settleMode ?? "manual", recoveryId: event.id },
          [{ type: "CANCEL_RECOVERY", id: state.recoveryId, reason: "superseded" }],
        );
      }
      return transition({ ...state, mode: "restoring", settleMode: event.settleMode ?? "manual", recoveryId: event.id });
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
