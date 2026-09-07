import { useCommittedCommand } from "./useCommittedCommand";
import { executeComposerMode, type ComposerModePorts, type ComposerModeRequest } from "../app-runtime/composerModeOwner";
import { restorableToolApprovalMode, toggleYoloToolApprovalMode, type RestorableToolApprovalMode } from "./toolApprovalMode";
import { updateUserPlanModeIntent, type UserPlanModeIntents } from "./composerProfile";
import type { SessionResource, useSessionOperations } from "../app-runtime/useSessionOperations";
import type { CollaborationMode, Mode, ToolApprovalMode } from "./types";

type ComposerModeActionsOptions = {
  target: SessionResource;
  remote: boolean;
  collaborationMode: CollaborationMode;
  toolApprovalMode: ToolApprovalMode;
  goal: string;
  operations: ReturnType<typeof useSessionOperations>;
  ports: Omit<ComposerModePorts, "rememberPlan" | "rememberApproval">;
  planIntentsRef: { current: UserPlanModeIntents };
  yoloRestoreRef: { current: Record<string, RestorableToolApprovalMode> };
  showError: (message: string) => void;
};

/** Display inputs commit here; source-bound execution lives outside React. */
export function useComposerModeActions(options: ComposerModeActionsOptions) {
  const notePlanModeForTab = useCommittedCommand((tabId: string, enabled: boolean) => {
    options.planIntentsRef.current = updateUserPlanModeIntent(options.planIntentsRef.current, tabId, enabled);
  });
  const rememberApprovalForTab = useCommittedCommand((tabId: string, previous: ToolApprovalMode, next: ToolApprovalMode) => {
    if (next !== "yolo") options.yoloRestoreRef.current[tabId] = restorableToolApprovalMode(next);
    else if (previous !== "yolo") options.yoloRestoreRef.current[tabId] = restorableToolApprovalMode(previous);
  });

  const run = useCommittedCommand(async (request: ComposerModeRequest): Promise<void> => {
    const { target, remote, collaborationMode, toolApprovalMode, goal, operations } = options;
    // All axes share the backend profile transaction; stop/send have other channels.
    const ports: ComposerModePorts = {
      ...options.ports,
      rememberPlan: notePlanModeForTab,
      rememberApproval: rememberApprovalForTab,
    };
    const result = await operations(target, "composer-profile", {
      target, remote, collaborationMode, toolApprovalMode, goal, ports, request,
    }, executeComposerMode);
    if (result.status === "failed") throw result.error;
  });
  const report = useCommittedCommand((error: unknown) => {
    options.showError(error instanceof Error ? error.message : String(error));
  });
  const applyMode = useCommittedCommand((mode: Mode) => { void run({ kind: "mode", mode }).catch(report); });
  const applyCollaborationMode = useCommittedCommand((mode: CollaborationMode) => run({ kind: "collaboration", mode }));
  const applyToolApprovalMode = useCommittedCommand((mode: ToolApprovalMode) => { void run({ kind: "approval", mode }).catch(report); });

  // Shift+Tab toggles only the collaboration axis; Ctrl/Cmd+Y toggles YOLO on the
  // tool-permission axis while preserving the Ask/Auto base mode.
  const toggleYoloApprovalMode = useCommittedCommand(() => {
    const tabId = options.target.tabId;
    if (!tabId) return;
    const next = toggleYoloToolApprovalMode(
      options.toolApprovalMode,
      options.yoloRestoreRef.current[tabId],
    );
    if (next.restore) {
      options.yoloRestoreRef.current[tabId] = next.restore;
    }
    applyToolApprovalMode(next.mode);
  });
  return { applyMode, applyCollaborationMode, applyToolApprovalMode, notePlanModeForTab, rememberApprovalForTab, toggleYoloApprovalMode };
}
