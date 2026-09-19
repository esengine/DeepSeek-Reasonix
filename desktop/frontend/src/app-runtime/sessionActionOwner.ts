import type { CollaborationMode, QuestionAnswer, ToolApprovalMode } from "../lib/types";
import type { InteractionTarget } from "../lib/interactionTarget";
export { interactionInstanceKey as sessionPromptInstanceKey } from "../lib/interactionTarget";
import type { SessionOperationAuthority } from "./useSessionOperations";

export type SessionPromptTarget = InteractionTarget;

export type PlanDecisionAction = "start_execution" | "revise_plan" | "exit_plan";
export type RecoveryAction = "continue" | "continue_task" | "revise" | "stop";
export type MCPInteractionAction = "accept" | "decline" | "cancel";


export type SessionActionPorts = {
  approveForTab: (target: SessionPromptTarget, allow: boolean, session: boolean, persist: boolean) => void | Promise<void>;
  resolvePlanForTab: (target: SessionPromptTarget, action: PlanDecisionAction) => void | Promise<void>;
  resolveRecoveryForTab: (target: SessionPromptTarget, action: RecoveryAction, feedback: string) => void | Promise<void>;
  answerQuestionForTab: (target: SessionPromptTarget, answers: QuestionAnswer[]) => Promise<void>;
  answerMCPForTab: (target: SessionPromptTarget, action: MCPInteractionAction, content?: Record<string, unknown>) => void;
  setCollaborationModeForTab: (tabId: string, mode: CollaborationMode) => Promise<void>;
  clearGoalForTab: (tabId: string) => Promise<void>;
  setRemoteComposerProfile: (
    tabId: string,
    mode: CollaborationMode,
    approvalMode: ToolApprovalMode,
    goal: string,
  ) => Promise<string[]>;
  patchComposerProfile: (tabId: string, mode: CollaborationMode) => void;
  notePlanMode: (tabId: string, enabled: boolean) => void;
  drainRemoteApprovals: (tabId: string, ids: string[]) => void;
};

export function submitApproval(
  target: SessionPromptTarget,
  input: { allow: boolean; session: boolean; persist: boolean },
  ports: Pick<SessionActionPorts, "approveForTab">,
): void | Promise<void> {
  return ports.approveForTab(target, input.allow, input.session, input.persist);
}

export async function submitPlanDecision(
  target: SessionPromptTarget,
  input: {
    action: PlanDecisionAction;
    leavePlanMode: boolean;
    remote: boolean;
    goal: string;
    toolApprovalMode: ToolApprovalMode;
  },
  ports: SessionActionPorts,
  authority: SessionOperationAuthority,
): Promise<void> {
  authority.checkpoint();
  if (input.leavePlanMode) {
    if (input.remote) {
      const drained = await ports.setRemoteComposerProfile(target.tabId, "normal", input.toolApprovalMode, "");
      authority.checkpoint();
      if (authority.ownsUI()) ports.drainRemoteApprovals(target.tabId, drained);
    } else {
      if (input.goal.trim()) {
        await ports.clearGoalForTab(target.tabId);
        authority.checkpoint();
      }
      await ports.setCollaborationModeForTab(target.tabId, "normal");
      authority.checkpoint();
    }
    ports.notePlanMode(target.tabId, false);
    ports.patchComposerProfile(target.tabId, "normal");
  }
  authority.checkpoint();
  await ports.resolvePlanForTab(target, input.action);
}

export function submitRecovery(
  target: SessionPromptTarget,
  action: RecoveryAction,
  feedback: string,
  ports: Pick<SessionActionPorts, "resolveRecoveryForTab">,
): void | Promise<void> {
  return ports.resolveRecoveryForTab(target, action, feedback);
}

export function submitQuestion(
  target: SessionPromptTarget,
  answers: QuestionAnswer[],
  ports: Pick<SessionActionPorts, "answerQuestionForTab">,
): Promise<void> {
  return ports.answerQuestionForTab(target, answers);
}

export function submitMCPInteraction(
  target: SessionPromptTarget,
  action: MCPInteractionAction,
  content: Record<string, unknown> | undefined,
  ports: Pick<SessionActionPorts, "answerMCPForTab">,
): void {
  ports.answerMCPForTab(target, action, content);
}
