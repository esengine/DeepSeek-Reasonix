import type { WriteStream } from "node:fs";
import { type MutableRefObject, useCallback, useEffect } from "react";
import { stopAndSaveCpuProfile } from "../../cpu-prof.js";

/** Ctrl+C / SIGINT → flush transcript + (if profiling) save .cpuprofile, then `process.exit(0)`. We call `process.exit` directly rather than Ink's `exit()` because the singleton stdin reader keeps a `data` listener attached — `exit()` would unmount the React tree but leave the event loop alive and the terminal would hang. */
export function useQuit(transcriptRef: MutableRefObject<WriteStream | null>): () => void {
  const quitProcess = useCallback(() => {
    transcriptRef.current?.end();
    void (async () => {
      await stopAndSaveCpuProfile();
      process.exit(0);
    })();
  }, [transcriptRef]);

  useEffect(() => {
    process.on("SIGINT", quitProcess);
    return () => {
      process.off("SIGINT", quitProcess);
    };
  }, [quitProcess]);

  return quitProcess;
}
