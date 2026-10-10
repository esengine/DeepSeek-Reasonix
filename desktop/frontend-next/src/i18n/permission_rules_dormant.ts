import { t } from "./index";

export const PERMISSION_RULES_DORMANT = "permission_rules_dormant";

const LIST_LABEL: Record<string, string> = { deny: "拒绝", ask: "询问", allow: "放行" };

export const listLabel = (list: string): string => t(LIST_LABEL[list] ?? list);

// The typed payload a dormant-rules notice carries as its detail, read as the
// variables its sentence is worded from. A detail that is not one reads as none,
// so the notice keeps the kernel's English.
export function permissionRulesDormantVars(detail: string | undefined): { n: number; list: string; rule: string } | undefined {
  if (!detail) return undefined;
  try {
    const p = JSON.parse(detail) as { rules?: { list?: unknown; rule?: unknown }[] };
    const first = p.rules?.[0];
    if (p.rules && typeof first?.list === "string" && typeof first.rule === "string") return { n: p.rules.length, list: listLabel(first.list), rule: first.rule };
  } catch {
    return undefined;
  }
  return undefined;
}
