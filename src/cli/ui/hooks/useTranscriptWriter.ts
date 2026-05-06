import type { WriteStream } from "node:fs";
import { type MutableRefObject, useCallback } from "react";
import type { LoopEvent } from "../../../loop.js";
import { recordFromLoopEvent, writeRecord } from "../../../transcript/log.js";

/** Returns a `LoopEvent` writer that no-ops when no transcript was opened. Wraps `recordFromLoopEvent` + `writeRecord` so callers don't carry the model/prefix metadata. */
export function useTranscriptWriter(
  transcriptRef: MutableRefObject<WriteStream | null>,
  model: string,
  prefixHash: string,
): (ev: LoopEvent) => void {
  return useCallback(
    (ev: LoopEvent) => {
      const stream = transcriptRef.current;
      if (!stream) return;
      writeRecord(stream, recordFromLoopEvent(ev, { model, prefixHash }));
    },
    [transcriptRef, model, prefixHash],
  );
}
