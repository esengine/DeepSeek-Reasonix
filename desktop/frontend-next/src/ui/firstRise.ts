import { useEffect, useState } from "react";

const WINDOW_MS = 2500;
let until = 0;

// Set when first-run setup hands over to the conversation: for a moment after,
// the composer rises into place and types its prompt, once.
export const startRise = () => { until = Date.now() + WINDOW_MS; };
export const isRising = () => Date.now() < until;

const calm = () => typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;

// The text typed out over 1.2 seconds right after setup, and whole at any other
// time. Typing into the box, or reduced motion, shows it whole at once.
export function useTypedPlaceholder(full: string, typing: boolean): string {
  const [active] = useState(() => isRising() && !calm());
  const [shown, setShown] = useState(active ? 0 : full.length);
  useEffect(() => {
    if (!active || typing) return;
    const step = Math.max(20, 1200 / Math.max(1, full.length));
    const timer = window.setInterval(() => setShown((n) => (n >= full.length ? n : n + 1)), step);
    return () => window.clearInterval(timer);
  }, [active, typing, full]);
  return !active || typing || shown >= full.length ? full : full.slice(0, shown);
}
