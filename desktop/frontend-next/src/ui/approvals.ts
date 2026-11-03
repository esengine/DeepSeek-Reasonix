import { t } from "../i18n";
import type { ApprovalMode } from "../port/port";

// One table for every surface that names an approval mode: the composer menu,
// Settings and the shelf. Ordered from strictest to most permissive.
export const APPROVALS: [ApprovalMode, string, string][] = [
  ["dontAsk", "approvals.doNotAsk", "approvals.noApprovalPromptsAnythingNeeding"],
  ["ask", "common.ask", "approvals.asksBeforeEveryAction"],
  ["auto", "common.autoApprove", "approvals.lowRiskStepsProceedWrites"],
  ["yolo", "approvals.allowAll", "approvals.stopsAskingOnlyForA"],
];

const row = (mode: ApprovalMode | undefined) => APPROVALS.find(([id]) => id === mode);

export const approvalName = (mode: ApprovalMode | undefined, fallback = "common.ask"): string => t(row(mode)?.[1] ?? fallback);

export const approvalNote = (mode: ApprovalMode | undefined): string => t(row(mode)?.[2] ?? "");
