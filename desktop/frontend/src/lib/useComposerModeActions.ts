import { useCommittedCommand } from "./useCommittedCommand";
import { executeComposerMode, type ComposerModePorts, type ComposerModeRequest } from "../app-runtime/composerModeOwner";
import type { SessionResource, useSessionOperations } from "../app-runtime/useSessionOperations";
import type { CollaborationMode, Mode, ToolApprovalMode } from "./types";

type ComposerModeActionsOptions = {
  target: SessionResource;
  remote: boolean;
  collaborationMode: CollaborationMode;
  toolApprovalMode: ToolApprovalMode;
  goal: string;
  operations: ReturnType<typeof useSessionOperations>;
  ports: ComposerModePorts;
  showError: (message: string) => void;
};

/** Display inputs commit here; source-bound execution lives outside React. */
export function useComposerModeActions(options: ComposerModeActionsOptions) {
  const run = useCommittedCommand(async (request: ComposerModeRequest): Promise<void> => {
    const { target, remote, collaborationMode, toolApprovalMode, goal, ports, operations } = options;
    // All axes share the backend profile transaction; stop/send have other channels.
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
  return { applyMode, applyCollaborationMode, applyToolApprovalMode };
}
