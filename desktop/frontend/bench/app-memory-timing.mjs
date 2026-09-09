import { performance } from "node:perf_hooks";

// Bounded host-side aggregates; no instrumentation or retained history in the app.
export function createTimings(now = () => performance.now()) {
  const totals = new Map();
  return {
    async measure(name, operation) {
      const start = now();
      try { return await operation(); }
      finally {
        const elapsed = now() - start;
        const previous = totals.get(name) ?? { count: 0, totalMs: 0, maxMs: 0 };
        totals.set(name, { count: previous.count + 1, totalMs: previous.totalMs + elapsed, maxMs: Math.max(previous.maxMs, elapsed) });
      }
    },
    snapshot() {
      return Object.fromEntries([...totals].map(([key, value]) => [key, {
        count: value.count, totalMs: Math.round(value.totalMs), maxMs: Math.round(value.maxMs),
      }]));
    },
  };
}
