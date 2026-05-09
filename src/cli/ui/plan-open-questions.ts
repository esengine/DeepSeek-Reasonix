/** Markdown header rule used by PlanConfirm to flag plans with open questions. No `\b` — it's ASCII-only and would skip the Chinese alternatives. */
const HEADER_RE =
  /^(#{1,6})\s*(open[-\s]?questions?|risks?|unknowns?|assumptions?|unclear|待确认|开放问题|风险|未知|假设|不确定)(?:[\s:：/、,，].*)?$/im;

export function hasOpenQuestionsSection(plan: string): boolean {
  return HEADER_RE.test(plan);
}

/** Markdown body of the first matching heading down to the next same-or-shallower heading; null when absent. */
export function extractOpenQuestionsSection(plan: string): string | null {
  const lines = plan.split("\n");
  let startIdx = -1;
  let startLevel = 0;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i] ?? "";
    const m = line.match(HEADER_RE);
    if (m) {
      startIdx = i;
      startLevel = (m[1] ?? "#").length;
      break;
    }
  }
  if (startIdx === -1) return null;

  let endIdx = lines.length;
  for (let j = startIdx + 1; j < lines.length; j++) {
    const line = lines[j] ?? "";
    const lh = line.match(/^(#{1,6})\s+\S/);
    if (lh && (lh[1] ?? "").length <= startLevel) {
      endIdx = j;
      break;
    }
  }

  const block = lines.slice(startIdx, endIdx).join("\n").replace(/\s+$/g, "");
  return block.length > 0 ? block : null;
}
