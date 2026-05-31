import type { WriteStream } from "node:fs";
import { type MutableRefObject, useCallback, useEffect, useRef } from "react";
import { stopAndSaveCpuProfile } from "../../cpu-prof.js";

export interface UseQuitOptions {
  beforeQuit?: () => Promise<void> | void;
}

/** Ctrl+C / SIGINT → flush transcript + (if profiling) save .cpuprofile, then `process.exit(0)`. We call `process.exit` directly rather than Ink's `exit()` because the singleton stdin reader keeps a `data` listener attached — `exit()` would unmount the React tree but leave the event loop alive and the terminal would hang. */
export function useQuit(
  transcriptRef: MutableRefObject<WriteStream | null>,
  opts?: UseQuitOptions,
): () => void {
  const quittingRef = useRef(false);

  const quitProcess = useCallback(() => {
    if (quittingRef.current) return;
    quittingRef.current = true;
    transcriptRef.current?.end();
    void (async () => {
      try {
        await opts?.beforeQuit?.();
      } catch {
        // beforeQuit must not block or crash the exit path
      }
      await stopAndSaveCpuProfile();
      process.exit(0);
    })();
  }, [transcriptRef, opts?.beforeQuit]);

  useEffect(() => {
    process.on("SIGINT", quitProcess);
    return () => {
      process.off("SIGINT", quitProcess);
    };
  }, [quitProcess]);

  return quitProcess;
}
