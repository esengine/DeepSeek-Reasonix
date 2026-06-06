// Single import boundary for `yoga-layout`. Sibling files re-export through
// here so a future swap (alternative layout backend, version pin, mock for
// tests) only has to touch this file.
import Yoga from 'yoga-layout';

export * from 'yoga-layout';
export default Yoga;

type YogaCounters = {
  visited: number;
  measured: number;
  cacheHits: number;
  live: number;
};

// Diagnostic counters surfaced to renderer telemetry. The TypeScript port of
// Yoga we use does not expose internal stats, so these are currently zeroed
// placeholders — kept so call sites (`ink.tsx`, `reconciler.ts`) continue to
// compile and so we have a hook to wire real numbers in later without an API
// change.
export function getYogaCounters(): YogaCounters {
  return { visited: 0, measured: 0, cacheHits: 0, live: 0 };
}
