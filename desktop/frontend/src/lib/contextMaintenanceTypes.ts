import type { Translator } from "./i18n";

export type ContextMaintenanceStatus = "planned" | "applied" | "noop" | "blocked" | "failed";
/** New writers only emit summary | noop. snip/prune/native are legacy restore-only. */
export type ContextMaintenanceAction = "summary" | "noop" | "snip" | "prune" | "native_tool_clear";

export interface WireContextMaintenance {
  status?: ContextMaintenanceStatus;
  action?: ContextMaintenanceAction;
  trigger?: string;
  operationId?: string;
  inputTokens?: number;
  resultTokens?: number;
  savedTokens?: number;
  affectedToolResults?: number;
  projectionVersion?: number;
  cacheBreak?: boolean;
  reason?: string;
}

export interface ContextMaintenanceReceipt extends WireContextMaintenance {
  sourceProjection?: number;
  coveredCount?: number;
  coveredPrefixHash?: string;
  inputHash?: string;
  outputHash?: string;
  summaryHash?: string;
  archive?: string;
  blockedInputHash?: string;
  createdAt?: string;
}

export interface ContextMaintenanceInfo {
  canonicalTokens?: number;
  projectedTokens?: number;
  summaryTokens?: number;
  lastSavedTokens?: number;
  /** @deprecated always 0; use triggerTokens */
  snipTrigger?: number;
  /** @deprecated alias of triggerTokens */
  foldTrigger?: number;
  /** @deprecated always 0; use triggerTokens */
  forceTrigger?: number;
  triggerTokens?: number;
  /** none | restored | applied — runtime only */
  checkpointState?: "none" | "restored" | "applied";
  hardInputCeiling?: number;
  headroom?: number;
  projectionVersion?: number;
  blocked?: boolean;
  lastReceipt?: ContextMaintenanceReceipt;
  contextBudget?: ContextBudgetInfo;
}

export type ContextBudgetSource = "unknown" | "explicit" | "official" | "opencode" | "learned";
export type ContextBudgetRecovery = "none" | "proactive_clip" | "learned_retry" | "compacted" | "failed";

export interface ContextBudgetInfo {
  windowMode?: string;
  limitMode?: string;
  source?: ContextBudgetSource | string;
  windowTokens?: number;
  promptTokens?: number;
  autoOutputTokens?: number;
  maxOutputTokens?: number;
  requestedOutputTokens?: number;
  effectiveOutputTokens?: number;
  reserveTokens?: number;
  physicalRemaining?: number;
  clipped?: boolean;
  lastRecovery?: ContextBudgetRecovery | string;
  observedWindow?: number;
  observedPrompt?: number;
  observedCompletion?: number;
}

export function formatContextMaintenanceNotice(m: WireContextMaintenance, t: Translator): string {
  switch (m.status) {
    case "applied":
      return t("context.maintenanceAppliedSummary");
    case "blocked":
      return t("context.maintenanceBlockedSummary");
    case "failed":
      return t("context.maintenanceFailedSummary");
    default:
      break;
  }
  const parts = [t("context.maintenanceTitle")];
  if (m.action === "summary") parts.push(t("summary.detail"));
  if (typeof m.inputTokens === "number" && typeof m.resultTokens === "number") {
    parts.push(t("context.tokensValue", {
      value: `${m.inputTokens.toLocaleString()} → ${m.resultTokens.toLocaleString()}`,
    }));
  }
  if (typeof m.savedTokens === "number" && m.savedTokens > 0) {
    parts.push(`−${t("context.tokensValue", { value: m.savedTokens.toLocaleString() })}`);
  }
  return parts.join(" · ");
}

const MAX_SEEN_MAINTENANCE_OPS = 64;

/** True when this operationId has not yet been rendered as a notice. */
export function isNewMaintenanceOperation(seen: readonly string[] | undefined, operationId?: string): boolean {
  const id = (operationId ?? "").trim();
  if (!id) return true;
  return !(seen ?? []).includes(id);
}

/** Remember an operationId for reconnect/replay dedupe (bounded FIFO). */
export function rememberMaintenanceOperation(seen: readonly string[] | undefined, operationId?: string): string[] {
  const id = (operationId ?? "").trim();
  if (!id) return [...(seen ?? [])];
  if ((seen ?? []).includes(id)) return [...(seen ?? [])];
  return [...(seen ?? []), id].slice(-MAX_SEEN_MAINTENANCE_OPS);
}
