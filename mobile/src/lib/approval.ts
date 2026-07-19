export type ApprovalRisk = "low" | "medium" | "high";

export interface ApprovalRequest {
  id: string;
  sessionId: string;
  tool: string;
  subject: string;
  reason?: string;
  risk: ApprovalRisk;
  /** Optional command or file path preview */
  command?: string;
  /** Unified diff snippet */
  diff?: string;
  /** Dangerous write — UI requires long-press to allow */
  dangerousWrite: boolean;
}

/* `\b` treats `_` as a word char, so snake_case tool names (delete_file,
   write_file) never hit the keyword checks — normalize separators first. */
function normalize(text: string): string {
  return text.toLowerCase().replace(/[_-]+/g, " ");
}

export function riskFromTool(tool: string, subject: string): ApprovalRisk {
  const t = normalize(`${tool} ${subject}`);
  if (/\b(rm|delete|unlink|drop|truncate|sudo|chmod|chown)\b/.test(t)) return "high";
  if (/\b(write|edit|patch|mv|move|git\s+push|apply)\b/.test(t)) return "medium";
  return "low";
}

export function isDangerousWrite(risk: ApprovalRisk, tool: string): boolean {
  if (risk === "high") return true;
  return risk === "medium" && /\b(write|edit|patch|delete|rm)\b/.test(normalize(tool));
}
