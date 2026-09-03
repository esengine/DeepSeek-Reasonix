export type StatsQueryObserver = (label: string, durationMs: number, rows: number) => void;

const SLOW_QUERY_MS = 250;
const SAMPLE_RATE = 0.02;

export function statsQueryObserver(route: string): StatsQueryObserver {
  return (label, durationMs, rows) => {
    if (durationMs < SLOW_QUERY_MS && Math.random() >= SAMPLE_RATE) return;
    console.log(JSON.stringify({
      event: "d1_query_timing",
      route,
      label,
      durationMs: Math.round(durationMs),
      rows,
    }));
  };
}
