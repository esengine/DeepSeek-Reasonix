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

export type TranscriptScrollState = {
  mode: TranscriptScrollMode;
  atBottom: boolean;
  scrollable: boolean;
  settleMode: "tail-follow" | "manual";
};

export type TranscriptScrollEvent =
  | { type: "RESET" }
  | { type: "USER_SCROLL_INTENT" }
  | { type: "AT_BOTTOM_CHANGED"; atBottom: boolean; scrollable: boolean }
  | { type: "TAIL_CONTENT_CHANGED" }
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
  | { type: "SCROLL_TO_OFFSET"; owner: TranscriptScrollOwner; top: number; behavior?: ScrollBehavior };

export type TranscriptScrollCommand =
  | { type: "AUTOSCROLL_TO_BOTTOM" }
  | { type: "SCROLL_TO_LAST"; behavior: "auto" | "smooth" }
  | { type: "SCROLL_TO_INDEX"; index: number; behavior: "auto" | "smooth" }
  | { type: "SCROLL_TO_OFFSET"; owner: TranscriptScrollOwner; top: number; behavior: ScrollBehavior };

export type TranscriptScrollTransition = {
  state: TranscriptScrollState;
  commands: readonly TranscriptScrollCommand[];
};

export const INITIAL_TRANSCRIPT_SCROLL_STATE: TranscriptScrollState = {
  mode: "tail-follow",
  atBottom: true,
  scrollable: false,
  settleMode: "tail-follow",
};

function transition(state: TranscriptScrollState, commands: readonly TranscriptScrollCommand[] = []): TranscriptScrollTransition {
  return { state, commands };
}

/**
 * Pure product-level scroll policy. Virtuoso remains the only layout/anchor
 * owner; this reducer decides when an imperative Virtuoso command is allowed.
 */
export function reduceTranscriptScroll(
  state: TranscriptScrollState,
  event: TranscriptScrollEvent,
): TranscriptScrollTransition {
  switch (event.type) {
    case "RESET":
      return transition(INITIAL_TRANSCRIPT_SCROLL_STATE);
    case "USER_SCROLL_INTENT":
      if (!state.scrollable) {
        return transition({ ...state, mode: "tail-follow", atBottom: true, settleMode: "tail-follow" });
      }
      return transition({ ...state, mode: "manual", atBottom: false, settleMode: "manual" });
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
    case "USER_RESIZE_BEGIN":
      return transition({ ...state, mode: "user-resize", settleMode: "manual" });
    case "USER_RESIZE_END":
      return transition(state.mode === "user-resize" ? { ...state, mode: "manual", settleMode: "manual" } : state);
    case "SELECTION_BEGIN":
      return transition({ ...state, mode: "selection", settleMode: "manual" });
    case "SELECTION_END":
      return transition(state.mode === "selection" ? { ...state, mode: "manual", settleMode: "manual" } : state);
    case "PROGRAMMATIC_BEGIN":
      return transition({ ...state, mode: "restoring", settleMode: event.settleMode ?? "manual" });
    case "PROGRAMMATIC_END":
      return transition(state.mode === "restoring" ? { ...state, mode: state.settleMode } : state);
    case "JUMP_TO_BOTTOM":
      return transition(
        { ...state, mode: "tail-follow", atBottom: true, settleMode: "tail-follow" },
        [{ type: "SCROLL_TO_LAST", behavior: event.behavior === "smooth" ? "smooth" : "auto" }],
      );
    case "JUMP_TO_INDEX":
      return transition(
        { ...state, mode: "restoring", atBottom: false, settleMode: "manual" },
        [{ type: "SCROLL_TO_INDEX", index: event.index, behavior: event.behavior ?? "auto" }],
      );
    case "SCROLL_TO_OFFSET":
      return transition(
        event.owner === "selection-edge-scroll"
          ? { ...state, mode: "selection", settleMode: "manual" }
          : event.owner === "jump-bottom"
            ? { ...state, mode: "tail-follow", atBottom: true, settleMode: "tail-follow" }
            : { ...state, mode: "restoring", atBottom: false, settleMode: "manual" },
        [{ type: "SCROLL_TO_OFFSET", owner: event.owner, top: event.top, behavior: event.behavior ?? "auto" }],
      );
  }
}

export function isTranscriptSelectionMode(mode: TranscriptScrollMode): boolean {
  return mode === "selection";
}
