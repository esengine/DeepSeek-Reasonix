import type { Translator } from "./i18n";
import type { TurnChanges, WireCompletionReceipt, WireCompletionSummary } from "./types";

const validCount = (value: unknown): value is number => typeof value === "number" && Number.isSafeInteger(value) && value >= 0;

export function normalizeTurnChanges(value?: TurnChanges): TurnChanges | undefined {
  if (!value) return undefined;
  const valid = Array.isArray(value.files) && validCount(value.added) && validCount(value.removed) && validCount(value.turn);
  const files = (Array.isArray(value.files) ? value.files : []).filter(file => file && typeof file.path === "string" && validCount(file.added) && validCount(file.removed));
  return {
    id: typeof value.id === "string" ? value.id : undefined,
    turn: validCount(value.turn) ? value.turn : 0,
    coverage: valid && files.length === value.files.length && (value.coverage === "complete" || value.coverage === "partial") ? value.coverage : "unknown",
    files,
    added: validCount(value.added) ? value.added : 0,
    removed: validCount(value.removed) ? value.removed : 0,
    reasons: (Array.isArray(value.reasons) ? value.reasons : []).filter((v): v is string => typeof v === "string"),
  };
}

export function normalizeCompletionReceipt(value?: WireCompletionReceipt): WireCompletionReceipt | undefined {
  if (!value) return undefined;
  return {
    verdict: String(value.verdict ?? "unknown"),
    diff: normalizeTurnChanges(value.diff),
    interrupted: value.interrupted === true,
    changes: Array.isArray(value.changes) ? value.changes : [],
    verifications: (Array.isArray(value.verifications) ? value.verifications : []).filter(v => v && typeof v.command === "string").map(v => ({
      ...v, passed: v.passed === true && (v.exitCode === undefined || v.exitCode === 0), stale: v.stale === true,
    })),
    gaps: (Array.isArray(value.gaps) ? value.gaps : []).filter(g => g && typeof g.kind === "string"),
    risks: (Array.isArray(value.risks) ? value.risks : []).filter((v): v is string => typeof v === "string"),
  };
}

export function mergeTurnResult(
  summary?: WireCompletionSummary,
  receipt?: WireCompletionReceipt,
  turnId?: string,
  checkpointTurn?: number,
): WireCompletionSummary {
  const normalized = normalizeCompletionReceipt(receipt);
  return {
    preset: "", verdict: normalized?.verdict ?? "unknown", mutations: 0,
    checks_passed: 0, checks_failed: 0, checks_suppressed: 0, review: "none", constraint_degraded: false,
    ...summary,
    receipt: normalized ?? summary?.receipt,
    turnId: turnId ?? summary?.turnId,
    checkpointTurn: validCount(checkpointTurn) ? checkpointTurn : summary?.checkpointTurn,
  };
}

export function turnResultHasContent(summary: WireCompletionSummary): boolean {
  const receipt = summary.receipt;
  return Boolean(summary.checking || summary.mutations > 0 || summary.checks_failed > 0 || summary.checks_passed > 0 || summary.checks_suppressed > 0 || summary.attention
    || receipt?.diff?.files.length || receipt?.diff?.reasons.length || receipt?.changes?.length || receipt?.verifications?.length || receipt?.gaps?.length || receipt?.risks?.length);
}

export function turnChangeText(summary: WireCompletionSummary, t: Translator): string {
  const diff = summary.receipt?.diff;
  if (!diff || diff.coverage === "unknown") return t("completion.changesUnknown");
  if (diff.files.length === 0) return t(diff.coverage === "complete" ? "completion.noNetChanges" : "completion.changesIncomplete");
  const files = t(diff.coverage === "complete" ? "completion.filesChanged" : "completion.filesCounted", { count: diff.files.length });
  const suffix = diff.coverage === "partial" ? ` · ${t("completion.partialStats")}` : "";
  return `${files} · +${diff.added} −${diff.removed}${suffix}`;
}

export type TurnCheckStatus = "running" | "failed" | "interrupted" | "stale" | "passed" | "none" | "unknown";

export function turnCheckState(summary: WireCompletionSummary): { status: TurnCheckStatus; count: number; stale: boolean } {
  const checks = summary.receipt?.verifications ?? [];
  const stale = checks.some(v => v.stale) || (summary.gap_kinds ?? []).some(kind => kind === "stale_check" || kind === "stale_verification");
  const failed = summary.receipt ? checks.filter(v => !v.passed && !v.interrupted).length : summary.checks_failed;
  const interrupted = checks.some(v => v.interrupted) || Boolean(summary.receipt?.interrupted && summary.liveChecks?.length);
  const status: TurnCheckStatus = summary.checking ? "running"
    : failed > 0 ? "failed"
    : interrupted ? "interrupted"
    : stale ? "stale"
    : !summary.receipt ? "unknown"
    : checks.length > 0 ? "passed" : "none";
  return { status, count: status === "failed" ? failed : checks.length, stale };
}

export function turnCheckText(summary: WireCompletionSummary, t: Translator): string {
  const { status, count, stale } = turnCheckState(summary);
  const labels = {
    running: "completion.checkRunning", failed: "completion.checkFailed", interrupted: "completion.checkInterrupted",
    stale: "completion.checkStale", passed: "completion.checkPassed", none: "completion.checkNone", unknown: "completion.checkUnknown",
  } as const;
  const text = t(labels[status], { count });
  return status === "failed" && stale ? `${text} · ${t("completion.someStale")}` : text;
}
