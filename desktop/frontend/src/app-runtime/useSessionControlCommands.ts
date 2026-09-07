import { useCommittedCommand } from "../lib/useCommittedCommand";
import type { CancelOutcome } from "../lib/inboxCancel";
import type { useSessionOperations } from "./useSessionOperations";

export type SessionControlCommandsInput = {
  activeTabId: string | undefined;
  sessionKey: string;
  operations: ReturnType<typeof useSessionOperations>;
  showToast: (message: string, level: "error") => void;
  clearWorkspaceConflict: () => void;
  ports: {
    cancel(queuedItemIDs: string[]): Promise<CancelOutcome>;
    cancelForTab(tabId: string, queuedItemIDs: string[]): Promise<CancelOutcome>;
    acceptDelivery(tabId: string): Promise<unknown>;
    disconnectRemote(hostId: string): Promise<unknown>;
    cancelJobForTab(tabId: string, jobId: string): Promise<boolean>;
    refreshBackgroundRuntimes(): Promise<void>;
  };
};

/**
 * Owns the session control commands: active-turn cancel (capturing the
 * committed source tab at the event boundary so presentation never reads the
 * active-tab mirror mid-flight), delivery accept, remote host disconnect,
 * workspace-conflict cancel and per-job runtime cancel through the session
 * operations authority.
 */
export function useSessionControlCommands(input: SessionControlCommandsInput) {
  const { activeTabId, sessionKey, operations, showToast, ports } = input;

  const cancelRuntimeJob = useCommittedCommand(async (tabId: string, jobId: string): Promise<boolean> => {
    const target = { tabId, sessionKey: tabId === activeTabId ? sessionKey : `tab:${tabId}` };
    const outcome = await operations(target, `runtime-cancel:${jobId}`, {}, async (_operationInput, authority) =>
      (await import("./sessionRuntimeOwner")).executeCancelRuntimeJob(target, jobId, {
        cancelForTab: (sourceTabId, sourceJobId) => ports.cancelJobForTab(sourceTabId, sourceJobId),
        refresh: () => ports.refreshBackgroundRuntimes(),
      }, authority),
    );
    if (outcome.status === "failed") {
      showToast(outcome.error instanceof Error ? outcome.error.message : String(outcome.error), "error");
      return false;
    }
    return outcome.status === "completed" ? outcome.value : false;
  });

  const handleCancelActive = useCommittedCommand((queuedItemIDs: string[] = []) => {
    const sourceTabId = activeTabId;
    return sourceTabId ? ports.cancelForTab(sourceTabId, queuedItemIDs) : ports.cancel(queuedItemIDs);
  });

  // Capture the committed source tab at the event boundary. Presentation must
  // never read the active-tab mirror while an async delivery operation is in flight.
  const handleAcceptDelivery = useCommittedCommand(() => {
    const sourceTabId = activeTabId;
    if (!sourceTabId) return;
    void ports.acceptDelivery(sourceTabId).catch((error) => {
      console.warn("Failed to accept delivery", error);
    });
  });

  const handleDisconnectRemote = useCommittedCommand((hostId: string) => {
    void ports.disconnectRemote(hostId).catch((error) => {
      console.warn("Failed to disconnect remote host", error);
    });
  });

  const cancelWorkspaceConflict = useCommittedCommand(() => {
    void handleCancelActive();
    input.clearWorkspaceConflict();
  });

  return {
    cancelRuntimeJob,
    handleCancelActive,
    handleAcceptDelivery,
    handleDisconnectRemote,
    cancelWorkspaceConflict,
  };
}
