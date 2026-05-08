import React, { type ReactNode, createContext, useContext, useEffect, useState } from "react";

export const FAST_TICK_MS = 120;
export const SLOW_TICK_MS = 1000;
export const TICK_MS = FAST_TICK_MS;

const TickerActiveContext = createContext(true);

export interface TickerProviderProps {
  children: ReactNode;
  disabled?: boolean;
}

export function TickerProvider({ children, disabled }: TickerProviderProps) {
  return <TickerActiveContext.Provider value={!disabled}>{children}</TickerActiveContext.Provider>;
}

function useTickerActive(): boolean {
  return useContext(TickerActiveContext);
}

function useTicker(interval: number): number {
  const isActive = useTickerActive();
  const [frame, setFrame] = useState(0);

  useEffect(() => {
    if (!isActive) return;
    setFrame(0);
    const id = setInterval(() => {
      setFrame((current) => current + 1);
    }, interval);
    return () => clearInterval(id);
  }, [interval, isActive]);

  return frame;
}

export function useTick(): number {
  return useTicker(FAST_TICK_MS);
}

export function useSlowTick(): number {
  return useTicker(SLOW_TICK_MS);
}

export function useElapsedSeconds(): number {
  const [start] = useState(() => Date.now());
  useSlowTick();
  return Math.floor((Date.now() - start) / 1000);
}
