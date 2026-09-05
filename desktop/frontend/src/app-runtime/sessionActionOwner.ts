import type { CollaborationMode, QuestionAnswer, ToolApprovalMode } from "../lib/types";

export type SessionPromptTarget = Readonly<{
  tabId: string;
  sessionKey: string;
  promptId: string;
}>;

export type PlanDecisionAction = "start_execution" | "revise_plan" | "exit_plan";
export type RecoveryAction = "continue" | "continue_task" | "revise" | "stop";
export type MCPInteractionAction = "accept" | "decline" | "cancel";

export type SessionActionPorts = {
  approveForTab: (tabId: string, id: string, allow: boolean, session: boolean, persist: boolean) => void;
  resolvePlanForTab: (tabId: string, id: string, action: PlanDecisionAction) => void;
  resolveRecoveryForTab: (tabId: string, id: string, action: RecoveryAction, feedback: string) => void;
  answerQuestionForTab: (tabId: string, id: string, answers: QuestionAnswer[]) => Promise<void>;
  answerMCPForTab: (tabId: string, id: string, action: MCPInteractionAction, content?: Record<string, unknown>) => void;
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
): void {
  ports.approveForTab(target.tabId, target.promptId, input.allow, input.session, input.persist);
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
): Promise<void> {
  if (input.leavePlanMode) {
    if (input.remote) {
      const drained = await ports.setRemoteComposerProfile(target.tabId, "normal", input.toolApprovalMode, "");
      ports.drainRemoteApprovals(target.tabId, drained);
    } else {
      if (input.goal.trim()) await ports.clearGoalForTab(target.tabId);
      await ports.setCollaborationModeForTab(target.tabId, "normal");
    }
    ports.notePlanMode(target.tabId, false);
    ports.patchComposerProfile(target.tabId, "normal");
  }
  ports.resolvePlanForTab(target.tabId, target.promptId, input.action);
}

export function submitRecovery(
  target: SessionPromptTarget,
  action: RecoveryAction,
  feedback: string,
  ports: Pick<SessionActionPorts, "resolveRecoveryForTab">,
): void {
  ports.resolveRecoveryForTab(target.tabId, target.promptId, action, feedback);
}

export function submitQuestion(
  target: SessionPromptTarget,
  answers: QuestionAnswer[],
  ports: Pick<SessionActionPorts, "answerQuestionForTab">,
): Promise<void> {
  return ports.answerQuestionForTab(target.tabId, target.promptId, answers);
}

export function submitMCPInteraction(
  target: SessionPromptTarget,
  action: MCPInteractionAction,
  content: Record<string, unknown> | undefined,
  ports: Pick<SessionActionPorts, "answerMCPForTab">,
): void {
  ports.answerMCPForTab(target.tabId, target.promptId, action, content);
}
