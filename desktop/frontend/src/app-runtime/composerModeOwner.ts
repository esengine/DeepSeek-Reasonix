import { composerProfileWithMode, type ComposerProfile, type ComposerProfileField } from "../lib/composerProfile";
import { modeHasPlan, type CollaborationMode, type Mode, type ToolApprovalMode } from "../lib/types";
import type { SessionOperationAuthority, SessionResource } from "./useSessionOperations";

export type ComposerModeRequest =
  | { kind: "mode"; mode: Mode }
  | { kind: "collaboration"; mode: CollaborationMode }
  | { kind: "approval"; mode: ToolApprovalMode };
export type ComposerModePorts = {
  setMode: (tabId: string, mode: Mode) => Promise<void> | void;
  setCollaboration: (tabId: string, mode: CollaborationMode) => Promise<void>;
  setApproval: (tabId: string, mode: ToolApprovalMode) => Promise<void> | void;
  clearGoal: (tabId: string) => Promise<void>;
  setRemote: (tabId: string, collaboration: CollaborationMode, approval: ToolApprovalMode, goal: string) => Promise<string[]>;
  drainRemote: (tabId: string, ids: string[]) => void;
  patch: (tabId: string, patch: Partial<Omit<ComposerProfile, "pending">>, fields: ComposerProfileField[]) => void;
  rememberPlan: (tabId: string, enabled: boolean) => void;
  rememberApproval: (tabId: string, previous: ToolApprovalMode, next: ToolApprovalMode) => void;
};
export type ComposerModeInput = {
  target: SessionResource;
  request: ComposerModeRequest;
  remote: boolean;
  collaborationMode: CollaborationMode;
  toolApprovalMode: ToolApprovalMode;
  goal: string;
  ports: ComposerModePorts;
};

export async function executeComposerMode(input: ComposerModeInput, authority: SessionOperationAuthority): Promise<void> {
  const { target: { tabId }, request, ports } = input;
  authority.checkpoint();
  let patch: Partial<Omit<ComposerProfile, "pending">>;
  let fields: ComposerProfileField[];
  if (request.kind === "mode") {
    patch = composerProfileWithMode(request.mode);
    fields = ["collaborationMode", "toolApprovalMode", "goal"];
    if (input.remote) {
      const ids = await ports.setRemote(tabId, patch.collaborationMode ?? "normal", patch.toolApprovalMode ?? "ask", "");
      authority.checkpoint();
      if (authority.ownsUI()) ports.drainRemote(tabId, ids);
    } else await ports.setMode(tabId, request.mode);
    authority.checkpoint();
    ports.rememberPlan(tabId, modeHasPlan(request.mode));
  } else if (request.kind === "collaboration") {
    const mode = request.mode === "goal" ? "normal" : request.mode;
    patch = { collaborationMode: mode, goalDraftMode: request.mode === "goal", goal: "" };
    fields = ["collaborationMode", "goal"];
    if (input.remote) {
      const ids = await ports.setRemote(tabId, mode, input.toolApprovalMode, "");
      authority.checkpoint();
      if (authority.ownsUI()) ports.drainRemote(tabId, ids);
    } else {
      if (input.goal.trim()) {
        await ports.clearGoal(tabId);
        authority.checkpoint();
      }
      await ports.setCollaboration(tabId, mode);
    }
    authority.checkpoint();
    ports.rememberPlan(tabId, request.mode === "plan");
  } else {
    patch = { toolApprovalMode: request.mode };
    fields = ["toolApprovalMode"];
    if (input.remote) {
      const mode = input.goal.trim() ? "goal" : input.collaborationMode === "plan" ? "plan" : "normal";
      const ids = await ports.setRemote(tabId, mode, request.mode, input.goal);
      authority.checkpoint();
      if (authority.ownsUI()) ports.drainRemote(tabId, ids);
    } else await ports.setApproval(tabId, request.mode);
    authority.checkpoint();
    ports.rememberApproval(tabId, input.toolApprovalMode, request.mode);
  }
  authority.checkpoint();
  ports.patch(tabId, patch, fields);
}
