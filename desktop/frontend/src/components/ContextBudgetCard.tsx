import type { Translator } from "../lib/i18n";
import type { ContextBudgetInfo } from "../lib/contextMaintenanceTypes";
import type { ContextInfo, ContextPanelInfo } from "../lib/types";
import { formatTokens } from "../lib/format";

export function resolveContextBudget(context?: ContextInfo | null, info?: ContextPanelInfo | null): ContextBudgetInfo | undefined {
  return info?.contextBudget ?? context?.contextBudget ?? context?.maintenance?.contextBudget;
}

export function contextBudgetSourceKey(source?: string): "context.budgetSourceExplicit" | "context.budgetSourceOfficial" | "context.budgetSourceOpencode" | "context.budgetSourceLearned" | "context.budgetSourceUnknown" {
  switch (source) {
    case "explicit":
      return "context.budgetSourceExplicit";
    case "official":
      return "context.budgetSourceOfficial";
    case "opencode":
      return "context.budgetSourceOpencode";
    case "learned":
      return "context.budgetSourceLearned";
    default:
      return "context.budgetSourceUnknown";
  }
}

export function contextBudgetRecoveryKey(kind?: string): "context.budgetRecoveryClip" | "context.budgetRecoveryRetry" | "context.budgetRecoveryCompacted" | "context.budgetRecoveryFailed" | "" {
  switch (kind) {
    case "proactive_clip":
      return "context.budgetRecoveryClip";
    case "learned_retry":
      return "context.budgetRecoveryRetry";
    case "compacted":
      return "context.budgetRecoveryCompacted";
    case "failed":
      return "context.budgetRecoveryFailed";
    default:
      return "";
  }
}

export function sharedContextPhysicalRemaining(budget?: ContextBudgetInfo): number | undefined {
  if (!budget || budget.windowMode !== "shared") return undefined;
  return Math.max(0, budget.physicalRemaining ?? 0);
}

export function showsSharedContextOverflowRisk(budget?: ContextBudgetInfo): boolean {
  return budget?.windowMode === "shared" && ((budget.physicalRemaining ?? 0) <= 0 || Boolean(budget.clipped));
}

export function ContextBudgetCard({
  budget,
  t,
}: {
  budget?: ContextBudgetInfo;
  t: Translator;
}) {
  if (!budget || ((budget.windowTokens ?? 0) <= 0 && (budget.promptTokens ?? 0) <= 0)) {
    return null;
  }
  const prompt = budget.promptTokens ?? 0;
  const reserved = budget.effectiveOutputTokens || budget.requestedOutputTokens || budget.autoOutputTokens || 0;
  const remaining = sharedContextPhysicalRemaining(budget);
  const recovery = contextBudgetRecoveryKey(budget.lastRecovery);
  const overflowRisk = showsSharedContextOverflowRisk(budget);
  return (
    <section className="context-panel__section context-panel__budget">
      <div className="context-panel__section-head">
        <span className="context-panel__section-title">{t("context.budgetTitle")}</span>
      </div>
      <div className="context-panel__budget-grid">
        <div>
          <span>{t("context.prompt")}</span>
          <strong>{formatTokens(prompt)}</strong>
        </div>
        <div>
          <span>{t("context.budgetOutputReserve")}</span>
          <strong>{formatTokens(reserved)}</strong>
        </div>
        <div>
          <span>{t("context.budgetPhysicalRemaining")}</span>
          <strong>{remaining === undefined ? "-" : formatTokens(remaining)}</strong>
        </div>
      </div>
      <p className="context-panel__budget-source">{t("context.budgetOutputSource")}: {t(contextBudgetSourceKey(budget.source))}</p>
      {overflowRisk && <p className="context-panel__budget-note">{t("context.budgetWhyOverflow")}</p>}
      {recovery && <p className="context-panel__budget-recovery">{t(recovery)}</p>}
    </section>
  );
}
