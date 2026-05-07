import { useAnimation } from "ink";
import React, { type ReactNode, createContext, useContext, useState } from "react";

/**
 * Two-tier global heartbeat backed by Ink 7's `useAnimation`. The
 * provider only stores an `isActive` boolean; the actual frame timer
 * lives inside Ink and consolidates with every other useAnimation
 * caller into a single shared interval.
 *
 *   - FAST_TICK_MS (120ms) — spinners, glyph pulses, anything that
 *     visibly animates frame-by-frame.
 *   - SLOW_TICK_MS (1000ms) — elapsed-seconds counters, expiry
 *     countdowns, polling pollers. Don't need 8Hz re-renders.
 *
 * Setting `disabled` flips `isActive` to `false`, which Ink propagates
 * to every active animation. Repaints stop entirely until isActive
 * flips back, at which point Ink resets the frame counter to 0 (so
 * spinners restart from frame 0 — visually identical to a fresh mount).
 */
export const FAST_TICK_MS = 120;
export const SLOW_TICK_MS = 1000;
/** @deprecated kept for callers that import the old name. */
export const TICK_MS = FAST_TICK_MS;

const TickerActiveContext = createContext(true);

export interface TickerProviderProps {
  children: ReactNode;
  /**
   * When true, every tick-driven animation pauses. Used by PLAIN_UI
   * mode, modal overlays, and the idle-gate so a quiescent TUI is
   * byte-stable (cursor blink and gradient pulses don't re-render).
   */
  disabled?: boolean;
}

export function TickerProvider({ children, disabled }: TickerProviderProps) {
  return <TickerActiveContext.Provider value={!disabled}>{children}</TickerActiveContext.Provider>;
}

function useTickerActive(): boolean {
  return useContext(TickerActiveContext);
}

/**
 * Fast tick — re-renders the calling component every FAST_TICK_MS
 * (120ms). Use for spinner frames, glyph pulses, anything that
 * visibly animates frame-by-frame.
 */
export function useTick(): number {
  const isActive = useTickerActive();
  return useAnimation({ interval: FAST_TICK_MS, isActive }).frame;
}

/**
 * Slow tick — re-renders the calling component every SLOW_TICK_MS
 * (1000ms). Use for elapsed-seconds counters, expiry countdowns,
 * or pollers that just need a "what's the time NOW?" trigger once
 * per second.
 */
export function useSlowTick(): number {
  const isActive = useTickerActive();
  return useAnimation({ interval: SLOW_TICK_MS, isActive }).frame;
}

/** Seconds elapsed since mount. Re-renders at 1Hz via the slow tick. */
export function useElapsedSeconds(): number {
  const [start] = useState(() => Date.now());
  useSlowTick();
  return Math.floor((Date.now() - start) / 1000);
}
