import type { Translator } from "./i18n";

export type ContextMaintenanceStatus = "planned" | "applied" | "noop" | "blocked" | "failed";
export type ContextMaintenanceAction = "snip" | "prune" | "summary" | "native_tool_clear" | "noop";

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
  snipTrigger?: number;
  foldTrigger?: number;
  forceTrigger?: number;
  hardInputCeiling?: number;
  headroom?: number;
  projectionVersion?: number;
  blocked?: boolean;
  lastReceipt?: ContextMaintenanceReceipt;
}

function maintenanceActionLabel(action: ContextMaintenanceAction | undefined, t: Translator): string | undefined {
  switch (action) {
    case "snip": return t("context.maintenanceActionSnip");
    case "prune": return t("context.maintenanceActionPrune");
    case "summary": return t("summary.detail");
    case "native_tool_clear": return t("context.maintenanceActionNative");
    default: return undefined;
  }
}

export function formatContextMaintenanceNotice(m: WireContextMaintenance, t: Translator): string {
  const parts = [t("context.maintenanceTitle")];
  const action = maintenanceActionLabel(m.action, t);
  if (action) parts.push(action);
  switch (m.status) {
    case "blocked": parts.push(t("projectTree.status.paused")); break;
    case "failed": parts.push(t("task.state.failed")); break;
    case "applied": parts.push(t("settings.typography.applied")); break;
  }
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
