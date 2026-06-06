export type AbortDraftSource = "user_input" | "skill_run" | "btw";

export type AbortDraftAction =
  | { type: "record"; source: AbortDraftSource; text: string }
  | { type: "clear" };

export function nextAbortDraftCandidate(
  _current: string | null,
  action: AbortDraftAction,
): string | null {
  if (action.type === "clear") return null;
  if (action.source === "btw") return null;

  const text = action.text.trim();
  return text ? text : null;
}

export function restoreAbortedDraft(
  currentDraft: string,
  interruptedText: string | null | undefined,
): string | null {
  const text = interruptedText?.trim();
  if (!text) return null;
  if (currentDraft.trim()) return null;
  return text;
}
