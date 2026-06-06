/** Render frequency + duration counters, gated by `REASONIX_TRACE_RENDERS` so prod paths stay free. */

import React from "react";

interface Stats {
  count: number;
  totalMs: number;
  maxMs: number;
  lastReportAt: number;
  lastReportCount: number;
}

const REPORT_INTERVAL_MS = 1000;
const enabled = ((): boolean => {
  const v = process.env.REASONIX_TRACE_RENDERS;
  return v === "1" || v === "true" || v === "yes";
})();

const stats = new Map<string, Stats>();

function recordRender(name: string, ms: number): void {
  let s = stats.get(name);
  if (!s) {
    s = { count: 0, totalMs: 0, maxMs: 0, lastReportAt: Date.now(), lastReportCount: 0 };
    stats.set(name, s);
  }
  s.count++;
  s.totalMs += ms;
  if (ms > s.maxMs) s.maxMs = ms;
  const now = Date.now();
  if (now - s.lastReportAt >= REPORT_INTERVAL_MS) {
    const windowCount = s.count - s.lastReportCount;
    const elapsedS = (now - s.lastReportAt) / 1000;
    const ratePerSec = windowCount / elapsedS;
    process.stderr.write(
      `[render-trace] ${name.padEnd(20)} ${windowCount.toString().padStart(4)} renders · ${ratePerSec.toFixed(1)}/s · avg ${(s.totalMs / s.count).toFixed(2)}ms · max ${s.maxMs.toFixed(1)}ms\n`,
    );
    s.lastReportAt = now;
    s.lastReportCount = s.count;
  }
}

/** Stamp a render — call at the top of a component body. Noop when the flag is off. */
export function useRenderTrace(name: string): void {
  const start = React.useRef(0);
  if (enabled) start.current = performance.now();
  React.useEffect(() => {
    if (enabled) recordRender(name, performance.now() - start.current);
  });
}

/** True when `REASONIX_TRACE_RENDERS` is set — skip expensive wiring otherwise. */
export const renderTraceEnabled = enabled;

/** Snapshot the trace counters. Used by probes that need raw numbers instead of stderr scraping. */
export function readRenderTraceStats(): Map<
  string,
  { count: number; totalMs: number; maxMs: number }
> {
  const out = new Map<string, { count: number; totalMs: number; maxMs: number }>();
  for (const [name, s] of stats) {
    out.set(name, { count: s.count, totalMs: s.totalMs, maxMs: s.maxMs });
  }
  return out;
}

/** Wipe accumulated counters. Used by probes between scenarios. */
export function resetRenderTraceStats(): void {
  stats.clear();
}
