export type IntervalUnit = "s" | "m" | "h";

export interface LoopRunStatus {
  prompt: string;
  intervalMs: number;
  iter: number;
  /** Wall-clock ms until the next fire — server reports a remaining duration, not an absolute. */
  nextFireMs: number;
}

/** Quick-pick intervals in ms — covers the 95% of cases users actually run. */
export const INTERVAL_PRESETS_MS: ReadonlyArray<{ ms: number; label: string }> = [
  { ms: 30_000, label: "30s" },
  { ms: 60_000, label: "1m" },
  { ms: 5 * 60_000, label: "5m" },
  { ms: 15 * 60_000, label: "15m" },
  { ms: 60 * 60_000, label: "1h" },
  { ms: 6 * 60 * 60_000, label: "6h" },
];

const UNIT_TO_MS: Record<IntervalUnit, number> = {
  s: 1_000,
  m: 60_000,
  h: 60 * 60_000,
};

const MIN_INTERVAL_MS = 5_000;
const MAX_INTERVAL_MS = 6 * 60 * 60_000;

/** Convert a "30" + "s" pair to ms, returning null if out of [5s, 6h]. */
export function parseCustomInterval(value: string, unit: IntervalUnit): number | null {
  const n = Number.parseFloat(value);
  if (!Number.isFinite(n) || n <= 0) return null;
  const ms = Math.round(n * UNIT_TO_MS[unit]);
  if (ms < MIN_INTERVAL_MS || ms > MAX_INTERVAL_MS) return null;
  return ms;
}

/** Human-friendly "5m 12s" / "12s" / "2h 45m" — shows two largest non-zero units. */
export function formatRemaining(ms: number): string {
  const safe = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(safe / 3600);
  const m = Math.floor((safe % 3600) / 60);
  const s = safe % 60;
  if (h > 0) return m > 0 ? `${h}h ${m}m` : `${h}h`;
  if (m > 0) return s > 0 ? `${m}m ${s}s` : `${m}m`;
  return `${s}s`;
}
