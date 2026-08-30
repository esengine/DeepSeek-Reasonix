import type { Translator } from "./i18n";
import type { WireCompletionSummary } from "./types";

function count(value: number): number {
  return Number.isFinite(value) ? Math.max(0, Math.trunc(value)) : 0;
}

export function normalizeCompletionSummary(summary: WireCompletionSummary): WireCompletionSummary {
  return {
    preset: String(summary.preset ?? "").trim().toLowerCase(),
    verdict: String(summary.verdict ?? "").trim().toLowerCase(),
    mutations: count(summary.mutations),
    checks_passed: count(summary.checks_passed),
    checks_failed: count(summary.checks_failed),
    checks_suppressed: count(summary.checks_suppressed),
    review: String(summary.review ?? "").trim().toLowerCase(),
    gap_kinds: [...new Set((summary.gap_kinds ?? []).map((gap) => String(gap).trim().toLowerCase()).filter(Boolean))].slice(0, 8),
    constraint_degraded: Boolean(summary.constraint_degraded),
    floor: String(summary.floor ?? "").trim().toLowerCase(),
    attention: Boolean(summary.attention),
  };
}

export function sessionQualityFloor(meta?: { qualityFloor?: string; tokenMode?: string } | null): "standard" | "delivery" {
  if ((meta?.qualityFloor ?? "").trim().toLowerCase() === "delivery") return "delivery";
  if ((meta?.tokenMode ?? "").trim().toLowerCase() === "delivery") return "delivery";
  return "standard";
}

export function completionSummaryNeedsAttention(
  summary?: WireCompletionSummary,
  floor: "standard" | "delivery" = "standard",
): boolean {
  if (!summary) return false;
  const recordedFloor = (summary.floor ?? "").trim().toLowerCase();
  if (recordedFloor === "standard" || recordedFloor === "delivery") return Boolean(summary.attention);
  const verdict = summary.verdict.trim().toLowerCase();
  const kinds = new Set((summary.gap_kinds ?? []).map((gap) => gap.trim().toLowerCase()).filter(Boolean));
  if (verdict === "blocked" || summary.checks_failed > 0 || summary.checks_suppressed > 0) return true;
  if (kinds.has("unbacked_claim") || kinds.has("failed_verification")) return true;
  if (floor === "delivery") {
    return kinds.has("unverified_change") || kinds.has("missing_check") || kinds.has("stale_verification") || kinds.has("unproven_criterion");
  }
  return false;
}

export function completionSummaryNotice(summary: WireCompletionSummary, t: Translator): { title: string; body: string } {
  const kinds = new Set((summary.gap_kinds ?? []).map((gap) => gap.trim().toLowerCase()).filter(Boolean));
  if (summary.verdict === "blocked") {
    return { title: t("completion.verdictBlocked"), body: t("notice.completionGapsBody") };
  }
  if (kinds.has("failed_verification") || summary.checks_failed > 0) {
    return { title: t("notice.completionFailedTitle"), body: t("notice.completionFailedBody") };
  }
  if (summary.checks_suppressed > 0 || kinds.has("suppressed") || kinds.has("suppressed_requirement")) {
    return { title: t("notice.completionAttentionTitle"), body: t("notice.completionGapsBody") };
  }
  return { title: t("notice.completionDeliveryTitle"), body: t("notice.completionDeliveryBody") };
}

export function completionSummaryChangeNotice(summary: WireCompletionSummary, t: Translator): { title: string; body: string } {
  return {
    title: t("notice.completionChangesTitle"),
    body: t("notice.completionChangesBody", { count: String(summary.mutations) }),
  };
}

export function completionSummaryPresentation(
  summary: WireCompletionSummary,
  fallbackFloor: "standard" | "delivery",
  t: Translator,
): { level: "info" | "warn"; title: string; body: string } | undefined {
  if (completionSummaryNeedsAttention(summary, fallbackFloor)) {
    return { level: "warn", ...completionSummaryNotice(summary, t) };
  }
  if (summary.mutations > 0) {
    return { level: "info", ...completionSummaryChangeNotice(summary, t) };
  }
  return undefined;
}
