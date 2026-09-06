import type { CollaborationMode, QuestionAnswer, ToolApprovalMode } from "../lib/types";
import type { SessionOperationAuthority, SessionResource } from "./useSessionOperations";

export type SessionPromptTarget = Readonly<{
  tabId: string;
  sessionKey: string;
  promptId: string;
}>;

export type PlanDecisionAction = "start_execution" | "revise_plan" | "exit_plan";
export type RecoveryAction = "continue" | "continue_task" | "revise" | "stop";
export type MCPInteractionAction = "accept" | "decline" | "cancel";

export type ClearSessionPorts = {
  clearSession: () => Promise<void>;
  clearRemoteSession: (tabId: string) => Promise<void>;
  retryRemoteHydration: () => Promise<void>;
};

/** Clears a captured session. Resource completion may finish after navigation,
 * but UI follow-up is only allowed while the captured session owns the surface. */
export async function executeClearSession(
  target: SessionResource,
  input: { remote: boolean },
  ports: ClearSessionPorts,
  authority: SessionOperationAuthority,
): Promise<void> {
  authority.checkpoint();
  if (input.remote) {
    await ports.clearRemoteSession(target.tabId);
    authority.checkpoint();
    await ports.retryRemoteHydration();
  } else {
    await ports.clearSession();
  }
  authority.checkpoint();
}

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
