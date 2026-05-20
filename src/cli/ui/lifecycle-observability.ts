import type { EngineeringLifecycleSnapshot } from "../../code/lifecycle.js";
import { t } from "../../i18n/index.js";

interface LifecycleRejectionPayload {
  rejectedReason?: unknown;
  state?: unknown;
  nextAction?: unknown;
  stepId?: unknown;
  consecutiveInterceptorRejection?: unknown;
}

export function formatLifecycleStatus(
  snapshot: EngineeringLifecycleSnapshot | null | undefined,
): string | null {
  if (!snapshot || snapshot.mode === "off") return null;

  const completed = snapshot.completedStepIds.length;
  const total = snapshot.planSteps.length;
  const progress =
    total > 0
      ? `${Math.min(completed, total)}/${total}`
      : t("handlers.observability.lifecycleNoPlan");
  const evidence = snapshot.mutatedSinceLastStep
    ? ` · ${t("handlers.observability.lifecycleEvidencePending")}`
    : "";

  return t("handlers.observability.statusLifecycle", {
    mode: snapshot.mode,
    state: snapshot.state,
    progress,
    evidence,
  });
}

export function formatLifecycleRejection(
  toolName: string | undefined,
  content: string,
): string | null {
  const payload = parseLifecyclePayload(content);
  if (!payload) return null;

  const reason = text(payload.rejectedReason, "");
  if (reason !== "engineering-lifecycle" && reason !== "engineering-lifecycle-evidence") {
    return null;
  }

  const tool = toolName?.trim() || t("common.tool");
  if (payload.consecutiveInterceptorRejection === true) {
    return t("handlers.observability.lifecycleRepeatedRejected", { tool });
  }

  if (reason === "engineering-lifecycle-evidence") {
    return t("handlers.observability.lifecycleEvidenceRejected", {
      stepId: text(payload.stepId, "?"),
      next: text(payload.nextAction, "add_evidence"),
    });
  }

  return t("handlers.observability.lifecycleRejected", {
    tool,
    state: text(payload.state, "?"),
    next: text(payload.nextAction, "submit_plan"),
  });
}

function parseLifecyclePayload(content: string): LifecycleRejectionPayload | null {
  try {
    const parsed = JSON.parse(content) as unknown;
    if (!parsed || typeof parsed !== "object") return null;
    return parsed as LifecycleRejectionPayload;
  } catch {
    return null;
  }
}

function text(value: unknown, fallback: string): string {
  return typeof value === "string" && value.trim().length > 0 ? value.trim() : fallback;
}
