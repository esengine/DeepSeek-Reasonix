import { type MutableRefObject, useCallback, useEffect, useRef, useState } from "react";
import type { Scrollback } from "./useScrollback.js";

interface ActiveLoop {
  prompt: string;
  intervalMs: number;
  nextFireAt: number;
  iter: number;
}

export interface ActiveLoopSnapshot {
  prompt: string;
  intervalMs: number;
  iter: number;
  nextFireMs: number;
}

export interface UseLoopModeResult {
  startLoop: (intervalMs: number, prompt: string) => void;
  stopLoop: () => void;
  /** Snapshot for the `/loop` (no-arg) status branch. Returns null when no loop is active. */
  getLoopStatus: () => ActiveLoopSnapshot | null;
  /** Cheap predicate — used by handleSubmit's cancel-on-user-input guard and by apply-slash-result. */
  isLoopActive: () => boolean;
  /** True only during the timer's `handleSubmit` invocation — tells handleSubmit's cancel guard to skip itself so the loop's own re-submit doesn't kill the loop. */
  isLoopFiring: () => boolean;
  /** Reset by handleSubmit at the top of every call so the firing flag is one-shot. */
  clearFiringFlag: () => void;
  /** Reactive state for the LoopStatusRow render — null when no loop is active. */
  activeLoop: ActiveLoop | null;
}

export interface UseLoopModeOptions {
  log: Scrollback;
  busyRef: MutableRefObject<boolean>;
  /** Forward-ref to the latest `handleSubmit` — the closure shifts as state changes, so the timer dereferences fresh on each fire. */
  handleSubmitRef: MutableRefObject<((raw: string) => Promise<void>) | null>;
}

/** Owns the active /loop config + its setTimeout-based scheduler. Re-issuing /loop replaces the slot; cancellation is centralized in stopLoop. */
export function useLoopMode(opts: UseLoopModeOptions): UseLoopModeResult {
  const { log, busyRef, handleSubmitRef } = opts;
  const [activeLoop, setActiveLoop] = useState<ActiveLoop | null>(null);
  const activeLoopRef = useRef<ActiveLoop | null>(null);
  const loopTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const loopFiringRef = useRef<boolean>(false);

  useEffect(() => {
    activeLoopRef.current = activeLoop;
  }, [activeLoop]);

  const stopLoop = useCallback(() => {
    if (loopTimerRef.current) {
      clearTimeout(loopTimerRef.current);
      loopTimerRef.current = null;
    }
    const cur = activeLoopRef.current;
    if (!cur) return;
    setActiveLoop(null);
    log.pushInfo(`▸ loop stopped (after ${cur.iter} iter${cur.iter === 1 ? "" : "s"}).`);
  }, [log]);

  const startLoop = useCallback((intervalMs: number, prompt: string) => {
    if (loopTimerRef.current) {
      clearTimeout(loopTimerRef.current);
      loopTimerRef.current = null;
    }
    setActiveLoop({
      prompt,
      intervalMs,
      nextFireAt: Date.now() + intervalMs,
      iter: 0,
    });
  }, []);

  const getLoopStatus = useCallback((): ActiveLoopSnapshot | null => {
    const cur = activeLoopRef.current;
    if (!cur) return null;
    return {
      prompt: cur.prompt,
      intervalMs: cur.intervalMs,
      iter: cur.iter,
      nextFireMs: Math.max(0, cur.nextFireAt - Date.now()),
    };
  }, []);

  const isLoopActive = useCallback(() => activeLoopRef.current !== null, []);
  const isLoopFiring = useCallback(() => loopFiringRef.current, []);
  const clearFiringFlag = useCallback(() => {
    loopFiringRef.current = false;
  }, []);

  // /loop scheduler. Re-runs whenever activeLoop's `nextFireAt` shifts —
  // either because startLoop set a fresh schedule or because a previous
  // firing bumped the next-fire time. Cleanup clears the in-flight
  // timer so a stopLoop / replacement doesn't leak a fire after cancel.
  useEffect(() => {
    if (!activeLoop) return;
    const delay = Math.max(0, activeLoop.nextFireAt - Date.now());
    const timer = setTimeout(async () => {
      loopTimerRef.current = null;
      // Skip the firing entirely when a prior turn is still running.
      // Re-arm in 1s so the loop catches up the moment busy clears,
      // rather than waiting a full interval after a slow turn.
      if (busyRef.current) {
        setActiveLoop((cur) => (cur ? { ...cur, nextFireAt: Date.now() + 1000 } : cur));
        return;
      }
      const cur = activeLoopRef.current;
      if (!cur) return;
      const nextIter = cur.iter + 1;
      // Schedule the NEXT firing now (independent of how long this turn
      // takes). Keeps the cadence honest even when individual turns run
      // long.
      setActiveLoop((c) =>
        c ? { ...c, iter: nextIter, nextFireAt: Date.now() + cur.intervalMs } : c,
      );
      log.pushInfo(`▸ /loop iter ${nextIter} → ${cur.prompt}`);
      loopFiringRef.current = true;
      try {
        await handleSubmitRef.current?.(cur.prompt);
      } catch {
        // Persistent submission errors → kill the loop rather than spam
        // the screen. User can re-issue /loop once they fix the cause.
        stopLoop();
      } finally {
        loopFiringRef.current = false;
      }
    }, delay);
    loopTimerRef.current = timer;
    return () => clearTimeout(timer);
  }, [activeLoop, stopLoop, log, busyRef, handleSubmitRef]);

  return {
    startLoop,
    stopLoop,
    getLoopStatus,
    isLoopActive,
    isLoopFiring,
    clearFiringFlag,
    activeLoop,
  };
}
