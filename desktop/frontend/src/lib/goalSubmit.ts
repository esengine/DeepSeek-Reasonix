import type { StructuredInvocationSubmit } from "./invocationDisplay";
import type { WorkbenchActiveTarget, WorkbenchTargetKind } from "./workbenchTarget";

export type WorkbenchTargetToken = {
  kind: WorkbenchTargetKind;
  identityGen: number;
  requestSeq: number;
};

export function workbenchTargetToken(target: WorkbenchActiveTarget): WorkbenchTargetToken | null {
  if (
    !Number.isSafeInteger(target.identityGen) ||
    !Number.isSafeInteger(target.requestSeq) ||
    (target.identityGen ?? 0) <= 0 ||
    (target.requestSeq ?? -1) < 0
  ) {
    return null;
  }
  return {
    kind: target.kind,
    identityGen: target.identityGen!,
    requestSeq: target.requestSeq!,
  };
}

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
 * Tab- and target-scoped first Goal turn. The backend receives both the source
 * tab and the workbench projection token in one call, so a later target switch
 * cannot split Goal activation from the structured Skill submit.
 */
export async function activateGoalAndSubmitOnTab({
  tabId,
  target,
  displayText,
  submitText,
  structured,
  sendToTab,
}: {
  tabId: string;
  target: WorkbenchTargetToken;
  displayText: string;
  submitText: string;
  structured?: StructuredInvocationSubmit;
  sendToTab: (
    tabId: string,
    goal: string,
    displayText: string,
    submitText: string,
    structured?: StructuredInvocationSubmit,
    target?: WorkbenchTargetToken,
  ) => void | Promise<void>;
}): Promise<void> {
  const sourceTabId = tabId;
  const sourceTarget = { ...target };
  const goal = displayText.trim();
  await sendToTab(
    sourceTabId,
    goal,
    goal,
    structured ? submitText.trim() : `/goal ${submitText.trim()}`,
    structured,
    sourceTarget,
  );
}
