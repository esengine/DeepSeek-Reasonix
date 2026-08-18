import { useCallback, useEffect, useState } from "react";

import { prefersReducedMotion } from "./motion";

const TERMINAL_TRANSITION_MS = 220;

// Load xterm lazily, then keep it mounted so closing the drawer does not lose
// its live canvas. Fit is paused while the grid row animates because fitting
// every intermediate height also emits redundant PTY resize traffic.
export function useWarmTerminalPanel(open: boolean, resizing: boolean): {
  mounted: boolean;
  fitEnabled: boolean;
  prefetch: () => void;
} {
  const [mounted, setMounted] = useState(open);
  const [fitEnabled, setFitEnabled] = useState(false);
  useEffect(() => {
    if (open) setMounted(true);
  }, [open]);
  useEffect(() => {
    if (!open) {
      setFitEnabled(false);
      return;
    }
    if (resizing) {
      setFitEnabled(true);
      return;
    }
    const delay = prefersReducedMotion() ? 0 : TERMINAL_TRANSITION_MS;
    const timer = window.setTimeout(() => setFitEnabled(true), delay);
    return () => window.clearTimeout(timer);
  }, [open, resizing]);
  const prefetch = useCallback(() => {
    void import("../components/TerminalPanel");
  }, []);
  return { mounted, fitEnabled, prefetch };
}
