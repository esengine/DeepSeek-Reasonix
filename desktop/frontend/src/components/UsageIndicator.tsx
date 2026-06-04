import type { WireUsage } from "../lib/types";

// UsageIndicator is a compact, info-dense readout of the latest turn's
// token usage. It mounts in the status bar to the right of the cache-
// hit % indicator. The three numbers are styled with a small caption
// + value pair so the user can tell at a glance "this turn cost 4.2k
// completion tokens, mostly from the prompt cache".
//
// We use a pill instead of a sparkline because the controller exposes
// only the latest WireUsage payload (not a per-turn history). A
// sparkline would need a separate ring buffer in the controller; a
// future PR can add that and swap the pill for a real line chart.
//
// The component is intentionally tiny: 60 lines, no state, no
// effects. It re-renders when the WireUsage prop changes (the
// controller dispatches on every usage event during the turn).
export function UsageIndicator({ usage }: { usage?: WireUsage }) {
  if (!usage) return null;
  // "k" rounding: 0–999 shows as the raw number, ≥1000 as "1.2k".
  // Two significant digits feel right for both 800 (informative) and
  // 184,000 (don't drown the status bar).
  const fmt = (n: number): string => {
    if (n < 1000) return String(n);
    return `${(n / 1000).toFixed(n < 10_000 ? 1 : 0)}k`;
  };
  // The "saved" caption shows the cache-hit count as a fraction of
  // the prompt tokens; deepSeek and Anthropic charge less for cache
  // hits, so this number is the user-facing ROI.
  const promptSaved = usage.cacheHitTokens;
  return (
    <div className="usage" title="Latest turn token usage">
      <span className="usage__cell">
        <span className="usage__lbl">↑</span>
        <span className="usage__val">{fmt(usage.promptTokens)}</span>
      </span>
      <span className="usage__cell">
        <span className="usage__lbl">↓</span>
        <span className="usage__val">{fmt(usage.completionTokens)}</span>
      </span>
      {promptSaved > 0 && (
        <span className="usage__cell usage__cell--saved" title="Cache hits this turn">
          <span className="usage__lbl">↻</span>
          <span className="usage__val">{fmt(promptSaved)}</span>
        </span>
      )}
    </div>
  );
}
