import { useCommittedCommand } from "../lib/useCommittedCommand";
import type { QuestionAnswer, ToolApprovalMode } from "../lib/types";
import { executeSessionPrompt, type PromptPorts, type PromptRequest, type SessionPromptKind } from "./sessionPromptExecutor";
import type { MCPInteractionAction, RecoveryAction } from "./sessionActionOwner";
import type { SessionResource, useSessionOperations } from "./useSessionOperations";

type Input = {
  target: SessionResource;
  approval?: { id: string; tool: string };
  questionId?: string;
  remote: boolean;
  goal: string;
  toolApprovalMode: ToolApprovalMode;
  ports: PromptPorts;
  operations: ReturnType<typeof useSessionOperations>;
  reportError: (error: unknown) => void;
};

export function useSessionPromptCommands(input: Input) {
  const run = useCommittedCommand(async (promptId: string | undefined, promptKind: SessionPromptKind, request: PromptRequest) => {
    if (!promptId || !input.target.tabId) return;
    const target = { ...input.target, promptId };
    const result = await input.operations(target, `prompt:${promptKind}`, { target, promptKind, request, ports: input.ports }, executeSessionPrompt);
    if (result.status === "failed") throw result.error;
  });
  const plan = useCommittedCommand((action: "start_execution" | "revise_plan" | "exit_plan", revision?: string) => run(input.approval?.id, "approval", {
    kind: "plan", action, leavePlanMode: action !== "revise_plan", remote: input.remote,
    goal: input.goal, toolApprovalMode: input.toolApprovalMode, revision,
  }));
  const report = useCommittedCommand(input.reportError);
  const handleApprovalAnswer = useCommittedCommand((allow: boolean, session: boolean, persist: boolean) => (
    input.approval?.tool === "exit_plan_mode"
      ? plan(allow ? "start_execution" : "revise_plan")
      : run(input.approval?.id, "approval", { kind: "approval", allow, session, persist })
  ));
  const handleRecoveryAnswer = useCommittedCommand((action: RecoveryAction, feedback = "") => {
    void run(input.approval?.id, "approval", { kind: "recovery", action, feedback }).catch(report);
  });
  const handleRevisePlan = useCommittedCommand((revision: string) => { void plan("revise_plan", revision).catch(report); });
  const handleExitPlan = useCommittedCommand(() => plan("exit_plan"));
  const handleQuestionAnswer = useCommittedCommand((id: string, answers: QuestionAnswer[]) => run(id, "ask", { kind: "question", answers }));
  const handleQuestionDismiss = useCommittedCommand(() => run(input.questionId, "ask", { kind: "question", answers: [] }));
  const handleMCPAnswer = useCommittedCommand((id: string, action: MCPInteractionAction, content?: Record<string, unknown>) => {
    void run(id, "mcpInteraction", { kind: "mcp", action, content }).catch(report);
  });
  return { handleApprovalAnswer, handleRecoveryAnswer, handleRevisePlan, handleExitPlan, handleQuestionAnswer, handleQuestionDismiss, handleMCPAnswer };
}
