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
  };
}

export function completionSummaryNeedsAttention(summary?: WireCompletionSummary): boolean {
  if (!summary) return false;
  const verdict = summary.verdict.trim().toLowerCase();
  const review = summary.review.trim().toLowerCase();
  return verdict === "partial"
    || verdict === "blocked"
    || summary.checks_failed > 0
    || summary.checks_suppressed > 0
    || review === "warned"
    || review === "failed"
    || review === "unavailable"
    || (summary.gap_kinds?.length ?? 0) > 0
    || summary.constraint_degraded;
}

export function completionSummaryNotice(summary: WireCompletionSummary, t: Translator): { title: string; body: string } {
  if (summary.verdict === "blocked") {
    return { title: t("completion.verdictBlocked"), body: t("notice.completionGapsBody") };
  }
  return { title: t("notice.completionAttentionTitle"), body: t("notice.completionGapsBody") };
}
