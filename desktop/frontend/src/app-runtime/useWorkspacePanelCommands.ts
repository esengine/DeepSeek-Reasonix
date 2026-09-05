import { useEffect, useLayoutEffect } from "react";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { loadWorkspacePanelOpen, saveWorkspacePanelOpen, useLayoutStore, type RightDockMode } from "../store/layout";
import { useRemoteStore } from "../store/remote";

type Input = {
  workspaceRoot: string;
  creation: boolean;
  visible: boolean;
  closeOverlays: () => void;
  clearLiveWidth: (width: null) => void;
};

/** One project-scoped preference owner, with no mirrored layout state. */
export function useWorkspacePanelCommands(input: Input) {
  const mode = useLayoutStore(state => state.rightDockMode);
  const explorerOpen = useRemoteStore(state => state.explorerOpen);
  const hostCount = useRemoteStore(state => state.hosts.length);
  const openRightDockMode = useCommittedCommand((requestedMode?: RightDockMode) => {
    input.closeOverlays();
    const layout = useLayoutStore.getState();
    const next = requestedMode ?? layout.rightDockMode;
    if (next === "context" || next !== layout.rightDockMode) layout.setWorkspacePreviewActive(false);
    layout.setRightDockMode(next);
    layout.setWorkspacePanelMaximized(false);
    if (layout.workspacePanelOpen && !layout.workspacePanelMaximized) return;
    layout.setWorkspacePanelOpen(true);
    saveWorkspacePanelOpen(true, input.workspaceRoot);
  });
  const closeWorkspacePanel = useCommittedCommand(() => {
    input.closeOverlays();
    const layout = useLayoutStore.getState();
    if (!layout.workspacePanelOpen) return;
    input.clearLiveWidth(null);
    layout.setWorkspacePanelMaximized(false);
    layout.setWorkspacePanelOpen(false);
    saveWorkspacePanelOpen(false, input.workspaceRoot);
  });
  const toggleWorkspacePanel = useCommittedCommand(() => {
    if (input.visible) { closeWorkspacePanel(); return; }
    const current = useLayoutStore.getState().rightDockMode;
    openRightDockMode(input.creation ? current === "changed" ? "changed" : "files" : current);
  });
  const toggleWorkspaceMaximized = useCommittedCommand(() => {
    input.closeOverlays();
    const layout = useLayoutStore.getState();
    layout.setWorkspacePanelMaximized(!layout.workspacePanelMaximized);
  });
  const handleWorkspacePreviewModeChange = useCommittedCommand((active: boolean) => {
    const layout = useLayoutStore.getState();
    if (layout.workspacePreviewActive === active) return;
    input.closeOverlays();
    layout.setWorkspacePreviewActive(active);
  });
  const openRemoteDock = useCommittedCommand(() => {
    const remote = useRemoteStore.getState();
    const fallback = remote.hosts.find(host => ["connected", "degraded"].includes(remote.statuses[host.id]?.state)) ?? remote.hosts[0];
    const hostId = remote.hosts.some(host => host.id === remote.explorerHostId) ? remote.explorerHostId : fallback?.id;
    if (hostId) remote.openExplorer(hostId);
  });
  useLayoutEffect(() => {
    useLayoutStore.getState().setWorkspacePanelOpen(loadWorkspacePanelOpen(input.workspaceRoot));
  }, [input.workspaceRoot]);
  useLayoutEffect(() => {
    if (input.creation && mode === "context") useLayoutStore.getState().setRightDockMode("files");
  }, [input.creation, mode]);
  useEffect(() => {
    if (!explorerOpen) return;
    openRightDockMode("remote");
    useRemoteStore.getState().closeExplorer();
  }, [explorerOpen, openRightDockMode]);
  useEffect(() => {
    if (hostCount === 0 && mode === "remote") useLayoutStore.getState().setRightDockMode("files");
  }, [hostCount, mode]);
  return { openRightDockMode, closeWorkspacePanel, toggleWorkspacePanel, toggleWorkspaceMaximized, handleWorkspacePreviewModeChange, openRemoteDock };
}
