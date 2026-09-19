import { useCallback, useMemo } from "react";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { CommandCancelled } from "../lib/commandOutcome";
import { hasSessionGeneration } from "../lib/sessionIdentity";
import type { PromptDiagnosticTarget } from "../lib/promptSubmissionDiagnostics";
import type { QuestionAnswer, ToolApprovalMode, WireApproval, WireAsk, WireMCPInteraction } from "../lib/types";
import { executeSessionPrompt, type PromptPorts, type PromptRequest, type SessionPromptKind } from "./sessionPromptExecutor";
import { interactionInstanceKey, type InteractionKind, type InteractionTarget } from "../lib/interactionTarget";
import type { SessionRef } from "../lib/sessionRef";
import type { MCPInteractionAction, RecoveryAction } from "./sessionActionOwner";
import type { SessionResource, useSessionOperations } from "./useSessionOperations";

type Input = {
  target: SessionResource;
  sessionGeneration?: number;
  session?: SessionRef | null;
  approval?: WireApproval;
  question?: WireAsk;
  mcpInteraction?: WireMCPInteraction;
  remote: boolean;
  goal: string;
  toolApprovalMode: ToolApprovalMode;
  ports: PromptPorts;
  operations: ReturnType<typeof useSessionOperations>;
  reportError: (error: unknown) => void;
};

// Diagnostics must neither block authorization nor turn a logging failure into
// a failed user action. The recorder is shared with session diagnostic exports.
function recordOutcome(target: PromptDiagnosticTarget, status: string, error?: unknown) {
  void import("../lib/promptSubmissionDiagnostics").then(({ notePromptSubmission, promptFailureClass }) => {
    notePromptSubmission(target, "command", error === undefined ? status : `${status}:${promptFailureClass(error)}`);
  }).catch(() => {});
}

export function useSessionPromptCommands(input: Input) {
  const { target: sessionTarget, session, sessionGeneration, operations, ports } = input;
  const makeTarget = useCallback((prompt: WireApproval | WireAsk | WireMCPInteraction | undefined, promptKind: SessionPromptKind): InteractionTarget | undefined => {
    if (!prompt?.id || !sessionTarget.tabId || !session?.sessionId || !hasSessionGeneration(sessionGeneration)) return undefined;
    let kind: InteractionKind;
    if (promptKind === "mcpInteraction") kind = "mcp";
    else if (promptKind === "ask") kind = "ask";
    else {
      const approval = prompt as WireApproval;
      kind = approval.kind === "recovery" || approval.recovery
        ? "recovery" : approval.tool === "exit_plan_mode" ? "plan" : "approval";
    }
    const base = {
      ...sessionTarget,
      hostId: session.hostId,
      sessionId: session.sessionId,
      sessionGeneration,
      promptId: prompt.id,
      turnId: prompt.turnId,
      runtimeEpoch: prompt.runtimeEpoch,
      kind,
      requestGeneration: "generation" in prompt ? prompt.generation : undefined,
      permissionRevision: "permissionRevision" in prompt ? prompt.permissionRevision : undefined,
    };
    return { ...base, instanceKey: interactionInstanceKey(base) };
  }, [sessionTarget, session, sessionGeneration]);
  const approvalTarget = makeTarget(input.approval, "approval");
  const questionTarget = makeTarget(input.question, "ask");
  const mcpTarget = makeTarget(input.mcpInteraction, "mcpInteraction");
  const run = useCallback(async (target: InteractionTarget | undefined, promptKind: SessionPromptKind, request: PromptRequest) => {
    const diagnosticTarget = target ?? { ...sessionTarget, sessionId: session?.sessionId, sessionGeneration };
    recordOutcome(diagnosticTarget, "started");
    if (!target) {
      recordOutcome(diagnosticTarget, "not-ready");
      throw new CommandCancelled("not-ready");
    }
    const result = await operations(
      { tabId: target.tabId, sessionKey: target.sessionKey },
      `prompt:${promptKind}`,
      { target, promptKind, request, ports },
      executeSessionPrompt,
    );
    recordOutcome(diagnosticTarget, result.status === "cancelled" ? result.reason : result.status,
      result.status === "failed" ? result.error : undefined);
    if (result.status === "failed") throw result.error;
    if (result.status === "cancelled") throw new CommandCancelled(result.reason);
  }, [operations, ports, sessionTarget, session, sessionGeneration]);
  const plan = useCallback((action: "start_execution" | "revise_plan" | "exit_plan", revision?: string) => run(approvalTarget, "approval", {
    kind: "plan", action, leavePlanMode: action !== "revise_plan", remote: input.remote,
    goal: input.goal, toolApprovalMode: input.toolApprovalMode, revision,
  }), [approvalTarget, input.remote, input.goal, input.toolApprovalMode, run]);
  const report = useCommittedCommand(input.reportError);
  const handleApprovalAnswer = useCallback((allow: boolean, session: boolean, persist: boolean) => (
    input.approval?.tool === "exit_plan_mode"
      ? plan(allow ? "start_execution" : "revise_plan")
      : run(approvalTarget, "approval", { kind: "approval", allow, session, persist })
  ), [approvalTarget, input.approval?.tool, plan, run]);
  const handleRecoveryAnswer = useCallback((action: RecoveryAction, feedback = "") => {
    return run(approvalTarget, "approval", { kind: "recovery", action, feedback });
  }, [approvalTarget, run]);
  const handleRevisePlan = useCallback((revision: string) => plan("revise_plan", revision), [plan]);
  const handleExitPlan = useCallback(() => plan("exit_plan"), [plan]);
  const handleQuestionAnswer = useCallback((_id: string, answers: QuestionAnswer[]) => run(questionTarget, "ask", { kind: "question", answers }), [questionTarget, run]);
  const handleQuestionDismiss = useCallback(() => run(questionTarget, "ask", { kind: "question", answers: [] }), [questionTarget, run]);
  const handleMCPAnswer = useCallback((_id: string, action: MCPInteractionAction, content?: Record<string, unknown>) => {
    void run(mcpTarget, "mcpInteraction", { kind: "mcp", action, content }).catch(report);
  }, [mcpTarget, report, run]);
  return useMemo(() => ({
    approvalTarget, questionTarget, mcpTarget,
    handleApprovalAnswer, handleRecoveryAnswer, handleRevisePlan, handleExitPlan,
    handleQuestionAnswer, handleQuestionDismiss, handleMCPAnswer,
  }), [approvalTarget, questionTarget, mcpTarget, handleApprovalAnswer, handleRecoveryAnswer, handleRevisePlan, handleExitPlan, handleQuestionAnswer, handleQuestionDismiss, handleMCPAnswer]);
}
