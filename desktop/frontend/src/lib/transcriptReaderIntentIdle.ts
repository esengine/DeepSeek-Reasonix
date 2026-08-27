import type { TranscriptScrollEvent, TranscriptScrollState } from "./transcriptScrollArbiter";

type MutableRef<T> = { current: T };
const READER_INTENT_IDLE_MS = 180;

/** Own the bounded idle close so a coalesced final wheel can establish its
 * second physical-bottom sample before reader-buffer geometry is released. */
export function createTranscriptReaderIntentIdle({
  scrollRef,
  stateRef,
  generationRef,
  deliverScrollRef,
  dispatch,
}: {
  scrollRef: MutableRef<HTMLDivElement | null>;
  stateRef: MutableRef<TranscriptScrollState>;
  generationRef: MutableRef<number>;
  deliverScrollRef: MutableRef<((element?: HTMLDivElement) => void) | null>;
  dispatch: (event: TranscriptScrollEvent) => void;
}) {
  let timer: number | null = null;
  let frame: number | null = null;

  const cancel = () => {
    if (timer !== null) window.clearTimeout(timer);
    if (frame !== null) cancelAnimationFrame(frame);
    timer = null;
    frame = null;
  };
  const end = () => {
    cancel();
    dispatch({ type: "READER_INTENT_ENDED" });
  };
  const arm = () => {
    cancel();
    timer = window.setTimeout(() => {
      timer = null;
      deliverScrollRef.current?.(scrollRef.current ?? undefined);
      const state = stateRef.current;
      if (
        state.mode !== "reader-gesture"
        || !state.readerIntent
        || !state.readerIntentCanClaimTail
        || state.bottomHoldCount === 0
      ) {
        end();
        return;
      }
      const generation = generationRef.current;
      const element = scrollRef.current;
      frame = requestAnimationFrame(() => {
        frame = null;
        if (generationRef.current !== generation || scrollRef.current !== element) return;
        deliverScrollRef.current?.(element ?? undefined);
        if (stateRef.current.readerIntent) end();
      });
    }, READER_INTENT_IDLE_MS);
  };
  return [end, arm, cancel] as const;
}
