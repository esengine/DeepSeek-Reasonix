import type { WireGuardian } from "./types";

export function formatGuardianAssessmentNotice(guardian: WireGuardian): string {
  const outcome = guardian.outcome || "unknown";
  const parts = [`Guardian ${outcome}`];
  if (guardian.tool) parts.push(guardian.tool);
  if (guardian.subject) parts.push(guardian.subject);
  if (guardian.risk_level) parts.push(`risk=${guardian.risk_level}`);
  if (guardian.user_authorization) parts.push(`authorization=${guardian.user_authorization}`);
  if (guardian.rationale) parts.push(guardian.rationale);
  return parts.join(" · ");
}
