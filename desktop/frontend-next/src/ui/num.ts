import { useEffect, useRef, useState } from "react";
import { WINDOW_MS, tokensPerSecond, type Sample } from "../port/tokens";

// Metric values are drawn as they arrived. Easing one from its old reading to
// its new one puts numbers on screen that no request ever produced, and nothing
// on screen says which of them were real — so what moves here is geometry and
// emphasis, never a figure a reader would take for a measurement.

// One sample a second while the turn runs, capped at `span`. A rate is the one
// figure on the rail whose shape says more than its current value — whether it
// is climbing, stalling or ragged — and a single number cannot hold that. The
// timer stops when the turn does: an idle rail must not tick.
export function useTrail(value: number, on: boolean, span = 60): number[] {
  const [trail, setTrail] = useState<number[]>([]);
  const cur = useRef(value);
  cur.current = value;
  useEffect(() => {
    if (!on) return;
    const id = setInterval(() => setTrail((t) => [...t, cur.current].slice(-span)), 1000);
    return () => clearInterval(id);
  }, [on, span]);
  return trail;
}

// A rate is an observation of what arrived, so only an arrival may make one.
// Wall time gets one power over it and no other: to end it. Between arrivals
// the last observation stands, and at the window's edge it goes to zero in a
// single step rather than sliding there through readings nothing produced.
export function useRate(win: Sample[], on: boolean): number {
  const [rate, setRate] = useState(0);
  useEffect(() => {
    const last = on ? win[win.length - 1] : undefined;
    if (!last) {
      setRate(0);
      return;
    }
    setRate(tokensPerSecond(win, Date.now()));
    const id = setTimeout(() => setRate(0), Math.max(0, last.t + WINDOW_MS - Date.now()));
    return () => clearTimeout(id);
  }, [win, on]);
  return rate;
}
