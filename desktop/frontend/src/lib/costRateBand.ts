import type { Translator } from "./i18n";

export type DisplayRateBand = "peak" | "off_peak" | "mixed";
export type AggregatedRateBand = DisplayRateBand | "unknown";

export function normalizeRateBand(value: string | undefined): DisplayRateBand | undefined {
  if (value === "peak" || value === "off_peak" || value === "mixed") return value;
  return undefined;
}

// Missing/legacy/static quotes intentionally poison an aggregate: once a turn
// contains an unknown band, the UI must not claim that the whole turn was peak
// or off-peak.
export function mergeRateBand(current: AggregatedRateBand | undefined, value: string | undefined): AggregatedRateBand {
  const next = normalizeRateBand(value) ?? "unknown";
  if (!current) return next;
  if (current === "unknown" || next === "unknown") return "unknown";
  if (current === next) return current;
  return "mixed";
}

export function rateBandLabel(value: string | undefined, t: Translator): string | undefined {
  switch (normalizeRateBand(value)) {
    case "peak": return t("billing.rateBand.peak");
    case "off_peak": return t("billing.rateBand.offPeak");
    case "mixed": return t("billing.rateBand.mixed");
    default: return undefined;
  }
}

export function appendRateBand(value: string, band: string | undefined, t: Translator): string {
  const label = rateBandLabel(band, t);
  return label ? `${value} · ${label}` : value;
}
