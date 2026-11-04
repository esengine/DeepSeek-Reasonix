import { t } from "../i18n";
import type { ApprovalMode } from "../port/port";

// One table for every surface that names an approval mode: the composer menu,
// Settings and the shelf. Ordered from strictest to most permissive.
export const APPROVALS: [ApprovalMode, string, string][] = [
  ["dontAsk", "safety.do_not_ask", "safety.no_approval_prompts_anything_needing"],
  ["ask", "safety.ask", "safety.asks_before_every_action"],
  ["auto", "safety.auto_approve", "safety.low_risk_steps_proceed_writes"],
  ["yolo", "safety.allow_all", "safety.stops_asking_only_for_a"],
];

const row = (mode: ApprovalMode | undefined) => APPROVALS.find(([id]) => id === mode);

export const approvalName = (mode: ApprovalMode | undefined, fallback = "safety.ask"): string => t(row(mode)?.[1] ?? fallback);

export const approvalNote = (mode: ApprovalMode | undefined): string => t(row(mode)?.[2] ?? "");
