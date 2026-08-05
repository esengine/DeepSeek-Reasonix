import type { StructuredInvocationSubmit } from "./invocationDisplay";

/**
 * Activate a Goal, then submit the first turn.
 *
 * For structured Skill/Subagent submissions there is no `/goal` prose fallback:
 * if Goal activation fails, this must not call `send` (the Skill would otherwise
 * run without an active Goal). Callers should let activation errors propagate.
 */
export async function activateGoalAndSubmit({
  displayText,
  submitText,
  structured,
  applyGoal,
  send,
}: {
  displayText: string;
  submitText: string;
  structured?: StructuredInvocationSubmit;
  applyGoal: (goal: string) => void | Promise<void>;
  send: (displayText: string, submitText: string, structured?: StructuredInvocationSubmit) => void | Promise<void>;
}): Promise<void> {
  const goal = displayText.trim();
  // Fail closed: structured paths have no `/goal` wrap, so a no-op or rejected
  // activation must abort before SubmitInvocationsToTab.
  await applyGoal(goal);
  await send(
    goal,
    structured ? submitText.trim() : `/goal ${submitText.trim()}`,
    structured,
  );
}

/**
 * Tab-scoped first Goal turn. The backend receives the source tab in one call,
 * so Goal activation and the structured Skill submit cannot be split by a tab
 * switch.
 */
export async function activateGoalAndSubmitOnTab({
  tabId,
  displayText,
  submitText,
  structured,
  sendToTab,
}: {
  tabId: string;
  displayText: string;
  submitText: string;
  structured?: StructuredInvocationSubmit;
  sendToTab: (
    tabId: string,
    goal: string,
    displayText: string,
    submitText: string,
    structured?: StructuredInvocationSubmit,
  ) => void | Promise<void>;
}): Promise<void> {
  const sourceTabId = tabId;
  const goal = displayText.trim();
  await sendToTab(
    sourceTabId,
    goal,
    goal,
    structured ? submitText.trim() : `/goal ${submitText.trim()}`,
    structured,
  );
}
