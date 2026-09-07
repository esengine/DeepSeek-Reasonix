import { CommandCancelled } from "../lib/commandOutcome";
import type { QuestionAnswer, ToolApprovalMode } from "../lib/types";
import type { MCPInteractionAction, PlanDecisionAction, RecoveryAction, SessionActionPorts, SessionPromptTarget } from "./sessionActionOwner";
import type { SessionOperationAuthority } from "./useSessionOperations";

export type SessionPromptKind = "approval" | "ask" | "mcpInteraction";
export type PromptRequest =
  | { kind: "approval"; allow: boolean; session: boolean; persist: boolean }
  | { kind: "plan"; action: PlanDecisionAction; leavePlanMode: boolean; remote: boolean; goal: string; toolApprovalMode: ToolApprovalMode; revision?: string }
  | { kind: "recovery"; action: RecoveryAction; feedback: string }
  | { kind: "question"; answers: QuestionAnswer[] }
  | { kind: "mcp"; action: MCPInteractionAction; content?: Record<string, unknown> };
export type PromptPorts = SessionActionPorts & {
  isPromptCurrentForTab: (tabId: string, kind: SessionPromptKind, promptId: string) => boolean;
  rememberRevision: (tabId: string, revision: string) => void;
};
export type PromptInput = { target: SessionPromptTarget; promptKind: SessionPromptKind; request: PromptRequest; ports: PromptPorts };

/** Lazy loading and every business continuation share the same source receipt. */
export async function executeSessionPrompt(input: PromptInput, source: SessionOperationAuthority) {
  const { target, request, ports } = input;
  const authority: SessionOperationAuthority = {
    checkpoint() {
      source.checkpoint();
      if (!ports.isPromptCurrentForTab(target.tabId, input.promptKind, target.promptId)) throw new CommandCancelled("superseded");
    },
    ownsUI: () => source.ownsUI(),
  };
  authority.checkpoint();
  const owner = await import("./sessionActionOwner");
  authority.checkpoint();
  switch (request.kind) {
    case "approval": return owner.submitApproval(target, request, ports);
    case "plan":
      if (request.revision !== undefined) ports.rememberRevision(target.tabId, request.revision);
      return owner.submitPlanDecision(target, request, ports, authority);
    case "recovery": return owner.submitRecovery(target, request.action, request.feedback, ports);
    case "question": return owner.submitQuestion(target, request.answers, ports);
    case "mcp": return owner.submitMCPInteraction(target, request.action, request.content, ports);
  }
}
