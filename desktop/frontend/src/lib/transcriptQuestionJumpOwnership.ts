import type { TranscriptScrollEvent } from "./transcriptScrollArbiter";

/** Token-fenced ownership for one masked question paging/landing transaction. */
export function createTranscriptQuestionJumpOwnership({
  invalidateAsyncFrames,
  endReaderIntent,
  dispatch,
}: {
  invalidateAsyncFrames: () => void;
  endReaderIntent: () => void;
  dispatch: (event: TranscriptScrollEvent) => unknown;
}) {
  let token: number | null = null;
  return {
    begin(nextToken: number) {
      invalidateAsyncFrames();
      endReaderIntent();
      token = nextToken;
      dispatch({ type: "PROGRAMMATIC_BEGIN", settleMode: "manual" });
    },
    finish(completedToken: number) {
      if (token !== completedToken) return false;
      token = null;
      dispatch({ type: "PROGRAMMATIC_END" });
      endReaderIntent();
      return true;
    },
    reset() { token = null; },
    blocksGenericFinish() { return token !== null; },
  };
}
