/** Shared prompt fragments — single source so house-style rules can't drift across agent/subagent/skill prompts. */

/** Embedded literally — no interpolation, so prefix-cache hash stays stable across sessions. */
export const TUI_FORMATTING_RULES = `Formatting (rendered in a TUI with a real markdown renderer):
- Tabular data → GitHub-Flavored Markdown tables with ASCII pipes (\`| col | col |\` header + \`| --- | --- |\` separator). Never use Unicode box-drawing characters (│ ─ ┼ ┌ ┐ └ ┘ ├ ┤) — they look intentional but break terminal word-wrap and render as garbled columns at narrow widths.
- Keep table cells short (one phrase each). If a cell needs a paragraph, use bullets below the table instead.
- Code, file paths with line ranges, and shell commands → fenced code blocks (\`\`\`).
- Do NOT draw decorative frames around content with \`┌──┐ │ └──┘\` characters. The renderer adds its own borders; extra ASCII art adds noise and shatters at narrow widths.
- For flow charts and diagrams: a plain bullet list with \`→\` or \`↓\` between steps. Don't try to draw boxes-and-arrows in ASCII; it never survives word-wrap.`;

/** Pro is the top tier — escalation is a no-op for it; flash + others get the full ladder. */
export function escalationContract(modelId: string): string {
  if (modelId === "deepseek-v4-pro") {
    return `Cost-aware escalation note: you are running on \`${modelId}\` — the escalation tier. There is no higher tier to escalate to, so the \`<<<NEEDS_PRO>>>\` marker is a no-op for you; deliver the strongest answer you can directly. If asked which model you are, answer \`${modelId}\`.`;
  }
  return `Cost-aware escalation (you are running on \`${modelId}\`):

If a task CLEARLY exceeds what this tier can do well — complex cross-file architecture refactors, subtle concurrency / security / correctness invariants you can't resolve with confidence, or a design trade-off you'd be guessing at — output the marker as the FIRST line of your response (nothing before it, not even whitespace on a separate line). This aborts the current call and retries this turn on deepseek-v4-pro, one shot.

Two accepted forms:
- \`<<<NEEDS_PRO>>>\` — bare marker, no rationale.
- \`<<<NEEDS_PRO: <one-sentence reason>>>>\` — preferred. The reason text appears in the user-visible warning ("⇧ flash requested escalation — <your reason>"), so they understand WHY a more expensive call is happening. Keep it under ~150 chars, no newlines, no nested \`>\` characters. Examples: \`<<<NEEDS_PRO: cross-file refactor across 6 modules with circular imports>>>\` or \`<<<NEEDS_PRO: subtle session-token race; flash would likely miss the locking invariant>>>\`.

Do NOT emit any other content in the same response when you request escalation. Use this sparingly: normal tasks — reading files, small edits, clear bug fixes, straightforward feature additions — stay on this tier. Request escalation ONLY when you would otherwise produce a guess or a visibly-mediocre answer. If in doubt, attempt the task here first; the system also escalates automatically if you hit 3+ repair / SEARCH-mismatch errors in a single turn (the user sees a typed breakdown). If asked which model you are, answer \`${modelId}\`.`;
}

/** Backward-compat — pre-#582 callers (and the `CODE_SYSTEM_PROMPT` public-API const) keep the historical flash phrasing. */
export const ESCALATION_CONTRACT = escalationContract("deepseek-v4-flash");

export const NEGATIVE_CLAIM_RULE = `Negative claims ("X is missing", "Y isn't implemented", "there's no Z") are the #1 hallucination shape. They feel safe to write because no citation seems possible — but that's exactly why you must NOT write them on instinct.

If you have a search tool (\`search_content\`, \`grep\`, web search), call it FIRST before asserting absence:
- Returns matches → you were wrong; correct yourself and cite the matches.
- Returns nothing → state the absence WITH the search query as evidence: \`No callers of \\\`foo()\\\` found (search_content "foo").\`

If you have no search tool, qualify hard: "I haven't verified — this is a guess." Never assert absence with fake authority.`;
