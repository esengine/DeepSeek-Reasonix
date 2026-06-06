import React, { createContext, type PropsWithChildren, useEffect, useState } from 'react';
import { FRAME_INTERVAL_MS } from '../constants.js';
import { useTerminalFocus } from '../hooks/use-terminal-focus.js';

export type Clock = {
  subscribe: (onChange: () => void, keepAlive: boolean) => () => void;
  now: () => number;
  setTickInterval: (ms: number) => void;
};

/** Build a shared animation clock. */
export function createClock(tickIntervalMs: number): Clock {
  const subscribers = new Map<() => void, boolean>();
  let interval: ReturnType<typeof setInterval> | null = null;
  let currentTickIntervalMs = tickIntervalMs;
  let startTime = 0;
  // Snapshot of the current tick time. All subscribers run with the same
  // value, so two animations that started in the same frame stay in sync
  // even if one of them takes longer to react.
  let tickTime = 0;

  function tick(): void {
    tickTime = Date.now() - startTime;
    for (const onChange of subscribers.keys()) {
      onChange();
    }
  }

  function updateInterval(): void {
    let anyKeepAlive = false;
    for (const keepAlive of subscribers.values()) {
      if (keepAlive) {
        anyKeepAlive = true;
        break;
      }
    }

    if (anyKeepAlive) {
      if (interval) {
        clearInterval(interval);
        interval = null;
      }
      if (startTime === 0) startTime = Date.now();
      interval = setInterval(tick, currentTickIntervalMs);
    } else if (interval) {
      clearInterval(interval);
      interval = null;
    }
  }

  return {
    subscribe(onChange, keepAlive) {
      subscribers.set(onChange, keepAlive);
      updateInterval();
      return () => {
        subscribers.delete(onChange);
        updateInterval();
      };
    },
    now() {
      if (startTime === 0) startTime = Date.now();
      // Live interval → return the per-tick snapshot so concurrent
      // subscribers stay coherent. Paused → return real time, because
      // `tickTime` is stuck at whatever the last burst left it at.
      if (interval && tickTime) return tickTime;
      return Date.now() - startTime;
    },
    setTickInterval(ms) {
      if (ms === currentTickIntervalMs) return;
      currentTickIntervalMs = ms;
      updateInterval();
    },
  };
}

/** Carries the active `Clock` instance to descendants. */
export const ClockContext = createContext<Clock | null>(null);

/** While the terminal window is unfocused, drop the tick rate in half. */
const BLURRED_TICK_INTERVAL_MS = FRAME_INTERVAL_MS * 2;

const initialClock = () => createClock(FRAME_INTERVAL_MS);

/** Owns one `Clock` for the whole app and re-rates it on focus changes. */
export function ClockProvider({ children }: PropsWithChildren) {
  const [clock] = useState(initialClock);
  const focused = useTerminalFocus();

  useEffect(() => {
    clock.setTickInterval(focused ? FRAME_INTERVAL_MS : BLURRED_TICK_INTERVAL_MS);
  }, [clock, focused]);

  return <ClockContext.Provider value={clock}>{children}</ClockContext.Provider>;
}
