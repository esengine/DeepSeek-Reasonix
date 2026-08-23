import type {
  TranscriptScrollCommand,
  TranscriptScrollEvent,
  TranscriptScrollState,
} from "./transcriptScrollArbiter";
import { isFrontendDiagnosticsBuild } from "./frontendDiagnosticsBuild";
import { recordTranscriptScrollDiagnostic } from "./transcriptScrollProbe";

export const CAPTURE_TRANSCRIPT_SCROLL_DIAGNOSTICS = isFrontendDiagnosticsBuild(
  typeof __BUILD_CHANNEL__ === "string" ? __BUILD_CHANNEL__ : "development",
  Boolean(import.meta.env?.DEV),
);

export type TranscriptScrollDiagnosticSource =
  | "reset"
  | "user-scroll-intent"
  | "manual-reading"
  | "reader-intent-ended"
  | "scroll-delivered"
  | "tail-content-changed"
  | "content-shrank"
  | "layout-height-changed"
  | "viewport-resized"
  | "user-resize-begin"
  | "user-resize-end"
  | "selection-begin"
  | "selection-end"
  | "programmatic-begin"
  | "programmatic-end"
  | "jump-bottom"
  | "jump-index"
  | "scroll-offset"
  | "recovery-begin"
  | "recovery-end"
  | "native-scrollbar-release";

export type TranscriptTailWriteDiagnostic = {
  source: TranscriptScrollDiagnosticSource;
  phase: "initial" | "settle";
  settle?: { frame: number; offBottomFrames: number; stagnantFrames: number };
};

function sourceForEvent(event: TranscriptScrollEvent["type"]): TranscriptScrollDiagnosticSource {
  switch (event) {
    case "RESET": return "reset";
    case "USER_SCROLL_INTENT": return "user-scroll-intent";
    case "MANUAL_READING": return "manual-reading";
    case "READER_INTENT_ENDED": return "reader-intent-ended";
    case "SCROLL_DELIVERED": return "scroll-delivered";
    case "TAIL_CONTENT_CHANGED": return "tail-content-changed";
    case "CONTENT_SHRANK": return "content-shrank";
    case "LAYOUT_HEIGHT_CHANGED": return "layout-height-changed";
    case "VIEWPORT_RESIZED": return "viewport-resized";
    case "USER_RESIZE_BEGIN": return "user-resize-begin";
    case "USER_RESIZE_END": return "user-resize-end";
    case "SELECTION_BEGIN": return "selection-begin";
    case "SELECTION_END": return "selection-end";
    case "PROGRAMMATIC_BEGIN": return "programmatic-begin";
    case "PROGRAMMATIC_END": return "programmatic-end";
    case "JUMP_TO_BOTTOM": return "jump-bottom";
    case "JUMP_TO_INDEX": return "jump-index";
    case "SCROLL_TO_OFFSET": return "scroll-offset";
    case "RECOVERY_BEGIN": return "recovery-begin";
    case "RECOVERY_END": return "recovery-end";
  }
}

export function recordTranscriptScrollTransition(
  event: TranscriptScrollEvent,
  previousState: TranscriptScrollState,
  nextState: TranscriptScrollState,
  commands: readonly TranscriptScrollCommand[],
  element: { scrollTop: number; scrollHeight: number; clientHeight: number } | null,
): TranscriptScrollDiagnosticSource | undefined {
  if (!CAPTURE_TRANSCRIPT_SCROLL_DIAGNOSTICS) return undefined;
  const source = sourceForEvent(event.type);
  const tailCommand = commands.some((command) => command.type === "AUTOSCROLL_TO_BOTTOM" || command.type === "SCROLL_TO_LAST");
  const stateChanged = previousState.mode !== nextState.mode
    || previousState.atBottom !== nextState.atBottom
    || previousState.scrollable !== nextState.scrollable
    || previousState.readerIntent !== nextState.readerIntent
    || previousState.readerIntentCanClaimTail !== nextState.readerIntentCanClaimTail
    || previousState.recoveryId !== nextState.recoveryId;
  if (stateChanged || tailCommand || event.type === "CONTENT_SHRANK" || event.type === "LAYOUT_HEIGHT_CHANGED") {
    recordTranscriptScrollDiagnostic("scroll-state", {
      source,
      previousMode: previousState.mode,
      mode: nextState.mode,
      atBottom: nextState.atBottom,
      scrollable: nextState.scrollable,
      readerIntent: nextState.readerIntent,
      canClaimTail: nextState.readerIntentCanClaimTail,
      substantial: event.type === "SCROLL_DELIVERED" ? event.substantial === true : undefined,
      tailCommand,
      scrollTop: element?.scrollTop,
      scrollHeight: element?.scrollHeight,
      clientHeight: element?.clientHeight,
      bottomDistance: element ? element.scrollHeight - element.scrollTop - element.clientHeight : undefined,
    });
  }
  return source;
}
