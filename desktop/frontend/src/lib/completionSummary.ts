import type { Translator } from "./i18n";
import type { WireCompletionSummary } from "./types";
import { normalizeCompletionReceipt, turnChangeText, turnCheckText, turnResultHasContent } from "./turnResult";

function count(value: number): number {
  return Number.isFinite(value) ? Math.max(0, Math.trunc(value)) : 0;
}

export function normalizeCompletionSummary(summary: WireCompletionSummary): WireCompletionSummary {
  return {
    receipt: normalizeCompletionReceipt(summary.receipt),
    turnId: summary.turnId,
    checkpointTurn: Number.isSafeInteger(summary.checkpointTurn) && summary.checkpointTurn! >= 0 ? summary.checkpointTurn : undefined,
    checking: summary.checking === true,
    liveChecks: Array.isArray(summary.liveChecks) ? summary.liveChecks : undefined,
    preset: String(summary.preset ?? "").trim().toLowerCase(),
    verdict: String(summary.verdict ?? "").trim().toLowerCase(),
    mutations: count(summary.mutations),
    changed_files: summary.changed_files === undefined ? undefined : count(summary.changed_files),
    checks_passed: count(summary.checks_passed),
    checks_failed: count(summary.checks_failed),
    checks_suppressed: count(summary.checks_suppressed),
    review: String(summary.review ?? "").trim().toLowerCase(),
    gap_kinds: [...new Set((Array.isArray(summary.gap_kinds) ? summary.gap_kinds : []).map((gap) => String(gap).trim().toLowerCase()).filter(Boolean))].slice(0, 8),
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


export function completionSummaryPresentation(
  summary: WireCompletionSummary,
  fallbackFloor: "standard" | "delivery",
  t: Translator,
): { level: "info" | "warn"; title: string; body: string } | undefined {
  if (!turnResultHasContent(summary)) return undefined;
  return {
    level: completionSummaryNeedsAttention(summary, fallbackFloor) ? "warn" : "info",
    title: t("notice.completionChangesTitle"),
    body: `${turnChangeText(summary, t)}\n${turnCheckText(summary, t)}`,
  };
}
