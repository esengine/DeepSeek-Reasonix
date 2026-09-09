import type { Translator } from "./i18n";

export type DisplayRateBand = "peak" | "off_peak" | "mixed";
export type AggregatedRateBand = DisplayRateBand | "unknown";

function normalize(value: string | undefined): DisplayRateBand | undefined {
  if (value === "peak" || value === "off_peak" || value === "mixed") return value;
  return undefined;
}

// Missing/legacy/static quotes intentionally poison an aggregate: once a turn
// contains an unknown band, the UI must not claim that the whole turn was peak
// or off-peak.
function merge(current: AggregatedRateBand | undefined, value: string | undefined): AggregatedRateBand {
  const next = normalize(value) ?? "unknown";
  if (!current) return next;
  if (current === "unknown" || next === "unknown") return "unknown";
  if (current === next) return current;
  return "mixed";
}

function label(value: string | undefined, t: Translator): string | undefined {
  const band = normalize(value);
  return band
    ? t(`billing.rateBand.${band === "off_peak" ? "offPeak" : band}` as Parameters<Translator>[0])
    : undefined;
}

function append(value: string, band: string | undefined, t: Translator): string {
  const suffix = label(band, t);
  return suffix ? `${value} · ${suffix}` : value;
}

export { append as appendRateBand, label as rateBandLabel, merge as mergeRateBand, normalize as normalizeRateBand };
