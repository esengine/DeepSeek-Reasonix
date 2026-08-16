/**
 * Observability probe for every imperative scroll write against the
 * transcript Virtuoso handle. The single-writer contract (only the scroll
 * arbiter may call `virtuosoRef.current.scroll*`) is enforced statically by
 * scripts/check-single-scroll-writer.mjs; this probe is the runtime mirror:
 * tests and diagnostics can observe who wrote, what kind of write, and where
 * it landed, without intercepting the DOM.
 */
export type TranscriptScrollWriteRecord = {
  /** Logical writer, e.g. "tail-follow", "jump", "recovery", or a
   *  TranscriptScrollOwner such as "selection-edge-scroll". */
  owner: string;
  kind: "scrollTo" | "scrollBy" | "scrollToIndex";
  top?: number;
  index?: number | "LAST";
};

declare global {
  interface Window {
    __REASONIX_TRANSCRIPT_SCROLL_WRITE__?: (write: TranscriptScrollWriteRecord) => void;
  }
}

export function noteTranscriptScrollWrite(write: TranscriptScrollWriteRecord): void {
  window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__?.(write);
}
