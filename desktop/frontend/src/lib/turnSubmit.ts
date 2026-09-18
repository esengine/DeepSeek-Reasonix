import type { AppBindings } from "./bridge";
import type { StructuredInvocationSubmit } from "./invocationDisplay";
import type { CollaborationMode, ToolApprovalMode } from "./types";
import { submitAttachmentTurn } from "./attachmentSubmit";
import { draftSubmit } from "./draftSubmit";

export async function submitTurn(
  app: AppBindings,
  tabId: string,
  submissionId: string,
  display: string,
  submit: string,
  original: string,
  structured?: StructuredInvocationSubmit,
  initialGoal?: { goal: string; collaborationMode: CollaborationMode; toolApprovalMode: ToolApprovalMode },
): Promise<[number, (string | string[])?]> {
  let receipt: unknown;
  if (structured?.attachments?.length) receipt = await submitAttachmentTurn(app, submissionId, structured, original, initialGoal);
  else if (initialGoal) {
    receipt = await app.SubmitInitialGoalToTabWithID(
      tabId,
      initialGoal.goal,
      structured?.display.trim() || display,
      structured?.input.trim() || submit,
      structured?.invocations ?? [],
      initialGoal.collaborationMode,
      initialGoal.toolApprovalMode,
      submissionId,
    );
  } else if (structured) receipt = await app.SubmitInvocationsToTabWithID(tabId, structured.display.trim(), structured.input.trim(), structured.invocations, submissionId);
  else if (original) receipt = await app.SubmitEditedDisplayToTabWithID(tabId, display, submit, original, submissionId);
  else if (display !== submit) receipt = await app.SubmitDisplayToTabWithID(tabId, display, submit, submissionId);
  else receipt = await draftSubmit(app, tabId, submit, submissionId);
  if (initialGoal) return [1, Array.isArray(receipt) ? receipt : []];
  if (receipt && typeof receipt === "object" && "disposition" in receipt && receipt.disposition === "management_handled") return [2];
  if (receipt && typeof receipt === "object" && "turnId" in receipt && typeof receipt.turnId === "string") return [3, receipt.turnId];
  return [0];
}
